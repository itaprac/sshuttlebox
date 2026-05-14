package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/tunnelstate"
)

type doctorLevel int

const (
	doctorOK doctorLevel = iota
	doctorWarn
	doctorFail
)

type doctorCheck struct {
	level doctorLevel
	text  string
}

func (a App) runDoctor(args []string) error {
	fix := false
	switch len(args) {
	case 0:
	case 1:
		if args[0] != "--fix" {
			return errors.New("usage: shbx doctor [--fix]")
		}
		fix = true
	default:
		return errors.New("usage: shbx doctor [--fix]")
	}

	if fix {
		return a.runDoctorFix()
	}
	fmt.Fprint(a.out, buildDoctorReport())
	return nil
}

func (a App) runDoctorFix() error {
	fixes, err := applyDoctorFixes()
	if err != nil {
		return err
	}
	if len(fixes) == 0 {
		fmt.Fprintln(a.out, "No fixes applied")
		return nil
	}
	for _, fix := range fixes {
		fmt.Fprintf(a.out, "Fixed: %s\n", fix)
	}
	return nil
}

func buildDoctorReport() string {
	checks := collectDoctorChecks()
	warnings := 0
	failures := 0
	for _, check := range checks {
		switch check.level {
		case doctorWarn:
			warnings++
		case doctorFail:
			failures++
		}
	}

	var b strings.Builder
	b.WriteString("sshuttlebox doctor\n\n")
	for _, check := range checks {
		fmt.Fprintf(&b, "[%s] %s\n", doctorLevelLabel(check.level), check.text)
	}
	b.WriteString("\n")
	switch {
	case failures > 0:
		fmt.Fprintf(&b, "Result: %d failed, %d warning(s)\n", failures, warnings)
	case warnings > 0:
		fmt.Fprintf(&b, "Result: ok with %d warning(s)\n", warnings)
	default:
		b.WriteString("Result: ok\n")
	}
	return b.String()
}

func collectDoctorChecks() []doctorCheck {
	var checks []doctorCheck

	path, err := config.Path()
	if err != nil {
		return append(checks, doctorCheck{level: doctorFail, text: "resolve config path: " + err.Error()})
	}

	exists, err := config.Exists()
	if err != nil {
		checks = append(checks, doctorCheck{level: doctorFail, text: "check config file: " + err.Error()})
	} else if exists {
		checks = append(checks, doctorCheck{level: doctorOK, text: "config file: " + path})
		checks = append(checks, configPermissionChecks(path)...)
	} else {
		checks = append(checks, doctorCheck{level: doctorWarn, text: "config file missing: run shbx config init"})
	}

	checks = append(checks, sshBinaryCheck())

	cfg, err := config.Load()
	if err != nil {
		if exists {
			checks = append(checks, doctorCheck{level: doctorFail, text: "load config: " + err.Error()})
		}
		return checks
	}

	checks = append(checks, doctorCheck{level: doctorOK, text: fmt.Sprintf("saved hosts: %d", len(cfg.Hosts))})
	checks = append(checks, doctorCheck{level: doctorOK, text: fmt.Sprintf("saved tunnels: %d", len(cfg.Tunnels))})
	checks = append(checks, hostDoctorChecks(cfg.Hosts)...)
	checks = append(checks, tunnelDoctorChecks(cfg)...)
	checks = append(checks, passwordDoctorChecks(cfg.Hosts)...)
	checks = append(checks, tunnelStateDoctorChecks()...)

	return checks
}

func doctorLevelLabel(level doctorLevel) string {
	switch level {
	case doctorWarn:
		return "WARN"
	case doctorFail:
		return "FAIL"
	default:
		return "OK"
	}
}

func configPermissionChecks(path string) []doctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		return []doctorCheck{{level: doctorFail, text: "stat config file: " + err.Error()}}
	}
	if info.IsDir() {
		return []doctorCheck{{level: doctorFail, text: "config path is a directory"}}
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	if info.Mode().Perm()&0o077 != 0 {
		return []doctorCheck{{level: doctorWarn, text: fmt.Sprintf("config permissions are %04o; prefer 0600", info.Mode().Perm())}}
	}
	return []doctorCheck{{level: doctorOK, text: fmt.Sprintf("config permissions: %04o", info.Mode().Perm())}}
}

func sshBinaryCheck() doctorCheck {
	bin := sshBinary()
	path, err := exec.LookPath(bin)
	if err != nil {
		return doctorCheck{level: doctorFail, text: fmt.Sprintf("ssh binary %q not found", bin)}
	}
	return doctorCheck{level: doctorOK, text: "ssh binary: " + path}
}

func hostDoctorChecks(hosts map[string]config.Host) []doctorCheck {
	names := make([]string, 0, len(hosts))
	for name := range hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	var checks []doctorCheck
	for _, name := range names {
		host := hosts[name]
		if strings.TrimSpace(name) == "" {
			checks = append(checks, doctorCheck{level: doctorFail, text: "host with empty name"})
		}
		if strings.TrimSpace(host.Host) == "" {
			checks = append(checks, doctorCheck{level: doctorFail, text: fmt.Sprintf("host %q has empty SSH host", name)})
		}
		if host.Port < 0 || host.Port > 65535 {
			checks = append(checks, doctorCheck{level: doctorFail, text: fmt.Sprintf("host %q has invalid port %d", name, host.Port)})
		}
		if warning := identityFileWarning(host.IdentityFile); warning != "" {
			checks = append(checks, doctorCheck{level: doctorWarn, text: fmt.Sprintf("host %q: %s", name, warning)})
		}
	}
	return checks
}

func tunnelDoctorChecks(cfg config.Config) []doctorCheck {
	names := make([]string, 0, len(cfg.Tunnels))
	for name := range cfg.Tunnels {
		names = append(names, name)
	}
	sort.Strings(names)

	var checks []doctorCheck
	localPorts := map[string]string{}
	for _, name := range names {
		tunnel := cfg.Tunnels[name]
		if strings.TrimSpace(name) == "" {
			checks = append(checks, doctorCheck{level: doctorFail, text: "tunnel with empty name"})
		}
		if err := validateTunnel(tunnel, cfg.Hosts); err != nil {
			checks = append(checks, doctorCheck{level: doctorFail, text: fmt.Sprintf("tunnel %q: %s", name, err)})
			continue
		}
		if tunnel.Type == "local" || tunnel.Type == "dynamic" {
			bind := tunnel.BindAddress
			if bind == "" {
				bind = "localhost"
			}
			key := fmt.Sprintf("%s:%d", bind, tunnel.LocalPort)
			if existing := localPorts[key]; existing != "" {
				checks = append(checks, doctorCheck{level: doctorWarn, text: fmt.Sprintf("tunnels %q and %q use local port %s", existing, name, key)})
			} else {
				localPorts[key] = name
			}
		}
	}
	return checks
}

func passwordDoctorChecks(hosts map[string]config.Host) []doctorCheck {
	count := 0
	for _, host := range hosts {
		if host.Password != "" {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return []doctorCheck{{level: doctorWarn, text: fmt.Sprintf("saved passwords: %d host(s); prefer SSH keys when possible", count)}}
}

func tunnelStateDoctorChecks() []doctorCheck {
	state, err := tunnelstate.Load()
	if err != nil {
		return []doctorCheck{{level: doctorFail, text: "check tunnel state: " + err.Error()}}
	}
	running := 0
	stale := 0
	for _, entry := range state.Tunnels {
		if tunnelstate.EntryRunning(entry) {
			running++
		} else {
			stale++
		}
	}
	checks := []doctorCheck{{level: doctorOK, text: fmt.Sprintf("running tunnels: %d", running)}}
	if stale > 0 {
		checks = append(checks, doctorCheck{level: doctorWarn, text: fmt.Sprintf("stale tunnel state: %d; run shbx doctor --fix", stale)})
	}
	return checks
}

func applyDoctorFixes() ([]string, error) {
	var fixes []string

	path, err := config.Path()
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	exists, err := config.Exists()
	if err != nil {
		return nil, fmt.Errorf("check config file: %w", err)
	}
	if !exists {
		createdPath, created, err := config.Init()
		if err != nil {
			return nil, fmt.Errorf("create config: %w", err)
		}
		if created {
			fixes = append(fixes, "created config: "+createdPath)
		}
	}

	configModeFixed, err := fixConfigPermissions(path)
	if err != nil {
		return nil, err
	}
	if configModeFixed {
		fixes = append(fixes, "config permissions: 0600")
	}

	dirFixes, err := fixTunnelStateDirs()
	if err != nil {
		return nil, err
	}
	fixes = append(fixes, dirFixes...)

	pruned, err := pruneDoctorTunnelState()
	if err != nil {
		return nil, err
	}
	if pruned > 0 {
		fixes = append(fixes, fmt.Sprintf("pruned stale tunnel state: %d", pruned))
	}

	return fixes, nil
}

func fixConfigPermissions(path string) (bool, error) {
	if runtime.GOOS == "windows" {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat config file: %w", err)
	}
	if info.IsDir() {
		return false, errors.New("config path is a directory")
	}
	if info.Mode().Perm() == 0o600 {
		return false, nil
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return false, fmt.Errorf("chmod config file: %w", err)
	}
	return true, nil
}

func fixTunnelStateDirs() ([]string, error) {
	var fixes []string

	statePath, err := tunnelstate.Path()
	if err != nil {
		return nil, fmt.Errorf("resolve tunnel state path: %w", err)
	}
	created, err := ensureDoctorDir(filepath.Dir(statePath), "tunnel state dir")
	if err != nil {
		return nil, err
	}
	if created != "" {
		fixes = append(fixes, created)
	}

	controlPath, err := tunnelstate.ControlPath("doctor")
	if err != nil {
		return nil, fmt.Errorf("resolve tunnel control path: %w", err)
	}
	created, err = ensureDoctorDir(filepath.Dir(controlPath), "tunnel control dir")
	if err != nil {
		return nil, err
	}
	if created != "" {
		fixes = append(fixes, created)
	}
	return fixes, nil
}

func ensureDoctorDir(path, label string) (string, error) {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%s path is not a directory: %s", label, path)
		}
		return "", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat %s: %w", label, err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", label, err)
	}
	return fmt.Sprintf("created %s: %s", label, path), nil
}

func pruneDoctorTunnelState() (int, error) {
	state, err := tunnelstate.Load()
	if err != nil {
		return 0, fmt.Errorf("load tunnel state: %w", err)
	}
	pruned := 0
	for name, entry := range state.Tunnels {
		if !tunnelstate.EntryRunning(entry) {
			delete(state.Tunnels, name)
			pruned++
		}
	}
	if pruned == 0 {
		return 0, nil
	}
	if err := tunnelstate.Save(state); err != nil {
		return 0, fmt.Errorf("save tunnel state: %w", err)
	}
	return pruned, nil
}
