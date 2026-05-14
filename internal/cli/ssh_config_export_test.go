package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itaprac/sshuttlebox/internal/config"
)

func TestExportSSHConfigDryRunPrintsOpenSSHBlocks(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{
		Host:         "192.0.2.10",
		User:         "deploy",
		Password:     "secret",
		Port:         2222,
		IdentityFile: "~/.ssh/id_ed25519",
	})
	addHost(t, "bastion", config.Host{Host: "bastion.example", User: "ops"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"export", "ssh-config", "--dry-run"}); err != nil {
		t.Fatalf("export ssh-config --dry-run: %v", err)
	}

	want := strings.Join([]string{
		"Host bastion",
		"  HostName bastion.example",
		"  User ops",
		"",
		"Host prod",
		"  HostName 192.0.2.10",
		"  User deploy",
		"  Port 2222",
		"  IdentityFile ~/.ssh/id_ed25519",
		"",
	}, "\n")
	if out.String() != want {
		t.Fatalf("export output = %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String(), "secret") || strings.Contains(out.String(), "Password") {
		t.Fatalf("export leaked password data in %q", out.String())
	}
}

func TestExportSSHConfigOutputRefusesExistingFileWithoutForce(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example"})

	path := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	err := app.Run([]string{"export", "ssh-config", "--output", path})
	if err == nil {
		t.Fatalf("expected existing output error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "existing\n" {
		t.Fatalf("existing output was overwritten: %q", data)
	}
}

func TestExportSSHConfigOutputForceOverwritesFile(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example"})

	path := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"export", "ssh-config", "--output", path, "--force"}); err != nil {
		t.Fatalf("export ssh-config --output --force: %v", err)
	}
	if !strings.Contains(out.String(), "Wrote SSH config: "+path) {
		t.Fatalf("unexpected output: %q", out.String())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "Host prod\n  HostName prod.example\n" {
		t.Fatalf("forced output = %q", data)
	}
}
