package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/itaprac/sshuttlebox/internal/config"
)

type sshConfigHost struct {
	Name string
	Host config.Host
}

func (a App) runImport(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shbx import ssh-config [--path path] [--dry-run]")
	}

	switch args[0] {
	case "ssh-config":
		return a.runImportSSHConfig(args[1:])
	default:
		return fmt.Errorf("unknown import command %q; available: ssh-config", args[0])
	}
}

func (a App) runImportSSHConfig(args []string) error {
	fs := flag.NewFlagSet("import ssh-config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	pathFlag := fs.String("path", "", "Path to OpenSSH config")
	dryRunFlag := fs.Bool("dry-run", false, "Preview import without saving")
	if err := fs.Parse(args); err != nil {
		return errors.New("usage: shbx import ssh-config [--path path] [--dry-run]")
	}
	if fs.NArg() != 0 {
		return errors.New("usage: shbx import ssh-config [--path path] [--dry-run]")
	}

	sshConfigPath := strings.TrimSpace(*pathFlag)
	if sshConfigPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve user home dir: %w", err)
		}
		sshConfigPath = filepath.Join(home, ".ssh", "config")
	}

	file, err := os.Open(sshConfigPath)
	if err != nil {
		return err
	}
	defer file.Close()

	hosts, err := parseSSHConfigHosts(file)
	if err != nil {
		return err
	}

	cfg := config.Default()
	path := ""
	if *dryRunFlag {
		exists, err := config.Exists()
		if err != nil {
			return err
		}
		if exists {
			cfg, err = config.Load()
			if err != nil {
				return err
			}
		}
	} else {
		var err error
		path, _, err = config.Init()
		if err != nil {
			return err
		}
		cfg, err = config.Load()
		if err != nil {
			return err
		}
	}

	var imported []sshConfigHost
	var skipped []string
	for _, host := range hosts {
		if _, exists := cfg.Hosts[host.Name]; exists {
			skipped = append(skipped, host.Name)
			continue
		}
		imported = append(imported, host)
	}

	sort.Slice(imported, func(i, j int) bool { return imported[i].Name < imported[j].Name })
	sort.Strings(skipped)

	if *dryRunFlag {
		fmt.Fprintf(a.out, "Would import %d host(s)\n", len(imported))
	} else {
		for _, host := range imported {
			cfg.Hosts[host.Name] = host.Host
		}
		if err := config.Save(path, cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Imported %d host(s)\n", len(imported))
	}
	for _, host := range imported {
		fmt.Fprintf(a.out, "  %s -> %s\n", host.Name, formatListTarget(host.Host))
	}
	for _, name := range skipped {
		fmt.Fprintf(a.out, "Skipped existing host %q\n", name)
	}
	return nil
}

func parseSSHConfigHosts(r io.Reader) ([]sshConfigHost, error) {
	scanner := bufio.NewScanner(r)

	var parsed []sshConfigHost
	var aliases []string
	current := config.Host{}
	inHost := false
	lineNo := 0

	flush := func() {
		if !inHost {
			return
		}
		for _, alias := range aliases {
			if isSSHConfigPattern(alias) {
				continue
			}
			host := current
			if host.Host == "" {
				host.Host = alias
			}
			parsed = append(parsed, sshConfigHost{Name: alias, Host: host})
		}
	}

	for scanner.Scan() {
		lineNo++
		fields := sshConfigFields(scanner.Text())
		if len(fields) == 0 {
			continue
		}

		key := strings.ToLower(fields[0])
		values := fields[1:]
		if key == "host" {
			flush()
			inHost = true
			aliases = values
			current = config.Host{}
			continue
		}
		if !inHost || len(values) == 0 {
			continue
		}

		value := values[0]
		switch key {
		case "hostname":
			current.Host = value
		case "user":
			current.User = value
		case "port":
			port, err := strconv.Atoi(value)
			if err != nil || port < 0 || port > 65535 {
				return nil, fmt.Errorf("invalid port on line %d", lineNo)
			}
			current.Port = port
		case "identityfile":
			if current.IdentityFile == "" {
				current.IdentityFile = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	flush()
	return parsed, nil
}

func sshConfigFields(line string) []string {
	line = strings.TrimSpace(stripSSHConfigComment(line))
	if line == "" {
		return nil
	}

	fields := strings.Fields(line)
	for i, field := range fields {
		fields[i] = strings.Trim(field, `"'`)
	}
	return fields
}

func stripSSHConfigComment(line string) string {
	inSingle := false
	inDouble := false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func isSSHConfigPattern(alias string) bool {
	return alias == "" || strings.ContainsAny(alias, "*?!")
}
