package tunnelstate

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	appDirName = "sshuttlebox"
	stateFile  = "tunnels.json"
)

type State struct {
	Tunnels map[string]Entry `json:"tunnels"`
}

type Entry struct {
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
			return Default(), nil
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
	return state, nil
}

func Save(state State) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if state.Tunnels == nil {
		state.Tunnels = map[string]Entry{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create tunnel state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tunnel state: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write tunnel state: %w", err)
	}
	return nil
}

func Get(name string) (Entry, bool, error) {
	state, err := Load()
	if err != nil {
		return Entry{}, false, err
	}
	entry, ok := state.Tunnels[name]
	if !ok || !IsRunning(entry.PID) {
		if ok {
			delete(state.Tunnels, name)
			_ = Save(state)
		}
		return Entry{}, false, nil
	}
	return entry, true, nil
}

func Set(name string, entry Entry) error {
	state, err := Load()
	if err != nil {
		return err
	}
	state.Tunnels[name] = entry
	return Save(state)
}

func Stop(name string) (Entry, bool, error) {
	state, err := Load()
	if err != nil {
		return Entry{}, false, err
	}
	entry, ok := state.Tunnels[name]
	if !ok {
		return Entry{}, false, nil
	}
	delete(state.Tunnels, name)
	if err := Save(state); err != nil {
		return Entry{}, false, err
	}
	if IsRunning(entry.PID) {
		_ = signalProcess(entry.PID, syscall.SIGTERM)
	}
	return entry, true, nil
}

func Prune() (State, error) {
	state, err := Load()
	if err != nil {
		return State{}, err
	}
	changed := false
	for name, entry := range state.Tunnels {
		if !IsRunning(entry.PID) {
			delete(state.Tunnels, name)
			changed = true
		}
	}
	if changed {
		if err := Save(state); err != nil {
			return State{}, err
		}
	}
	return state, nil
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
	return Entry{
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
