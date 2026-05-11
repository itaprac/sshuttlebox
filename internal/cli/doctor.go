package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	if len(args) != 0 {
		return errors.New("usage: shbx doctor")
	}

	fmt.Fprint(a.out, buildDoctorReport())
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
	state, err := tunnelstate.Prune()
	if err != nil {
		return []doctorCheck{{level: doctorFail, text: "check tunnel state: " + err.Error()}}
	}
	running := 0
	for _, entry := range state.Tunnels {
		if tunnelstate.IsRunning(entry.PID) {
			running++
		}
	}
	if running == 0 {
		return []doctorCheck{{level: doctorOK, text: "running tunnels: 0"}}
	}
	return []doctorCheck{{level: doctorOK, text: fmt.Sprintf("running tunnels: %d", running)}}
}
