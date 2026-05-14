package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/tunnelstate"
)

func TestDoctorFixCreatesMissingConfigAndStateDirs(t *testing.T) {
	withDoctorTempHome(t)
	t.Setenv("SHBX_SSH_BIN", doctorFakeSSHExitBinary(t, 0))

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"doctor", "--fix"}); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"Fixed: created config:",
		"Fixed: created tunnel state dir:",
		"Fixed: created tunnel control dir:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor --fix output missing %q in %q", want, got)
		}
	}

	if exists, err := config.Exists(); err != nil {
		t.Fatalf("check config exists: %v", err)
	} else if !exists {
		t.Fatalf("doctor --fix did not create config")
	}
	assertDoctorDir(t, doctorTunnelStateDir(t))
	assertDoctorDir(t, doctorTunnelControlDir(t))
}

func TestDoctorReadOnlyDoesNotPruneStaleTunnelState(t *testing.T) {
	withDoctorTempHome(t)
	t.Setenv("SHBX_SSH_BIN", doctorFakeSSHExitBinary(t, 0))
	doctorInitConfig(t)
	if err := tunnelstate.Save(tunnelstate.State{Tunnels: map[string]tunnelstate.Entry{
		"stale": tunnelstate.NewEntry(-1, "ssh -N old", ""),
	}}); err != nil {
		t.Fatalf("save tunnel state: %v", err)
	}

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"doctor"}); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if got := out.String(); !strings.Contains(got, "[WARN] stale tunnel state: 1; run shbx doctor --fix") {
		t.Fatalf("doctor output missing stale warning in %q", got)
	}
	state, err := tunnelstate.Load()
	if err != nil {
		t.Fatalf("load tunnel state: %v", err)
	}
	if _, ok := state.Tunnels["stale"]; !ok {
		t.Fatalf("doctor pruned stale state in read-only mode")
	}
}

func TestDoctorFixChmodsConfigAndPrunesStaleTunnelState(t *testing.T) {
	withDoctorTempHome(t)
	t.Setenv("SHBX_SSH_BIN", doctorFakeSSHExitBinary(t, 0))
	path := doctorInitConfig(t)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod config: %v", err)
		}
	}
	if err := tunnelstate.Save(tunnelstate.State{Tunnels: map[string]tunnelstate.Entry{
		"stale": tunnelstate.NewEntry(-1, "ssh -N old", ""),
	}}); err != nil {
		t.Fatalf("save tunnel state: %v", err)
	}

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"doctor", "--fix"}); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}

	got := out.String()
	if runtime.GOOS != "windows" && !strings.Contains(got, "Fixed: config permissions: 0600") {
		t.Fatalf("doctor --fix output missing chmod fix in %q", got)
	}
	if !strings.Contains(got, "Fixed: pruned stale tunnel state: 1") {
		t.Fatalf("doctor --fix output missing prune fix in %q", got)
	}

	state, err := tunnelstate.Load()
	if err != nil {
		t.Fatalf("load tunnel state: %v", err)
	}
	if len(state.Tunnels) != 0 {
		t.Fatalf("stale tunnel state remains: %#v", state.Tunnels)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat config: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("config mode = %04o, want 0600", got)
		}
	}
}

func TestDoctorFixDoesNotRemoveMissingIdentityFile(t *testing.T) {
	home := withDoctorTempHome(t)
	t.Setenv("SHBX_SSH_BIN", doctorFakeSSHExitBinary(t, 0))
	path := doctorInitConfig(t)
	missingKey := filepath.Join(home, "missing_key")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Hosts["prod"] = config.Host{Host: "prod.example", IdentityFile: missingKey}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := os.MkdirAll(doctorTunnelControlDir(t), 0o700); err != nil {
		t.Fatalf("create tunnel control dir: %v", err)
	}

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"doctor", "--fix"}); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}

	if got := out.String(); got != "No fixes applied\n" {
		t.Fatalf("doctor --fix output = %q, want no fixes", got)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Hosts["prod"].IdentityFile; got != missingKey {
		t.Fatalf("identity file = %q, want %q", got, missingKey)
	}
}

func withDoctorTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	return home
}

func doctorInitConfig(t *testing.T) string {
	t.Helper()
	path, _, err := config.Init()
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	return path
}

func doctorTunnelStateDir(t *testing.T) string {
	t.Helper()
	path, err := tunnelstate.Path()
	if err != nil {
		t.Fatalf("tunnel state path: %v", err)
	}
	return filepath.Dir(path)
}

func doctorTunnelControlDir(t *testing.T) string {
	t.Helper()
	path, err := tunnelstate.ControlPath("doctor")
	if err != nil {
		t.Fatalf("tunnel control path: %v", err)
	}
	return filepath.Dir(path)
}

func assertDoctorDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}

func doctorFakeSSHExitBinary(t *testing.T, code int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	return path
}
