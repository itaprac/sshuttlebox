package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"sshuttlebox/internal/config"
)

const Version = "0.1.0-dev"

const defaultSSHPort = 22

type App struct {
	in  io.Reader
	out io.Writer
}

func Run(args []string) error {
	app := App{in: os.Stdin, out: os.Stdout}
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
	case "add":
		return a.runAdd(args[1:])
	case "list":
		return a.runList(args[1:])
	case "show":
		return a.runShow(args[1:])
	case "connect":
		return a.runConnect(args[1:])
	case "remove":
		return a.runRemove(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run: shbx help", args[0])
	}
}

func (a App) runAdd(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: shbx add <name> [--host host] [--user user] [--port port] [--identity-file path]")
	}

	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	hostFlag := fs.String("host", "", "SSH host")
	userFlag := fs.String("user", "", "SSH user")
	portFlag := fs.Int("port", 0, "SSH port")
	identityFileFlag := fs.String("identity-file", "", "SSH identity file")

	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("usage: shbx add <name> [--host host] [--user user] [--port port] [--identity-file path]")
	}

	if fs.NArg() != 0 {
		return fmt.Errorf("usage: shbx add <name> [--host host] [--user user] [--port port] [--identity-file path]")
	}

	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("host name cannot be empty")
	}

	host := strings.TrimSpace(*hostFlag)
	user := strings.TrimSpace(*userFlag)
	identityFile := strings.TrimSpace(*identityFileFlag)
	port := *portFlag

	if host == "" {
		if !isTerminalInput(a.in) {
			return errors.New("missing SSH host; pass it with --host")
		}

		reader := bufio.NewReader(a.in)
		var err error

		host, err = prompt(reader, a.out, "Host")
		if err != nil {
			return err
		}
		if user == "" {
			user, err = prompt(reader, a.out, "User (optional)")
			if err != nil {
				return err
			}
		}
		if port == 0 {
			portText, err := prompt(reader, a.out, fmt.Sprintf("Port (optional, default %d)", defaultSSHPort))
			if err != nil {
				return err
			}
			if portText != "" {
				port, err = strconv.Atoi(portText)
				if err != nil {
					return fmt.Errorf("invalid port %q", portText)
				}
			}
		}
		if identityFile == "" {
			identityFile, err = prompt(reader, a.out, "Identity file (optional)")
			if err != nil {
				return err
			}
		}
	}

	if host == "" {
		return errors.New("SSH host cannot be empty")
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}

	if _, _, err := config.Init(); err != nil {
		return err
	}

	path, err := config.Path()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	_, existed := cfg.Hosts[name]
	cfg.Hosts[name] = config.Host{
		Host:         host,
		User:         user,
		Port:         port,
		IdentityFile: identityFile,
	}

	if err := config.Save(path, cfg); err != nil {
		return err
	}

	action := "Added"
	if existed {
		action = "Updated"
	}
	fmt.Fprintf(a.out, "%s host %q\n", action, name)
	return nil
}

func (a App) runList(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: shbx list")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	names := make([]string, 0, len(cfg.Hosts))
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	if len(names) == 0 {
		fmt.Fprintln(a.out, "No saved hosts.")
		return nil
	}

	for _, name := range names {
		fmt.Fprintln(a.out, name)
	}
	return nil
}

func (a App) runShow(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: shbx show <name>")
	}

	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("host name cannot be empty")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	host, ok := cfg.Hosts[name]
	if !ok {
		return fmt.Errorf("host %q not found", name)
	}

	fmt.Fprintf(a.out, "Name: %s\n", name)
	fmt.Fprintf(a.out, "Host: %s\n", host.Host)
	if host.User != "" {
		fmt.Fprintf(a.out, "User: %s\n", host.User)
	}
	if host.Port != 0 {
		fmt.Fprintf(a.out, "Port: %d\n", host.Port)
	} else {
		fmt.Fprintf(a.out, "Port: %d (default)\n", defaultSSHPort)
	}
	if host.IdentityFile != "" {
		fmt.Fprintf(a.out, "Identity file: %s\n", host.IdentityFile)
	}
	return nil
}

func (a App) runConnect(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: shbx connect <name>")
	}

	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("host name cannot be empty")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	host, ok := cfg.Hosts[name]
	if !ok {
		return fmt.Errorf("host %q not found", name)
	}

	sshArgs := buildSSHArgs(host)
	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (a App) runRemove(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: shbx remove <name>")
	}

	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("host name cannot be empty")
	}

	path, err := config.Path()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if _, ok := cfg.Hosts[name]; !ok {
		return fmt.Errorf("host %q not found", name)
	}

	delete(cfg.Hosts, name)

	if err := config.Save(path, cfg); err != nil {
		return err
	}

	fmt.Fprintf(a.out, "Removed host %q\n", name)
	return nil
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
  add <name>       Add or update SSH host
  list             List saved hosts
  show <name>      Show saved host details
  connect <name>   Connect to saved host over SSH
  remove <name>    Remove saved host
  config init      Create config file if it does not exist
  config path      Print config file path
  config status    Show config file status
  version          Print version
  help             Show this help

Add options:
  --host <host>                SSH hostname or IP
  --user <user>                SSH username
  --port <port>                SSH port (default: 22)
  --identity-file <path>       SSH private key path

Next planned commands:
  edit <name>      Edit saved host
`)
}

func buildSSHArgs(host config.Host) []string {
	port := host.Port
	if port == 0 {
		port = defaultSSHPort
	}

	args := []string{"-p", strconv.Itoa(port)}
	if host.IdentityFile != "" {
		args = append(args, "-i", host.IdentityFile)
	}

	target := host.Host
	if host.User != "" {
		target = host.User + "@" + host.Host
	}

	return append(args, target)
}

func prompt(reader *bufio.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprintf(out, "%s: ", label)

	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	return strings.TrimSpace(text), nil
}

func isTerminalInput(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return true
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}
