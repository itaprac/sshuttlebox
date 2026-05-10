package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
