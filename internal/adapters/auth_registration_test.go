package adapters

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestRegisteredSSHKeyDoesNotOpenRegistration(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	for _, dir := range []string{bin, filepath.Join(home, ".ssh")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	socketPath := filepath.Join(os.TempDir(), "userland-auth-"+strconv.Itoa(os.Getpid())+".sock")
	socket, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	defer os.Remove(socketPath)
	if err := os.WriteFile(filepath.Join(home, ".ssh/life-auth.pub"), []byte("ssh-ed25519 AAAAtest local-comment\n"), 0600); err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"ssh":     "echo 'Permission denied (publickey).' >&2; exit 255",
		"ssh-add": `case "$1" in -L) printf '%s\n' 'ssh-ed25519 AAAAtest agent-comment' ;; -T) exit 0 ;; *) exit 2 ;; esac`,
		"curl":    `printf '%s\n' '[{"key":"ssh-ed25519 AAAAtest github-comment"}]'`,
		"jq":      `case "$*" in *'any('* ) printf 'true\n' ;; *) printf '1\n' ;; esac`,
		"open":    `printf '%s\n' "$*" >> "$HOME/opened"`,
		"pbcopy":  `cat >/dev/null; touch "$HOME/copied"`,
		"gh":      "exit 0", "codex": "exit 0",
	}
	for name, source := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+source+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	script, err := filepath.Abs("../../cfg/auth-wizard")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	c := &Context{Context: context.Background(), Env: platform.NewEnvironment([]string{"HOME=" + home, "PATH=" + bin + ":" + os.Getenv("PATH"), "SSH_AUTH_SOCK=" + socketPath, "USERLAND_UI_MODE=plain"}), Stdin: strings.NewReader("\n"), Output: &out}
	if result := platform.Run(context.Background(), c.Env.List, nil, script, "--check-ssh-registration"); result.Code != 0 {
		t.Fatalf("registration probe failed: %d %s", result.Code, result.Output)
	}
	code := personalAuthWizard(c, script)
	if _, err := os.Stat(filepath.Join(home, "opened")); !os.IsNotExist(err) {
		t.Fatalf("registered key opened registration: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, "copied")); !os.IsNotExist(err) {
		t.Fatal("registered key copied for registration")
	}
	if !strings.Contains(out.String(), "already registered") {
		t.Fatalf("missing registered-key explanation: %s", out.String())
	}
	if code != 2 {
		t.Fatalf("broken SSH must remain incomplete, got %d", code)
	}
}

func TestSSHAgentFailuresDoNotOfferADeadEndRetry(t *testing.T) {
	for _, detail := range []string{
		"FAIL agent config: 1Password SSH agent config is missing",
		"FAIL agent inventory: life/auth is not offered",
		"FAIL agent signing: life/auth was refused",
	} {
		if !sshFailureNeedsManualAgentAction(detail) {
			t.Fatalf("manual agent failure was not classified: %q", detail)
		}
	}
	if sshFailureNeedsManualAgentAction("FAIL GitHub: did not accept life/auth") {
		t.Fatal("GitHub-only failure should retain the retry path")
	}
}

func TestSSHAgentPathWithSpacesParsesInOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp("/tmp", "ul-auth-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	socketPath := filepath.Join(home, "Group Containers", "agent.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		t.Fatal(err)
	}
	socket, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	bin := filepath.Join(home, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	// Parse the exact options handed to SSH without making a network connection.
	source := "#!/bin/sh\n" + shellSingleQuote(ssh) + " -G -F /dev/null \"$@\" >\"$HOME/parsed\"\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	script, _ := filepath.Abs("../../cfg/auth-wizard")
	result := platform.Run(context.Background(), []string{"HOME=" + home, "PATH=" + bin + ":" + os.Getenv("PATH"), "SSH_AUTH_SOCK=" + socketPath}, nil, script, "--check-stage", "ssh")
	if strings.Contains(string(result.Output), "extra arguments") {
		t.Fatalf("SSH cannot parse agent path with spaces: %s", result.Output)
	}
}
