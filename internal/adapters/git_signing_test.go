package adapters

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShippedGitSigningUsesLifeSign(t *testing.T) {
	root, _ := filepath.Abs("../..")
	config := filepath.Join(root, "cfg/xdg/git/config")
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINoa9KM7X6vxq/mXeA86sKpOGeszjDOJi8qIhVOVcmxS"
	for name, want := range map[string]string{"user.signingkey": key, "gpg.format": "ssh", "commit.gpgsign": "true", "gpg.ssh.program": "~/.local/bin/userland-git-sign"} {
		out, err := exec.Command("git", "config", "--file", config, "--get", name).Output()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Errorf("%s = %q (%v), want %q", name, out, err, want)
		}
	}
	policy, err := os.ReadFile(filepath.Join(root, "cfg/home/1Password/ssh/agent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []string{"life/auth", "life/sign"} {
		if !strings.Contains(string(policy), `item = "`+item+`"`) {
			t.Errorf("missing agent key %s", item)
		}
	}
}
