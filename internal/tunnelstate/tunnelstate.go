package tunnelstate

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/itaprac/sshuttlebox/internal/store"
)

const (
	appDirName = "sshuttlebox"
	stateFile  = "tunnels.json"
)

type State struct {
	baseline map[string]Entry
	Tunnels  map[string]Entry `json:"tunnels"`
}

type Entry struct {
	Target      string    `json:"target,omitempty"`
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"startedAt"`
	Command     string    `json:"command"`
	ControlPath string    `json:"controlPath,omitempty"`
}

func Default() State {
	return State{Tunnels: map[string]Entry{}}
}

func Path() (string, error) {
	base, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, stateFile), nil
}

func Load() (State, error) {
	path, err := Path()
	if err != nil {
		return State{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			state := Default()
			state.baseline = map[string]Entry{}
			return state, nil
		}
		return State{}, err
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse tunnel state: %w", err)
	}
	if state.Tunnels == nil {
		state.Tunnels = map[string]Entry{}
	}
	state.baseline = store.CloneMap(state.Tunnels)
	return state, nil
}

func Save(state State) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return store.WithLock(path+".lock", func() error {
		current, err := Load()
		if err != nil {
			return err
		}
		base := state.baseline
		if base == nil {
			base = map[string]Entry{}
		}
		state.Tunnels, err = store.MergeMap("tunnel state", base, state.Tunnels, current.Tunnels)
		if err != nil {
			return err
		}
		return saveUnlocked(path, state)
	})
}

func saveUnlocked(path string, state State) error {
	if state.Tunnels == nil {
		state.Tunnels = map[string]Entry{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return store.AtomicWrite(path, append(data, '\n'))
}

// Get is read-only. A failed check is returned as an error, not a stopped tunnel.
func Get(name string) (Entry, bool, error) {
	state, err := Load()
	if err != nil {
		return Entry{}, false, err
	}
	entry, ok := state.Tunnels[name]
	if !ok {
		return Entry{}, false, nil
	}
	running, err := Check(context.Background(), entry)
	return entry, running, err
}

func Set(name string, entry Entry) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return store.WithLock(path+".lock", func() error {
		state, err := Load()
		if err != nil {
			return err
		}
		state.Tunnels[name] = entry
		return saveUnlocked(path, state)
	})
}

// WithTunnelLock serializes lifecycle operations for one tunnel across processes.
func WithTunnelLock(name string, fn func() error) error {
	path, err := ControlPath(name)
	if err != nil {
		return err
	}
	return store.WithLock(path+".lock", fn)
}

// Stop removes state only after the recorded master is confirmed to have stopped.
func Stop(name string) (Entry, bool, error) {
	var entry Entry
	var found bool
	err := WithTunnelLock(name, func() error { var err error; entry, found, err = stopUnlocked(name); return err })
	return entry, found, err
}

// WithStopped prevents a concurrent start while configuration is changed.
// The callback must not call Stop or acquire this tunnel's lifecycle lock again.
func WithStopped(name string, force bool, fn func() error) error {
	return WithTunnelLock(name, func() error {
		if force {
			if _, _, err := stopUnlocked(name); err != nil {
				return err
			}
		} else {
			_, running, err := Get(name)
			if err != nil {
				return err
			}
			if running {
				return fmt.Errorf("tunnel %q is running; stop it before changing or removing it", name)
			}
		}
		return fn()
	})
}

func stopUnlocked(name string) (Entry, bool, error) {
	var stopped Entry
	found := false
	err := func() error {
		state, err := Load()
		if err != nil {
			return err
		}
		entry, ok := state.Tunnels[name]
		if !ok {
			return nil
		}
		stopped = entry
		found = true
		running, err := Check(context.Background(), entry)
		if err != nil {
			return err
		}
		if running {
			if strings.TrimSpace(entry.ControlPath) == "" {
				return fmt.Errorf("cannot safely stop legacy tunnel %q: process identity is unavailable for PID %d; stop it manually and prune state", name, entry.PID)
			}
			if err := controlCommand(context.Background(), entry, "exit"); err != nil {
				return fmt.Errorf("stop tunnel %q: %w", name, err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				running, err = Check(context.Background(), entry)
				if err != nil {
					return err
				}
				if !running {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("tunnel %q is still running after exit", name)
				}
				time.Sleep(25 * time.Millisecond)
			}
		}
		path, err := Path()
		if err != nil {
			return err
		}
		return store.WithLock(path+".lock", func() error {
			current, err := Load()
			if err != nil {
				return err
			}
			if latest, ok := current.Tunnels[name]; ok && latest != entry {
				return fmt.Errorf("tunnel %q state changed while stopping", name)
			}
			delete(current.Tunnels, name)
			return saveUnlocked(path, current)
		})
	}()
	return stopped, found, err
}

func Prune() (State, error) {
	state, err := Load()
	if err != nil {
		return State{}, err
	}
	statuses, err := snapshotState(context.Background(), state, nil)
	if err != nil {
		return State{}, err
	}
	changed := false
	for name, status := range statuses {
		if status.Err != nil {
			return state, fmt.Errorf("check tunnel %q: %w", name, status.Err)
		}
		if !status.Running {
			delete(state.Tunnels, name)
			changed = true
		}
	}
	if !changed {
		return Load()
	}
	if err := Save(state); err != nil {
		return State{}, err
	}
	return Load()
}

type Status struct {
	Entry   Entry
	Running bool
	Err     error
}

// Snapshot reads state once and probes at most four masters at a time.
func Snapshot(ctx context.Context, names []string) (map[string]Status, error) {
	state, err := Load()
	if err != nil {
		return nil, err
	}
	return snapshotState(ctx, state, names)
}

func snapshotState(ctx context.Context, state State, names []string) (map[string]Status, error) {
	if names == nil {
		for name := range state.Tunnels {
			names = append(names, name)
		}
	}
	result := make(map[string]Status, len(names))
	jobs := make(chan string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				entry, ok := state.Tunnels[name]
				status := Status{Entry: entry}
				if ok {
					status.Running, status.Err = Check(ctx, entry)
				}
				mu.Lock()
				result[name] = status
				mu.Unlock()
			}
		}()
	}
	for _, name := range names {
		jobs <- name
	}
	close(jobs)
	wg.Wait()
	return result, nil
}

func EntryRunning(entry Entry) bool { running, _ := Check(context.Background(), entry); return running }

// Check treats a missing/refused socket as stopped and all other failures as unknown.
func Check(ctx context.Context, entry Entry) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if strings.TrimSpace(entry.ControlPath) == "" {
		if entry.PID <= 0 {
			return false, nil
		}
		err := signalProcess(entry.PID, syscall.Signal(0))
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("check PID %d: %w", entry.PID, err)
		}
		return true, nil
	}
	err := controlCommand(ctx, entry, "check")
	if err == nil {
		return true, nil
	}
	// A missing socket is conclusive only after the SSH client ran. This also
	// allows a stale stored PID to refer to an unrelated, reused process.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if _, statErr := os.Stat(entry.ControlPath); errors.Is(statErr, os.ErrNotExist) {
			return false, nil
		}
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "control socket connect(") && (strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such file or directory")) {
			return false, nil
		}
	}
	return false, err
}

func controlCommand(parent context.Context, entry Entry, operation string) error {
	target := strings.TrimSpace(entry.Target)
	if target == "" {
		var ok bool
		target, ok = sshTargetFromCommand(entry.Command)
		if !ok {
			return fmt.Errorf("tunnel state has no SSH target")
		}
	}
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("invalid SSH target in tunnel state")
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sshBinary(), "-S", strings.TrimSpace(entry.ControlPath), "-O", operation, "-F", os.DevNull, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", target)
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("SSH control %s: %w", operation, ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("SSH control %s: %w: %s", operation, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return false
	}
	return signalProcess(pid, syscall.Signal(0)) == nil
}

func ControlPath(name string) (string, error) {
	base, err := stateDir()
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(name))
	safeName := sanitizeName(name)
	if len(safeName) > 24 {
		safeName = safeName[:24]
	}
	filename := fmt.Sprintf("%s-%s.sock", safeName, hex.EncodeToString(sum[:])[:12])
	return filepath.Join(base, "control", filename), nil
}

func NewEntry(pid int, command, controlPath string) Entry {
	target, _ := sshTargetFromCommand(command)
	return Entry{
		Target:      target,
		PID:         pid,
		StartedAt:   time.Now().UTC(),
		Command:     command,
		ControlPath: controlPath,
	}
}

func stateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, appDirName), nil
}

func sshTargetFromCommand(command string) (string, bool) {
	fields := shellFields(command)
	if len(fields) < 2 || fields[0] != "ssh" {
		return "", false
	}
	return fields[len(fields)-1], true
}

func shellFields(command string) []string {
	var fields []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	hadValue := false

	flush := func() {
		if hadValue {
			fields = append(fields, b.String())
			b.Reset()
			hadValue = false
		}
	}

	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			hadValue = true
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			hadValue = true
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			hadValue = true
			continue
		}
		if !inSingle && !inDouble {
			if r == '#' && !hadValue {
				break
			}
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				flush()
				continue
			}
		}
		b.WriteRune(r)
		hadValue = true
	}
	flush()
	return fields
}

func sshBinary() string {
	if value := strings.TrimSpace(os.Getenv("SHBX_SSH_BIN")); value != "" {
		return value
	}
	return "ssh"
}

func sanitizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	value := strings.Trim(b.String(), "-")
	if value == "" {
		return "tunnel"
	}
	return value
}

func signalProcess(pid int, signal syscall.Signal) error {
	return syscall.Kill(pid, signal)
}
