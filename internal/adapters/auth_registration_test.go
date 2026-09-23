package adapters

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestRegisteredSSHKeyDoesNotOpenRegistration(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	for _, dir := range []string{bin, filepath.Join(home, ".ssh"), filepath.Join(home, ".1password")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	socket, err := net.Listen("unix", filepath.Join(home, ".1password/agent.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	if err := os.WriteFile(filepath.Join(home, ".ssh/life-auth.pub"), []byte("ssh-ed25519 AAAAtest local-comment\n"), 0600); err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"ssh":    "echo 'Permission denied (publickey).' >&2; exit 255",
		"curl":   `printf '%s\n' '[{"key":"ssh-ed25519 AAAAtest github-comment"}]'`,
		"open":   `printf '%s\n' "$*" >> "$HOME/opened"`,
		"pbcopy": `cat >/dev/null; touch "$HOME/copied"`,
		"gh":     "exit 0", "codex": "exit 0",
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
	c := &Context{Context: context.Background(), Env: platform.NewEnvironment([]string{"HOME=" + home, "PATH=" + bin + ":" + os.Getenv("PATH"), "USERLAND_UI_MODE=plain"}), Stdin: strings.NewReader("\n"), Output: &out}
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
