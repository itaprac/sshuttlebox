package cli

import (
	"bytes"
	"fmt"
	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/tunnelstate"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func setupManagedTunnel(t *testing.T) (config.Host, config.Tunnel) {
	t.Helper()
	home := withTempHome(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("SHBX_SSH_BIN", fakeSSHBinary(t))
	host := config.Host{Host: "example.invalid"}
	tunnel := config.Tunnel{Host: "prod", Type: "dynamic", LocalPort: 1080}
	addHost(t, "prod", host)
	path, _ := config.Path()
	cfg, _ := config.Load()
	cfg.Tunnels["db"] = tunnel
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, _, err := tunnelstate.Stop("db"); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return host, tunnel
}

func TestRemoveYesCannotOrphanRunningTunnel(t *testing.T) {
	host, tunnel := setupManagedTunnel(t)
	if _, err := startTunnelProcess("db", host, tunnel, false); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	a := App{in: strings.NewReader(""), out: &out}
	if err := a.Run([]string{"tunnel", "remove", "db", "--yes"}); err == nil {
		t.Fatal("removed running tunnel without force")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Tunnels["db"]; !ok {
		t.Fatal("lost config")
	}
	if _, running, err := tunnelstate.Get("db"); err != nil || !running {
		t.Fatalf("lost process: %v", err)
	}
	if err := a.Run([]string{"tunnel", "remove", "db", "--force"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.Load()
	state, _ := tunnelstate.Load()
	if _, ok := cfg.Tunnels["db"]; ok {
		t.Fatal("config remains")
	}
	if _, ok := state.Tunnels["db"]; ok {
		t.Fatal("state remains")
	}
}

func TestConcurrentStartsReuseOneMaster(t *testing.T) {
	host, tunnel := setupManagedTunnel(t)
	var wg sync.WaitGroup
	pids := make([]int, 2)
	errs := make([]error, 2)
	for i := range pids {
		wg.Add(1)
		go func(i int) { defer wg.Done(); pids[i], errs[i] = startTunnelProcess("db", host, tunnel, false) }(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if pids[0] <= 0 || pids[0] != pids[1] {
		t.Fatalf("started two masters: %v", pids)
	}
}

func TestStopCanRecoverOrphanedConfig(t *testing.T) {
	host, tunnel := setupManagedTunnel(t)
	if _, err := startTunnelProcess("db", host, tunnel, false); err != nil {
		t.Fatal(err)
	}
	path, _ := config.Path()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	a := App{in: strings.NewReader(""), out: &bytes.Buffer{}}
	if err := a.Run([]string{"tunnel", "stop", "db"}); err != nil {
		t.Fatal(err)
	}
	if _, running, err := tunnelstate.Get("db"); err != nil || running {
		t.Fatalf("orphan remains: %v", err)
	}
}

func TestStartRejectsStaleTunnelParameters(t *testing.T) {
	host, tunnel := setupManagedTunnel(t)
	path, _ := config.Path()
	cfg, _ := config.Load()
	updated := tunnel
	updated.LocalPort++
	cfg.Tunnels["db"] = updated
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := startTunnelProcess("db", host, tunnel, false); err == nil {
		t.Fatal("started stale forwarding")
	}
}

func TestAddCannotReuseActiveOrphanName(t *testing.T) {
	host, tunnel := setupManagedTunnel(t)
	if _, err := startTunnelProcess("db", host, tunnel, false); err != nil {
		t.Fatal(err)
	}
	path, _ := config.Path()
	cfg, _ := config.Load()
	delete(cfg.Tunnels, "db")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	a := App{in: strings.NewReader(""), out: &bytes.Buffer{}}
	if err := a.Run([]string{"tunnel", "add", "db", "--host", "prod", "--dynamic-port", "1081"}); err == nil {
		t.Fatal("reused active orphan name")
	}
	cfg, _ = config.Load()
	if _, ok := cfg.Tunnels["db"]; ok {
		t.Fatal("saved incompatible config")
	}
}

func TestStartupReportsFailedCleanupAfterStateWriteFailure(t *testing.T) {
	host, tunnel := setupManagedTunnel(t)
	realSSH := os.Getenv("SHBX_SSH_BIN")
	statePath, _ := tunnelstate.Path()
	controlPath, _ := tunnelstate.ControlPath("db")
	wrapper := filepath.Join(t.TempDir(), "ssh-wrapper")
	script := fmt.Sprintf(`#!/bin/sh
operation=""
previous=""
for argument in "$@"; do
 if [ "$previous" = "-O" ]; then operation="$argument"; fi
 previous="$argument"
done
if [ "$operation" = "exit" ]; then
 echo 'simulated cleanup refusal' >&2
 exit 1
fi
%s "$@"
result=$?
if [ -z "$operation" ] && [ "$result" = 0 ]; then
 mkdir -p %s
 printf broken > %s
fi
exit "$result"
`, shellQuote(realSSH), shellQuote(filepath.Dir(statePath)), shellQuote(statePath))
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHBX_SSH_BIN", wrapper)
	t.Cleanup(func() {
		_ = exec.Command(realSSH, "-S", controlPath, "-O", "exit", host.Host).Run()
		if err := os.WriteFile(statePath, []byte(`{"tunnels":{}}`), 0600); err != nil {
			t.Error(err)
		}
	})
	_, err := startTunnelProcess("db", host, tunnel, false)
	if err == nil || !strings.Contains(err.Error(), "cleanup failed") || !strings.Contains(err.Error(), "recovery command") || !strings.Contains(err.Error(), controlPath) {
		t.Fatalf("missing cleanup failure recovery: %v", err)
	}
}

func TestReadOnlyCommandsPreserveUnknownTunnelState(t *testing.T) {
	_, _ = setupManagedTunnel(t)
	socket := filepath.Join(t.TempDir(), "existing-socket")
	if err := os.WriteFile(socket, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHBX_SSH_BIN", fakeSSHFailureBinary(t, "permission denied for control socket"))
	if err := tunnelstate.Set("db", tunnelstate.NewEntry(99999999, "ssh example.invalid", socket)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { path, _ := tunnelstate.Path(); _ = os.WriteFile(path, []byte(`{"tunnels":{}}`), 0600) })
	path, _ := tunnelstate.Path()
	before, _ := os.ReadFile(path)
	var out bytes.Buffer
	a := App{in: strings.NewReader(""), out: &out}
	if err := a.Run([]string{"tunnel", "list", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"status":"unknown"`) || !strings.Contains(out.String(), `"error"`) {
		t.Fatalf("unknown check hidden: %s", out.String())
	}
	out.Reset()
	if err := a.Run([]string{"doctor"}); err == nil || !strings.Contains(out.String(), "status unknown") {
		t.Fatalf("doctor hid failed probe: %s %v", out.String(), err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("read-only commands changed state")
	}
}
