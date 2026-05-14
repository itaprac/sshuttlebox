package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itaprac/sshuttlebox/internal/config"
)

func TestConfigCommands(t *testing.T) {
	withTempHome(t)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}

	if err := app.Run([]string{"config", "status"}); err != nil {
		t.Fatalf("status before init: %v", err)
	}
	if !strings.Contains(out.String(), "missing") {
		t.Fatalf("expected missing status, got %q", out.String())
	}

	out.Reset()
	if err := app.Run([]string{"config", "init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(out.String(), "Created config:") {
		t.Fatalf("expected created message, got %q", out.String())
	}

	out.Reset()
	if err := app.Run([]string{"config", "path"}); err != nil {
		t.Fatalf("path: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(out.String()), filepath.Join(".config", config.AppDirName, config.ConfigFileName)) {
		t.Fatalf("unexpected config path %q", out.String())
	}
}

func TestConfigExportRequiresOutputWhenPasswordsExist(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy", Password: "secret"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}

	err := app.Run([]string{"config", "export"})
	if err == nil {
		t.Fatalf("expected export error")
	}
	if strings.Contains(out.String(), "secret") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("export leaked password: out=%q err=%q", out.String(), err)
	}

	exportPath := filepath.Join(t.TempDir(), "config.json")
	if err := app.Run([]string{"config", "export", "--output", exportPath}); err != nil {
		t.Fatalf("export with output: %v", err)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !strings.Contains(string(data), "secret") {
		t.Fatalf("expected exported file to contain saved password")
	}
}

func TestConfigRestoreAcceptsDryRunAfterFile(t *testing.T) {
	withTempHome(t)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"config", "init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	restorePath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(restorePath, config.Default()); err != nil {
		t.Fatalf("write restore file: %v", err)
	}

	out.Reset()
	if err := app.Run([]string{"config", "restore", restorePath, "--dry-run"}); err != nil {
		t.Fatalf("restore file --dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "Restore OK") {
		t.Fatalf("expected dry-run output, got %q", out.String())
	}
}

func TestAddWithoutNamePromptsAndListShowsTable(t *testing.T) {
	withTempHome(t)

	var out bytes.Buffer
	app := App{
		in:  strings.NewReader("prod\n192.0.2.10\ndeploy\n\n2222\n~/.ssh/id_ed25519\n"),
		out: &out,
	}

	stderr := captureStderr(t, func() {
		if err := app.Run([]string{"add"}); err != nil {
			t.Fatalf("add: %v", err)
		}
	})
	if !strings.Contains(stderr, "Warning: identity file does not exist: ~/.ssh/id_ed25519") {
		t.Fatalf("expected identity-file warning, got %q", stderr)
	}

	out.Reset()
	app = App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"list"}); err != nil {
		t.Fatalf("list: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"NAME",
		"TARGET",
		"KEY",
		"prod",
		"deploy@192.0.2.10:2222",
		"~/.ssh/id_ed25519",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("list output missing %q in %q", want, got)
		}
	}
}

func TestAddExistingHostAsksBeforeUpdate(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "old.example", User: "deploy", Port: 22})

	var out bytes.Buffer
	app := App{in: strings.NewReader("\n"), out: &out}
	if err := app.Run([]string{"add", "prod", "--host", "new.example"}); err != nil {
		t.Fatalf("cancel update: %v", err)
	}
	if !strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("expected cancellation output, got %q", out.String())
	}
	assertHost(t, "prod", config.Host{Host: "old.example", User: "deploy", Port: 22})

	out.Reset()
	app = App{in: strings.NewReader("y\n"), out: &out}
	if err := app.Run([]string{"add", "prod", "--host", "new.example"}); err != nil {
		t.Fatalf("confirm update: %v", err)
	}
	if !strings.Contains(out.String(), "Updated host \"prod\"") {
		t.Fatalf("expected update output, got %q", out.String())
	}
	assertHost(t, "prod", config.Host{Host: "new.example", User: "deploy", Port: 22})
}

func TestAddStoresPasswordAndShowMasksIt(t *testing.T) {
	withTempHome(t)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"add", "prod", "--host", "192.0.2.10", "--user", "deploy", "--password", "secret"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	assertHost(t, "prod", config.Host{Host: "192.0.2.10", User: "deploy", Password: "secret"})

	out.Reset()
	if err := app.Run([]string{"show", "prod"}); err != nil {
		t.Fatalf("show: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Password: set") {
		t.Fatalf("expected masked password marker, got %q", got)
	}
	if strings.Contains(got, "secret") {
		t.Fatalf("show leaked password in %q", got)
	}
}

func TestAddCanReuseIdentityFileFromSavedHost(t *testing.T) {
	withTempHome(t)
	addHost(t, "bastion", config.Host{Host: "bastion.example", IdentityFile: "~/.ssh/bastion_key"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"add", "prod", "--host", "prod.example", "--user", "deploy", "--identity-from", "bastion"}); err != nil {
		t.Fatalf("add with --identity-from: %v", err)
	}

	assertHost(t, "prod", config.Host{Host: "prod.example", User: "deploy", IdentityFile: "~/.ssh/bastion_key"})
}

func TestEditCanReuseIdentityFileFromSavedHost(t *testing.T) {
	withTempHome(t)
	addHost(t, "bastion", config.Host{Host: "bastion.example", IdentityFile: "~/.ssh/bastion_key"})
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"edit", "prod", "--identity-from", "bastion"}); err != nil {
		t.Fatalf("edit with --identity-from: %v", err)
	}

	assertHost(t, "prod", config.Host{Host: "prod.example", User: "deploy", IdentityFile: "~/.ssh/bastion_key"})
}

func TestAddInteractiveCanChooseExistingIdentityFileByNumber(t *testing.T) {
	withTempHome(t)
	addHost(t, "bastion", config.Host{Host: "bastion.example", IdentityFile: "~/.ssh/bastion_key"})

	var out bytes.Buffer
	app := App{
		in:  strings.NewReader("prod\nprod.example\ndeploy\n\n\n1\n"),
		out: &out,
	}
	if err := app.Run([]string{"add"}); err != nil {
		t.Fatalf("interactive add with identity choice: %v", err)
	}

	assertHost(t, "prod", config.Host{Host: "prod.example", User: "deploy", IdentityFile: "~/.ssh/bastion_key"})
	if !strings.Contains(out.String(), "Saved identity files:") || !strings.Contains(out.String(), "1) ~/.ssh/bastion_key (from bastion)") {
		t.Fatalf("expected identity choices in output, got %q", out.String())
	}
}

func TestIdentityFromRequiresHostWithIdentityFile(t *testing.T) {
	withTempHome(t)
	addHost(t, "no-key", config.Host{Host: "example.com"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	err := app.Run([]string{"add", "prod", "--host", "prod.example", "--identity-from", "no-key"})
	if err == nil || !strings.Contains(err.Error(), "host \"no-key\" has no identity file to reuse") {
		t.Fatalf("expected missing identity error, got %v", err)
	}
}

func TestShowDisplaysDefaultPort(t *testing.T) {
	withTempHome(t)
	addHost(t, "dev", config.Host{Host: "example.com", User: "root"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"show", "dev"}); err != nil {
		t.Fatalf("show: %v", err)
	}

	if !strings.Contains(out.String(), "Port: 22 default") {
		t.Fatalf("expected default port, got %q", out.String())
	}
}

func TestConnectDryRunAndPrint(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{
		Host:         "192.0.2.10",
		User:         "deploy",
		Port:         2222,
		IdentityFile: "/tmp/key with space",
	})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"connect", "prod", "--dry-run"}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	want := "ssh -p 2222 -i '/tmp/key with space' deploy@192.0.2.10\n"
	if out.String() != want {
		t.Fatalf("dry-run output = %q, want %q", out.String(), want)
	}

	out.Reset()
	if err := app.Run([]string{"connect", "prod", "--print"}); err != nil {
		t.Fatalf("print: %v", err)
	}
	if out.String() != want {
		t.Fatalf("print output = %q, want %q", out.String(), want)
	}
}

func TestConnectDryRunWithPasswordUsesSSHPasswordWrapper(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{
		Host:     "192.0.2.10",
		User:     "deploy",
		Password: "secret",
		Port:     2222,
	})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"connect", "prod", "--dry-run"}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	want := "ssh -p 2222 deploy@192.0.2.10 # password: set\n"
	if out.String() != want {
		t.Fatalf("dry-run output = %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String(), "secret") {
		t.Fatalf("dry-run leaked password in %q", out.String())
	}
}

func TestConnectShowsStartAndClosedStatus(t *testing.T) {
	withTempHome(t)
	t.Setenv("SHBX_SSH_BIN", fakeSSHExitBinary(t, 0))
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"connect", "prod"}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"Connecting to \"prod\" (deploy@prod.example:22)...",
		"Connection closed.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("connect output missing %q in %q", want, got)
		}
	}
}

func TestTunnelAddListShowStartDryRunAndRemove(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{
		Host:         "prod.example",
		User:         "deploy",
		Port:         2222,
		IdentityFile: "/tmp/key with space",
	})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"tunnel", "add", "db", "--host", "prod", "--local-port", "5432", "--remote-host", "127.0.0.1", "--remote-port", "5432"}); err != nil {
		t.Fatalf("tunnel add: %v", err)
	}
	assertTunnel(t, "db", config.Tunnel{Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432})

	out.Reset()
	if err := app.Run([]string{"tunnel", "list"}); err != nil {
		t.Fatalf("tunnel list: %v", err)
	}
	for _, want := range []string{"NAME", "TYPE", "SSH HOST", "FORWARD", "db", "local", "prod", "localhost:5432 -> 127.0.0.1:5432"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("tunnel list missing %q in %q", want, out.String())
		}
	}

	out.Reset()
	if err := app.Run([]string{"tunnel", "show", "db"}); err != nil {
		t.Fatalf("tunnel show: %v", err)
	}
	for _, want := range []string{"Command: ssh -p 2222 -i '/tmp/key with space'", "-M -S", "-f -N -T -L 5432:127.0.0.1:5432 deploy@prod.example"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("tunnel show missing %q in %q", want, out.String())
		}
	}

	out.Reset()
	if err := app.Run([]string{"tunnel", "start", "db", "--dry-run"}); err != nil {
		t.Fatalf("tunnel start --dry-run: %v", err)
	}
	for _, want := range []string{"ssh -p 2222 -i '/tmp/key with space'", "-M -S", "-f -N -T -L 5432:127.0.0.1:5432 deploy@prod.example\n"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("tunnel dry-run output missing %q in %q", want, out.String())
		}
	}

	out.Reset()
	if err := app.Run([]string{"tunnel", "remove", "db", "--yes"}); err != nil {
		t.Fatalf("tunnel remove: %v", err)
	}
	assertTunnelMissing(t, "db")
}

func TestTunnelDynamicDryRun(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"tunnel", "add", "socks", "--host", "prod", "--dynamic-port", "1080", "--bind", "127.0.0.1"}); err != nil {
		t.Fatalf("tunnel add dynamic: %v", err)
	}

	out.Reset()
	if err := app.Run([]string{"tunnel", "start", "socks", "--print"}); err != nil {
		t.Fatalf("tunnel dynamic print: %v", err)
	}
	for _, want := range []string{"ssh -p 22", "-M -S", "-f -N -T -D 127.0.0.1:1080 deploy@prod.example\n"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("dynamic tunnel output missing %q in %q", want, out.String())
		}
	}
}

func TestTunnelStartRunsInBackgroundAndStopTerminatesIt(t *testing.T) {
	withTempHome(t)
	t.Setenv("SHBX_SSH_BIN", fakeSSHBinary(t))
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"tunnel", "add", "db", "--host", "prod", "--local-port", "5432", "--remote-host", "127.0.0.1", "--remote-port", "5432"}); err != nil {
		t.Fatalf("tunnel add: %v", err)
	}

	out.Reset()
	if err := app.Run([]string{"tunnel", "start", "db"}); err != nil {
		t.Fatalf("tunnel start: %v", err)
	}
	if !strings.Contains(out.String(), "Starting tunnel \"db\"...") {
		t.Fatalf("expected starting status, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Started tunnel \"db\" in background with pid") {
		t.Fatalf("unexpected start output %q", out.String())
	}

	out.Reset()
	if err := app.Run([]string{"tunnel", "list"}); err != nil {
		t.Fatalf("tunnel list: %v", err)
	}
	if !strings.Contains(out.String(), "running") {
		t.Fatalf("expected running status, got %q", out.String())
	}

	out.Reset()
	if err := app.Run([]string{"tunnel", "stop", "db"}); err != nil {
		t.Fatalf("tunnel stop: %v", err)
	}
	if !strings.Contains(out.String(), "Stopping tunnel \"db\" with pid") {
		t.Fatalf("expected stopping status, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Stopped tunnel \"db\" with pid") {
		t.Fatalf("unexpected stop output %q", out.String())
	}
}

func TestTunnelStartSupportsSavedPassword(t *testing.T) {
	withTempHome(t)
	t.Setenv("SHBX_SSH_BIN", fakeSSHBinary(t))
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy", Password: "secret"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"tunnel", "add", "db", "--host", "prod", "--local-port", "5432", "--remote-host", "127.0.0.1", "--remote-port", "5432"}); err != nil {
		t.Fatalf("tunnel add: %v", err)
	}
	out.Reset()
	if err := app.Run([]string{"tunnel", "start", "db"}); err != nil {
		t.Fatalf("tunnel start with saved password: %v", err)
	}
	if !strings.Contains(out.String(), "Started tunnel \"db\" in background with pid") {
		t.Fatalf("unexpected start output %q", out.String())
	}
	if err := app.Run([]string{"tunnel", "stop", "db"}); err != nil {
		t.Fatalf("tunnel stop: %v", err)
	}
}

func TestTunnelAddWithoutNamePrompts(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy"})

	var out bytes.Buffer
	app := App{
		in:  strings.NewReader("db\n1\nlocal\n\n5432\n127.0.0.1\n5432\n"),
		out: &out,
	}
	if err := app.Run([]string{"tunnel", "add"}); err != nil {
		t.Fatalf("interactive tunnel add: %v", err)
	}

	assertTunnel(t, "db", config.Tunnel{Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432})
	if !strings.Contains(out.String(), "Saved hosts:") || !strings.Contains(out.String(), "Added tunnel \"db\"") {
		t.Fatalf("expected interactive tunnel output, got %q", out.String())
	}
}

func TestTunnelValidationRequiresSavedHostAndPorts(t *testing.T) {
	withTempHome(t)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	err := app.Run([]string{"tunnel", "add", "db", "--host", "missing", "--local-port", "5432", "--remote-host", "127.0.0.1", "--remote-port", "5432"})
	if err == nil || !strings.Contains(err.Error(), "host \"missing\" not found") {
		t.Fatalf("expected missing host error, got %v", err)
	}

	addHost(t, "prod", config.Host{Host: "prod.example"})
	err = app.Run([]string{"tunnel", "add", "db", "--host", "prod", "--local-port", "0", "--remote-host", "127.0.0.1", "--remote-port", "5432"})
	if err == nil || !strings.Contains(err.Error(), "invalid local port 0") {
		t.Fatalf("expected invalid port error, got %v", err)
	}
}

func TestGroupsCanContainHostsAndTunnels(t *testing.T) {
	withTempHome(t)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"group", "add", "work"}); err != nil {
		t.Fatalf("group add: %v", err)
	}
	if err := app.Run([]string{"add", "prod", "--host", "prod.example", "--group", "work"}); err != nil {
		t.Fatalf("add grouped host: %v", err)
	}
	if err := app.Run([]string{"tunnel", "add", "db", "--host", "prod", "--local-port", "5432", "--remote-host", "127.0.0.1", "--remote-port", "5432", "--group", "work"}); err != nil {
		t.Fatalf("add grouped tunnel: %v", err)
	}

	assertHost(t, "prod", config.Host{Host: "prod.example", Group: "work"})
	assertTunnel(t, "db", config.Tunnel{Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432, Group: "work"})

	out.Reset()
	if err := app.Run([]string{"group", "show", "work"}); err != nil {
		t.Fatalf("group show: %v", err)
	}
	for _, want := range []string{"Name: work", "Hosts: prod", "Tunnels: db"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("group show missing %q in %q", want, out.String())
		}
	}

	out.Reset()
	if err := app.Run([]string{"group", "rename", "work", "prod"}); err != nil {
		t.Fatalf("group rename: %v", err)
	}
	assertHost(t, "prod", config.Host{Host: "prod.example", Group: "prod"})
	assertTunnel(t, "db", config.Tunnel{Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432, Group: "prod"})

	out.Reset()
	if err := app.Run([]string{"group", "remove", "prod", "--force"}); err != nil {
		t.Fatalf("group remove --force: %v", err)
	}
	assertHost(t, "prod", config.Host{Host: "prod.example"})
	assertTunnel(t, "db", config.Tunnel{Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432})
}

func TestCompletionListsMatchingHosts(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example"})
	addHost(t, "preprod", config.Host{Host: "preprod.example"})
	addHost(t, "dev", config.Host{Host: "dev.example"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"__complete", "hosts", "--", "pr"}); err != nil {
		t.Fatalf("complete hosts: %v", err)
	}

	got := out.String()
	if got != "preprod\nprod\n" {
		t.Fatalf("completion output = %q, want %q", got, "preprod\nprod\n")
	}
}

func TestCompletionListsMatchingTunnels(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example"})
	addTunnel(t, "db", config.Tunnel{Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432})
	addTunnel(t, "dev-db", config.Tunnel{Host: "prod", Type: "local", LocalPort: 15432, RemoteHost: "127.0.0.1", RemotePort: 5432})
	addTunnel(t, "socks", config.Tunnel{Host: "prod", Type: "dynamic", LocalPort: 1080})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"__complete", "tunnels", "--", "d"}); err != nil {
		t.Fatalf("complete tunnels: %v", err)
	}

	got := out.String()
	if got != "db\ndev-db\n" {
		t.Fatalf("completion output = %q, want %q", got, "db\ndev-db\n")
	}
}

func TestDoctorReportsConfigAndWarnings(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("SHBX_SSH_BIN", fakeSSHExitBinary(t, 0))

	missingKey := filepath.Join(home, "missing_key")
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy", Password: "secret", IdentityFile: missingKey})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"doctor"}); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"sshuttlebox doctor",
		"[OK] config file:",
		"[OK] ssh binary:",
		"[OK] saved hosts: 1",
		"[WARN] host \"prod\": Warning: identity file does not exist:",
		"[WARN] saved passwords: 1 host(s)",
		"Result: ok with",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q in %q", want, got)
		}
	}
}

func TestCompletionScriptMentionsHostCompletingCommands(t *testing.T) {
	tests := []struct {
		shell string
		want  []string
	}{
		{
			shell: "bash",
			want: []string{
				"connect|show|edit|remove",
				"shbx __complete hosts",
				"shbx __complete tunnels",
				"show\" || \"${COMP_WORDS[2]}\" = \"start\" || \"${COMP_WORDS[2]}\" = \"stop\" || \"${COMP_WORDS[2]}\" = \"remove",
			},
		},
		{
			shell: "zsh",
			want: []string{
				"#compdef shbx",
				"connect|show|edit|remove",
				"shbx __complete hosts",
				"shbx __complete tunnels",
				"[[ \"$words[3]\" == (show|start|stop|remove) ]]",
			},
		},
		{
			shell: "fish",
			want: []string{
				"__shbx_tunnel_needs_subcommand",
				"__shbx_tunnel_uses_name_command",
				"shbx __complete hosts",
				"shbx __complete tunnels",
				"contains -- $cmd[3] show start stop remove",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			var out bytes.Buffer
			app := App{in: strings.NewReader(""), out: &out}
			if err := app.Run([]string{"completion", tt.shell}); err != nil {
				t.Fatalf("completion %s: %v", tt.shell, err)
			}

			got := out.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("%s completion missing %q in %q", tt.shell, want, got)
				}
			}
		})
	}
}

func TestInstalledCompletionScriptsIncludeTunnelNameCompletion(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"completion", "install", "--shell", "all", "--no-rc"}); err != nil {
		t.Fatalf("completion install all no-rc: %v", err)
	}

	for _, path := range []string{
		filepath.Join(home, ".local", "share", "shbx", "completions", "bash", "shbx"),
		filepath.Join(home, ".local", "share", "shbx", "completions", "zsh", "_shbx"),
		filepath.Join(home, ".config", "fish", "completions", "shbx.fish"),
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read completion file %s: %v", path, err)
		}
		if !strings.Contains(string(content), "shbx __complete tunnels") {
			t.Fatalf("completion file %s missing tunnel name completion in %q", path, string(content))
		}
	}
}

func TestCompletionInstallZshWritesCompletionAndStartupSnippet(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"completion", "install", "--shell", "zsh"}); err != nil {
		t.Fatalf("completion install zsh: %v", err)
	}
	if err := app.Run([]string{"completion", "install", "--shell", "zsh"}); err != nil {
		t.Fatalf("completion install zsh second run: %v", err)
	}

	completionPath := filepath.Join(home, ".local", "share", "shbx", "completions", "zsh", "_shbx")
	completion, err := os.ReadFile(completionPath)
	if err != nil {
		t.Fatalf("read zsh completion: %v", err)
	}
	if !strings.Contains(string(completion), "#compdef shbx") {
		t.Fatalf("unexpected zsh completion content %q", string(completion))
	}

	zshrcPath := filepath.Join(home, ".zshrc")
	zshrc, err := os.ReadFile(zshrcPath)
	if err != nil {
		t.Fatalf("read zshrc: %v", err)
	}
	if got := strings.Count(string(zshrc), "# shbx completion"); got != 1 {
		t.Fatalf("expected one shbx startup block, got %d in %q", got, string(zshrc))
	}
	if !strings.Contains(string(zshrc), "fpath=(") || !strings.Contains(string(zshrc), "compinit") {
		t.Fatalf("zshrc missing fpath or compinit setup in %q", string(zshrc))
	}
}

func TestCompletionInstallAllNoRCWritesCompletionFiles(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"completion", "install", "--shell", "all", "--no-rc"}); err != nil {
		t.Fatalf("completion install all no-rc: %v", err)
	}

	for _, path := range []string{
		filepath.Join(home, ".local", "share", "shbx", "completions", "bash", "shbx"),
		filepath.Join(home, ".local", "share", "shbx", "completions", "zsh", "_shbx"),
		filepath.Join(home, ".config", "fish", "completions", "shbx.fish"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected completion file %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no zshrc with --no-rc, got err %v", err)
	}
}

func TestLooksLikePasswordPrompt(t *testing.T) {
	for _, prompt := range []string{
		"deploy@192.0.2.10's password:",
		"Password:",
		"\r\nuser password:",
	} {
		if !looksLikePasswordPrompt(prompt) {
			t.Fatalf("expected password prompt for %q", prompt)
		}
	}

	for _, prompt := range []string{
		"Permission denied, please try again.",
		"Password authentication failed",
		"password accepted",
	} {
		if looksLikePasswordPrompt(prompt) {
			t.Fatalf("unexpected password prompt for %q", prompt)
		}
	}
}

func TestEditRenamesAndUpdatesHost(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "old.example", User: "deploy", Port: 22})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"edit", "prod", "--name", "staging", "--host", "new.example", "--port", "2222"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !strings.Contains(out.String(), "Updated host \"prod\" as \"staging\"") {
		t.Fatalf("unexpected edit output %q", out.String())
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, ok := cfg.Hosts["prod"]; ok {
		t.Fatalf("old host name still exists")
	}
	assertHost(t, "staging", config.Host{Host: "new.example", User: "deploy", Port: 2222})
}

func TestRemoveRequiresConfirmationAndSupportsSkipFlags(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "example.com"})

	var out bytes.Buffer
	app := App{in: strings.NewReader("\n"), out: &out}
	if err := app.Run([]string{"remove", "prod"}); err != nil {
		t.Fatalf("cancel remove: %v", err)
	}
	if !strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("expected cancellation output, got %q", out.String())
	}
	assertHost(t, "prod", config.Host{Host: "example.com"})

	out.Reset()
	app = App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"remove", "prod", "--yes"}); err != nil {
		t.Fatalf("remove --yes: %v", err)
	}
	assertHostMissing(t, "prod")

	addHost(t, "dev", config.Host{Host: "example.org"})
	out.Reset()
	if err := app.Run([]string{"remove", "dev", "--force"}); err != nil {
		t.Fatalf("remove --force: %v", err)
	}
	assertHostMissing(t, "dev")
}

func TestIdentityFileWarningDoesNotBlockSave(t *testing.T) {
	withTempHome(t)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	stderr := captureStderr(t, func() {
		if err := app.Run([]string{"add", "missing", "--host", "example.com", "--identity-file", "~/missing_key"}); err != nil {
			t.Fatalf("add with missing key: %v", err)
		}
	})

	if !strings.Contains(stderr, "Warning: identity file does not exist: ~/missing_key") {
		t.Fatalf("expected warning, got %q", stderr)
	}
	assertHost(t, "missing", config.Host{Host: "example.com", IdentityFile: "~/missing_key"})
}

func withTempHome(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func addHost(t *testing.T, name string, host config.Host) {
	t.Helper()

	path, _, err := config.Init()
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Hosts[name] = host
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

func addTunnel(t *testing.T, name string, tunnel config.Tunnel) {
	t.Helper()

	path, _, err := config.Init()
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Tunnels[name] = tunnel
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

func assertHost(t *testing.T, name string, want config.Host) {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	got, ok := cfg.Hosts[name]
	if !ok {
		t.Fatalf("host %q missing", name)
	}
	if got != want {
		t.Fatalf("host %q = %+v, want %+v", name, got, want)
	}
}

func assertHostMissing(t *testing.T, name string) {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, ok := cfg.Hosts[name]; ok {
		t.Fatalf("host %q still exists", name)
	}
}

func assertTunnel(t *testing.T, name string, want config.Tunnel) {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	got, ok := cfg.Tunnels[name]
	if !ok {
		t.Fatalf("tunnel %q missing", name)
	}
	if got != want {
		t.Fatalf("tunnel %q = %+v, want %+v", name, got, want)
	}
}

func assertTunnelMissing(t *testing.T, name string) {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, ok := cfg.Tunnels[name]; ok {
		t.Fatalf("tunnel %q still exists", name)
	}
}

func fakeSSHBinary(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-ssh")
	script := `#!/bin/sh
socket=""
operation=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -S)
      socket="$2"
      shift 2
      ;;
    -O)
      operation="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
pidfile="${socket}.pid"
if [ "$operation" = "check" ]; then
  pid="$(cat "$pidfile" 2>/dev/null)"
  if [ -n "$pid" ]; then
    echo "Master running (pid=$pid)" >&2
    exit 0
  fi
  echo "No master running" >&2
  exit 255
fi
if [ "$operation" = "exit" ]; then
  pid="$(cat "$pidfile" 2>/dev/null)"
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    rm -f "$pidfile"
  fi
  exit 0
fi
mkdir -p "$(dirname "$socket")"
nohup sh -c "trap 'exit 0' TERM INT; while true; do sleep 1; done" >/dev/null 2>&1 &
echo $! > "$pidfile"
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	return path
}

func fakeSSHExitBinary(t *testing.T, code int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-ssh")
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", code)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	return path
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stderr
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = writeFile
	t.Cleanup(func() {
		os.Stderr = old
	})

	fn()

	if err := writeFile.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	os.Stderr = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, readFile); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := readFile.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}

	return buf.String()
}
