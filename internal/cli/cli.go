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
	case "edit":
		return a.runEdit(args[1:])
	case "remove":
		return a.runRemove(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run: shbx help", args[0])
	}
}

func (a App) runAdd(args []string) error {
	const usage = "usage: shbx add [name] [--host host] [--user user] [--port port] [--identity-file path]"

	name := ""
	flagArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = strings.TrimSpace(args[0])
		flagArgs = args[1:]
	}

	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	hostFlag := fs.String("host", "", "SSH host")
	userFlag := fs.String("user", "", "SSH user")
	portFlag := fs.Int("port", 0, "SSH port")
	identityFileFlag := fs.String("identity-file", "", "SSH identity file")

	if err := fs.Parse(flagArgs); err != nil {
		return fmt.Errorf(usage)
	}

	if fs.NArg() != 0 {
		return fmt.Errorf(usage)
	}

	interactive := isTerminalInput(a.in)
	var reader *bufio.Reader
	if interactive {
		reader = bufio.NewReader(a.in)
	}

	if name == "" {
		if !interactive {
			return errors.New("missing host name; pass it as an argument")
		}

		var err error
		name, err = prompt(reader, a.out, "Name")
		if err != nil {
			return err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("host name cannot be empty")
		}
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

	existingHost, existed := cfg.Hosts[name]
	if existed {
		if !interactive {
			return fmt.Errorf("host %q already exists; run shbx edit %s or run shbx add interactively to confirm update", name, name)
		}

		update, err := confirm(reader, a.out, fmt.Sprintf("Host %q already exists. Update it?", name))
		if err != nil {
			return err
		}
		if !update {
			fmt.Fprintln(a.out, "Cancelled.")
			return nil
		}
	}

	host := strings.TrimSpace(*hostFlag)
	user := strings.TrimSpace(*userFlag)
	identityFile := strings.TrimSpace(*identityFileFlag)
	port := *portFlag

	if existed {
		if !flagWasSet(fs, "host") {
			host = existingHost.Host
		}
		if !flagWasSet(fs, "user") {
			user = existingHost.User
		}
		if !flagWasSet(fs, "port") {
			port = existingHost.Port
		}
		if !flagWasSet(fs, "identity-file") {
			identityFile = existingHost.IdentityFile
		}
	}

	if host == "" {
		if !interactive {
			return errors.New("missing SSH host; pass it with --host")
		}

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

	if interactive && existed && fs.NFlag() == 0 {
		var err error
		host, err = promptWithDefault(reader, a.out, "Host", host)
		if err != nil {
			return err
		}
		user, err = promptWithDefault(reader, a.out, "User (optional)", user)
		if err != nil {
			return err
		}

		currentPort := ""
		if port != 0 {
			currentPort = strconv.Itoa(port)
		}
		portText, err := promptWithDefault(reader, a.out, fmt.Sprintf("Port (optional, default %d)", defaultSSHPort), currentPort)
		if err != nil {
			return err
		}
		if portText == "" {
			port = 0
		} else {
			port, err = strconv.Atoi(portText)
			if err != nil {
				return fmt.Errorf("invalid port %q", portText)
			}
		}

		identityFile, err = promptWithDefault(reader, a.out, "Identity file (optional)", identityFile)
		if err != nil {
			return err
		}
	}

	if host == "" {
		return errors.New("SSH host cannot be empty")
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}
	warnMissingIdentityFile(identityFile)

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

	fmt.Fprintf(a.out, "%-16s %-28s %s\n", "NAME", "TARGET", "KEY")
	for _, name := range names {
		host := cfg.Hosts[name]
		fmt.Fprintf(a.out, "%-16s %-28s %s\n", name, formatListTarget(host), formatListKey(host))
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
	const usage = "usage: shbx connect <name> [--dry-run|--print]"

	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New(usage)
	}

	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	dryRunFlag := fs.Bool("dry-run", false, "Print SSH command without running it")
	printFlag := fs.Bool("print", false, "Print SSH command without running it")

	if err := fs.Parse(args[1:]); err != nil {
		return errors.New(usage)
	}
	if fs.NArg() != 0 {
		return errors.New(usage)
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
	if *dryRunFlag || *printFlag {
		fmt.Fprintln(a.out, shellCommandString("ssh", sshArgs))
		return nil
	}

	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (a App) runEdit(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: shbx edit <name> [--name new-name] [--host host] [--user user] [--port port] [--identity-file path]")
	}

	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	newNameFlag := fs.String("name", "", "New saved host name")
	hostFlag := fs.String("host", "", "SSH host")
	userFlag := fs.String("user", "", "SSH user")
	portFlag := fs.Int("port", 0, "SSH port")
	identityFileFlag := fs.String("identity-file", "", "SSH identity file")

	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("usage: shbx edit <name> [--name new-name] [--host host] [--user user] [--port port] [--identity-file path]")
	}

	if fs.NArg() != 0 {
		return fmt.Errorf("usage: shbx edit <name> [--name new-name] [--host host] [--user user] [--port port] [--identity-file path]")
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

	host, ok := cfg.Hosts[name]
	if !ok {
		return fmt.Errorf("host %q not found", name)
	}
	newName := name

	if fs.NFlag() == 0 {
		if !isTerminalInput(a.in) {
			return errors.New("missing edit options; pass at least one of --name, --host, --user, --port, --identity-file")
		}

		reader := bufio.NewReader(a.in)

		nameText, err := promptWithDefault(reader, a.out, "Name", name)
		if err != nil {
			return err
		}
		newName = strings.TrimSpace(nameText)

		hostText, err := promptWithDefault(reader, a.out, "Host", host.Host)
		if err != nil {
			return err
		}
		host.Host = hostText

		userText, err := promptWithDefault(reader, a.out, "User (optional)", host.User)
		if err != nil {
			return err
		}
		host.User = userText

		currentPort := ""
		if host.Port != 0 {
			currentPort = strconv.Itoa(host.Port)
		}
		portText, err := promptWithDefault(reader, a.out, fmt.Sprintf("Port (optional, default %d)", defaultSSHPort), currentPort)
		if err != nil {
			return err
		}
		if portText == "" {
			host.Port = 0
		} else {
			port, err := strconv.Atoi(portText)
			if err != nil {
				return fmt.Errorf("invalid port %q", portText)
			}
			host.Port = port
		}

		identityFileText, err := promptWithDefault(reader, a.out, "Identity file (optional)", host.IdentityFile)
		if err != nil {
			return err
		}
		host.IdentityFile = identityFileText
	} else {
		if flagWasSet(fs, "name") {
			newName = strings.TrimSpace(*newNameFlag)
		}
		if flagWasSet(fs, "host") {
			host.Host = strings.TrimSpace(*hostFlag)
		}
		if flagWasSet(fs, "user") {
			host.User = strings.TrimSpace(*userFlag)
		}
		if flagWasSet(fs, "port") {
			host.Port = *portFlag
		}
		if flagWasSet(fs, "identity-file") {
			host.IdentityFile = strings.TrimSpace(*identityFileFlag)
		}
	}

	if newName == "" {
		return errors.New("host name cannot be empty")
	}
	if newName != name {
		if _, exists := cfg.Hosts[newName]; exists {
			return fmt.Errorf("host %q already exists", newName)
		}
	}
	if host.Host == "" {
		return errors.New("SSH host cannot be empty")
	}
	if host.Port < 0 || host.Port > 65535 {
		return fmt.Errorf("invalid port %d", host.Port)
	}
	warnMissingIdentityFile(host.IdentityFile)

	if newName != name {
		delete(cfg.Hosts, name)
	}
	cfg.Hosts[newName] = host
	if err := config.Save(path, cfg); err != nil {
		return err
	}

	if newName != name {
		fmt.Fprintf(a.out, "Updated host %q as %q\n", name, newName)
	} else {
		fmt.Fprintf(a.out, "Updated host %q\n", name)
	}
	return nil
}

func (a App) runRemove(args []string) error {
	const usage = "usage: shbx remove <name> [--yes|--force]"

	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New(usage)
	}

	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	yesFlag := fs.Bool("yes", false, "Skip confirmation")
	forceFlag := fs.Bool("force", false, "Skip confirmation")

	if err := fs.Parse(args[1:]); err != nil {
		return errors.New(usage)
	}
	if fs.NArg() != 0 {
		return errors.New(usage)
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

	if !*yesFlag && !*forceFlag {
		if !isTerminalInput(a.in) {
			return fmt.Errorf("confirmation required; pass --yes to remove host %q", name)
		}

		reader := bufio.NewReader(a.in)
		remove, err := confirm(reader, a.out, fmt.Sprintf("Remove host %q?", name))
		if err != nil {
			return err
		}
		if !remove {
			fmt.Fprintln(a.out, "Cancelled.")
			return nil
		}
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
  add [name]       Add or update SSH host
  list             List saved hosts with SSH targets
  show <name>      Show saved host details
  connect <name>   Connect to saved host over SSH
  edit <name>      Edit saved host
  remove <name>    Remove saved host
  config init      Create config file if it does not exist
  config path      Print config file path
  config status    Show config file status
  version          Print version
  help             Show this help

Add options:
  shbx add                     Add host interactively
  shbx add <name>              Add host with a saved name
  --host <host>                SSH hostname or IP
  --user <user>                SSH username
  --port <port>                SSH port (default: 22)
  --identity-file <path>       SSH private key path

Edit options:
  --name <name>                Rename saved host
  --host <host>                SSH hostname or IP
  --user <user>                SSH username (empty clears it)
  --port <port>                SSH port (0 uses default: 22)
  --identity-file <path>       SSH private key path (empty clears it)

Connect options:
  --dry-run                    Print SSH command without connecting
  --print                      Alias for --dry-run

Remove options:
  --yes                        Remove without confirmation
  --force                      Alias for --yes
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

	return append(args, formatSSHTarget(host))
}

func formatSSHTarget(host config.Host) string {
	target := host.Host
	if host.User != "" {
		target = host.User + "@" + host.Host
	}
	return target
}

func formatListTarget(host config.Host) string {
	port := host.Port
	if port == 0 {
		port = defaultSSHPort
	}

	return fmt.Sprintf("%s:%d", formatSSHTarget(host), port)
}

func formatListKey(host config.Host) string {
	if host.IdentityFile == "" {
		return "-"
	}
	return host.IdentityFile
}

func shellCommandString(command string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, command)
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') &&
			!(r >= 'a' && r <= 'z') &&
			!(r >= '0' && r <= '9') &&
			!strings.ContainsRune("@%_+=:,./-", r)
	}) == -1 {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func warnMissingIdentityFile(path string) {
	if path == "" {
		return
	}

	expandedPath := expandHomePath(path)
	info, err := os.Stat(expandedPath)
	if err == nil {
		if info.IsDir() {
			fmt.Fprintf(os.Stderr, "Warning: identity file is a directory: %s\n", path)
		}
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "Warning: identity file does not exist: %s\n", path)
		return
	}
	fmt.Fprintf(os.Stderr, "Warning: cannot check identity file %s: %v\n", path, err)
}

func expandHomePath(path string) string {
	if path == "~" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			return homeDir
		}
	}
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			return homeDir + path[1:]
		}
	}
	return path
}

func prompt(reader *bufio.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprintf(out, "%s: ", label)

	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	return strings.TrimSpace(text), nil
}

func promptWithDefault(reader *bufio.Reader, out io.Writer, label, current string) (string, error) {
	if current != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, current)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}

	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return current, nil
	}
	return text, nil
}

func confirm(reader *bufio.Reader, out io.Writer, label string) (bool, error) {
	fmt.Fprintf(out, "%s [y/N]: ", label)

	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	switch strings.ToLower(strings.TrimSpace(text)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	wasSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			wasSet = true
		}
	})
	return wasSet
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
