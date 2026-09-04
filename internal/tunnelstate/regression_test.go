package tunnelstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentSetPreservesAllEntries(t *testing.T) {
	useTempStateHome(t)
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Set(name, Entry{PID: -1, Target: name}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	state, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tunnels) != 8 {
		t.Fatalf("lost update: %+v", state.Tunnels)
	}
}

func TestStopRefusesLegacyProcessAndPreservesState(t *testing.T) {
	useTempStateHome(t)
	if err := Set("legacy", Entry{PID: os.Getpid(), Command: "ssh -N legacy"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Stop("legacy"); err == nil || !strings.Contains(err.Error(), "cannot safely stop") {
		t.Fatalf("expected identity error: %v", err)
	}
	state, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Tunnels["legacy"]; !ok {
		t.Fatal("state removed before successful stop")
	}
	called := false
	if err := WithStopped("legacy", false, func() error { called = true; return nil }); err == nil || called {
		t.Fatal("changed running tunnel")
	}
}

func TestStopVerifiesControlExitAndRetainsFailedState(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			useTempStateHome(t)
			dir := t.TempDir()
			socket := filepath.Join(dir, "socket")
			ssh := filepath.Join(dir, "ssh")
			script := `#!/bin/sh
if [ "$4" = check ]; then
 [ -f "$2" ] && exit 0
 echo 'No such file or directory' >&2
 exit 255
fi
if [ "$4" = exit ]; then
 if [ "$SHBX_TEST_EXIT_FAIL" = yes ]; then echo 'exit rejected' >&2; exit 1; fi
 rm "$2"
 exit 0
fi
exit 1
`
			if err := os.WriteFile(ssh, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(socket, nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SHBX_SSH_BIN", ssh)
			if fail {
				t.Setenv("SHBX_TEST_EXIT_FAIL", "yes")
			}
			entry := Entry{PID: os.Getpid(), Target: "saved-target", Command: "unparseable old display text", ControlPath: socket}
			if err := Set("db", entry); err != nil {
				t.Fatal(err)
			}
			_, found, err := Stop("db")
			if !found {
				t.Fatal("missing entry")
			}
			if fail && err == nil {
				t.Fatal("expected exit error")
			}
			if !fail && err != nil {
				t.Fatal(err)
			}
			state, _ := Load()
			_, exists := state.Tunnels["db"]
			if exists != fail {
				t.Fatalf("retained=%v, failure=%v", exists, fail)
			}
		})
	}
}

func TestSnapshotBoundsChecksAndPreservesStateOnTimeout(t *testing.T) {
	useTempStateHome(t)
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	socket := filepath.Join(dir, "socket")
	if err := os.WriteFile(socket, nil, 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
exec sleep 5
`
	if err := os.WriteFile(ssh, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHBX_SSH_BIN", ssh)
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		if err := Set(name, Entry{Target: name, ControlPath: socket}); err != nil {
			t.Fatal(err)
		}
	}
	path, _ := Path()
	before, _ := os.ReadFile(path)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	statuses, err := Snapshot(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("snapshot ignored cancellation")
	}
	for name, status := range statuses {
		if status.Running || !errors.Is(status.Err, context.DeadlineExceeded) {
			t.Fatalf("%s: %+v", name, status)
		}
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("snapshot changed saved state")
	}
}

func TestSnapshotHasAtMostFourConcurrentChecks(t *testing.T) {
	useTempStateHome(t)
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	t.Setenv("SHBX_CHECK_TEST_DIR", dir)
	script := `#!/bin/sh
marker="$SHBX_CHECK_TEST_DIR/active-$$"
touch "$marker"
set -- "$SHBX_CHECK_TEST_DIR"/active-*
if [ "$#" -gt 4 ]; then touch "$SHBX_CHECK_TEST_DIR/too-many"; fi
sleep 0.05
rm "$marker"
exit 0
`
	if err := os.WriteFile(ssh, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHBX_SSH_BIN", ssh)
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		if err := Set(name, Entry{Target: name, ControlPath: filepath.Join(dir, "socket")}); err != nil {
			t.Fatal(err)
		}
	}
	statuses, err := Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 8 {
		t.Fatalf("status count %d", len(statuses))
	}
	for _, status := range statuses {
		if !status.Running || status.Err != nil {
			t.Fatalf("bad status %+v", status)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "too-many")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("more than four concurrent probes")
	}
}

func TestMissingStateSnapshotDoesNotOverwriteConcurrentSet(t *testing.T) {
	useTempStateHome(t)
	empty, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := Set("new", Entry{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	if err := Save(empty); err != nil {
		t.Fatal(err)
	}
	current, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := current.Tunnels["new"]; !ok {
		t.Fatal("empty stale snapshot erased concurrent Set")
	}
}

func TestPruneMissingStateDoesNotCreateFile(t *testing.T) {
	useTempStateHome(t)
	if _, err := Prune(); err != nil {
		t.Fatal(err)
	}
	path, _ := Path()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only prune created file")
	}
}

func TestFreshStateDoesNotOverwriteConcurrentFirstWriter(t *testing.T) {
	useTempStateHome(t)
	first := Default()
	second := Default()
	first.Tunnels["first"] = Entry{Target: "first"}
	second.Tunnels["second"] = Entry{Target: "second"}
	if err := Save(first); err != nil {
		t.Fatal(err)
	}
	if err := Save(second); err != nil {
		t.Fatal(err)
	}
	state, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tunnels) != 2 {
		t.Fatal("fresh state erased previous state")
	}
}
