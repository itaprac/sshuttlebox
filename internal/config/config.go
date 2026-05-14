package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	AppDirName     = "sshuttlebox"
	ConfigFileName = "config.json"
)

type Config struct {
	Version int               `json:"version"`
	Hosts   map[string]Host   `json:"hosts"`
	Tunnels map[string]Tunnel `json:"tunnels"`
	Groups  map[string]Group  `json:"groups,omitempty"`
}

type Host struct {
	Host         string `json:"host"`
	User         string `json:"user,omitempty"`
	Password     string `json:"password,omitempty"`
	Port         int    `json:"port,omitempty"`
	IdentityFile string `json:"identityFile,omitempty"`
	Group        string `json:"group,omitempty"`
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

	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}

	cfg := Default()
	if err := Save(path, cfg); err != nil {
		return "", false, err
	}

	return path, true, nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("config does not exist; run: shbx config init")
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Hosts == nil {
		cfg.Hosts = map[string]Host{}
	}
	if cfg.Tunnels == nil {
		cfg.Tunnels = map[string]Tunnel{}
	}
	if cfg.Groups == nil {
		cfg.Groups = map[string]Group{}
	}

	return cfg, nil
}

func ValidateData(data []byte) error {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if cfg.Version <= 0 {
		return fmt.Errorf("config version must be positive")
	}
	return nil
}

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
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("config does not exist; run: shbx config init")
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("config path is a directory")
	}

	backupPath, err := Backup()
	if err != nil {
		return "", err
	}
	if err := writeFile(path, data, info.Mode().Perm()); err != nil {
		return "", err
	}
	return backupPath, nil
}

func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
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
	return writeFile(dest, data, info.Mode().Perm())
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("set file permissions: %w", err)
	}
	return nil
}
