package adapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	brewfile := "tap \"declared/tap\"\nbrew \"ggshield\"\ncask \"raycast\"\n"
	if err := os.WriteFile(filepath.Join(root, "cfg", "brewfile"), []byte(brewfile), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(base, "calls")
	brew := filepath.Join(base, "brew")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >>%q
case "$*" in
  --version) echo 'Homebrew 5.0.0' ;;
  *'bundle check'*--verbose*) echo '→ Cask raycast needs to be installed.'; exit 1 ;;
  'info --json=v2 --cask raycast') echo '{"casks":[{"artifacts":[{"app":["Raycast.app"],"target":"%s/Applications/Raycast.app"}]}]}' ;;
  'outdated --formula --json=v2') echo '{"formulae":[{"name":"ggshield","full_name":null}]}' ;;
  'outdated --cask --json=v2') echo '{"casks":[]}' ;;
  'trust --json=v1') echo '{"taps":["declared/tap"]}' ;;
  'list --formula --full-name'|'list --cask --full-name') : ;;
  tap) echo 'declared/tap'; echo 'unused/tap' ;;
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
	if len(items) != 3 {
		t.Fatalf("expected adopt, upgrade, and cleanup, got %#v", items)
	}
	assertPlanItem(t, items, "raycast", "adopt the existing application into Homebrew ownership")
	assertPlanItem(t, items, "ggshield", "upgrade the outdated installed Homebrew formula")
	assertPlanItem(t, items, "unused/tap", "untap after proving no installed formula or cask depends on it")

	applyContext := &Context{Context: context.Background(), Env: env}
	if code := homebrew(applyContext, Apply); code != 0 {
		t.Fatalf("apply returned %d", code)
	}
	trace, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"bundle --file " + filepath.Join(root, "cfg", "brewfile") + " --no-upgrade", "upgrade ggshield", "untap unused/tap"} {
		if !containsLine(string(trace), expected) {
			t.Fatalf("trace omitted %q:\n%s", expected, trace)
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
