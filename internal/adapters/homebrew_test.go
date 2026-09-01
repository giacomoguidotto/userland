package adapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestHomebrewPlansAndAppliesTypedHealthIssues(t *testing.T) {
	base := t.TempDir()
	root, home := filepath.Join(base, "root"), filepath.Join(base, "home")
	for _, directory := range []string{filepath.Join(root, "cfg"), filepath.Join(home, "Applications", "Raycast.app"), filepath.Join(base, "cache"), filepath.Join(base, "state")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	brewfile := "tap \"declared/tap\"\nbrew \"ggshield\"\ncask \"raycast\"\ncask \"zed\"\n"
	if err := os.WriteFile(filepath.Join(root, "cfg", "brewfile"), []byte(brewfile), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(base, "calls")
	brew := filepath.Join(base, "brew")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >>%q
case "$*" in
  *'bundle check'*--verbose*) echo '→ Cask raycast needs to be installed.'; exit 1 ;;
  'info --json=v2 --cask raycast') echo '{"casks":[{"artifacts":[{"app":["Raycast.app"],"target":"%s/Applications/Raycast.app"}]}]}' ;;
  'outdated --json=v2') echo '{"formulae":[{"name":"ggshield","full_name":null},{"name":"bat","full_name":null}],"casks":[{"name":"zed","full_name":null},{"name":"unmanaged","full_name":null}]}' ;;
  'trust --json=v1') echo '{"taps":["declared/tap"]}' ;;
  'list --full-name') : ;;
  tap) echo 'declared/tap'; echo 'unused/tap' ;;
  *'bundle --file'*--verbose*) echo 'Using raycast'; echo 'Using zed' ;;
esac
`, calls, home)
	if err := os.WriteFile(brew, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	environ := []string{
		"USERLAND_ROOT=" + root, "USERLAND_HOME=" + home,
		"USERLAND_CACHE_DIR=" + filepath.Join(base, "cache"), "USERLAND_STATE_DIR=" + filepath.Join(base, "state"),
		"USERLAND_UNAME=Darwin", "USERLAND_BREW=" + brew, "PATH=/usr/bin:/bin",
	}
	env := platform.NewEnvironment(environ)
	value := plan.New()
	planContext := &Context{Context: context.Background(), Env: env, Plan: value}
	if code := homebrew(planContext, Plan); code != 0 {
		t.Fatalf("plan returned %d", code)
	}
	items := value.Items()
	if len(items) != 4 {
		t.Fatalf("expected adopt, two owned upgrades, and cleanup, got %#v", items)
	}
	assertPlanItem(t, items, "raycast", "adopt the existing application into Homebrew ownership")
	assertPlanItem(t, items, "ggshield", "upgrade the outdated installed Homebrew formula")
	assertPlanItem(t, items, "zed", "upgrade the outdated installed Homebrew cask")
	assertPlanItem(t, items, "unused/tap", "untap after proving no installed formula or cask depends on it")
	for _, item := range items {
		if item.Target == "bat" || item.Target == "unmanaged" {
			t.Fatalf("Homebrew planned an upgrade owned outside the Brewfile: %#v", item)
		}
	}

	var progress []string
	applyContext := &Context{Context: context.Background(), Env: env, Progress: func(current, total int, detail string) {
		progress = append(progress, fmt.Sprintf("%d/%d:%s", current, total, detail))
	}}
	if code := homebrew(applyContext, Apply); code != 0 {
		t.Fatalf("apply returned %d", code)
	}
	trace, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"bundle --file " + filepath.Join(root, "cfg", "brewfile") + " --no-upgrade --verbose", "upgrade ggshield", "upgrade --cask zed", "untap unused/tap"} {
		if !containsLine(string(trace), expected) {
			t.Fatalf("trace omitted %q:\n%s", expected, trace)
		}
	}
	for _, removed := range []string{"--version", "outdated --formula --json=v2", "outdated --cask --json=v2", "list --formula --full-name", "list --cask --full-name"} {
		if containsLine(string(trace), removed) {
			t.Fatalf("trace retained redundant probe %q:\n%s", removed, trace)
		}
	}
	if actual, expected := strings.Join(progress, "\n"), "1/4:raycast\n2/4:ggshield\n3/4:zed\n4/4:unused/tap"; actual != expected {
		t.Fatalf("Homebrew progress = %q, want %q", actual, expected)
	}
}

func TestRealmHomebrewOwnsPostmanAndReportsItemProgress(t *testing.T) {
	base := t.TempDir()
	root, home := filepath.Join(base, "root"), filepath.Join(base, "home")
	state, cache := filepath.Join(base, "state"), filepath.Join(base, "cache")
	realmRoot := filepath.Join(base, "danfoss")
	for _, directory := range []string{filepath.Join(root, "cfg"), state, cache, filepath.Join(realmRoot, ".userland")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "cfg", "brewfile"), []byte("cask \"raycast\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cfg", "realms.csv"), []byte(
		"name,repository,configuration_path,default_path,branch,mode\n"+
			"danfoss,git@example.test:private/danfoss.git,"+realmRoot+","+realmRoot+",main,optional\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "realms.csv"), []byte("name,path\ndanfoss,"+realmRoot+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	realmBrewfile := filepath.Join(realmRoot, ".userland", "brewfile")
	if err := os.WriteFile(realmBrewfile, []byte("cask \"postman\", args: { adopt: true }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(base, "calls")
	brew := filepath.Join(base, "brew")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >>%q
case "$*" in
  *'bundle check'*danfoss*--verbose*) echo '→ Cask postman needs to be installed.'; exit 1 ;;
  'outdated --json=v2') echo '{"formulae":[],"casks":[]}' ;;
  'trust --json=v1') echo '{"taps":[]}' ;;
  'list --full-name'|'tap') : ;;
  *'bundle --file'*danfoss*--verbose*) echo 'Installing postman cask. It is not currently installed.' ;;
esac
`, calls)
	if err := os.WriteFile(brew, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{
		"USERLAND_ROOT=" + root, "USERLAND_HOME=" + home, "USERLAND_CACHE_DIR=" + cache, "USERLAND_STATE_DIR=" + state,
		"USERLAND_UNAME=Darwin", "USERLAND_BREW=" + brew, "PATH=/usr/bin:/bin",
	})
	value := plan.New()
	if code := realmHomebrew(&Context{Context: context.Background(), Env: env, Plan: value}, Plan); code != 0 {
		t.Fatalf("plan returned %d", code)
	}
	assertPlanItem(t, value.Items(), "postman", "install the missing Homebrew cask")
	var progress []string
	apply := &Context{Context: context.Background(), Env: env, Progress: func(current, total int, detail string) {
		progress = append(progress, fmt.Sprintf("%d/%d:%s", current, total, detail))
	}}
	if code := realmHomebrew(apply, Apply); code != 0 {
		t.Fatalf("apply returned %d", code)
	}
	if strings.Join(progress, "\n") != "1/1:postman" {
		t.Fatalf("realm cask progress = %#v", progress)
	}
	trace, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if !containsLine(string(trace), "bundle --file "+realmBrewfile+" --no-upgrade --verbose") {
		t.Fatalf("realm Brewfile was not applied:\n%s", trace)
	}
}

func TestShippedPersonalBrewfileOmitsRealmOnlyPostmanAndRemovedSignal(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	declarations, err := brewDeclarations(filepath.Join(root, "cfg", "brewfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range declarations {
		if declaration[1] == "postman" || declaration[1] == "signal" {
			t.Fatalf("personal Brewfile still owns %s", declaration[1])
		}
	}
}

func assertPlanItem(t *testing.T, items []plan.Item, target, detail string) {
	t.Helper()
	for _, item := range items {
		if item.Target == target && item.Detail == detail {
			return
		}
	}
	t.Fatalf("missing %s: %s in %#v", target, detail, items)
}

func containsLine(value, expected string) bool {
	for _, line := range strings.Split(value, "\n") {
		if line == expected {
			return true
		}
	}
	return false
}
