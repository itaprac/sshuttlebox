package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveMergedPreservesConcurrentChangesAndRejectsConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, _, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Load()
	b, _ := Load()
	a.Hosts["a"] = Host{Host: "a.example"}
	if err := Save(path, a); err != nil {
		t.Fatal(err)
	}
	b.Hosts["b"] = Host{Host: "b.example"}
	merged, err := SaveMerged(path, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Hosts) != 2 {
		t.Fatalf("lost concurrent host: %+v", merged.Hosts)
	}
	left := Clone(merged)
	right := Clone(merged)
	left.Hosts["a"] = Host{Host: "left.example"}
	right.Hosts["a"] = Host{Host: "right.example"}
	if err := Save(path, left); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, right); err == nil || !strings.Contains(err.Error(), "another process") {
		t.Fatalf("missing conflict: %v", err)
	}
	latest, _ := Load()
	if latest.Hosts["a"].Host != "left.example" {
		t.Fatal("conflict overwrote disk")
	}
	if merged.Hosts["a"].Host != "a.example" {
		t.Fatal("clone mutated original")
	}
}

func TestSaveRefusesBrokenConfigAndSecuresExistingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, _, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := Load()
	cfg.Hosts["a"] = Host{Host: "a.example", Password: "fixture-secret"}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	broken := []byte("{broken")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Default()); err == nil {
		t.Fatal("accepted damaged current config")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(broken) {
		t.Fatal("damaged config was overwritten")
	}
}

func TestValidateRejectsInvalidDomain(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"version", func(c *Config) { c.Version = 2 }},
		{"option address", func(c *Config) { c.Hosts["a"] = Host{Host: "-oProxyCommand=bad"} }},
		{"option user", func(c *Config) { c.Hosts["a"] = Host{Host: "example", User: "-oBad"} }},
		{"line break", func(c *Config) { c.Hosts["a\nextra"] = Host{Host: "example"} }},
		{"NUL", func(c *Config) { c.Hosts["a"] = Host{Host: "example\x00extra"} }},

		{"empty host", func(c *Config) { c.Hosts["a"] = Host{} }},
		{"host port", func(c *Config) { c.Hosts["a"] = Host{Host: "a", Port: 65536} }},
		{"dangling tunnel", func(c *Config) { c.Tunnels["t"] = Tunnel{Host: "missing", Type: "dynamic", LocalPort: 1080} }},
		{"tunnel port", func(c *Config) {
			c.Hosts["a"] = Host{Host: "a"}
			c.Tunnels["t"] = Tunnel{Host: "a", Type: "dynamic", LocalPort: 0}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.edit(&cfg)
			if err := Validate(cfg); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}

func TestConcurrentSaveProcesses(t *testing.T) {
	if child := os.Getenv("SHBX_CONFIG_SAVE_CHILD"); child != "" {
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		dir := os.Getenv("SHBX_CONFIG_SAVE_BARRIER")
		if err := os.WriteFile(filepath.Join(dir, "ready-"+child), nil, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(dir, "go")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("barrier timeout")
			}
			time.Sleep(5 * time.Millisecond)
		}
		cfg.Hosts[child] = Host{Host: child + ".example"}
		path, _ := Path()
		if err := Save(path, cfg); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Setenv("HOME", t.TempDir())
	if _, _, err := Init(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var cmds []*exec.Cmd
	for i := 0; i < 4; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentSaveProcesses$")
		cmd.Env = append(os.Environ(), fmt.Sprintf("SHBX_CONFIG_SAVE_CHILD=%d", i), "SHBX_CONFIG_SAVE_BARRIER="+dir)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	t.Cleanup(func() {
		for _, cmd := range cmds {
			_ = cmd.Process.Kill()
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		files, _ := filepath.Glob(filepath.Join(dir, "ready-*"))
		if len(files) == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child startup timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(dir, "go"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hosts) != 4 {
		t.Fatalf("lost process update: %v", cfg.Hosts)
	}
}

func TestFailedSavePreservesPreviousConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	t.Setenv("HOME", t.TempDir())
	path, _, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hosts["new"] = Host{Host: "new.example"}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	if _, err := SaveMerged(path, cfg); err == nil {
		t.Fatal("expected write failure")
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(original) {
		t.Fatal("failed save changed previous config")
	}
}

func TestFreshConfigDoesNotOverwriteConcurrentFirstWriter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, _ := Path()
	first := Default()
	second := Default()
	first.Hosts["first"] = Host{Host: "first.example"}
	second.Hosts["second"] = Host{Host: "second.example"}
	if err := Save(path, first); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, second); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hosts) != 2 {
		t.Fatal("fresh config overwrote concurrent first writer")
	}
	second.Hosts["first"] = Host{Host: "changed.example"}
	if err := Save(path, second); err == nil {
		t.Fatal("fresh config overwrote existing record")
	}
}

func TestHostEditRejectsNewDependentTunnelFromAnotherProcess(t *testing.T) {
	for _, reassign := range []bool{false, true} {
		name := "new tunnel"
		if reassign {
			name = "reassigned tunnel"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			path, _, err := Init()
			if err != nil {
				t.Fatal(err)
			}
			initial := Default()
			initial.Hosts["edited"] = Host{Host: "old.example"}
			initial.Hosts["other"] = Host{Host: "other.example"}
			if reassign {
				initial.Tunnels["new"] = Tunnel{Host: "other", Type: "dynamic", LocalPort: 1080}
			}
			if err := Save(path, initial); err != nil {
				t.Fatal(err)
			}
			editor, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			concurrent, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			concurrent.Tunnels["new"] = Tunnel{Host: "edited", Type: "dynamic", LocalPort: 1080}
			if err := Save(path, concurrent); err != nil {
				t.Fatal(err)
			}
			editor.Hosts["edited"] = Host{Host: "changed.example"}
			if err := Save(path, editor); err == nil || !strings.Contains(err.Error(), "gained dependent tunnel") {
				t.Fatalf("expected dependency conflict: %v", err)
			}
			current, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if current.Hosts["edited"].Host != "old.example" || current.Tunnels["new"].Host != "edited" {
				t.Fatal("conflict changed saved records")
			}
		})
	}
}
