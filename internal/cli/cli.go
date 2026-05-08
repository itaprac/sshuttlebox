package cli

import (
	"bufio"
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
	"golang.org/x/term"
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
	case "completion":
		return a.runCompletion(args[1:])
	case "__complete":
		return a.runComplete(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run: shbx help", args[0])
	}
}

func (a App) runAdd(args []string) error {
	const usage = "usage: shbx add [name] [--host host] [--user user] [--password password] [--port port] [--identity-file path]"

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
	password := *passwordFlag
	identityFile := strings.TrimSpace(*identityFileFlag)
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
			identityFile, err = prompt(reader, a.out, "Identity file")
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

		identityFile, err = promptWithDefault(reader, a.out, "Identity file", identityFile)
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

	if host.Password != "" {
		return runSSHWithPassword(host.Password, sshArgs)
	}

	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (a App) runEdit(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: shbx edit <name> [--name new-name] [--host host] [--user user] [--password password] [--port port] [--identity-file path]")
	}

	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	newNameFlag := fs.String("name", "", "New saved host name")
	hostFlag := fs.String("host", "", "SSH host")
	userFlag := fs.String("user", "", "SSH user")
	passwordFlag := fs.String("password", "", "SSH password")
	portFlag := fs.Int("port", 0, "SSH port")
	identityFileFlag := fs.String("identity-file", "", "SSH identity file")

	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("usage: shbx edit <name> [--name new-name] [--host host] [--user user] [--password password] [--port port] [--identity-file path]")
	}

	if fs.NArg() != 0 {
		return fmt.Errorf("usage: shbx edit <name> [--name new-name] [--host host] [--user user] [--password password] [--port port] [--identity-file path]")
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
			return errors.New("missing edit options; pass at least one of --name, --host, --user, --password, --port, --identity-file")
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

		identityFileText, err := promptWithDefault(reader, a.out, "Identity file", host.IdentityFile)
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
		if flagWasSet(fs, "password") {
			host.Password = *passwordFlag
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
	}
	return nil
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

Edit options:
  --name <name>                Rename saved host
  --host <host>                SSH hostname or IP
  --user <user>                SSH username, empty clears
  --password <password>        SSH password, empty clears
  --port <port>                SSH port, 0 uses default 22
  --identity-file <path>       SSH private key path, empty clears

Connect options:
  --dry-run                    Print SSH command without connecting
  --print                      Alias for --dry-run

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
	commands := []string{"add", "list", "show", "connect", "edit", "remove", "completion", "config", "version", "help"}
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
  local -a commands hosts shells

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

func connectCommandString(host config.Host, sshArgs []string) string {
	if host.Password == "" {
		return shellCommandString("ssh", sshArgs)
	}
	return shellCommandString("ssh", sshArgs) + " # password: set"
}

func runSSHWithPassword(password string, sshArgs []string) error {
	stdin, ok := terminalFile(os.Stdin)
	if !ok {
		return errors.New("password auto-login requires an interactive terminal")
	}

	cmd := exec.Command("ssh", sshArgs...)
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
