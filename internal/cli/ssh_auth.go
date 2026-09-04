package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

const askpassSocketEnv = "SHBX_ASKPASS_SOCKET"
const tunnelAuthenticationTimeout = 30 * time.Second

type askpassRequest struct {
	Prompt string
	Hint   string
}

type askpassReply struct {
	Password    string
	UseTerminal bool
	Accepted    bool
}

// askpassBroker keeps the password in memory. Its private socket is available
// only while the SSH client runs. No session output is inspected for prompts.
type askpassBroker struct {
	directory string
	listener  net.Listener
	done      chan struct{}
	helper    string
}

func newAskpassBroker(password string, interactive bool) (*askpassBroker, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Use a short path: Unix socket paths on macOS must fit in 104 bytes.
	directory, err := os.MkdirTemp("/tmp", "shbx-auth-")
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", filepath.Join(directory, "socket"))
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	broker := &askpassBroker{directory: directory, listener: listener, done: make(chan struct{}), helper: filepath.Join(directory, "askpass")}
	// SSH_ASKPASS accepts an executable path, not a command with arguments.
	// The wrapper contains only the executable path, never the password.
	script := "#!/bin/sh\nexec " + shellQuote(executable) + " --internal-askpass \"$@\"\n"
	if err := os.WriteFile(broker.helper, []byte(script), 0o700); err != nil {
		_ = listener.Close()
		_ = os.RemoveAll(directory)
		return nil, err
	}
	go broker.serve(password, interactive)
	return broker, nil
}

func (b *askpassBroker) serve(password string, interactive bool) {
	defer close(b.done)
	passwordSent := false
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		var request askpassRequest
		if err := json.NewDecoder(io.LimitReader(conn, 8192)).Decode(&request); err == nil {
			reply := askpassReply{}
			if request.Hint == "" && looksLikePasswordPrompt(request.Prompt) {
				if !passwordSent {
					reply.Password = password
					reply.Accepted = true
					passwordSent = true
				}
			} else {
				// Host-key confirmation and encrypted-key prompts need a human.
				reply.UseTerminal = interactive && request.Hint != "none"
			}
			_ = json.NewEncoder(conn).Encode(reply)
		}
		_ = conn.Close()
	}
}

func (b *askpassBroker) Close() {
	_ = b.listener.Close()
	<-b.done
	_ = os.RemoveAll(b.directory)
}

func (b *askpassBroker) Environment() []string {
	env := os.Environ()
	for _, key := range []string{"SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", askpassSocketEnv} {
		filtered := env[:0]
		for _, value := range env {
			if !strings.HasPrefix(value, key+"=") {
				filtered = append(filtered, value)
			}
		}
		env = filtered
	}
	return append(env, "SSH_ASKPASS="+b.helper, "SSH_ASKPASS_REQUIRE=force", askpassSocketEnv+"="+filepath.Join(b.directory, "socket"))
}

// RunAskpass is the private entry point used by the OpenSSH authentication
// helper. OpenSSH calls it separately from the remote session's input/output.
func RunAskpass(args []string) error {
	if len(args) != 1 || os.Getenv(askpassSocketEnv) == "" {
		return errors.New("invalid authentication helper request")
	}
	conn, err := net.DialTimeout("unix", os.Getenv(askpassSocketEnv), 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	request := askpassRequest{Prompt: args[0], Hint: os.Getenv("SSH_ASKPASS_PROMPT")}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return err
	}
	var reply askpassReply
	if err := json.NewDecoder(io.LimitReader(conn, 65536)).Decode(&reply); err != nil {
		return err
	}
	if reply.Accepted {
		_, err := fmt.Fprintln(os.Stdout, reply.Password)
		return err
	}
	if !reply.UseTerminal {
		return errors.New("authentication prompt cannot use the stored password")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return errors.New("authentication needs a terminal; connect interactively first")
	}
	defer tty.Close()
	fmt.Fprint(tty, request.Prompt+" ")
	var answer string
	if request.Hint == "confirm" {
		answer, err = bufio.NewReader(tty).ReadString('\n')
	} else {
		var input []byte
		input, err = term.ReadPassword(int(tty.Fd()))
		fmt.Fprintln(tty)
		answer = string(input)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, strings.TrimRight(answer, "\r\n"))
	return err
}

func passwordAuthArgs(args []string, interactive bool) []string {
	options := []string{"-o", "BatchMode=no", "-o", "NumberOfPasswordPrompts=1", "-o", "ConnectTimeout=15", "-o", "ConnectionAttempts=1"}
	if !interactive {
		options = append(options, "-o", "StrictHostKeyChecking=yes")
	}
	// OpenSSH uses the first option value. These must precede SFTP's -b,
	// which adds BatchMode=yes to the SSH transport's arguments.
	return append(options, args...)
}

func runSSHWithPassword(password string, sshArgs []string) error {
	return runWithAskpass(context.Background(), sshBinary(), password, sshArgs, true, false)
}

func runSFTPWithPassword(password string, sftpArgs []string) error {
	return runWithAskpass(context.Background(), sftpBinary(), password, sftpArgs, true, false)
}

func runSSHWithStoredPassword(password string, sshArgs []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), tunnelAuthenticationTimeout)
	defer cancel()
	return runWithAskpass(ctx, sshBinary(), password, sshArgs, false, false)
}

func runSFTPBatchWithPassword(password string, sftpArgs []string) error {
	// Bound connection setup, but do not put a time limit on file transfers.
	return runWithAskpass(context.Background(), sftpBinary(), password, sftpArgs, false, true)
}

func runWithAskpass(ctx context.Context, binary, password string, args []string, interactive, showOutput bool) error {
	broker, err := newAskpassBroker(password, interactive)
	if err != nil {
		return fmt.Errorf("prepare authentication helper: %w", err)
	}
	defer broker.Close()
	cmd := exec.CommandContext(ctx, binary, passwordAuthArgs(args, interactive)...)
	cmd.Env = broker.Environment()
	// Bound waiting for inherited pipes after a background SSH master forks.
	cmd.WaitDelay = 2 * time.Second
	var output boundedOutput
	if interactive {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = &output
		cmd.Stderr = &output
		if showOutput {
			cmd.Stdout = os.Stdout
			cmd.Stderr = io.MultiWriter(os.Stderr, &output)
		}
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("SSH authentication timed out: %w", ctx.Err())
		}
		return sshRunError(err, output.String())
	}
	return nil
}

func looksLikePasswordPrompt(text string) bool {
	// This is used only on the OpenSSH askpass channel, never session output.
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(text)), "password:")
}
