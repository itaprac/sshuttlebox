package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/itaprac/sshuttlebox/internal/config"
)

const (
	HistoryFileName = "history.json"
	maxEvents       = 50
)

type Event struct {
	HostName string    `json:"host"`
	At       time.Time `json:"at"`
}

type Log struct {
	Version int     `json:"version"`
	Events  []Event `json:"events"`
}

func Path() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, ".config", config.AppDirName, HistoryFileName), nil
}

func Load() (Log, error) {
	path, err := Path()
	if err != nil {
		return Log{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Log{Version: 1}, nil
		}
		return Log{}, err
	}
	var log Log
	if err := json.Unmarshal(data, &log); err != nil {
		return Log{}, fmt.Errorf("parse history: %w", err)
	}
	if log.Version == 0 {
		log.Version = 1
	}
	return log, nil
}

func Append(hostName string, at time.Time) error {
	if hostName == "" {
		return nil
	}
	log, err := Load()
	if err != nil {
		return err
	}
	log.Events = append(log.Events, Event{HostName: hostName, At: at})
	if len(log.Events) > maxEvents {
		log.Events = log.Events[len(log.Events)-maxEvents:]
	}
	return save(log)
}

func save(log Log) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write history: %w", err)
	}
	return nil
}

func (l Log) Recent(n int) []Event {
	if n <= 0 || len(l.Events) == 0 {
		return nil
	}
	out := make([]Event, 0, n)
	for i := len(l.Events) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, l.Events[i])
	}
	return out
}

func (l Log) RecentNames(n int, exists func(string) bool) []string {
	if n <= 0 || len(l.Events) == 0 {
		return nil
	}
	seen := make(map[string]bool, n)
	out := make([]string, 0, n)
	for i := len(l.Events) - 1; i >= 0 && len(out) < n; i-- {
		name := l.Events[i].HostName
		if seen[name] {
			continue
		}
		if exists != nil && !exists(name) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
