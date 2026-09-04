package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/itaprac/sshuttlebox/internal/config"
)

func (a App) runConfigStatus(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: shbx config status")
	}

	path, err := config.Path()
	if err != nil {
		return err
	}
	exists, err := config.Exists()
	if err != nil {
		return err
	}
	if !exists {
		fmt.Fprintf(a.out, "Config: %s (missing)\n", path)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	running, stale, err := tunnelStateCounts()
	if err != nil {
		return err
	}

	fmt.Fprintf(a.out, "Config: %s (present)\n", path)
	fmt.Fprintf(a.out, "Hosts: %d  Tunnels: %d  Groups: %d  Running: %d\n", len(cfg.Hosts), len(cfg.Tunnels), len(cfg.Groups), running)
	if stale > 0 {
		fmt.Fprintf(a.out, "Warn: stale tunnel state: %d\n", stale)
	}
	if passwords := configPasswordCount(cfg); passwords > 0 {
		fmt.Fprintf(a.out, "Warn: saved passwords: %d\n", passwords)
	}
	if warning, err := configPermissionWarning(path); err != nil {
		return err
	} else if warning != "" {
		fmt.Fprintln(a.out, warning)
	}
	return nil
}

func (a App) runConfigExport(args []string) error {
	fs := flag.NewFlagSet("config export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := fs.String("output", "", "Output file")
	if err := fs.Parse(args); err != nil {
		return errors.New("usage: shbx config export [--output file]")
	}
	if fs.NArg() != 0 {
		return errors.New("usage: shbx config export [--output file]")
	}

	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *output == "" {
		if configHasPasswords(cfg) {
			return errors.New("config contains saved passwords; pass --output to export")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = a.out.Write(data)
		return err
	}

	if err := config.Export(*output); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Exported config: %s\n", *output)
	return nil
}

func (a App) runConfigBackup(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: shbx config backup")
	}
	path, err := config.Backup()
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Backup: %s\n", path)
	return nil
}

func (a App) runConfigRestore(args []string) error {
	source, dryRun, err := parseConfigRestoreArgs(args)
	if err != nil {
		return errors.New("usage: shbx config restore <file> [--dry-run]")
	}

	backupPath, err := config.Restore(source, dryRun)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintln(a.out, "Restore OK")
		return nil
	}
	fmt.Fprintf(a.out, "Restored config; backup: %s\n", backupPath)
	return nil
}

func parseConfigRestoreArgs(args []string) (string, bool, error) {
	source := ""
	dryRun := false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, fmt.Errorf("unknown flag %s", arg)
			}
			if source != "" {
				return "", false, errors.New("too many restore files")
			}
			source = arg
		}
	}
	if source == "" {
		return "", false, errors.New("missing restore file")
	}
	return source, dryRun, nil
}

func configHasPasswords(cfg config.Config) bool {
	return configPasswordCount(cfg) > 0
}

func configPasswordCount(cfg config.Config) int {
	count := 0
	for _, host := range cfg.Hosts {
		if host.Password != "" {
			count++
		}
	}
	return count
}

func tunnelStateCounts() (int, int, error) {
	statuses, err := tunnelStatusSnapshot(nil)
	if err != nil {
		return 0, 0, fmt.Errorf("check tunnel state: %w", err)
	}
	running, stale := 0, 0
	for name, status := range statuses {
		if status.Err != nil {
			return 0, 0, fmt.Errorf("tunnel %q status unknown: %w", name, status.Err)
		}
		if status.Running {
			running++
		} else {
			stale++
		}
	}
	return running, stale, nil
}

func configPermissionWarning(path string) (string, error) {
	if runtime.GOOS == "windows" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat config file: %w", err)
	}
	if info.Mode().Perm()&0o077 == 0 {
		return "", nil
	}
	return fmt.Sprintf("Warn: config permissions: %04o (prefer 0600)", info.Mode().Perm()), nil
}
