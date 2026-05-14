package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/itaprac/sshuttlebox/internal/config"
)

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
	for _, host := range cfg.Hosts {
		if host.Password != "" {
			return true
		}
	}
	return false
}
