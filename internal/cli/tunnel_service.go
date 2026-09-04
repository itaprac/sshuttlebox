package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/tunnelstate"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func startTunnelProcess(name string, host config.Host, tunnel config.Tunnel, allowPrompt bool) (pid int, err error) {
	err = tunnelstate.WithTunnelLock(name, func() error {
		current, err := config.Load()
		if err != nil {
			return err
		}
		if currentTunnel, ok := current.Tunnels[name]; !ok || currentTunnel != tunnel || current.Hosts[tunnel.Host] != host {
			return fmt.Errorf("tunnel %q changed before startup; reload and try again", name)
		}
		entry, running, err := tunnelstate.Get(name)
		if err != nil {
			return err
		}
		if running {
			pid = entry.PID
			return nil
		}
		controlPath, err := tunnelstate.ControlPath(name)
		if err != nil {
			return err
		}
		candidate := tunnelstate.NewEntry(0, connectCommandString(host, buildSSHArgs(host)), controlPath)
		if active, err := tunnelstate.Check(context.Background(), candidate); err != nil {
			return err
		} else if active {
			return fmt.Errorf("untracked SSH master exists at %s; stop it before starting tunnel %q", controlPath, name)
		}
		pid, err = startTunnelProcessLocked(name, host, tunnel, allowPrompt)
		return err
	})
	return pid, err
}

func startTunnelProcessLocked(name string, host config.Host, tunnel config.Tunnel, allowPrompt bool) (pid int, err error) {
	defer func() {
		if err != nil {
			if cleanupErr := cleanupFailedTunnelStartup(name, host); cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()

	controlPath, err := tunnelstate.ControlPath(name)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(controlPath), 0700); err != nil {
		return 0, err
	}
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

	pid, err = tunnelMasterPID(name, host)
	if err != nil {
		return 0, err
	}
	if err := tunnelstate.Set(name, tunnelstate.NewEntry(pid, tunnelCommandString(host, sshArgs), controlPath)); err != nil {
		return 0, err
	}
	return pid, nil
}

func cleanupFailedTunnelStartup(name string, host config.Host) error {
	controlPath, err := tunnelstate.ControlPath(name)
	if err != nil {
		return err
	}
	entry := tunnelstate.NewEntry(0, connectCommandString(host, buildSSHArgs(host)), controlPath)
	running, err := tunnelstate.Check(context.Background(), entry)
	if err != nil {
		return fmt.Errorf("cannot verify failed startup cleanup at %s: %w", controlPath, err)
	}
	if !running {
		return nil
	}
	args, err := buildTunnelControlSSHArgs(name, host, "exit")
	if err == nil {
		err = runSSHAndWait(args, false)
	}
	if err != nil {
		return fmt.Errorf("startup cleanup failed; SSH master may still run at %s; recovery command: %s: %w", controlPath, shellCommandString("ssh", args), err)
	}
	running, err = tunnelstate.Check(context.Background(), entry)
	if err != nil {
		return fmt.Errorf("cannot verify cleanup at %s: %w", controlPath, err)
	}
	if running {
		return fmt.Errorf("SSH master still runs after cleanup at %s; recovery command: %s", controlPath, shellCommandString("ssh", args))
	}
	return nil
}

func stopTunnelByName(name string, host config.Host) (bool, tunnelstate.Entry, error) {
	entry, stopped, err := tunnelstate.Stop(name)
	return stopped, entry, err
}

func runSSHAndWait(sshArgs []string, allowPrompt bool) error {
	timeout := 30 * time.Second
	if allowPrompt {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if !allowPrompt {
		sshArgs = append([]string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes"}, sshArgs...)
	}
	cmd := exec.CommandContext(ctx, sshBinary(), sshArgs...)
	cmd.WaitDelay = time.Second
	var stderr boundedOutput
	if allowPrompt {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	} else {
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = &stderr
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("SSH operation timed out: %w", ctx.Err())
		}
		return sshRunError(err, stderr.String())
	}
	return nil
}

func tunnelMasterPID(name string, host config.Host) (int, error) {
	controlArgs, err := buildTunnelControlSSHArgs(name, host, "check")
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sshBinary(), controlArgs...)
	cmd.WaitDelay = time.Second
	var output boundedOutput
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

// withStoppedTunnels uses a stable lock order for operations on several tunnels.
func withStoppedTunnels(names []string, force bool, fn func() error) error {
	names = append([]string(nil), names...)
	sort.Strings(names)
	var next func(int) error
	next = func(index int) error {
		if index == len(names) {
			return fn()
		}
		return tunnelstate.WithStopped(names[index], force, func() error { return next(index + 1) })
	}
	return next(0)
}

func tunnelStatusSnapshot(names []string) (map[string]tunnelstate.Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return tunnelstate.Snapshot(ctx, names)
}
