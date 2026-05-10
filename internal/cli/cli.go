package cli

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/tunnelstate"
	"golang.org/x/term"
)

const Version = "0.2.1"

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
		return a.runUI(nil)
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
	case "tunnel":
		return a.runTunnel(args[1:])
	case "group":
		return a.runGroup(args[1:])
	case "edit":
		return a.runEdit(args[1:])
	case "remove":
		return a.runRemove(args[1:])
	case "ui":
		return a.runUI(args[1:])
	case "completion":
		return a.runCompletion(args[1:])
	case "__complete":
		return a.runComplete(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run: shbx help", args[0])
	}
}

func (a App) runAdd(args []string) error {
	const usage = "usage: shbx add [name] [--host host] [--user user] [--password password] [--port port] [--identity-file path|--identity-from host] [--group group]"

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
	passwordFlag := fs.String("password", "", "SSH password")
	portFlag := fs.Int("port", 0, "SSH port")
	identityFileFlag := fs.String("identity-file", "", "SSH identity file")
	identityFromFlag := fs.String("identity-from", "", "Reuse SSH identity file from another saved host")
	groupFlag := fs.String("group", "", "Group name")

	if err := fs.Parse(flagArgs); err != nil {
		return fmt.Errorf(usage)
	}

	if fs.NArg() != 0 {
		return fmt.Errorf(usage)
	}
	if flagWasSet(fs, "identity-file") && flagWasSet(fs, "identity-from") {
		return errors.New("--identity-file and --identity-from cannot be used together")
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
	password := *passwordFlag
	identityFile := strings.TrimSpace(*identityFileFlag)
	identityFrom := strings.TrimSpace(*identityFromFlag)
	group := strings.TrimSpace(*groupFlag)
	port := *portFlag

	if existed {
		if !flagWasSet(fs, "host") {
			host = existingHost.Host
		}
		if !flagWasSet(fs, "user") {
			user = existingHost.User
		}
		if !flagWasSet(fs, "password") {
			password = existingHost.Password
		}
		if !flagWasSet(fs, "port") {
			port = existingHost.Port
		}
		if !flagWasSet(fs, "identity-file") {
			identityFile = existingHost.IdentityFile
		}
		if !flagWasSet(fs, "group") {
			group = existingHost.Group
		}
	}
	if flagWasSet(fs, "identity-from") {
		var err error
		identityFile, err = identityFileFromHost(cfg.Hosts, identityFrom)
		if err != nil {
			return err
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
			user, err = prompt(reader, a.out, "User")
			if err != nil {
				return err
			}
		}
		if password == "" {
			password, err = promptPassword(reader, a.in, a.out, "Password")
			if err != nil {
				return err
			}
		}
		if port == 0 {
			portText, err := prompt(reader, a.out, fmt.Sprintf("Port [default %d]", defaultSSHPort))
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
			identityFile, err = promptIdentityFile(reader, a.out, cfg.Hosts, name, identityFile)
			if err != nil {
				return err
			}
		}
		if group == "" {
			group, err = promptGroup(reader, a.out, cfg.Groups, group)
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
		user, err = promptWithDefault(reader, a.out, "User", user)
		if err != nil {
			return err
		}
		password, err = promptPasswordEdit(reader, a.in, a.out, password)
		if err != nil {
			return err
		}

		currentPort := ""
		if port != 0 {
			currentPort = strconv.Itoa(port)
		}
		portText, err := promptWithDefault(reader, a.out, fmt.Sprintf("Port [default %d]", defaultSSHPort), currentPort)
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

		identityFile, err = promptIdentityFile(reader, a.out, cfg.Hosts, name, identityFile)
		if err != nil {
			return err
		}
		group, err = promptGroup(reader, a.out, cfg.Groups, group)
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
		Password:     password,
		Port:         port,
		IdentityFile: identityFile,
		Group:        group,
	}
	ensureGroup(&cfg, group)

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

	fmt.Fprintf(a.out, "%-16s %-14s %-28s %s\n", "NAME", "GROUP", "TARGET", "KEY")
	for _, name := range names {
		host := cfg.Hosts[name]
		fmt.Fprintf(a.out, "%-16s %-14s %-28s %s\n", name, optionalValue(host.Group), formatListTarget(host), formatListKey(host))
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
	if host.Password != "" {
		fmt.Fprintln(a.out, "Password: set")
	}
	if host.Port != 0 {
		fmt.Fprintf(a.out, "Port: %d\n", host.Port)
	} else {
		fmt.Fprintf(a.out, "Port: %d default\n", defaultSSHPort)
	}
	if host.IdentityFile != "" {
		fmt.Fprintf(a.out, "Identity file: %s\n", host.IdentityFile)
	}
	if host.Group != "" {
		fmt.Fprintf(a.out, "Group: %s\n", host.Group)
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
		fmt.Fprintln(a.out, connectCommandString(host, sshArgs))
		return nil
	}

	fmt.Fprintf(a.out, "Connecting to %q (%s)...\n", name, formatListTarget(host))
	var runErr error
	if host.Password != "" {
		runErr = runSSHWithPassword(host.Password, sshArgs)
	} else {
		cmd := exec.Command(sshBinary(), sshArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		runErr = cmd.Run()
	}
	if runErr != nil {
		return fmt.Errorf("connect %q: %w", name, runErr)
	}
	fmt.Fprintln(a.out, "Connection closed.")
	return nil
}

func (a App) runTunnel(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shbx tunnel <add|list|show|start|stop|remove>")
	}

	switch args[0] {
	case "add":
		return a.runTunnelAdd(args[1:])
	case "list":
		return a.runTunnelList(args[1:])
	case "show":
		return a.runTunnelShow(args[1:])
	case "start":
		return a.runTunnelStart(args[1:])
	case "stop":
		return a.runTunnelStop(args[1:])
	case "remove":
		return a.runTunnelRemove(args[1:])
	default:
		return fmt.Errorf("unknown tunnel command %q; available: add, list, show, start, stop, remove", args[0])
	}
}

func (a App) runTunnelAdd(args []string) error {
	const usage = "usage: shbx tunnel add [name] --host <saved-host> (--local-port port --remote-host host --remote-port port | --dynamic-port port) [--type local|remote|dynamic] [--bind address] [--group group]"

	name := ""
	flagArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = strings.TrimSpace(args[0])
		flagArgs = args[1:]
	}

	fs := flag.NewFlagSet("tunnel add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	hostFlag := fs.String("host", "", "Saved SSH host to use")
	typeFlag := fs.String("type", "", "Tunnel type: local, remote, or dynamic")
	bindFlag := fs.String("bind", "", "Bind address")
	localPortFlag := fs.Int("local-port", 0, "Local port for local/dynamic forwarding")
	dynamicPortFlag := fs.Int("dynamic-port", 0, "Local SOCKS port for dynamic forwarding")
	remoteHostFlag := fs.String("remote-host", "", "Remote target host")
	remotePortFlag := fs.Int("remote-port", 0, "Remote target/listen port")
	groupFlag := fs.String("group", "", "Group name")

	if err := fs.Parse(flagArgs); err != nil {
		return errors.New(usage)
	}
	if fs.NArg() != 0 {
		return errors.New(usage)
	}

	path, err := config.Path()
	if err != nil {
		return err
	}
	if _, _, err := config.Init(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	interactive := isTerminalInput(a.in)
	var reader *bufio.Reader
	if interactive {
		reader = bufio.NewReader(a.in)
	}

	if name == "" {
		if !interactive {
			return errors.New("missing tunnel name; pass it as an argument")
		}
		name, err = prompt(reader, a.out, "Name")
		if err != nil {
			return err
		}
		name = strings.TrimSpace(name)
	}
	if name == "" {
		return errors.New("tunnel name cannot be empty")
	}
	if _, exists := cfg.Tunnels[name]; exists {
		return fmt.Errorf("tunnel %q already exists; remove it first or choose a different name", name)
	}

	tunnel := config.Tunnel{
		Host:        strings.TrimSpace(*hostFlag),
		Type:        strings.ToLower(strings.TrimSpace(*typeFlag)),
		BindAddress: strings.TrimSpace(*bindFlag),
		LocalPort:   *localPortFlag,
		RemoteHost:  strings.TrimSpace(*remoteHostFlag),
		RemotePort:  *remotePortFlag,
		Group:       strings.TrimSpace(*groupFlag),
	}
	if *dynamicPortFlag != 0 {
		if flagWasSet(fs, "local-port") {
			return errors.New("--dynamic-port cannot be used with --local-port")
		}
		tunnel.Type = "dynamic"
		tunnel.LocalPort = *dynamicPortFlag
	}
	if tunnel.Type == "" {
		tunnel.Type = "local"
	}
	if interactive && fs.NFlag() == 0 {
		tunnel, err = promptTunnel(reader, a.out, cfg.Hosts, cfg.Groups, tunnel)
		if err != nil {
			return err
		}
	}

	if err := validateTunnel(tunnel, cfg.Hosts); err != nil {
		return err
	}

	cfg.Tunnels[name] = tunnel
	ensureGroup(&cfg, tunnel.Group)
	if err := config.Save(path, cfg); err != nil {
		return err
	}

	fmt.Fprintf(a.out, "Added tunnel %q\n", name)
	return nil
}

func (a App) runTunnelList(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: shbx tunnel list")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	names := make([]string, 0, len(cfg.Tunnels))
	for name := range cfg.Tunnels {
		names = append(names, name)
	}
	sort.Strings(names)

	if len(names) == 0 {
		fmt.Fprintln(a.out, "No saved tunnels.")
		return nil
	}

	state, err := tunnelstate.Prune()
	if err != nil {
		return err
	}

	fmt.Fprintf(a.out, "%-16s %-9s %-12s %-14s %-16s %s\n", "NAME", "STATUS", "TYPE", "GROUP", "SSH HOST", "FORWARD")
	for _, name := range names {
		tunnel := cfg.Tunnels[name]
		fmt.Fprintf(a.out, "%-16s %-9s %-12s %-14s %-16s %s\n", name, formatTunnelStatus(state, name), tunnel.Type, optionalValue(tunnel.Group), tunnel.Host, formatTunnelForward(tunnel))
	}
	return nil
}

func (a App) runTunnelShow(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: shbx tunnel show <name>")
	}

	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("tunnel name cannot be empty")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	tunnel, ok := cfg.Tunnels[name]
	if !ok {
		return fmt.Errorf("tunnel %q not found", name)
	}
	host, ok := cfg.Hosts[tunnel.Host]
	if !ok {
		return fmt.Errorf("host %q for tunnel %q not found", tunnel.Host, name)
	}
	if err := validateTunnel(tunnel, cfg.Hosts); err != nil {
		return err
	}
	entry, running, err := tunnelstate.Get(name)
	if err != nil {
		return err
	}

	sshArgs, err := buildTunnelStartSSHArgs(name, host, tunnel)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Name: %s\n", name)
	if running {
		fmt.Fprintf(a.out, "Status: running pid %d since %s\n", entry.PID, entry.StartedAt.Local().Format("2006-01-02 15:04:05"))
	} else {
		fmt.Fprintln(a.out, "Status: stopped")
	}
	fmt.Fprintf(a.out, "Host: %s\n", tunnel.Host)
	fmt.Fprintf(a.out, "Type: %s\n", tunnel.Type)
	if tunnel.Group != "" {
		fmt.Fprintf(a.out, "Group: %s\n", tunnel.Group)
	}
	fmt.Fprintf(a.out, "Forward: %s\n", formatTunnelForward(tunnel))
	fmt.Fprintf(a.out, "Command: %s\n", tunnelCommandString(host, sshArgs))
	return nil
}

func (a App) runTunnelStart(args []string) error {
	const usage = "usage: shbx tunnel start <name> [--dry-run|--print]"

	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New(usage)
	}

	fs := flag.NewFlagSet("tunnel start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	dryRunFlag := fs.Bool("dry-run", false, "Print SSH tunnel command without running it")
	printFlag := fs.Bool("print", false, "Print SSH tunnel command without running it")
	if err := fs.Parse(args[1:]); err != nil {
		return errors.New(usage)
	}
	if fs.NArg() != 0 {
		return errors.New(usage)
	}

	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("tunnel name cannot be empty")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	tunnel, ok := cfg.Tunnels[name]
	if !ok {
		return fmt.Errorf("tunnel %q not found", name)
	}
	host, ok := cfg.Hosts[tunnel.Host]
	if !ok {
		return fmt.Errorf("host %q for tunnel %q not found", tunnel.Host, name)
	}
	if err := validateTunnel(tunnel, cfg.Hosts); err != nil {
		return err
	}

	sshArgs, err := buildTunnelStartSSHArgs(name, host, tunnel)
	if err != nil {
		return err
	}
	if *dryRunFlag || *printFlag {
		fmt.Fprintln(a.out, tunnelCommandString(host, sshArgs))
		return nil
	}
	if entry, running, err := tunnelstate.Get(name); err != nil {
		return err
	} else if running {
		fmt.Fprintf(a.out, "Tunnel %q already running with pid %d\n", name, entry.PID)
		return nil
	}

	fmt.Fprintf(a.out, "Starting tunnel %q...\n", name)
	pid, err := startTunnelProcess(name, host, tunnel, true)
	if err != nil {
		return fmt.Errorf("start tunnel %q: %w", name, err)
	}
	fmt.Fprintf(a.out, "Started tunnel %q in background with pid %d\n", name, pid)
	return nil
}

func (a App) runTunnelStop(args []string) error {
	const usage = "usage: shbx tunnel stop <name>"

	if len(args) != 1 {
		return errors.New(usage)
	}

	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("tunnel name cannot be empty")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	tunnel, ok := cfg.Tunnels[name]
	if !ok {
		return fmt.Errorf("tunnel %q not found", name)
	}
	host, ok := cfg.Hosts[tunnel.Host]
	if !ok {
		return fmt.Errorf("host %q for tunnel %q not found", tunnel.Host, name)
	}

	if entry, running, err := tunnelstate.Get(name); err != nil {
		return err
	} else if !running {
		fmt.Fprintf(a.out, "Tunnel %q is not running\n", name)
		return nil
	} else {
		fmt.Fprintf(a.out, "Stopping tunnel %q with pid %d...\n", name, entry.PID)
	}
	stopped, entry, err := stopTunnelByName(name, host)
	if err != nil {
		return err
	}
	if !stopped {
		fmt.Fprintf(a.out, "Tunnel %q is not running\n", name)
		return nil
	}
	fmt.Fprintf(a.out, "Stopped tunnel %q with pid %d\n", name, entry.PID)
	return nil
}

func (a App) runTunnelRemove(args []string) error {
	const usage = "usage: shbx tunnel remove <name> [--yes|--force]"

	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New(usage)
	}

	fs := flag.NewFlagSet("tunnel remove", flag.ContinueOnError)
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
		return errors.New("tunnel name cannot be empty")
	}

	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	tunnel, ok := cfg.Tunnels[name]
	if !ok {
		return fmt.Errorf("tunnel %q not found", name)
	}
	if entry, running, err := tunnelstate.Get(name); err != nil {
		return err
	} else if running && !*yesFlag && !*forceFlag {
		return fmt.Errorf("tunnel %q is running with pid %d; stop it first or pass --force", name, entry.PID)
	}

	if !*yesFlag && !*forceFlag {
		if !isTerminalInput(a.in) {
			return fmt.Errorf("confirmation required; pass --yes to remove tunnel %q", name)
		}

		reader := bufio.NewReader(a.in)
		remove, err := confirm(reader, a.out, fmt.Sprintf("Remove tunnel %q?", name))
		if err != nil {
			return err
		}
		if !remove {
			fmt.Fprintln(a.out, "Cancelled.")
			return nil
		}
	}

	if *forceFlag {
		if host, ok := cfg.Hosts[tunnel.Host]; ok {
			_, _, _ = stopTunnelByName(name, host)
		} else {
			_, _, _ = tunnelstate.Stop(name)
		}
	}
	delete(cfg.Tunnels, name)
	if err := config.Save(path, cfg); err != nil {
		return err
	}

	fmt.Fprintf(a.out, "Removed tunnel %q\n", name)
	return nil
}

func (a App) runGroup(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shbx group <add|list|show|remove|rename>")
	}

	switch args[0] {
	case "add":
		return a.runGroupAdd(args[1:])
	case "list":
		return a.runGroupList(args[1:])
	case "show":
		return a.runGroupShow(args[1:])
	case "remove":
		return a.runGroupRemove(args[1:])
	case "rename":
		return a.runGroupRename(args[1:])
	default:
		return fmt.Errorf("unknown group command %q; available: add, list, show, remove, rename", args[0])
	}
}

func (a App) runGroupAdd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: shbx group add <name>")
	}
	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("group name cannot be empty")
	}
	path, _, err := config.Init()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if groupExists(cfg, name) {
		return fmt.Errorf("group %q already exists", name)
	}
	ensureGroup(&cfg, name)
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Added group %q\n", name)
	return nil
}

func (a App) runGroupList(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: shbx group list")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	names := groupNames(cfg)
	if len(names) == 0 {
		fmt.Fprintln(a.out, "No saved groups.")
		return nil
	}
	fmt.Fprintf(a.out, "%-16s %-7s %s\n", "NAME", "HOSTS", "TUNNELS")
	for _, name := range names {
		hosts, tunnels := groupMembers(cfg, name)
		fmt.Fprintf(a.out, "%-16s %-7d %d\n", name, len(hosts), len(tunnels))
	}
	return nil
}

func (a App) runGroupShow(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: shbx group show <name>")
	}
	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("group name cannot be empty")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !groupExists(cfg, name) {
		return fmt.Errorf("group %q not found", name)
	}
	hosts, tunnels := groupMembers(cfg, name)
	fmt.Fprintf(a.out, "Name: %s\n", name)
	fmt.Fprintf(a.out, "Hosts: %s\n", joinOrDefault(hosts, "-"))
	fmt.Fprintf(a.out, "Tunnels: %s\n", joinOrDefault(tunnels, "-"))
	return nil
}

func (a App) runGroupRemove(args []string) error {
	const usage = "usage: shbx group remove <name> [--yes|--force]"
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("group remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yesFlag := fs.Bool("yes", false, "Skip confirmation")
	forceFlag := fs.Bool("force", false, "Clear group from members")
	if err := fs.Parse(args[1:]); err != nil {
		return errors.New(usage)
	}
	if fs.NArg() != 0 {
		return errors.New(usage)
	}
	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("group name cannot be empty")
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !groupExists(cfg, name) {
		return fmt.Errorf("group %q not found", name)
	}
	hosts, tunnels := groupMembers(cfg, name)
	if len(hosts)+len(tunnels) > 0 && !*forceFlag {
		return fmt.Errorf("group %q is not empty; pass --force to clear it from members", name)
	}
	if !*yesFlag && !*forceFlag {
		if !isTerminalInput(a.in) {
			return fmt.Errorf("confirmation required; pass --yes to remove group %q", name)
		}
		reader := bufio.NewReader(a.in)
		remove, err := confirm(reader, a.out, fmt.Sprintf("Remove group %q?", name))
		if err != nil {
			return err
		}
		if !remove {
			fmt.Fprintln(a.out, "Cancelled.")
			return nil
		}
	}
	clearGroup(&cfg, name)
	delete(cfg.Groups, name)
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Removed group %q\n", name)
	return nil
}

func (a App) runGroupRename(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: shbx group rename <old-name> <new-name>")
	}
	oldName := strings.TrimSpace(args[0])
	newName := strings.TrimSpace(args[1])
	if oldName == "" || newName == "" {
		return errors.New("group name cannot be empty")
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !groupExists(cfg, oldName) {
		return fmt.Errorf("group %q not found", oldName)
	}
	if groupExists(cfg, newName) {
		return fmt.Errorf("group %q already exists", newName)
	}
	delete(cfg.Groups, oldName)
	ensureGroup(&cfg, newName)
	renameGroupMembers(&cfg, oldName, newName)
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Renamed group %q as %q\n", oldName, newName)
	return nil
}

func (a App) runEdit(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: shbx edit <name> [--name new-name] [--host host] [--user user] [--password password] [--port port] [--identity-file path|--identity-from host] [--group group]")
	}

	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	newNameFlag := fs.String("name", "", "New saved host name")
	hostFlag := fs.String("host", "", "SSH host")
	userFlag := fs.String("user", "", "SSH user")
	passwordFlag := fs.String("password", "", "SSH password")
	portFlag := fs.Int("port", 0, "SSH port")
	identityFileFlag := fs.String("identity-file", "", "SSH identity file")
	identityFromFlag := fs.String("identity-from", "", "Reuse SSH identity file from another saved host")
	groupFlag := fs.String("group", "", "Group name")

	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("usage: shbx edit <name> [--name new-name] [--host host] [--user user] [--password password] [--port port] [--identity-file path|--identity-from host] [--group group]")
	}

	if fs.NArg() != 0 {
		return fmt.Errorf("usage: shbx edit <name> [--name new-name] [--host host] [--user user] [--password password] [--port port] [--identity-file path|--identity-from host] [--group group]")
	}
	if flagWasSet(fs, "identity-file") && flagWasSet(fs, "identity-from") {
		return errors.New("--identity-file and --identity-from cannot be used together")
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
			return errors.New("missing edit options; pass at least one of --name, --host, --user, --password, --port, --identity-file, --group")
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

		userText, err := promptWithDefault(reader, a.out, "User", host.User)
		if err != nil {
			return err
		}
		host.User = userText

		passwordText, err := promptPasswordEdit(reader, a.in, a.out, host.Password)
		if err != nil {
			return err
		}
		host.Password = passwordText

		currentPort := ""
		if host.Port != 0 {
			currentPort = strconv.Itoa(host.Port)
		}
		portText, err := promptWithDefault(reader, a.out, fmt.Sprintf("Port [default %d]", defaultSSHPort), currentPort)
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

		identityFileText, err := promptIdentityFile(reader, a.out, cfg.Hosts, name, host.IdentityFile)
		if err != nil {
			return err
		}
		host.IdentityFile = identityFileText

		groupText, err := promptGroup(reader, a.out, cfg.Groups, host.Group)
		if err != nil {
			return err
		}
		host.Group = groupText
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
		if flagWasSet(fs, "password") {
			host.Password = *passwordFlag
		}
		if flagWasSet(fs, "port") {
			host.Port = *portFlag
		}
		if flagWasSet(fs, "identity-file") {
			host.IdentityFile = strings.TrimSpace(*identityFileFlag)
		}
		if flagWasSet(fs, "identity-from") {
			identityFile, err := identityFileFromHost(cfg.Hosts, strings.TrimSpace(*identityFromFlag))
			if err != nil {
				return err
			}
			host.IdentityFile = identityFile
		}
		if flagWasSet(fs, "group") {
			host.Group = strings.TrimSpace(*groupFlag)
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
	ensureGroup(&cfg, host.Group)
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
		fmt.Fprintf(a.out, "Config: %s %s\n", path, status)
		return nil
	default:
		return fmt.Errorf("unknown config command %q; available: init, path, status", strings.Join(args, " "))
	}
}

func (a App) runCompletion(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shbx completion <bash|zsh|fish|install>")
	}
	if args[0] == "install" {
		return a.runCompletionInstall(args[1:])
	}
	if len(args) != 1 {
		return errors.New("usage: shbx completion <bash|zsh|fish|install>")
	}

	switch args[0] {
	case "bash":
		fmt.Fprint(a.out, bashCompletionScript)
	case "zsh":
		fmt.Fprint(a.out, zshCompletionScript)
	case "fish":
		fmt.Fprint(a.out, fishCompletionScript)
	default:
		return fmt.Errorf("unknown shell %q; available: bash, zsh, fish", args[0])
	}
	return nil
}

func (a App) runCompletionInstall(args []string) error {
	fs := flag.NewFlagSet("completion install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	shellFlag := fs.String("shell", "auto", "Shell to install completion for: auto, all, bash, zsh, fish")
	noRCFlag := fs.Bool("no-rc", false, "Do not update shell startup files")
	if err := fs.Parse(args); err != nil {
		return errors.New("usage: shbx completion install [--shell auto|all|bash|zsh|fish] [--no-rc]")
	}
	if fs.NArg() != 0 {
		return errors.New("usage: shbx completion install [--shell auto|all|bash|zsh|fish] [--no-rc]")
	}

	shells, err := completionInstallShells(*shellFlag)
	if err != nil {
		return err
	}

	for _, shell := range shells {
		result, err := installCompletion(shell, !*noRCFlag)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Installed %s completion: %s\n", shell, result.completionPath)
		for _, path := range result.updatedRCFiles {
			fmt.Fprintf(a.out, "Updated startup file: %s\n", path)
		}
		for _, path := range result.unchangedRCFiles {
			fmt.Fprintf(a.out, "Startup file already configured: %s\n", path)
		}
		for _, note := range result.notes {
			fmt.Fprintln(a.out, note)
		}
	}
	return nil
}

func (a App) runComplete(args []string) error {
	if len(args) == 0 {
		return nil
	}

	switch args[0] {
	case "commands":
		prefix := completePrefix(args[1:])
		for _, command := range completeCommandNames(prefix) {
			fmt.Fprintln(a.out, command)
		}
	case "hosts":
		prefix := completePrefix(args[1:])
		for _, name := range completeHostNames(prefix) {
			fmt.Fprintln(a.out, name)
		}
	case "tunnels":
		prefix := completePrefix(args[1:])
		for _, name := range completeTunnelNames(prefix) {
			fmt.Fprintln(a.out, name)
		}
	case "groups":
		prefix := completePrefix(args[1:])
		for _, name := range completeGroupNames(prefix) {
			fmt.Fprintln(a.out, name)
		}
	}
	return nil
}

func (a App) printHelp() {
	fmt.Fprint(a.out, `sshuttlebox (shbx) - SSH connection helper

Usage:
  shbx                         Open the interactive terminal UI
  shbx <command> [arguments]

Commands:
  add [name]       Add or update SSH host
  list             List saved hosts with SSH targets
  show <name>      Show saved host details
  connect <name>   Connect to saved host over SSH
  tunnel           Add, list, show, start, stop, or remove SSH tunnels
  group            Add, list, show, rename, or remove groups
  edit <name>      Edit saved host
  remove <name>    Remove saved host
  ui               Open the interactive terminal UI (default)
  completion       Generate shell completion script
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
  --password <password>        SSH password for automatic login
  --port <port>                SSH port, default 22
  --identity-file <path>       SSH private key path
  --identity-from <host>       Reuse private key path from saved host
  --group <group>              Put host in a group

Edit options:
  --name <name>                Rename saved host
  --host <host>                SSH hostname or IP
  --user <user>                SSH username, empty clears
  --password <password>        SSH password, empty clears
  --port <port>                SSH port, 0 uses default 22
  --identity-file <path>       SSH private key path, empty clears
  --identity-from <host>       Reuse private key path from saved host
  --group <group>              Set host group, empty clears

Connect options:
  --dry-run                    Print SSH command without connecting
  --print                      Alias for --dry-run

Tunnel examples:
  shbx tunnel add               Add tunnel interactively
  shbx tunnel add db --host prod --local-port 5432 --remote-host 127.0.0.1 --remote-port 5432
  shbx tunnel add socks --host prod --dynamic-port 1080
  shbx tunnel list              Show saved tunnels and running status
  shbx tunnel show db           Show tunnel details and start command
  shbx tunnel start db
  shbx tunnel stop db
  shbx tunnel start db --dry-run

Tunnel behavior:
  start                         Starts SSH in the background and stores its PID
  stop                          Stops the running SSH tunnel
  password prompts              If SSH asks for a password, type it normally

Groups:
  shbx group add work
  shbx add prod --host 192.0.2.10 --group work
  shbx tunnel add db --host prod --local-port 5432 --remote-host 127.0.0.1 --remote-port 5432 --group work
  shbx group list
  shbx group show work
  shbx group rename work prod
  shbx group remove prod --force

Remove options:
  --yes                        Remove without confirmation
  --force                      Alias for --yes

Completion:
  shbx completion zsh           Print zsh completion script
  shbx completion bash          Print bash completion script
  shbx completion fish          Print fish completion script
  shbx completion install       Install completion for the current shell
  shbx completion install --shell all
                                Install completion for bash, zsh, and fish
`)
}

type completionInstallResult struct {
	completionPath   string
	updatedRCFiles   []string
	unchangedRCFiles []string
	notes            []string
}

func completionInstallShells(shell string) ([]string, error) {
	switch shell {
	case "auto":
		detected := detectShellName()
		switch detected {
		case "bash", "zsh", "fish":
			return []string{detected}, nil
		case "":
			return nil, errors.New("cannot detect shell; pass --shell bash, --shell zsh, --shell fish, or --shell all")
		default:
			return nil, fmt.Errorf("unsupported shell %q; pass --shell bash, --shell zsh, --shell fish, or --shell all", detected)
		}
	case "all":
		return []string{"bash", "zsh", "fish"}, nil
	case "bash", "zsh", "fish":
		return []string{shell}, nil
	default:
		return nil, fmt.Errorf("unsupported shell %q; available: auto, all, bash, zsh, fish", shell)
	}
}

func detectShellName() string {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		return ""
	}
	return filepath.Base(shell)
}

func installCompletion(shell string, updateRC bool) (completionInstallResult, error) {
	switch shell {
	case "bash":
		return installBashCompletion(updateRC)
	case "zsh":
		return installZshCompletion(updateRC)
	case "fish":
		return installFishCompletion()
	default:
		return completionInstallResult{}, fmt.Errorf("unsupported shell %q", shell)
	}
}

func installBashCompletion(updateRC bool) (completionInstallResult, error) {
	path, err := userDataPath("shbx", "completions", "bash", "shbx")
	if err != nil {
		return completionInstallResult{}, err
	}
	if err := writeCompletionFile(path, bashCompletionScript); err != nil {
		return completionInstallResult{}, err
	}

	result := completionInstallResult{completionPath: path}
	if updateRC {
		home, err := os.UserHomeDir()
		if err != nil {
			return completionInstallResult{}, err
		}
		snippet := fmt.Sprintf(`
# shbx completion
if [ -r %q ]; then
  source %q
fi
`, path, path)
		rcFiles := []string{filepath.Join(home, ".bashrc")}
		if runtime.GOOS == "darwin" {
			rcFiles = append(rcFiles, filepath.Join(home, ".bash_profile"))
		}
		updated, unchanged, err := ensureStartupSnippet(rcFiles, "# shbx completion", snippet)
		if err != nil {
			return completionInstallResult{}, err
		}
		result.updatedRCFiles = updated
		result.unchangedRCFiles = unchanged
	}
	return result, nil
}

func installZshCompletion(updateRC bool) (completionInstallResult, error) {
	dir, err := userDataPath("shbx", "completions", "zsh")
	if err != nil {
		return completionInstallResult{}, err
	}
	path := filepath.Join(dir, "_shbx")
	if err := writeCompletionFile(path, zshCompletionScript); err != nil {
		return completionInstallResult{}, err
	}

	result := completionInstallResult{completionPath: path}
	if updateRC {
		home, err := os.UserHomeDir()
		if err != nil {
			return completionInstallResult{}, err
		}
		zshrc := filepath.Join(home, ".zshrc")
		snippet := fmt.Sprintf(`
# shbx completion
if [ -d %q ]; then
  fpath=(%q $fpath)
fi
autoload -Uz compinit
compinit
`, dir, dir)
		updated, unchanged, err := ensureStartupSnippet([]string{zshrc}, "# shbx completion", snippet)
		if err != nil {
			return completionInstallResult{}, err
		}
		result.updatedRCFiles = updated
		result.unchangedRCFiles = unchanged
	}
	return result, nil
}

func installFishCompletion() (completionInstallResult, error) {
	path, err := userConfigPath("fish", "completions", "shbx.fish")
	if err != nil {
		return completionInstallResult{}, err
	}
	if err := writeCompletionFile(path, fishCompletionScript); err != nil {
		return completionInstallResult{}, err
	}
	return completionInstallResult{
		completionPath: path,
		notes:          []string{"Fish loads completions from this directory automatically."},
	}, nil
}

func userDataPath(parts ...string) (string, error) {
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(append([]string{base}, parts...)...), nil
}

func userConfigPath(parts ...string) (string, error) {
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(append([]string{base}, parts...)...), nil
}

func writeCompletionFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func ensureStartupSnippet(paths []string, marker, snippet string) ([]string, []string, error) {
	var updated []string
	var unchanged []string
	for _, path := range paths {
		if path == "" {
			continue
		}
		changed, err := appendStartupSnippet(path, marker, snippet)
		if err != nil {
			return nil, nil, err
		}
		if changed {
			updated = append(updated, path)
		} else {
			unchanged = append(unchanged, path)
		}
	}
	return updated, unchanged, nil
}

func appendStartupSnippet(path, marker, snippet string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if strings.Contains(string(data), marker) {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	defer file.Close()

	if len(data) > 0 && data[len(data)-1] != '\n' {
		if _, err := file.WriteString("\n"); err != nil {
			return false, err
		}
	}
	if _, err := file.WriteString(snippet); err != nil {
		return false, err
	}
	return true, nil
}

func completePrefix(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if args[0] == "--" {
		if len(args) < 2 {
			return ""
		}
		return args[1]
	}
	return args[0]
}

func completeCommandNames(prefix string) []string {
	commands := []string{"add", "list", "show", "connect", "tunnel", "group", "edit", "remove", "ui", "completion", "config", "version", "help"}
	return filterSortedPrefix(commands, prefix)
}

func completeHostNames(prefix string) []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(cfg.Hosts))
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	return filterSortedPrefix(names, prefix)
}

func completeTunnelNames(prefix string) []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(cfg.Tunnels))
	for name := range cfg.Tunnels {
		names = append(names, name)
	}
	return filterSortedPrefix(names, prefix)
}

func completeGroupNames(prefix string) []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	return filterSortedPrefix(groupNames(cfg), prefix)
}

func filterSortedPrefix(values []string, prefix string) []string {
	matches := values[:0]
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			matches = append(matches, value)
		}
	}
	sort.Strings(matches)
	return matches
}

const bashCompletionScript = `_shbx_completion()
{
    local cur prev
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    if [[ ${COMP_CWORD} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "$(shbx __complete commands -- "$cur")" -- "$cur") )
        return 0
    fi

    case "${COMP_WORDS[1]}" in
        connect|show|edit|remove)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "$(shbx __complete hosts -- "$cur")" -- "$cur") )
                return 0
            fi
            ;;
        tunnel)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "add list show start stop remove" -- "$cur") )
                return 0
            fi
            if [[ ${COMP_CWORD} -eq 3 && ( "${COMP_WORDS[2]}" = "show" || "${COMP_WORDS[2]}" = "start" || "${COMP_WORDS[2]}" = "stop" || "${COMP_WORDS[2]}" = "remove" ) ]]; then
                COMPREPLY=( $(compgen -W "$(shbx __complete tunnels -- "$cur")" -- "$cur") )
                return 0
            fi
            ;;
        group)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "add list show remove rename" -- "$cur") )
                return 0
            fi
            if [[ ${COMP_CWORD} -eq 3 && ( "${COMP_WORDS[2]}" = "show" || "${COMP_WORDS[2]}" = "remove" || "${COMP_WORDS[2]}" = "rename" ) ]]; then
                COMPREPLY=( $(compgen -W "$(shbx __complete groups -- "$cur")" -- "$cur") )
                return 0
            fi
            ;;
        completion)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") )
                return 0
            fi
            ;;
    esac
}
complete -F _shbx_completion shbx
`

const zshCompletionScript = `#compdef shbx

_shbx() {
  local -a commands hosts tunnels groups tunnel_commands group_commands shells

  if (( CURRENT == 2 )); then
    commands=("${(@f)$(shbx __complete commands -- "$words[CURRENT]")}")
    _describe 'commands' commands
    return
  fi

  case "$words[2]" in
    connect|show|edit|remove)
      if (( CURRENT == 3 )); then
        hosts=("${(@f)$(shbx __complete hosts -- "$words[CURRENT]")}")
        _describe 'saved hosts' hosts
        return
      fi
      ;;
    tunnel)
      if (( CURRENT == 3 )); then
        tunnel_commands=(add list show start stop remove)
        _describe 'tunnel commands' tunnel_commands
        return
      fi
      if (( CURRENT == 4 )) && [[ "$words[3]" == (show|start|stop|remove) ]]; then
        tunnels=("${(@f)$(shbx __complete tunnels -- "$words[CURRENT]")}")
        _describe 'saved tunnels' tunnels
        return
      fi
      ;;
    group)
      if (( CURRENT == 3 )); then
        group_commands=(add list show remove rename)
        _describe 'group commands' group_commands
        return
      fi
      if (( CURRENT == 4 )) && [[ "$words[3]" == (show|remove|rename) ]]; then
        groups=("${(@f)$(shbx __complete groups -- "$words[CURRENT]")}")
        _describe 'saved groups' groups
        return
      fi
      ;;
    completion)
      if (( CURRENT == 3 )); then
        shells=(bash zsh fish)
        _describe 'shells' shells
        return
      fi
      ;;
  esac
}

compdef _shbx shbx
`

const fishCompletionScript = `function __shbx_needs_command
    set -l cmd (commandline -opc)
    test (count $cmd) -eq 1
end

function __shbx_using_command
    set -l cmd (commandline -opc)
    test (count $cmd) -ge 2; and test $cmd[2] = $argv[1]
end

complete -c shbx -n '__shbx_needs_command' -a '(shbx __complete commands -- (commandline -ct))'
complete -c shbx -n '__shbx_using_command connect; or __shbx_using_command show; or __shbx_using_command edit; or __shbx_using_command remove' -a '(shbx __complete hosts -- (commandline -ct))'
complete -c shbx -n '__shbx_using_command tunnel' -a 'add list show start stop remove'
complete -c shbx -n '__shbx_using_command group' -a 'add list show remove rename'
complete -c shbx -n '__shbx_using_command completion' -a 'bash zsh fish'
`

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

func buildTunnelSSHArgs(host config.Host, tunnel config.Tunnel) []string {
	args := buildSSHArgs(host)
	forwardFlag, spec := tunnelForwardSpec(tunnel)
	args = append(args[:len(args)-1], "-N", "-T", forwardFlag, spec, args[len(args)-1])
	return args
}

func buildTunnelStartSSHArgs(name string, host config.Host, tunnel config.Tunnel) ([]string, error) {
	controlPath, err := tunnelstate.ControlPath(name)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(controlPath), 0o700); err != nil {
		return nil, err
	}
	args := buildSSHArgs(host)
	forwardFlag, spec := tunnelForwardSpec(tunnel)
	args = append(args[:len(args)-1], "-M", "-S", controlPath, "-f", "-N", "-T", forwardFlag, spec, args[len(args)-1])
	return args, nil
}

func buildTunnelControlSSHArgs(name string, host config.Host, operation string) ([]string, error) {
	controlPath, err := tunnelstate.ControlPath(name)
	if err != nil {
		return nil, err
	}
	args := buildSSHArgs(host)
	args = append(args[:len(args)-1], "-S", controlPath, "-O", operation, args[len(args)-1])
	return args, nil
}

func tunnelForwardSpec(tunnel config.Tunnel) (string, string) {
	bind := ""
	if tunnel.BindAddress != "" {
		bind = tunnel.BindAddress + ":"
	}

	switch tunnel.Type {
	case "remote":
		return "-R", fmt.Sprintf("%s%d:%s:%d", bind, tunnel.RemotePort, tunnel.RemoteHost, tunnel.LocalPort)
	case "dynamic":
		return "-D", fmt.Sprintf("%s%d", bind, tunnel.LocalPort)
	default:
		return "-L", fmt.Sprintf("%s%d:%s:%d", bind, tunnel.LocalPort, tunnel.RemoteHost, tunnel.RemotePort)
	}
}

func validateTunnel(tunnel config.Tunnel, hosts map[string]config.Host) error {
	if strings.TrimSpace(tunnel.Host) == "" {
		return errors.New("missing saved host; pass --host")
	}
	if _, ok := hosts[tunnel.Host]; !ok {
		return fmt.Errorf("host %q not found", tunnel.Host)
	}

	switch tunnel.Type {
	case "local":
		if err := validatePort("local port", tunnel.LocalPort); err != nil {
			return err
		}
		if strings.TrimSpace(tunnel.RemoteHost) == "" {
			return errors.New("missing remote host; pass --remote-host")
		}
		return validatePort("remote port", tunnel.RemotePort)
	case "remote":
		if err := validatePort("remote port", tunnel.RemotePort); err != nil {
			return err
		}
		if strings.TrimSpace(tunnel.RemoteHost) == "" {
			return errors.New("missing local target host; pass --remote-host")
		}
		return validatePort("local target port", tunnel.LocalPort)
	case "dynamic":
		if strings.TrimSpace(tunnel.RemoteHost) != "" || tunnel.RemotePort != 0 {
			return errors.New("dynamic tunnels cannot use --remote-host or --remote-port")
		}
		return validatePort("dynamic port", tunnel.LocalPort)
	default:
		return fmt.Errorf("invalid tunnel type %q; expected local, remote, or dynamic", tunnel.Type)
	}
}

func validatePort(label string, port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid %s %d", label, port)
	}
	return nil
}

func startTunnelProcess(name string, host config.Host, tunnel config.Tunnel, allowPrompt bool) (int, error) {
	sshArgs, err := buildTunnelStartSSHArgs(name, host, tunnel)
	if err != nil {
		return 0, err
	}
	if host.Password != "" {
		if err := runSSHWithStoredPassword(host.Password, sshArgs); err != nil {
			return 0, err
		}
	} else {
		if err := runSSHAndWait(sshArgs, allowPrompt); err != nil {
			return 0, err
		}
	}

	pid, err := tunnelMasterPID(name, host)
	if err != nil {
		return 0, err
	}
	controlPath, err := tunnelstate.ControlPath(name)
	if err != nil {
		return 0, err
	}
	if err := tunnelstate.Set(name, tunnelstate.NewEntry(pid, tunnelCommandString(host, sshArgs), controlPath)); err != nil {
		return 0, err
	}
	return pid, nil
}

func stopTunnelByName(name string, host config.Host) (bool, tunnelstate.Entry, error) {
	entry, running, err := tunnelstate.Get(name)
	if err != nil {
		return false, tunnelstate.Entry{}, err
	}
	if !running {
		return false, tunnelstate.Entry{}, nil
	}
	controlArgs, err := buildTunnelControlSSHArgs(name, host, "exit")
	if err == nil {
		_ = runSSHAndWait(controlArgs, false)
	}
	_, _, stopErr := tunnelstate.Stop(name)
	if stopErr != nil {
		return false, tunnelstate.Entry{}, stopErr
	}
	return true, entry, nil
}

func runSSHAndWait(sshArgs []string, allowPrompt bool) error {
	cmd := exec.Command(sshBinary(), sshArgs...)
	if allowPrompt {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil
	}
	return cmd.Run()
}

func tunnelMasterPID(name string, host config.Host) (int, error) {
	controlArgs, err := buildTunnelControlSSHArgs(name, host, "check")
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(sshBinary(), controlArgs...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("check tunnel master: %w: %s", err, strings.TrimSpace(output.String()))
	}
	pid, ok := parseMasterPID(output.String())
	if !ok {
		return 0, fmt.Errorf("cannot read tunnel master pid from: %s", strings.TrimSpace(output.String()))
	}
	return pid, nil
}

func parseMasterPID(output string) (int, bool) {
	start := strings.Index(output, "pid=")
	if start == -1 {
		return 0, false
	}
	start += len("pid=")
	end := start
	for end < len(output) && output[end] >= '0' && output[end] <= '9' {
		end++
	}
	if end == start {
		return 0, false
	}
	pid, err := strconv.Atoi(output[start:end])
	if err != nil {
		return 0, false
	}
	return pid, true
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

func formatTunnelForward(tunnel config.Tunnel) string {
	bind := tunnel.BindAddress
	if bind == "" {
		bind = "localhost"
	}

	switch tunnel.Type {
	case "remote":
		return fmt.Sprintf("%s:%d -> %s:%d", bind, tunnel.RemotePort, tunnel.RemoteHost, tunnel.LocalPort)
	case "dynamic":
		return fmt.Sprintf("SOCKS %s:%d", bind, tunnel.LocalPort)
	default:
		return fmt.Sprintf("%s:%d -> %s:%d", bind, tunnel.LocalPort, tunnel.RemoteHost, tunnel.RemotePort)
	}
}

func formatTunnelStatus(state tunnelstate.State, name string) string {
	entry, ok := state.Tunnels[name]
	if !ok || !tunnelstate.IsRunning(entry.PID) {
		return "stopped"
	}
	return "running"
}

func ensureGroup(cfg *config.Config, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if cfg.Groups == nil {
		cfg.Groups = map[string]config.Group{}
	}
	cfg.Groups[name] = config.Group{Name: name}
}

func groupNames(cfg config.Config) []string {
	seen := map[string]bool{}
	for name := range cfg.Groups {
		if strings.TrimSpace(name) != "" {
			seen[name] = true
		}
	}
	for _, host := range cfg.Hosts {
		if strings.TrimSpace(host.Group) != "" {
			seen[host.Group] = true
		}
	}
	for _, tunnel := range cfg.Tunnels {
		if strings.TrimSpace(tunnel.Group) != "" {
			seen[tunnel.Group] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func groupExists(cfg config.Config, name string) bool {
	for _, existing := range groupNames(cfg) {
		if existing == name {
			return true
		}
	}
	return false
}

func groupMembers(cfg config.Config, group string) ([]string, []string) {
	hosts := make([]string, 0)
	for name, host := range cfg.Hosts {
		if host.Group == group {
			hosts = append(hosts, name)
		}
	}
	tunnels := make([]string, 0)
	for name, tunnel := range cfg.Tunnels {
		if tunnel.Group == group {
			tunnels = append(tunnels, name)
		}
	}
	sort.Strings(hosts)
	sort.Strings(tunnels)
	return hosts, tunnels
}

func clearGroup(cfg *config.Config, group string) {
	for name, host := range cfg.Hosts {
		if host.Group == group {
			host.Group = ""
			cfg.Hosts[name] = host
		}
	}
	for name, tunnel := range cfg.Tunnels {
		if tunnel.Group == group {
			tunnel.Group = ""
			cfg.Tunnels[name] = tunnel
		}
	}
}

func renameGroupMembers(cfg *config.Config, oldName, newName string) {
	for name, host := range cfg.Hosts {
		if host.Group == oldName {
			host.Group = newName
			cfg.Hosts[name] = host
		}
	}
	for name, tunnel := range cfg.Tunnels {
		if tunnel.Group == oldName {
			tunnel.Group = newName
			cfg.Tunnels[name] = tunnel
		}
	}
}

func joinOrDefault(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return strings.Join(values, ", ")
}

type identityFileChoice struct {
	path  string
	hosts []string
}

func identityFileFromHost(hosts map[string]config.Host, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("--identity-from requires a saved host name")
	}

	host, ok := hosts[name]
	if !ok {
		return "", fmt.Errorf("host %q not found", name)
	}
	if strings.TrimSpace(host.IdentityFile) == "" {
		return "", fmt.Errorf("host %q has no identity file to reuse", name)
	}
	return host.IdentityFile, nil
}

func identityFileChoices(hosts map[string]config.Host, excludeName string) []identityFileChoice {
	byPath := map[string][]string{}
	for name, host := range hosts {
		if name == excludeName || strings.TrimSpace(host.IdentityFile) == "" {
			continue
		}
		byPath[host.IdentityFile] = append(byPath[host.IdentityFile], name)
	}

	choices := make([]identityFileChoice, 0, len(byPath))
	for path, names := range byPath {
		sort.Strings(names)
		choices = append(choices, identityFileChoice{path: path, hosts: names})
	}
	sort.Slice(choices, func(i, j int) bool {
		return choices[i].path < choices[j].path
	})
	return choices
}

func identityFileChoiceLabel(choice identityFileChoice) string {
	return fmt.Sprintf("%s (from %s)", choice.path, strings.Join(choice.hosts, ", "))
}

func shellCommandString(command string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, command)
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func connectCommandString(host config.Host, sshArgs []string) string {
	if host.Password == "" {
		return shellCommandString("ssh", sshArgs)
	}
	return shellCommandString("ssh", sshArgs) + " # password: set"
}

func tunnelCommandString(host config.Host, sshArgs []string) string {
	return connectCommandString(host, sshArgs)
}

func sshBinary() string {
	if value := strings.TrimSpace(os.Getenv("SHBX_SSH_BIN")); value != "" {
		return value
	}
	return "ssh"
}

func runSSHWithPassword(password string, sshArgs []string) error {
	stdin, ok := terminalFile(os.Stdin)
	if !ok {
		return errors.New("password auto-login requires an interactive terminal")
	}

	cmd := exec.Command(sshBinary(), sshArgs...)
	ptmx, err := startSSHPTY(cmd, stdin)
	if err != nil {
		return err
	}
	defer ptmx.Close()

	resizeSignals := make(chan os.Signal, 1)
	signal.Notify(resizeSignals, syscall.SIGWINCH)
	defer func() {
		signal.Stop(resizeSignals)
		close(resizeSignals)
	}()
	go func() {
		for range resizeSignals {
			_ = pty.InheritSize(stdin, ptmx)
		}
	}()
	resizeSignals <- syscall.SIGWINCH

	oldState, err := term.MakeRaw(int(stdin.Fd()))
	if err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("set terminal raw mode: %w", err)
	}
	defer term.Restore(int(stdin.Fd()), oldState)

	go func() {
		_, _ = io.Copy(ptmx, stdin)
	}()

	outputDone := make(chan error, 1)
	go func() {
		outputDone <- copySSHOutputAndInjectPassword(os.Stdout, ptmx, password)
	}()

	waitErr := cmd.Wait()
	_ = ptmx.Close()

	outputErr := <-outputDone
	if waitErr != nil {
		return waitErr
	}
	if outputErr != nil && !errors.Is(outputErr, os.ErrClosed) {
		return outputErr
	}
	return nil
}

func runSSHWithStoredPassword(password string, sshArgs []string) error {
	cmd := exec.Command(sshBinary(), sshArgs...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}

	outputDone := make(chan error, 1)
	go func() {
		outputDone <- copySSHOutputAndInjectPassword(io.Discard, ptmx, password)
	}()

	waitErr := cmd.Wait()
	_ = ptmx.Close()

	outputErr := <-outputDone
	if waitErr != nil {
		return waitErr
	}
	if outputErr != nil && !errors.Is(outputErr, os.ErrClosed) {
		return outputErr
	}
	return nil
}

func startSSHPTY(cmd *exec.Cmd, stdin *os.File) (*os.File, error) {
	size, err := pty.GetsizeFull(stdin)
	if err == nil {
		return pty.StartWithSize(cmd, size)
	}
	return pty.Start(cmd)
}

func copySSHOutputAndInjectPassword(out io.Writer, ptmx *os.File, password string) error {
	buf := make([]byte, 4096)
	window := ""
	passwordSent := false

	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, writeErr := out.Write(chunk); writeErr != nil {
				return writeErr
			}

			window += string(chunk)
			if len(window) > 512 {
				window = window[len(window)-512:]
			}

			if !passwordSent && looksLikePasswordPrompt(window) {
				if _, writeErr := ptmx.Write([]byte(password + "\n")); writeErr != nil {
					return writeErr
				}
				passwordSent = true
				window = ""
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EIO) {
				return nil
			}
			return err
		}
	}
}

func looksLikePasswordPrompt(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if !strings.HasSuffix(normalized, "password:") {
		return false
	}
	return strings.Contains(normalized, "password:")
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

func promptIdentityFile(reader *bufio.Reader, out io.Writer, hosts map[string]config.Host, currentHostName, current string) (string, error) {
	choices := identityFileChoices(hosts, currentHostName)
	if len(choices) > 0 {
		fmt.Fprintln(out, "Saved identity files:")
		for i, choice := range choices {
			fmt.Fprintf(out, "  %d) %s\n", i+1, identityFileChoiceLabel(choice))
		}
	}

	label := "Identity file"
	if len(choices) > 0 {
		label = "Identity file [number or path]"
	}
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
	if index, err := strconv.Atoi(text); err == nil && index >= 1 && index <= len(choices) {
		return choices[index-1].path, nil
	}
	return text, nil
}

func promptTunnel(reader *bufio.Reader, out io.Writer, hosts map[string]config.Host, groups map[string]config.Group, current config.Tunnel) (config.Tunnel, error) {
	var err error

	current.Host, err = promptSavedHost(reader, out, hosts, current.Host)
	if err != nil {
		return config.Tunnel{}, err
	}

	current.Type, err = promptWithDefault(reader, out, "Type [local|remote|dynamic]", defaultString(current.Type, "local"))
	if err != nil {
		return config.Tunnel{}, err
	}
	current.Type = strings.ToLower(strings.TrimSpace(current.Type))
	if current.Type == "" {
		current.Type = "local"
	}

	current.BindAddress, err = promptWithDefault(reader, out, "Bind address [Enter localhost/default]", current.BindAddress)
	if err != nil {
		return config.Tunnel{}, err
	}

	switch current.Type {
	case "dynamic":
		current.LocalPort, err = promptPort(reader, out, "Dynamic local port", current.LocalPort)
		if err != nil {
			return config.Tunnel{}, err
		}
		current.RemoteHost = ""
		current.RemotePort = 0
	case "remote":
		current.RemotePort, err = promptPort(reader, out, "Remote listen port", current.RemotePort)
		if err != nil {
			return config.Tunnel{}, err
		}
		current.RemoteHost, err = promptWithDefault(reader, out, "Local target host", current.RemoteHost)
		if err != nil {
			return config.Tunnel{}, err
		}
		current.LocalPort, err = promptPort(reader, out, "Local target port", current.LocalPort)
		if err != nil {
			return config.Tunnel{}, err
		}
	default:
		current.LocalPort, err = promptPort(reader, out, "Local port", current.LocalPort)
		if err != nil {
			return config.Tunnel{}, err
		}
		current.RemoteHost, err = promptWithDefault(reader, out, "Remote host", current.RemoteHost)
		if err != nil {
			return config.Tunnel{}, err
		}
		current.RemotePort, err = promptPort(reader, out, "Remote port", current.RemotePort)
		if err != nil {
			return config.Tunnel{}, err
		}
	}
	current.Group, err = promptGroup(reader, out, groups, current.Group)
	if err != nil {
		return config.Tunnel{}, err
	}
	return current, nil
}

func promptGroup(reader *bufio.Reader, out io.Writer, groups map[string]config.Group, current string) (string, error) {
	names := make([]string, 0, len(groups))
	for name := range groups {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		fmt.Fprintln(out, "Saved groups:")
		for i, name := range names {
			fmt.Fprintf(out, "  %d) %s\n", i+1, name)
		}
	}

	label := "Group"
	if len(names) > 0 {
		label = "Group [number or name]"
	}
	text, err := promptWithDefault(reader, out, label, current)
	if err != nil {
		return "", err
	}
	if index, err := strconv.Atoi(text); err == nil && index >= 1 && index <= len(names) {
		return names[index-1], nil
	}
	return strings.TrimSpace(text), nil
}

func promptSavedHost(reader *bufio.Reader, out io.Writer, hosts map[string]config.Host, current string) (string, error) {
	names := make([]string, 0, len(hosts))
	for name := range hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 0 {
		fmt.Fprintln(out, "Saved hosts:")
		for i, name := range names {
			fmt.Fprintf(out, "  %d) %s (%s)\n", i+1, name, formatListTarget(hosts[name]))
		}
	}

	label := "Saved SSH host"
	if len(names) > 0 {
		label = "Saved SSH host [number or name]"
	}
	text, err := promptWithDefault(reader, out, label, current)
	if err != nil {
		return "", err
	}
	if index, err := strconv.Atoi(text); err == nil && index >= 1 && index <= len(names) {
		return names[index-1], nil
	}
	return text, nil
}

func promptPort(reader *bufio.Reader, out io.Writer, label string, current int) (int, error) {
	currentText := ""
	if current != 0 {
		currentText = strconv.Itoa(current)
	}
	text, err := promptWithDefault(reader, out, label, currentText)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", strings.ToLower(label), text)
	}
	return port, nil
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func promptPassword(reader *bufio.Reader, in io.Reader, out io.Writer, label string) (string, error) {
	if file, ok := terminalFile(in); ok {
		fmt.Fprintf(out, "%s: ", label)
		password, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(password)), nil
	}

	return prompt(reader, out, label)
}

func promptPasswordEdit(reader *bufio.Reader, in io.Reader, out io.Writer, current string) (string, error) {
	label := "Password [Enter keep, - clear]"
	if current == "" {
		label = "Password [Enter skip]"
	}
	text, err := promptPassword(reader, in, out, label)
	if err != nil {
		return "", err
	}
	if current != "" && text == "" {
		return current, nil
	}
	if text == "-" {
		return "", nil
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
	_, ok := terminalFile(in)
	if ok {
		return true
	}
	_, isFile := in.(*os.File)
	return !isFile
}

func terminalFile(in io.Reader) (*os.File, bool) {
	file, ok := in.(*os.File)
	if !ok {
		return nil, false
	}

	info, err := file.Stat()
	if err != nil {
		return nil, false
	}

	return file, info.Mode()&os.ModeCharDevice != 0
}
