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
 --check-ssh-registration) exit 1 ;;
 --apply-stage)
  if [ -t 0 ] || IFS= read -r unexpected; then exit 7; fi
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

func TestPersonalAuthenticationDoesNotStartWizardWithoutInteractiveTerminal(t *testing.T) {
	root := t.TempDir()
	applied := filepath.Join(root, "applied")
	script := filepath.Join(root, "auth-helper")
	source := "#!/bin/sh\nif [ \"${1:-}\" = --check ]; then exit 1; fi\ntouch " + shellSingleQuote(applied) + "\n"
	if err := os.WriteFile(script, []byte(source), 0o700); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{"HOME=" + root, "USERLAND_NON_INTERACTIVE=1"})
	c := &Context{Context: context.Background(), Env: env, Stdin: strings.NewReader("unexpected\n"), Output: &bytes.Buffer{}, Terminal: false}
	if code := authenticationScript(c, Apply, "personal", script, env.List); code != 2 {
		t.Fatalf("authentication returned %d, want action-required 2", code)
	}
	if _, err := os.Stat(applied); !os.IsNotExist(err) {
		t.Fatal("non-interactive authentication started the wizard")
	}
}

func TestPersonalAuthenticationUsesPinnedUserlandTools(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(filepath.Join(root, "cfg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(root, "calls")
	mise := filepath.Join(bin, "mise")
	miseSource := `#!/bin/sh
printf '%s\n' "$*" >>"$TEST_CALLS"
test "$1 $2 $3 $4" = "-C $USERLAND_ROOT/cfg exec --"
`
	if err := os.WriteFile(mise, []byte(miseSource), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gh", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	script, err := filepath.Abs("../../cfg/auth-wizard")
	if err != nil {
		t.Fatal(err)
	}
	environ := []string{
		"HOME=" + root,
		"PATH=" + bin + ":/usr/bin:/bin",
		"TEST_CALLS=" + calls,
		"USERLAND_MISE=" + mise,
		"USERLAND_ROOT=" + root,
	}
	for _, stage := range []string{"github-cli", "codex"} {
		result := platform.Run(context.Background(), environ, nil, script, "--check-stage", stage)
		if result.Code != 0 {
			t.Fatalf("%s check returned %d: %s", stage, result.Code, result.Output)
		}
	}
	contents, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"-C " + root + "/cfg exec -- gh auth status --hostname github.com",
		"-C " + root + "/cfg exec -- codex login status",
	} {
		if !strings.Contains(string(contents), expected) {
			t.Fatalf("missing pinned invocation %q in %q", expected, contents)
		}
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
