package adapters

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestLoginItemsAppliesOnlyDeclaredApplications(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	state := filepath.Join(base, "state")
	application := filepath.Join(base, "Applications", "Example.app")
	osascript := filepath.Join(base, "osascript")
	calls := filepath.Join(base, "calls")
	for _, directory := range []string{filepath.Join(root, "cfg"), state, application} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "cfg", "login-items.csv"), []byte("name,path,hidden\nExample,"+application+",false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$2\" in *'make login item'*) printf '%s\\n' \"$@\" >>" + shellSingleQuote(calls) + ";; *) printf 'missing\\n';; esac\n"
	if err := os.WriteFile(osascript, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{
		"USERLAND_ROOT=" + root,
		"USERLAND_STATE_DIR=" + state,
		"USERLAND_OSASCRIPT=" + osascript,
		"USERLAND_UNAME=Darwin",
		"PATH=/usr/bin:/bin",
	})
	invocation := &Context{Context: context.Background(), Env: env}
	if code := loginItems(invocation, Apply); code != 0 {
		t.Fatalf("login item apply returned %d: %#v", code, invocation.Events)
	}
	contents, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "Example\n"+application+"\nfalse") {
		t.Fatalf("declaration was not passed as argv: %q", contents)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
