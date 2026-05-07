package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"sshuttlebox/internal/config"
)

const Version = "0.1.0-dev"

type App struct {
	out io.Writer
}

func Run(args []string) error {
	app := App{out: os.Stdout}
	return app.Run(args)
}

func (a App) Run(args []string) error {
	if len(args) == 0 {
		a.printHelp()
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		a.printHelp()
		return nil
	case "version", "-v", "--version":
		fmt.Fprintln(a.out, Version)
		return nil
	case "config":
		return a.runConfig(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run: shbx help", args[0])
	}
}

func (a App) runConfig(args []string) error {
	if len(args) == 0 {
		return errors.New("missing config command; available: init, path, status")
	}

	switch args[0] {
	case "init":
		path, created, err := config.Init()
		if err != nil {
			return err
		}
		if created {
			fmt.Fprintf(a.out, "Created config: %s\n", path)
		} else {
			fmt.Fprintf(a.out, "Config already exists: %s\n", path)
		}
		return nil
	case "path":
		path, err := config.Path()
		if err != nil {
			return err
		}
		fmt.Fprintln(a.out, path)
		return nil
	case "status":
		path, err := config.Path()
		if err != nil {
			return err
		}
		exists, err := config.Exists()
		if err != nil {
			return err
		}
		status := "missing"
		if exists {
			status = "present"
		}
		fmt.Fprintf(a.out, "Config: %s (%s)\n", path, status)
		return nil
	default:
		return fmt.Errorf("unknown config command %q; available: init, path, status", strings.Join(args, " "))
	}
}

func (a App) printHelp() {
	fmt.Fprint(a.out, `sshuttlebox (shbx) - SSH connection helper

Usage:
  shbx <command> [arguments]

Commands:
  config init      Create config file if it does not exist
  config path      Print config file path
  config status    Show config file status
  version          Print version
  help             Show this help

Next planned commands:
  add <name>       Add SSH host
  list             List saved hosts
  connect <name>   Connect to saved host over SSH
`)
}
