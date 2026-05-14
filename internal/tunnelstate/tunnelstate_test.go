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
