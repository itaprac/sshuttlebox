package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/itaprac/sshuttlebox/internal/store"
)

const (
	AppDirName     = "sshuttlebox"
	ConfigFileName = "config.json"
)

type Config struct {
	baseline *Config
	Version  int               `json:"version"`
	Hosts    map[string]Host   `json:"hosts"`
	Tunnels  map[string]Tunnel `json:"tunnels"`
	Groups   map[string]Group  `json:"groups,omitempty"`
}

type Host struct {
	SSHConfigFile string `json:"sshConfigFile,omitempty"`
	Host          string `json:"host"`
	User          string `json:"user,omitempty"`
	Password      string `json:"password,omitempty"`
	Port          int    `json:"port,omitempty"`
	IdentityFile  string `json:"identityFile,omitempty"`
	Group         string `json:"group,omitempty"`
}

type Tunnel struct {
	Host        string `json:"host"`
	Type        string `json:"type"`
	BindAddress string `json:"bindAddress,omitempty"`
	LocalPort   int    `json:"localPort,omitempty"`
	RemoteHost  string `json:"remoteHost,omitempty"`
	RemotePort  int    `json:"remotePort,omitempty"`
	Group       string `json:"group,omitempty"`
}

type Group struct {
	Name string `json:"name,omitempty"`
}

func Default() Config {
	return Config{
		Version: 1,
		Hosts:   map[string]Host{},
		Tunnels: map[string]Tunnel{},
		Groups:  map[string]Group{},
	}
}

func Path() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}

	return filepath.Join(homeDir, ".config", AppDirName, ConfigFileName), nil
}

func Exists() (bool, error) {
	path, err := Path()
	if err != nil {
		return false, err
	}

	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func Init() (string, bool, error) {
	path, err := Path()
	if err != nil {
		return "", false, err
	}
	created := false
	err = store.WithLock(path+".lock", func() error {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := saveUnlocked(path, Default()); err != nil {
			return err
		}
		created = true
		return nil
	})
	return path, created, err
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadPath(path)
}

func LoadPath(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("config does not exist; run: shbx config init: %w", err)
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return snapshot(cfg), nil
}

// Clone makes an editable copy without changing its conflict detection baseline.
func Clone(cfg Config) Config {
	cfg.Hosts = store.CloneMap(cfg.Hosts)
	cfg.Tunnels = store.CloneMap(cfg.Tunnels)
	cfg.Groups = store.CloneMap(cfg.Groups)
	return cfg
}

func snapshot(cfg Config) Config {
	cfg = Clone(cfg)
	base := Clone(cfg)
	base.baseline = nil
	cfg.baseline = &base
	return cfg
}

func ValidateData(data []byte) error {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	return Validate(cfg)
}

// Validate checks the supported format and the same domain constraints on every write.
func Validate(cfg Config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported config version %d; expected 1", cfg.Version)
	}
	for name, h := range cfg.Hosts {
		if invalidLine(name) || invalidLine(h.Host) || invalidLine(h.User) || invalidLine(h.IdentityFile) || invalidLine(h.SSHConfigFile) || invalidLine(h.Group) {
			return fmt.Errorf("host %q: fields cannot contain NUL or line breaks", name)
		}
		if strings.HasPrefix(strings.TrimSpace(h.Host), "-") || strings.HasPrefix(strings.TrimSpace(h.User), "-") {
			return fmt.Errorf("host %q: address and user cannot start with '-'", name)
		}
		if strings.TrimSpace(name) == "" || strings.TrimSpace(h.Host) == "" {
			return fmt.Errorf("host %q: name and address are required", name)
		}
		if h.Port < 0 || h.Port > 65535 {
			return fmt.Errorf("host %q: invalid port %d", name, h.Port)
		}
	}
	for name, t := range cfg.Tunnels {
		if invalidLine(name) || invalidLine(t.Host) || invalidLine(t.BindAddress) || invalidLine(t.RemoteHost) || invalidLine(t.Group) {
			return fmt.Errorf("tunnel %q: fields cannot contain NUL or line breaks", name)
		}
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("tunnel name is required")
		}
		if _, ok := cfg.Hosts[t.Host]; !ok {
			return fmt.Errorf("tunnel %q: host %q not found", name, t.Host)
		}
		if t.LocalPort < 1 || t.LocalPort > 65535 {
			return fmt.Errorf("tunnel %q: invalid local port %d", name, t.LocalPort)
		}
		switch t.Type {
		case "local", "remote":
			if strings.TrimSpace(t.RemoteHost) == "" || t.RemotePort < 1 || t.RemotePort > 65535 {
				return fmt.Errorf("tunnel %q: valid target host and remote port are required", name)
			}
		case "dynamic":
			if t.RemoteHost != "" || t.RemotePort != 0 {
				return fmt.Errorf("tunnel %q: dynamic tunnels cannot have a remote target", name)
			}
		default:
			return fmt.Errorf("tunnel %q: invalid type %q", name, t.Type)
		}
	}
	for name, group := range cfg.Groups {
		if invalidLine(name) || invalidLine(group.Name) {
			return fmt.Errorf("group %q: fields cannot contain NUL or line breaks", name)
		}
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("group name is required")
		}
	}
	return nil
}

func invalidLine(value string) bool { return strings.ContainsAny(value, "\x00\r\n") }

func Export(output string) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return CopyFile(path, output)
}

func Backup() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}

	backupPath := fmt.Sprintf("%s.backup-%s", path, time.Now().Format("20060102-150405.000000000"))
	if err := CopyFile(path, backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

func Restore(source string, dryRun bool) (string, error) {
	data, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read restore file: %w", err)
	}
	if err := ValidateData(data); err != nil {
		return "", err
	}
	if dryRun {
		return "", nil
	}

	path, err := Path()
	if err != nil {
		return "", err
	}
	var backupPath string
	err = store.WithLock(path+".lock", func() error {
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		backupPath = fmt.Sprintf("%s.backup-%s", path, time.Now().Format("20060102-150405.000000000"))
		if err := store.AtomicWrite(backupPath, original); err != nil {
			return err
		}
		return store.AtomicWrite(path, data)
	})
	return backupPath, err
}

func Save(path string, cfg Config) error {
	_, err := SaveMerged(path, cfg)
	return err
}

// SaveMerged preserves unrelated changes made since Load and returns a new snapshot.
// A config created without Load can add records but cannot replace existing records.
func SaveMerged(path string, cfg Config) (Config, error) {
	var saved Config
	err := store.WithLock(path+".lock", func() error {
		current, err := LoadPath(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if errors.Is(err, os.ErrNotExist) {
			if cfg.baseline != nil {
				return fmt.Errorf("config was removed in another process; reload and try again")
			}
			current = Default()
		}
		merged := Clone(cfg)
		base := cfg.baseline
		if base == nil {
			empty := Default()
			base = &empty
		}
		// Callers lock the dependent tunnel names from their loaded snapshot before
		// changing a host. A new reference needs a reload so it receives the same lock.
		for name, oldHost := range base.Hosts {
			newHost, exists := cfg.Hosts[name]
			if exists && newHost == oldHost {
				continue
			}
			for tunnelName, tunnel := range current.Tunnels {
				if tunnel.Host != name {
					continue
				}
				oldTunnel, existed := base.Tunnels[tunnelName]
				if !existed || oldTunnel.Host != name {
					return fmt.Errorf("host %q gained dependent tunnel %q in another process; reload and try again", name, tunnelName)
				}
			}
		}
		if merged.Hosts, err = store.MergeMap("host", base.Hosts, cfg.Hosts, current.Hosts); err != nil {
			return err
		}
		if merged.Tunnels, err = store.MergeMap("tunnel", base.Tunnels, cfg.Tunnels, current.Tunnels); err != nil {
			return err
		}
		if merged.Groups, err = store.MergeMap("group", base.Groups, cfg.Groups, current.Groups); err != nil {
			return err
		}
		if err := saveUnlocked(path, merged); err != nil {
			return err
		}
		saved = snapshot(merged)
		return nil
	})
	return saved, err
}

func saveUnlocked(path string, cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return store.AtomicWrite(path, append(data, '\n'))
}

func CopyFile(source, dest string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", source)
	}
	return store.AtomicWrite(dest, data)
}
