package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathUsesHomeConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}

	want := filepath.Join(home, ".config", AppDirName, ConfigFileName)
	if path != want {
		t.Fatalf("Path() = %q, want %q", path, want)
	}
}

func TestInitExistsAndLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	exists, err := Exists()
	if err != nil {
		t.Fatalf("Exists before init: %v", err)
	}
	if exists {
		t.Fatalf("config exists before init")
	}

	path, created, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !created {
		t.Fatalf("expected config to be created")
	}
	if !strings.HasSuffix(path, filepath.Join(".config", AppDirName, ConfigFileName)) {
		t.Fatalf("unexpected path %q", path)
	}

	exists, err = Exists()
	if err != nil {
		t.Fatalf("Exists after init: %v", err)
	}
	if !exists {
		t.Fatalf("config missing after init")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != 1 {
		t.Fatalf("Version = %d, want 1", cfg.Version)
	}
	if len(cfg.Hosts) != 0 {
		t.Fatalf("Hosts = %v, want empty", cfg.Hosts)
	}
	if len(cfg.Tunnels) != 0 {
		t.Fatalf("Tunnels = %v, want empty", cfg.Tunnels)
	}
	if len(cfg.Groups) != 0 {
		t.Fatalf("Groups = %v, want empty", cfg.Groups)
	}

	_, created, err = Init()
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if created {
		t.Fatalf("second init should not recreate config")
	}
}

func TestSaveAndLoadHosts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, _, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	cfg := Default()
	cfg.Hosts["prod"] = Host{
		Host:         "192.0.2.10",
		User:         "deploy",
		Password:     "secret",
		Port:         2222,
		IdentityFile: "~/.ssh/id_ed25519",
		Group:        "work",
	}
	cfg.Groups["work"] = Group{Name: "work"}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Hosts["prod"] != cfg.Hosts["prod"] {
		t.Fatalf("loaded host = %+v, want %+v", got.Hosts["prod"], cfg.Hosts["prod"])
	}
	if got.Groups["work"] != cfg.Groups["work"] {
		t.Fatalf("loaded group = %+v, want %+v", got.Groups["work"], cfg.Groups["work"])
	}
}

func TestSaveAndLoadTunnels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, _, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	cfg := Default()
	cfg.Hosts["prod"] = Host{Host: "prod.example"}
	cfg.Tunnels["db"] = Tunnel{
		Host:       "prod",
		Type:       "local",
		LocalPort:  5432,
		RemoteHost: "127.0.0.1",
		RemotePort: 5432,
		Group:      "work",
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Tunnels["db"] != cfg.Tunnels["db"] {
		t.Fatalf("loaded tunnel = %+v, want %+v", got.Tunnels["db"], cfg.Tunnels["db"])
	}
}

func TestLoadMissingConfigReturnsHelpfulError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatalf("expected missing config error")
	}
	if !strings.Contains(err.Error(), "config does not exist; run: shbx config init") {
		t.Fatalf("unexpected error %q", err)
	}
}

func TestExportBackupRestorePreserveBytesAndUsePrivateMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, _, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	original := []byte("{\n  \"hosts\": {},\n  \"version\": 1,\n  \"tunnels\": {}\n}\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatalf("write original: %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod original: %v", err)
	}

	exportPath := filepath.Join(t.TempDir(), "export.json")
	if err := Export(exportPath); err != nil {
		t.Fatalf("Export: %v", err)
	}
	assertFileBytesAndMode(t, exportPath, original, 0o600)

	backupPath, err := Backup()
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	assertFileBytesAndMode(t, backupPath, original, 0o600)

	restoreData := []byte("{\"version\":1,\"hosts\":{\"prod\":{\"host\":\"prod.example\"}},\"tunnels\":{}}\n")
	restorePath := filepath.Join(t.TempDir(), "restore.json")
	if err := os.WriteFile(restorePath, restoreData, 0o600); err != nil {
		t.Fatalf("write restore: %v", err)
	}

	restoreBackupPath, err := Restore(restorePath, false)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	assertFileBytesAndMode(t, restoreBackupPath, original, 0o600)
	assertFileBytesAndMode(t, path, restoreData, 0o600)
}

func TestRestoreRejectsInvalidConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, _, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	original := []byte("{\"version\":1,\"hosts\":{},\"tunnels\":{}}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}

	restorePath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(restorePath, []byte("{\"version\":0,\"hosts\":{},\"tunnels\":{}}\n"), 0o600); err != nil {
		t.Fatalf("write restore: %v", err)
	}

	_, err = Restore(restorePath, false)
	if err == nil {
		t.Fatalf("expected invalid restore error")
	}
	assertFileBytesAndMode(t, path, original, 0o600)
}

func assertFileBytesAndMode(t *testing.T, path string, want []byte, mode os.FileMode) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s bytes = %q, want %q", path, got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s mode = %04o, want %04o", path, info.Mode().Perm(), mode)
	}
}
