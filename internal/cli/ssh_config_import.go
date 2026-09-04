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

	sshConfigPath, err := filepath.Abs(expandHomePath(sshConfigPath))
	if err != nil {
		return err
	}
	file, err := os.Open(sshConfigPath)
	if err != nil {
		return err
	}
	defer file.Close()

	aliases, warnings, err := collectSSHConfigAliases(sshConfigPath)
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

	hosts := make([]sshConfigHost, 0, len(aliases))
	for _, alias := range aliases {
		hosts = append(hosts, sshConfigHost{Name: alias, Host: config.Host{Host: alias, SSHConfigFile: sshConfigPath}})
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
		fmt.Fprintf(a.out, "  %s -> OpenSSH alias %s\n", host.Name, host.Host.Host)
	}
	fmt.Fprintf(a.out, "OpenSSH settings remain in %s; keep this file available.\n", sshConfigPath)
	for _, warning := range warnings {
		fmt.Fprintf(a.out, "Warning: %s\n", warning)
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
		fields, err := splitSSHConfigFields(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
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
		if key == "match" {
			flush()
			inHost = false
			aliases = nil
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

// splitSSHConfigFields preserves quoted whitespace and OpenSSH key=value syntax.
func splitSSHConfigFields(line string) ([]string, error) {
	var fields []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	flush := func() {
		if started {
			fields = append(fields, word.String())
			word.Reset()
			started = false
		}
	}
	for _, r := range strings.TrimSpace(line) {
		if escaped {
			word.WriteRune(r)
			started = true
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		switch r {
		case '"', '\'':
			quote = r
			started = true
		case '#':
			flush()
			return fields, nil
		case ' ', '\t', '\r':
			flush()
		case '=':
			if len(fields) == 0 {
				flush()
			} else if len(fields) == 1 && !started {
				continue
			} else {
				word.WriteRune(r)
				started = true
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New("unterminated quote or escape in SSH config")
	}
	flush()
	return fields, nil
}

// collectSSHConfigAliases discovers literal aliases; OpenSSH evaluates all settings at connection time.
func collectSSHConfigAliases(path string) ([]string, []string, error) {
	aliases := map[string]bool{}
	visited := map[string]bool{}
	warningSet := map[string]bool{}
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 32 {
			return errors.New("SSH config Include depth exceeds 32")
		}
		canonical, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if visited[canonical] {
			return nil
		}
		visited[canonical] = true
		file, err := os.Open(canonical)
		if err != nil {
			return err
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for line := 1; scanner.Scan(); line++ {
			fields, err := splitSSHConfigFields(scanner.Text())
			if err != nil {
				return fmt.Errorf("%s:%d: %w", canonical, line, err)
			}
			if len(fields) < 2 {
				continue
			}
			switch strings.ToLower(fields[0]) {
			case "host":
				for _, alias := range fields[1:] {
					if !isSSHConfigPattern(alias) {
						aliases[alias] = true
					} else {
						warningSet["Host patterns are preserved by OpenSSH but cannot be listed as saved names."] = true
					}
				}
			case "match":
				warningSet["Aliases found under conditional blocks are checked by OpenSSH when you connect."] = true
			case "include":
				for _, pattern := range fields[1:] {
					if strings.ContainsAny(pattern, "%$") {
						warningSet["Include paths with tokens or environment variables are resolved only when you connect."] = true
						continue
					}
					pattern = expandHomePath(pattern)
					if !filepath.IsAbs(pattern) {
						home, err := os.UserHomeDir()
						if err != nil {
							return err
						}
						pattern = filepath.Join(home, ".ssh", pattern)
					}
					paths, err := filepath.Glob(pattern)
					if err != nil {
						return err
					}
					for _, included := range paths {
						if err := visit(included, depth+1); err != nil {
							return err
						}
					}
				}
			}
		}
		return scanner.Err()
	}
	if err := visit(path, 0); err != nil {
		return nil, nil, err
	}
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	warnings := make([]string, 0, len(warningSet))
	for warning := range warningSet {
		warnings = append(warnings, warning)
	}
	sort.Strings(warnings)
	return names, warnings, nil
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
