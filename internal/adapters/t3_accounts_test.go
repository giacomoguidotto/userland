package adapters

import (
	"path/filepath"
	"sort"
	"testing"
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
