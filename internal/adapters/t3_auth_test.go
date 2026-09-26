package adapters

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestT3ProbeFailureDoesNotRequestLogin(t *testing.T) {
	for _, action := range []Action{Plan, Apply, Doctor} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			for _, dir := range []string{filepath.Join(root, "cfg/t3-thread/profiles"), filepath.Join(home, ".codex-t3/alpha")} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			data := `{"providerInstances":{"codex":{"driver":"codex","displayName":"alpha","enabled":true,"config":{"binaryPath":"codex","homePath":"~/.codex","shadowHomePath":"~/.codex-t3/alpha"}}}}`
			if err := os.WriteFile(filepath.Join(root, "cfg/t3-thread/profiles/accounts.json"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "codex"), []byte("#!/bin/sh\necho 'mise ERROR missing claude installation' >&2\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			c := &Context{Context: context.Background(), Env: platform.NewEnvironment([]string{"USERLAND_ROOT=" + root, "USERLAND_HOME=" + home, "PATH=" + root}), Output: &out, Stdin: strings.NewReader("n\n"), Terminal: true}
			t3Authentication(c, action)
			for _, event := range c.Events {
				if event.Level == Manual || strings.Contains(event.Message, "needs account login") || strings.Contains(event.Message, "login skipped") || strings.Contains(event.Message, "needs login;") {
					t.Fatalf("failed probe treated as logged out: %#v", c.Events)
				}
			}
			if strings.Contains(out.String(), "Sign in to") {
				t.Fatalf("unknown status started login: %s", out.String())
			}
			if len(c.Events) == 0 {
				t.Fatal("unknown status silently hidden")
			}
		})
	}
}

func TestBootstrapDoesNotShadowWorkingCodexWithMiseShim(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "tools")
	shim := filepath.Join(home, ".local/share/mise/shims")
	for _, dir := range []string{bin, shim} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for path, source := range map[string]string{filepath.Join(bin, "codex"): "echo authenticated", filepath.Join(shim, "codex"): "echo missing-unrelated-tool; exit 1"} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+source+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	template, err := os.ReadFile("../../release/bootstrap-template.sh")
	if err != nil {
		t.Fatal(err)
	}
	var pathLine string
	for _, line := range strings.Split(string(template), "\n") {
		if strings.HasPrefix(line, "PATH=") {
			pathLine = line
			break
		}
	}
	cmd := exec.Command("/bin/sh", "-c", pathLine+"\nexport PATH\ncodex login status")
	cmd.Env = []string{"HOME=" + home, "bin_dir=" + filepath.Join(home, "bin"), "PATH=" + bin}
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "authenticated" {
		t.Fatalf("bootstrap changed working status check: %v %s", err, output)
	}
}

func TestT3AuthStatusClassification(t *testing.T) {
	for _, tc := range []struct {
		name, driver, output string
		code                 int
		want                 t3AuthState
	}{
		{"codex ready", "codex", "Logged in using ChatGPT\n", 0, t3AuthReady},
		{"codex logged out", "codex", "Not logged in\n", 1, t3AuthLoggedOut},
		{"keyring error", "codex", "Failed to load keyring", 1, t3AuthUnknown},
		{"mise error", "codex", "mise ERROR tool not installed", 1, t3AuthUnknown},
		{"claude ready", "claudeAgent", `{"loggedIn":true}`, 0, t3AuthReady},
		{"claude logged out", "claudeAgent", `{"loggedIn":false}`, 1, t3AuthLoggedOut},
		{"claude malformed", "claudeAgent", `{}`, 0, t3AuthUnknown},
		{"claude crash", "claudeAgent", "EINVAL kqueue", 1, t3AuthUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := classifyT3Auth(tc.driver, platform.Result{Output: []byte(tc.output), Code: tc.code})
			if state != tc.want {
				t.Fatalf("state=%v want=%v", state, tc.want)
			}
		})
	}
}

func TestFourAuthenticatedCodexHomesUseExistingCLI(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	bin := filepath.Join(home, ".local/bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	// Assert the selected shadow home and preserve the requested credential store.
	script := `#!/bin/sh
case "$CODEX_HOME" in */.codex-t3/alpha|*/.codex-t3/beta|*/.codex-t3/gamma|*/.codex-t3/delta) ;; *) exit 33;; esac
[ "$1" = -c ] && [ "$2" = 'cli_auth_credentials_store="keyring"' ] && [ "$3" = login ] && [ "$4" = status ] || exit 34
printf 'Logged in using ChatGPT\n'
`
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c := &Context{Context: context.Background(), Env: platform.NewEnvironment([]string{"USERLAND_ROOT=" + root, "USERLAND_HOME=" + home, "PATH=" + bin, "CODEX_HOME=/wrong/profile"})}
	accounts, err := t3AuthAccounts(c)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, a := range accounts {
		if a.Driver != "codex" {
			continue
		}
		if err := os.MkdirAll(a.home, 0700); err != nil {
			t.Fatal(err)
		}
		if state, reason := a.check(c); state != t3AuthReady {
			t.Fatalf("%s: %v %s", a.label(), state, reason)
		}
		checked++
	}
	if checked != 4 {
		t.Fatalf("checked %d profiles", checked)
	}
}

func TestT3ResolvesOnlyRequestedToolWhenPATHIsMissing(t *testing.T) {
	base := t.TempDir()
	cli := filepath.Join(base, "installed-codex")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf 'Logged in using ChatGPT\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	mise := filepath.Join(base, "mise")
	// An exec of the entire toolset would fail while unrelated pins are absent.
	source := `#!/bin/sh
[ "$3" = which ] && [ "$4" = codex ] || exit 60
[ "$MISE_AUTO_INSTALL" = 0 ] && [ "$MISE_EXEC_AUTO_INSTALL" = 0 ] || exit 61
printf '%s\n' "$TEST_CODEX"
`
	if err := os.WriteFile(mise, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	c := &Context{Context: context.Background(), Env: platform.NewEnvironment([]string{"USERLAND_ROOT=" + base, "USERLAND_MISE=" + mise, "TEST_CODEX=" + cli, "PATH=/nonexistent"})}
	a := t3AuthAccount{Driver: "codex", home: base}
	a.Config.BinaryPath = "codex"
	if state, reason := a.check(c); state != t3AuthReady {
		t.Fatalf("status %v: %s", state, reason)
	}
}

func TestClaudeLoginUsesConfiguredInput(t *testing.T) {
	base := t.TempDir()
	cli := filepath.Join(base, "claude")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf 'Login successful.\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c := &Context{
		Context: context.Background(),
		Env:     platform.NewEnvironment([]string{"USERLAND_ROOT=" + base, "USERLAND_HOME=" + base, "PATH=" + base}),
	}
	a := t3AuthAccount{Driver: "claudeAgent", home: base}
	a.Config.BinaryPath = "claude"
	result := a.runInteractive(c, context.Background(), strings.NewReader(""), "auth", "login", "--claudeai")
	if result.Code != 0 {
		t.Fatalf("Claude login received a TTY: exit=%d err=%v output=%q", result.Code, result.Err, result.Output)
	}
}
