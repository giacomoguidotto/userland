package adapters

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestLoginItemScriptsUseStringPaths(t *testing.T) {
	if !strings.Contains(inspectLoginItemScript, "path of currentItem as text") {
		t.Fatal("login item inspection must read the path without coercing it through an alias")
	}
	if strings.Contains(applyLoginItemScript, " as alias") || !strings.Contains(applyLoginItemScript, "path:wantedPath") {
		t.Fatal("login item creation must pass the declared path as text")
	}
	if !strings.Contains(applyLoginItemScript, "delete currentItem") {
		t.Fatal("login item application must repair an existing item with the wrong path or visibility")
	}
}

func TestLoginItemMatchNormalizesTrailingSeparators(t *testing.T) {
	if !loginItemMatches("/Applications/Shottr.app/\tfalse\n", "/Applications/Shottr.app", "false") {
		t.Fatal("equivalent macOS login item paths should be idempotent")
	}
}

func TestCaptureAppLoginItemsUseObservableVisibility(t *testing.T) {
	rows, err := readCSV(filepath.Join("..", "..", "cfg", "login-items.csv"), "name", "path", "hidden")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"Screen Studio": true, "Shottr": true}
	for _, row := range rows {
		if !want[row[0]] {
			continue
		}
		if row[2] != "false" {
			t.Fatalf("%s must use the visibility state macOS reports for application login items", row[0])
		}
		delete(want, row[0])
	}
	if len(want) != 0 {
		t.Fatalf("missing capture app login item declarations: %v", want)
	}
}

func TestLoginItemsAppliesOnlyDeclaredApplications(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	state := filepath.Join(base, "state")
	application := filepath.Join(base, "Applications", "Example.app")
	osascript := filepath.Join(base, "osascript")
	calls := filepath.Join(base, "calls")
	configured := filepath.Join(base, "configured")
	for _, directory := range []string{filepath.Join(root, "cfg"), state, application} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "cfg", "login-items.csv"), []byte("name,path,hidden\nExample,"+application+",false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$2\" in *'make login item'*) : >" + shellSingleQuote(configured) + "; printf '%s\\n' \"$@\" >>" + shellSingleQuote(calls) + ";; *) if [ -e " + shellSingleQuote(configured) + " ]; then printf '%s\\tfalse\\n' " + shellSingleQuote(application) + "; else printf 'missing\\n'; fi;; esac\n"
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

func TestResolveLoginItemPathFallsBackToUserApplications(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	userApplications := filepath.Join(base, "Applications")
	application := "Userland Login Item Fixture.app"
	if err := os.MkdirAll(filepath.Join(userApplications, application), 0o700); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{
		"USERLAND_ROOT=" + root,
		"USERLAND_HOME=" + base,
		"USERLAND_UNAME=Darwin",
	})
	c := &Context{Context: context.Background(), Env: env}
	got := resolveLoginItemPath(c, filepath.Join("/Applications", application))
	want := filepath.Join(userApplications, application)
	if got != want {
		t.Fatalf("resolveLoginItemPath() = %q, want %q", got, want)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
