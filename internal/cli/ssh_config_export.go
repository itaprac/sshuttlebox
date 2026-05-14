package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/itaprac/sshuttlebox/internal/config"
)

func (a App) runExport(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shbx export ssh-config [--dry-run] [--output path] [--force]")
	}

	switch args[0] {
	case "ssh-config":
		return a.runExportSSHConfig(args[1:])
	default:
		return fmt.Errorf("unknown export command %q; available: ssh-config", args[0])
	}
}

func (a App) runExportSSHConfig(args []string) error {
	const usage = "usage: shbx export ssh-config [--dry-run] [--output path] [--force]"

	fs := flag.NewFlagSet("export ssh-config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	dryRunFlag := fs.Bool("dry-run", false, "Print OpenSSH config to stdout")
	outputFlag := fs.String("output", "", "Write OpenSSH config to a file")
	forceFlag := fs.Bool("force", false, "Overwrite output file")

	if err := fs.Parse(args); err != nil {
		return errors.New(usage)
	}
	if fs.NArg() != 0 {
		return errors.New(usage)
	}

	output := strings.TrimSpace(*outputFlag)
	if *dryRunFlag && output != "" {
		return errors.New("--dry-run and --output cannot be used together")
	}
	if *forceFlag && output == "" {
		return errors.New("--force requires --output")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	content := renderSSHConfig(cfg.Hosts)
	if output == "" {
		fmt.Fprint(a.out, content)
		return nil
	}

	if err := writeExportFile(output, []byte(content), *forceFlag); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Wrote SSH config: %s\n", output)
	return nil
}

func renderSSHConfig(hosts map[string]config.Host) string {
	names := make([]string, 0, len(hosts))
	for name := range hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	for i, name := range names {
		host := hosts[name]
		if i > 0 {
			buf.WriteByte('\n')
		}
		fmt.Fprintf(&buf, "Host %s\n", sshConfigValue(name))
		fmt.Fprintf(&buf, "  HostName %s\n", sshConfigValue(host.Host))
		if host.User != "" {
			fmt.Fprintf(&buf, "  User %s\n", sshConfigValue(host.User))
		}
		if host.Port != 0 {
			fmt.Fprintf(&buf, "  Port %d\n", host.Port)
		}
		if host.IdentityFile != "" {
			fmt.Fprintf(&buf, "  IdentityFile %s\n", sshConfigValue(host.IdentityFile))
		}
	}
	return buf.String()
}

func sshConfigValue(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\r\n#\"\\") {
		return value
	}

	var buf strings.Builder
	buf.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\', '"':
			buf.WriteByte('\\')
			buf.WriteRune(r)
		case '\n', '\r', '\t':
			buf.WriteByte(' ')
		default:
			buf.WriteRune(r)
		}
	}
	buf.WriteByte('"')
	return buf.String()
}

func writeExportFile(path string, data []byte, force bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	flag := os.O_WRONLY | os.O_CREATE
	if force {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_EXCL
	}

	file, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("output file already exists: %s; pass --force to overwrite", path)
		}
		return err
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	return nil
}
