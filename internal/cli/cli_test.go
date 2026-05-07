package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sshuttlebox/internal/config"
)

func TestConfigCommands(t *testing.T) {
	withTempHome(t)

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}

	if err := app.Run([]string{"config", "status"}); err != nil {
		t.Fatalf("status before init: %v", err)
	}
	if !strings.Contains(out.String(), "(missing)") {
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

func TestAddWithoutNamePromptsAndListShowsTable(t *testing.T) {
	withTempHome(t)

	var out bytes.Buffer
	app := App{
		in:  strings.NewReader("prod\n192.0.2.10\ndeploy\n2222\n~/.ssh/id_ed25519\n"),
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

func TestShowDisplaysDefaultPort(t *testing.T) {
	withTempHome(t)
	addHost(t, "dev", config.Host{Host: "example.com", User: "root"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"show", "dev"}); err != nil {
		t.Fatalf("show: %v", err)
	}

	if !strings.Contains(out.String(), "Port: 22 (default)") {
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
