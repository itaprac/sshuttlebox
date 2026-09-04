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
	"github.com/itaprac/sshuttlebox/internal/store"
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

	for name, host := range cfg.Hosts {
		if host.SSHConfigFile != "" && name != host.Host {
			return fmt.Errorf("host %q refers to OpenSSH alias %q; export it under its original alias to preserve connection settings", name, host.Host)
		}
	}
	content := renderSSHConfig(cfg.Hosts)
	if output == "" {
		fmt.Fprint(a.out, content)
		return nil
	}

	if err := protectSSHConfigSources(output, cfg.Hosts); err != nil {
		return err
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
	sources := map[string][]string{}
	blocks := 0
	for _, name := range names {
		host := hosts[name]
		if host.SSHConfigFile != "" {
			sources[host.SSHConfigFile] = append(sources[host.SSHConfigFile], name)
			if host.User == "" && host.Port == 0 && host.IdentityFile == "" {
				continue
			}
		}
		if blocks > 0 {
			buf.WriteByte('\n')
		}
		blocks++
		fmt.Fprintf(&buf, "Host %s\n", sshConfigValue(name))
		if host.SSHConfigFile == "" {
			fmt.Fprintf(&buf, "  HostName %s\n", sshConfigValue(host.Host))
		}
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
	if len(sources) > 0 {
		paths := make([]string, 0, len(sources))
		for path := range sources {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		if blocks > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString("# Imported aliases retain their original OpenSSH settings.\n")
		for _, path := range paths {
			aliases := make([]string, 0, len(sources[path]))
			for _, name := range sources[path] {
				aliases = append(aliases, sshConfigValue(name))
			}
			fmt.Fprintf(&buf, "Host %s\n  Include %s\n", strings.Join(aliases, " "), sshConfigValue(path))
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
	return store.WithLock(path+".lock", func() error {
		if _, err := os.Lstat(path); err == nil && !force {
			return fmt.Errorf("output file already exists: %s; pass --force to overwrite", path)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return store.AtomicWrite(path, data)
	})
}

func protectSSHConfigSources(output string, hosts map[string]config.Host) error {
	absolute, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	outputInfo, statErr := os.Stat(absolute)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	for _, host := range hosts {
		if host.SSHConfigFile == "" {
			continue
		}
		source, err := filepath.Abs(host.SSHConfigFile)
		if err != nil {
			return err
		}
		same := absolute == source
		if outputInfo != nil {
			if info, err := os.Stat(source); err == nil && os.SameFile(outputInfo, info) {
				same = true
			}
		}
		if same {
			return fmt.Errorf("export output is a linked OpenSSH source: %s; choose another output file", output)
		}
	}
	return nil
}
