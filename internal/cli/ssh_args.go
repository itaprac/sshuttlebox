package cli

import (
	"fmt"
	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/tunnelstate"
	"strconv"
	"strings"
)

func buildSSHArgs(host config.Host) []string {
	port := host.Port
	if port == 0 && host.SSHConfigFile == "" {
		port = defaultSSHPort
	}

	args := []string{}
	if host.SSHConfigFile != "" {
		args = append(args, "-F", host.SSHConfigFile)
	}
	if port != 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	if host.IdentityFile != "" {
		args = append(args, "-i", host.IdentityFile)
	}

	return append(args, formatSSHTarget(host))
}

func buildSFTPArgs(host config.Host, remotePath string) []string {
	port := host.Port
	if port == 0 && host.SSHConfigFile == "" {
		port = defaultSSHPort
	}

	args := []string{}
	if host.SSHConfigFile != "" {
		args = append(args, "-F", host.SSHConfigFile)
	}
	if port != 0 {
		args = append(args, "-P", strconv.Itoa(port))
	}
	if host.IdentityFile != "" {
		args = append(args, "-i", host.IdentityFile)
	}

	return append(args, formatSFTPTarget(host, remotePath))
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
	args := buildSSHArgs(host)
	forwardFlag, spec := tunnelForwardSpec(tunnel)
	args = append(args[:len(args)-1], "-o", "ConnectTimeout=15", "-o", "ConnectionAttempts=1", "-o", "ExitOnForwardFailure=yes", "-M", "-S", controlPath, "-f", "-N", "-T", forwardFlag, spec, args[len(args)-1])
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
		bind = bracketIPv6(tunnel.BindAddress) + ":"
	}

	switch tunnel.Type {
	case "remote":
		return "-R", fmt.Sprintf("%s%d:%s:%d", bind, tunnel.RemotePort, bracketIPv6(tunnel.RemoteHost), tunnel.LocalPort)
	case "dynamic":
		return "-D", fmt.Sprintf("%s%d", bind, tunnel.LocalPort)
	default:
		return "-L", fmt.Sprintf("%s%d:%s:%d", bind, tunnel.LocalPort, bracketIPv6(tunnel.RemoteHost), tunnel.RemotePort)
	}
}

func formatSSHTarget(host config.Host) string {
	target := host.Host
	if host.User != "" {
		target = host.User + "@" + host.Host
	}
	return target
}

func formatSFTPTarget(host config.Host, remotePath string) string {
	host.Host = bracketIPv6(host.Host)
	target := formatSSHTarget(host)
	if remotePath != "" {
		target += ":" + remotePath
	}
	return target
}

func bracketIPv6(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}
