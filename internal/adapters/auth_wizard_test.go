package adapters

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestPersonalWizardChecksCompletionAndResumes(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "auth-helper")
	source := `#!/bin/sh
case "$1" in
 --check-stage) test -f "$HOME/$2" ;;
 --apply-stage)
  printf 'Open browser for %s\n' "$2"
  touch "$HOME/$2" ;;
esac
`
	if err := os.WriteFile(script, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	c := &Context{Context: context.Background(), Env: platform.NewEnvironment([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "USERLAND_UI_MODE=plain"}), Output: &output, Stdin: strings.NewReader("\n\nn\n")}
	if code := personalAuthWizard(c, script); code != 0 {
		t.Fatalf("first run: %d %#v %s", code, c.Events, output.String())
	}
	for _, stage := range personalAuthStages {
		if !strings.Contains(output.String(), "[info] Open browser for "+stage.id) {
			t.Fatalf("missing stage %s: %s", stage.id, output.String())
		}
	}
	output.Reset()
	c.Stdin = strings.NewReader("n\n")
	if code := personalAuthWizard(c, script); code != 0 {
		t.Fatalf("resume: %d", code)
	}
	if strings.Contains(output.String(), "Open browser for") {
		t.Fatal("repeated completed authentication")
	}
}

func TestSecretReferencesOnlyPersistReferencesAndKeepOtherEntries(t *testing.T) {
	root := t.TempDir()
	c := &Context{Env: platform.NewEnvironment([]string{"HOME=" + root, "USERLAND_HOME=" + root})}
	if err := saveSecretReference(c, "GH_TOKEN", "actual-token-value"); err == nil {
		t.Fatal("accepted token instead of reference")
	}
	for _, pair := range [][2]string{{"GH_TOKEN", "op://Private/GitHub/token"}, {"OPENAI_API_KEY", "op://Private/OpenAI/key"}, {"GH_TOKEN", "op://Private/New GitHub/token"}} {
		if err := saveSecretReference(c, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, ".config/userland/secret-refs.env")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "OPENAI_API_KEY=op://Private/OpenAI/key\nGH_TOKEN=op://Private/New GitHub/token\n"; got != want {
		t.Fatalf("got %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
}
