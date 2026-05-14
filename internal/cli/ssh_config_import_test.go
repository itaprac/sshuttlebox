package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itaprac/sshuttlebox/internal/config"
)

func TestImportSSHConfigAddsHostsAndSkipsPatterns(t *testing.T) {
	home := withTempHome(t)
	sshConfig := writeSSHConfig(t, home, `
Host *
  User default

Host prod prod-alias
  HostName 192.0.2.10
  User deploy
  Port 2222
  IdentityFile ~/.ssh/id_ed25519

Host *.example.com
  User wildcard

Host jump
  User ubuntu
`)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"import", "ssh-config", "--path", sshConfig}); err != nil {
		t.Fatalf("import ssh-config: %v", err)
	}

	if !strings.Contains(out.String(), "Imported 3 host(s)") {
		t.Fatalf("unexpected import output %q", out.String())
	}
	assertHost(t, "prod", config.Host{Host: "192.0.2.10", User: "deploy", Port: 2222, IdentityFile: "~/.ssh/id_ed25519"})
	assertHost(t, "prod-alias", config.Host{Host: "192.0.2.10", User: "deploy", Port: 2222, IdentityFile: "~/.ssh/id_ed25519"})
	assertHost(t, "jump", config.Host{Host: "jump", User: "ubuntu"})
	assertHostMissing(t, "*")
	assertHostMissing(t, "*.example.com")
}

func TestImportSSHConfigDryRunDoesNotSave(t *testing.T) {
	home := withTempHome(t)
	writeSSHConfig(t, home, `
Host prod
  HostName prod.example
  User deploy
`)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"import", "ssh-config", "--dry-run"}); err != nil {
		t.Fatalf("import ssh-config --dry-run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Would import 1 host(s)") || !strings.Contains(got, "prod -> deploy@prod.example:22") {
		t.Fatalf("unexpected dry-run output %q", got)
	}
	if exists, err := config.Exists(); err != nil {
		t.Fatalf("check config exists: %v", err)
	} else if exists {
		t.Fatalf("dry-run created config")
	}
}

func TestImportSSHConfigSkipsExistingHosts(t *testing.T) {
	home := withTempHome(t)
	addHost(t, "prod", config.Host{Host: "old.example", User: "root"})
	sshConfig := writeSSHConfig(t, home, `
Host prod
  HostName new.example
  User deploy

Host dev
  HostName dev.example
`)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"import", "ssh-config", "--path", sshConfig}); err != nil {
		t.Fatalf("import ssh-config: %v", err)
	}

	if !strings.Contains(out.String(), `Skipped existing host "prod"`) {
		t.Fatalf("expected skipped host output, got %q", out.String())
	}
	assertHost(t, "prod", config.Host{Host: "old.example", User: "root"})
	assertHost(t, "dev", config.Host{Host: "dev.example"})
}

func writeSSHConfig(t *testing.T, home, content string) string {
	t.Helper()

	path := filepath.Join(home, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create .ssh dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write ssh config: %v", err)
	}
	return path
}
