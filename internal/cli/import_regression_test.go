package cli

import (
	"bytes"
	"github.com/itaprac/sshuttlebox/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHConfigQuotedPathRoundTrip(t *testing.T) {
	hosts := map[string]config.Host{"prod": {Host: "example.invalid", IdentityFile: "/tmp/my keys/a\"b\\c"}}
	parsed, err := parseSSHConfigHosts(strings.NewReader(renderSSHConfig(hosts)))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Host != hosts["prod"] {
		t.Fatalf("round trip: %#v", parsed)
	}
}

func TestSSHConfigLexer(t *testing.T) {
	for _, line := range []string{`IdentityFile="/tmp/my keys/id"`, `IdentityFile = "/tmp/my keys/id"`, `IdentityFile "/tmp/my keys/id" # comment`} {
		fields, err := splitSSHConfigFields(line)
		if err != nil || len(fields) != 2 || fields[0] != "IdentityFile" || fields[1] != "/tmp/my keys/id" {
			t.Fatalf("%q: %q %v", line, fields, err)
		}
	}
	if _, err := splitSSHConfigFields(`Host "broken`); err == nil {
		t.Fatal("accepted broken quote")
	}
}

func TestImportedAliasesPreserveOpenSSHResolution(t *testing.T) {
	home := withTempHome(t)
	path := writeSSHConfig(t, home, `Host *
  User default-user
Host prod
  HostName 192.0.2.10
  User ignored-later-user
  Port 2222
  ProxyJump bastion
  IdentityFile "/tmp/my keys/id"
Include conf.d/*.conf
`)
	dir := filepath.Join(home, ".ssh", "conf.d")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.conf"), []byte("Host extra\n HostName extra.invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := (App{in: strings.NewReader(""), out: &out}).Run([]string{"import", "ssh-config", "--path", path}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hosts["extra"].SSHConfigFile != path {
		t.Fatal("Include alias not found")
	}
	bin, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip(err)
	}
	args := append([]string{"-G"}, buildSSHArgs(cfg.Hosts["prod"])...)
	output, err := exec.Command(bin, args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hostname 192.0.2.10", "user default-user", "port 2222", "proxyjump bastion", "identityfile /tmp/my keys/id"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
	// Exported references must keep the same OpenSSH resolution.
	exported := filepath.Join(home, "exported")
	if err := os.WriteFile(exported, []byte(renderSSHConfig(cfg.Hosts)), 0600); err != nil {
		t.Fatal(err)
	}
	output, err = exec.Command(bin, "-G", "-F", exported, "prod").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "proxyjump bastion") {
		t.Fatal("export lost ProxyJump")
	}
}

func TestDoctorFailureHasNonzeroResultAndJSON(t *testing.T) {
	home := withTempHome(t)
	path := filepath.Join(home, ".config", "sshuttlebox", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"doctor"}, {"doctor", "--json"}} {
		var out bytes.Buffer
		err := (App{in: strings.NewReader(""), out: &out}).Run(args)
		if err == nil || !strings.Contains(out.String(), "FAIL") {
			t.Fatalf("args %v: %s err=%v", args, out.String(), err)
		}
	}
}

func TestExportCannotOverwriteLinkedSource(t *testing.T) {
	home := withTempHome(t)
	source := writeSSHConfig(t, home, "Host prod\n HostName prod.invalid\n")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	a := App{in: strings.NewReader(""), out: &bytes.Buffer{}}
	if err := a.Run([]string{"import", "ssh-config", "--path", source}); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(home, "hardlink")
	if err := os.Link(source, hardlink); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(home, "symlink")
	if err := os.Symlink(source, symlink); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{source, hardlink, symlink} {
		if err := a.Run([]string{"export", "ssh-config", "--output", output, "--force"}); err == nil {
			t.Fatalf("overwrote source through %s", output)
		}
	}
	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) {
		t.Fatal("source changed")
	}
}

func TestExportPreservesLinkedOverrides(t *testing.T) {
	home := withTempHome(t)
	source := writeSSHConfig(t, home, "Host prod\n HostName prod.invalid\n User original\n Port 22\n")
	hosts := map[string]config.Host{"prod": {Host: "prod", SSHConfigFile: source, User: "override", Port: 2222, IdentityFile: "/tmp/key with spaces"}}
	exported := filepath.Join(home, "export")
	if err := os.WriteFile(exported, []byte(renderSSHConfig(hosts)), 0600); err != nil {
		t.Fatal(err)
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip(err)
	}
	out, err := exec.Command(ssh, "-G", "-F", exported, "prod").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hostname prod.invalid", "user override", "port 2222", "identityfile /tmp/key with spaces"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
}

func TestExportKeepsMultipleSourceDefaultsSeparate(t *testing.T) {
	home := withTempHome(t)
	first := filepath.Join(home, "first")
	second := filepath.Join(home, "second")
	if err := os.WriteFile(first, []byte("Host *\n User alice\nHost one\n HostName one.invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("Host *\n User bob\nHost two\n HostName two.invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	hosts := map[string]config.Host{"one": {Host: "one", SSHConfigFile: first}, "two": {Host: "two", SSHConfigFile: second}}
	exported := filepath.Join(home, "export")
	if err := os.WriteFile(exported, []byte(renderSSHConfig(hosts)), 0600); err != nil {
		t.Fatal(err)
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip(err)
	}
	for alias, user := range map[string]string{"one": "alice", "two": "bob"} {
		out, err := exec.Command(ssh, "-G", "-F", exported, alias).Output()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "user "+user) {
			t.Fatalf("%s lost user %s: %s", alias, user, out)
		}
	}
}
