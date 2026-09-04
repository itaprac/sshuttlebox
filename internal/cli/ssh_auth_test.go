package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/itaprac/sshuttlebox/internal/config"
)

// OpenSSH runs the test executable as its helper, as it runs shbx in production.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--internal-askpass" {
		if err := RunAskpass(os.Args[2:]); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAskpassBrokerOnlySuppliesOnePassword(t *testing.T) {
	const password = "private-test-secret"
	broker, err := newAskpassBroker(password, false)
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	info, err := os.Stat(broker.directory)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("broker directory permissions: %v, %v", info, err)
	}
	script, err := os.ReadFile(broker.helper)
	if err != nil || strings.Contains(string(script), password) {
		t.Fatalf("helper must contain no password: %v", err)
	}
	for _, item := range broker.Environment() {
		if strings.Contains(item, password) {
			t.Fatal("password leaked into process environment")
		}
	}
	request := func(prompt, hint string) askpassReply {
		t.Helper()
		conn, err := net.Dial("unix", filepath.Join(broker.directory, "socket"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := json.NewEncoder(conn).Encode(askpassRequest{Prompt: prompt, Hint: hint}); err != nil {
			t.Fatal(err)
		}
		var reply askpassReply
		if err := json.NewDecoder(conn).Decode(&reply); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	for _, prompt := range []askpassRequest{
		{Prompt: "Are you sure you want to continue connecting (yes/no)?", Hint: "confirm"},
		{Prompt: "untrusted password:", Hint: "confirm"},
		{Prompt: "notification password:", Hint: "none"},
		{Prompt: "Enter passphrase for key '/tmp/key':"},
		{Prompt: "Verification code:"},
	} {
		reply := request(prompt.Prompt, prompt.Hint)
		if reply.Accepted || reply.UseTerminal || reply.Password != "" {
			t.Fatalf("unexpected secret or prompt permission for %+v: %+v", prompt, reply)
		}
	}
	reply := request("user@host's password:", "")
	if !reply.Accepted || reply.Password != password {
		t.Fatal("SSH password request was not served")
	}
	reply = request("user@host's password:", "")
	if reply.Accepted || reply.UseTerminal || reply.Password != "" {
		t.Fatal("a second password attempt must fail without a terminal fallback")
	}
}

func TestAskpassSessionOutputDoesNotReceivePassword(t *testing.T) {
	dir := t.TempDir()
	result := filepath.Join(dir, "input")
	t.Setenv("SHBX_TEST_RESULT", result)
	binary := writeAuthScript(t, "printf 'Authenticated with public key.\\nApplication password:'\ncat > \"$SHBX_TEST_RESULT\"\n")
	if err := runWithAskpass(context.Background(), binary, "must-not-reach-session", nil, false, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("session received unexpected input: %q", data)
	}
}

func TestAskpassSubprocessAuthenticationAndFailure(t *testing.T) {
	for _, wrongPassword := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "wrong password"}[wrongPassword], func(t *testing.T) {
			result := filepath.Join(t.TempDir(), "result")
			t.Setenv("SHBX_TEST_RESULT", result)
			script := "answer=$(\"$SSH_ASKPASS\" \"user@host's password:\") || exit 80\n" +
				"[ \"$answer\" = 'private-test-secret' ] || exit 81\n" +
				"printf '%s\\n' \"$SSH_ASKPASS\" > \"$SHBX_TEST_RESULT\"\n" +
				"if \"$SSH_ASKPASS\" \"user@host's password:\"; then exit 82; fi\n"
			if wrongPassword {
				script += "printf 'Permission denied (password).\\n' >&2\nexit 255\n"
			}
			err := runWithAskpass(context.Background(), writeAuthScript(t, script), "private-test-secret", nil, false, false)
			if wrongPassword && (err == nil || !strings.Contains(err.Error(), "Permission denied")) {
				t.Fatalf("expected authentication failure: %v", err)
			}
			if !wrongPassword && err != nil {
				t.Fatal(err)
			}
			data, readErr := os.ReadFile(result)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if _, err := os.Stat(filepath.Dir(strings.TrimSpace(string(data)))); !os.IsNotExist(err) {
				t.Fatalf("helper directory was not removed: %v", err)
			}
		})
	}
}

func TestAskpassStartupTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := runWithAskpass(ctx, writeAuthScript(t, "exec sleep 10\n"), "secret", nil, false, false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded startup: %v", err)
	}
}

func TestSystemSFTPBatchPasswordOptions(t *testing.T) {
	sftp, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("OpenSSH SFTP is not installed")
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH is not installed")
	}
	dir := t.TempDir()
	capture := filepath.Join(dir, "transport-options")
	t.Setenv("SHBX_TEST_RESULT", capture)
	t.Setenv("SHBX_TEST_SYSTEM_SSH", ssh)
	// The real SFTP client builds the transport arguments. ssh -G evaluates
	// them without a network connection, including first-value precedence.
	transport := writeAuthScript(t, "exec \"$SHBX_TEST_SYSTEM_SSH\" -G \"$@\" > \"$SHBX_TEST_RESULT\"\n")
	batch := filepath.Join(dir, "batch")
	if err := os.WriteFile(batch, []byte("ls\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := passwordAuthArgs([]string{"-F", "/dev/null", "-S", transport, "-b", batch, "example.invalid"}, false)
	// The capture transport cannot speak SFTP; that expected protocol error
	// occurs only after OpenSSH has parsed the options we need to verify.
	_ = exec.Command(sftp, args...).Run()
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range []string{"batchmode no", "numberofpasswordprompts 1", "stricthostkeychecking true", "connecttimeout 15"} {
		if !strings.Contains(string(data), option+"\n") {
			t.Errorf("effective transport options do not include %q: %s", option, data)
		}
	}
}

func TestSSHArgumentsIPv6AndImportedAliases(t *testing.T) {
	host := config.Host{Host: "2001:db8::1", User: "user"}
	if target := formatSFTPTarget(host, "/files"); target != "user@[2001:db8::1]:/files" {
		t.Fatalf("SFTP IPv6 target: %s", target)
	}
	if target := formatSSHTarget(host); target != "user@2001:db8::1" {
		t.Fatalf("SSH IPv6 target: %s", target)
	}
	for _, tt := range []struct{ kind, want string }{
		{"local", "[::1]:8080:[2001:db8::2]:80"},
		{"remote", "[::1]:80:[2001:db8::2]:8080"},
		{"dynamic", "[::1]:8080"},
	} {
		_, spec := tunnelForwardSpec(config.Tunnel{Type: tt.kind, BindAddress: "::1", RemoteHost: "2001:db8::2", LocalPort: 8080, RemotePort: 80})
		if spec != tt.want {
			t.Fatalf("%s IPv6 forwarding: %s", tt.kind, spec)
		}
	}
	imported := config.Host{Host: "prod-alias", SSHConfigFile: "/tmp/ssh config"}
	for _, args := range [][]string{buildSSHArgs(imported), buildSFTPArgs(imported, "")} {
		if strings.Join(args, "|") != "-F|/tmp/ssh config|prod-alias" {
			t.Fatalf("imported alias must keep its original configuration: %v", args)
		}
	}
}

func TestTunnelArgumentBuilderHasNoFilesystemSideEffects(t *testing.T) {
	withTempHome(t)
	_, err := buildTunnelStartSSHArgs("preview", config.Host{Host: "example.invalid"}, config.Tunnel{Type: "dynamic", LocalPort: 1080})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("preview created files: %v", entries)
	}
}

func writeAuthScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "command")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProcessOutputRetainsBoundedErrorTail(t *testing.T) {
	var output boundedOutput
	for i := 0; i < 2000; i++ {
		if n, err := output.Write([]byte(strings.Repeat("x", 1024))); err != nil || n != 1024 {
			t.Fatalf("write: %d, %v", n, err)
		}
	}
	_, _ = output.Write([]byte("Permission denied\n"))
	if len(output.String()) != maxProcessOutput || !strings.HasSuffix(output.String(), "Permission denied\n") {
		t.Fatal("capture must retain the bounded error tail")
	}
	large := strings.Repeat("y", maxProcessOutput*2) + "last error"
	if n, err := output.Write([]byte(large)); err != nil || n != len(large) {
		t.Fatalf("large write: %d, %v", n, err)
	}
	if len(output.String()) != maxProcessOutput || !strings.HasSuffix(output.String(), "last error") {
		t.Fatal("a large write must retain only its last bytes")
	}
}

func TestAskpassInteractiveOTPRequiresManualInput(t *testing.T) {
	broker, err := newAskpassBroker("must-not-be-used-as-otp", true)
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	conn, err := net.Dial("unix", filepath.Join(broker.directory, "socket"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(askpassRequest{Prompt: "Verification code:"}); err != nil {
		t.Fatal(err)
	}
	var reply askpassReply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.Accepted || reply.Password != "" || !reply.UseTerminal {
		t.Fatalf("OTP must require manual input and never use the stored password: %+v", reply)
	}
}
