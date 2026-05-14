package tunnelstate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func useTempStateHome(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	return dir
}

func TestSaveLoadRoundTrip(t *testing.T) {
	base := useTempStateHome(t)
	startedAt := time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC)
	want := State{
		Tunnels: map[string]Entry{
			"db": {
				PID:         12345,
				StartedAt:   startedAt,
				Command:     "ssh -N db",
				ControlPath: filepath.Join(base, "control", "db.sock"),
			},
			"api": {
				PID:       23456,
				StartedAt: startedAt.Add(time.Minute),
				Command:   "ssh -N api",
			},
		},
	}

	if err := Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	statePath, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if !strings.HasPrefix(statePath, filepath.Join(base, appDirName)+string(os.PathSeparator)) {
		t.Fatalf("Path() = %q, want under temp state dir %q", statePath, base)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Tunnels) != len(want.Tunnels) {
		t.Fatalf("Load() tunnel count = %d, want %d", len(got.Tunnels), len(want.Tunnels))
	}
	for name, wantEntry := range want.Tunnels {
		gotEntry, ok := got.Tunnels[name]
		if !ok {
			t.Fatalf("Load() missing tunnel %q", name)
		}
		if gotEntry.PID != wantEntry.PID ||
			!gotEntry.StartedAt.Equal(wantEntry.StartedAt) ||
			gotEntry.Command != wantEntry.Command ||
			gotEntry.ControlPath != wantEntry.ControlPath {
			t.Fatalf("Load()[%q] = %+v, want %+v", name, gotEntry, wantEntry)
		}
	}
}

func TestLoadMissingFileReturnsDefaultState(t *testing.T) {
	useTempStateHome(t)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Tunnels == nil {
		t.Fatal("Load().Tunnels is nil, want initialized map")
	}
	if len(got.Tunnels) != 0 {
		t.Fatalf("Load() tunnel count = %d, want 0", len(got.Tunnels))
	}
}

func TestGetPrunesDeadPID(t *testing.T) {
	useTempStateHome(t)

	if err := Save(State{Tunnels: map[string]Entry{
		"dead": {PID: -1, Command: "ssh -N dead"},
	}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, ok, err := Get("dead"); err != nil {
		t.Fatalf("Get() error = %v", err)
	} else if ok {
		t.Fatal("Get() ok = true, want false for dead PID")
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := got.Tunnels["dead"]; ok {
		t.Fatalf("Get() did not prune dead tunnel, state = %+v", got.Tunnels)
	}
}

func TestGetPrunesFailedControlSocketCheckEvenWhenPIDIsAlive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake SSH script uses sh")
	}
	useTempStateHome(t)
	sshPath, _ := fakeSSH(t, 1)
	t.Setenv("SHBX_SSH_BIN", sshPath)

	controlPath := filepath.Join(t.TempDir(), "missing.sock")
	t.Setenv("SHBX_EXPECT_CONTROL", controlPath)
	t.Setenv("SHBX_EXPECT_TARGET", "user@example.com")

	if err := Save(State{Tunnels: map[string]Entry{
		"stale": {
			PID:         os.Getpid(),
			Command:     "ssh -p 22 -M -S " + controlPath + " -f -N -T -L 15432:localhost:5432 user@example.com",
			ControlPath: controlPath,
		},
	}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, ok, err := Get("stale"); err != nil {
		t.Fatalf("Get() error = %v", err)
	} else if ok {
		t.Fatal("Get() ok = true, want false for failed control check")
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := got.Tunnels["stale"]; ok {
		t.Fatalf("Get() did not prune stale tunnel, state = %+v", got.Tunnels)
	}
}

func TestGetUsesControlSocketCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake SSH script uses sh")
	}
	useTempStateHome(t)
	sshPath, argsPath := fakeSSH(t, 0)
	t.Setenv("SHBX_SSH_BIN", sshPath)

	controlPath := filepath.Join(t.TempDir(), "db.sock")
	if err := os.WriteFile(controlPath, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatalf("write control socket placeholder: %v", err)
	}
	t.Setenv("SHBX_EXPECT_CONTROL", controlPath)
	t.Setenv("SHBX_EXPECT_TARGET", "user@example.com")

	entry := Entry{
		PID:         os.Getpid(),
		Command:     "ssh -p 22 -M -S " + controlPath + " -f -N -T -L 15432:localhost:5432 user@example.com # password: set",
		ControlPath: controlPath,
	}
	if err := Save(State{Tunnels: map[string]Entry{"db": entry}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	gotEntry, ok, err := Get("db")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, want true after successful control check")
	}
	if gotEntry.ControlPath != controlPath {
		t.Fatalf("Get() entry = %+v, want control path %q", gotEntry, controlPath)
	}

	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake ssh args: %v", err)
	}
	wantArgs := "-S\n" + controlPath + "\n-O\ncheck\nuser@example.com\n"
	if string(args) != wantArgs {
		t.Fatalf("ssh args = %q, want %q", string(args), wantArgs)
	}
}

func TestPruneRemovesFailedControlSocketCheckEvenWhenPIDIsAlive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake SSH script uses sh")
	}
	useTempStateHome(t)
	sshPath, _ := fakeSSH(t, 1)
	t.Setenv("SHBX_SSH_BIN", sshPath)

	controlPath := filepath.Join(t.TempDir(), "db.sock")
	if err := os.WriteFile(controlPath, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatalf("write control socket placeholder: %v", err)
	}
	t.Setenv("SHBX_EXPECT_CONTROL", controlPath)
	t.Setenv("SHBX_EXPECT_TARGET", "user@example.com")

	if err := Save(State{Tunnels: map[string]Entry{
		"stale": {
			PID:         os.Getpid(),
			Command:     "ssh -p 22 -M -S " + controlPath + " -f -N -T -L 15432:localhost:5432 user@example.com",
			ControlPath: controlPath,
		},
	}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Prune()
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if _, ok := got.Tunnels["stale"]; ok {
		t.Fatalf("Prune() kept tunnel after failed control check, state = %+v", got.Tunnels)
	}
}

func TestPruneRemovesDeadPIDAndKeepsRunningPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("IsRunning is unsupported on windows")
	}
	useTempStateHome(t)

	running := Entry{PID: os.Getpid(), Command: "ssh -N live"}
	dead := Entry{PID: -1, Command: "ssh -N dead"}
	if err := Save(State{Tunnels: map[string]Entry{
		"live": running,
		"dead": dead,
	}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Prune()
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if _, ok := got.Tunnels["dead"]; ok {
		t.Fatalf("Prune() kept dead tunnel, state = %+v", got.Tunnels)
	}
	if gotEntry, ok := got.Tunnels["live"]; !ok {
		t.Fatalf("Prune() removed running tunnel, state = %+v", got.Tunnels)
	} else if gotEntry.PID != running.PID || gotEntry.Command != running.Command {
		t.Fatalf("Prune()[live] = %+v, want %+v", gotEntry, running)
	}
}

func fakeSSH(t *testing.T, exitCode int) (string, string) {
	t.Helper()

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	sshPath := filepath.Join(dir, "ssh")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$SHBX_SSH_ARGS_FILE"
if [ "$1" = "-S" ] && [ "$2" = "$SHBX_EXPECT_CONTROL" ] && [ "$3" = "-O" ] && [ "$4" = "check" ] && [ "$5" = "$SHBX_EXPECT_TARGET" ]; then
	exit "$SHBX_SSH_EXIT"
fi
exit 64
`
	if err := os.WriteFile(sshPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("SHBX_SSH_ARGS_FILE", argsPath)
	t.Setenv("SHBX_SSH_EXIT", string(rune('0'+exitCode)))
	return sshPath, argsPath
}

func TestControlPathSanitizesNameAndUsesStableHash(t *testing.T) {
	base := useTempStateHome(t)

	got, err := ControlPath("  Prod DB / EU:5432 !!!! with an extremely long suffix  ")
	if err != nil {
		t.Fatalf("ControlPath() error = %v", err)
	}

	wantDir := filepath.Join(base, appDirName, "control")
	if filepath.Dir(got) != wantDir {
		t.Fatalf("ControlPath() dir = %q, want %q", filepath.Dir(got), wantDir)
	}

	file := filepath.Base(got)
	if !strings.HasSuffix(file, ".sock") {
		t.Fatalf("ControlPath() file = %q, want .sock suffix", file)
	}
	if strings.ContainsAny(file, "/: !") {
		t.Fatalf("ControlPath() file = %q, want sanitized path-safe filename", file)
	}
	namePart := strings.TrimSuffix(file, ".sock")
	hashSeparator := strings.LastIndex(namePart, "-")
	if hashSeparator == -1 {
		t.Fatalf("ControlPath() file = %q, want name-hash format", file)
	}
	safePrefix := namePart[:hashSeparator]
	if !strings.HasPrefix(safePrefix, "prod-db---eu-5432") {
		t.Fatalf("ControlPath() file = %q, want sanitized name prefix", file)
	}
	if len(safePrefix) > 24 {
		t.Fatalf("ControlPath() safe prefix length = %d, want at most 24", len(safePrefix))
	}

	again, err := ControlPath("  Prod DB / EU:5432 !!!! with an extremely long suffix  ")
	if err != nil {
		t.Fatalf("ControlPath() second call error = %v", err)
	}
	if got != again {
		t.Fatalf("ControlPath() = %q then %q, want stable path", got, again)
	}

	other, err := ControlPath("Prod DB / EU:5432")
	if err != nil {
		t.Fatalf("ControlPath() other call error = %v", err)
	}
	if got == other {
		t.Fatalf("ControlPath() collision for different names: %q", got)
	}
}

func TestControlPathFallsBackForUnsanitizableName(t *testing.T) {
	useTempStateHome(t)

	got, err := ControlPath(" !!! ")
	if err != nil {
		t.Fatalf("ControlPath() error = %v", err)
	}

	if !strings.HasPrefix(filepath.Base(got), "tunnel-") {
		t.Fatalf("ControlPath() file = %q, want tunnel fallback prefix", filepath.Base(got))
	}
}

func TestIsRunning(t *testing.T) {
	if IsRunning(0) {
		t.Fatal("IsRunning(0) = true, want false")
	}
	if IsRunning(-1) {
		t.Fatal("IsRunning(-1) = true, want false")
	}
	if runtime.GOOS == "windows" {
		if IsRunning(os.Getpid()) {
			t.Fatal("IsRunning(current PID) = true on windows, want false")
		}
		return
	}
	if !IsRunning(os.Getpid()) {
		t.Fatal("IsRunning(current PID) = false, want true")
	}
}
