package adapters

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestDeclaredT3AccountsMatchCredentialFreeProfiles(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	accounts, err := declaredT3Accounts(root)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(accounts)
	want := []string{"claudeAgent", "claude_beta", "codex", "codex_beta", "codex_delta", "codex_gamma"}
	if len(accounts) != len(want) {
		t.Fatalf("declared accounts = %v, want %v", accounts, want)
	}
	for i := range want {
		if accounts[i] != want[i] {
			t.Fatalf("declared accounts = %v, want %v", accounts, want)
		}
	}
}

func TestT3AuthenticationProfilesUseSixPrivateHomes(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	accounts, err := t3AuthAccounts(&Context{Env: platform.NewEnvironment([]string{"USERLAND_ROOT=" + root, "USERLAND_HOME=" + home})})
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 6 {
		t.Fatalf("authentication profiles = %d, want 6", len(accounts))
	}
	seen := map[string]bool{}
	for _, account := range accounts {
		if seen[account.home] || account.home == home {
			t.Fatalf("profile home is not private: %q", account.home)
		}
		seen[account.home] = true
	}
	if _, err := os.Stat(filepath.Join(home, ".codex-t3", "alpha")); !os.IsNotExist(err) {
		t.Fatal("profile discovery created authentication state")
	}
}
