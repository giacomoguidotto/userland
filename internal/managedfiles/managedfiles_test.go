package managedfiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestPlanLegacyRecordsOnlyOwnedLinks(t *testing.T) {
	manager := testManager(t)
	owned := filepath.Join(manager.Env.Root, "cfg", "home", "zshrc")
	if err := os.MkdirAll(filepath.Dir(owned), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(owned, []byte("managed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(manager.Env.Home, ".zshrc")
	if err := os.Symlink(owned, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp/unmanaged-zshrc", filepath.Join(manager.Env.Home, ".zshenv")); err != nil {
		t.Fatal(err)
	}

	value := plan.New()
	manager.PlanLegacy(value)
	items := value.Items()
	if len(items) != 1 || items[0].Target != link || items[0].Proof != "legacy-link:"+owned {
		t.Fatalf("unexpected legacy plan: %#v", items)
	}
}

func TestRecoverRestoresInterruptedCutover(t *testing.T) {
	manager := testManager(t)
	target := filepath.Join(manager.Env.Home, ".zshrc")
	if err := os.WriteFile(target, []byte("before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	var value status
	value.Files = append(value.Files, struct {
		State  string `json:"state"`
		Source string `json:"source"`
		Target string `json:"target"`
	}{State: "differs", Source: "~/cfg/home/zshrc", Target: "~/.zshrc"})
	_, _, err := manager.begin(value, []byte(`{"files":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manager.Recover(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "before\n" {
		t.Fatalf("rollback restored %q", contents)
	}
	if _, err := os.Stat(filepath.Join(manager.Env.State, "recovery", "active")); !os.IsNotExist(err) {
		t.Fatal("active recovery marker remains")
	}
}

func TestLegacyDirectoryMigrationPreservesOnlyUnmanagedChildren(t *testing.T) {
	manager := testManager(t)
	old := filepath.Join(t.TempDir(), "workspace", "cfg", "xdg", "nvim")
	replacement := filepath.Join(manager.Env.Root, "cfg", "xdg", "nvim")
	for _, directory := range []string{old, replacement, filepath.Join(manager.Env.Home, ".config")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(old, "managed.lua"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "personal.lua"), []byte("personal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, "managed.lua"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(manager.Env.Home, ".config", "nvim")
	if err := os.Symlink(old, link); err != nil {
		t.Fatal(err)
	}

	if err := manager.prepareLegacy(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("legacy link was not replaced by a directory: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(link, "personal.lua")); err != nil {
		t.Fatal("unmanaged child was not preserved")
	}
	if _, err := os.Stat(filepath.Join(link, "managed.lua")); !os.IsNotExist(err) {
		t.Fatal("managed child leaked from the old source")
	}
}

func TestCancelledApplyRollsBackBeforeReturning(t *testing.T) {
	manager := testManager(t)
	target := filepath.Join(manager.Env.Home, ".zshrc")
	if err := os.WriteFile(target, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mise := filepath.Join(t.TempDir(), "mise")
	script := `#!/bin/sh
case "$*" in
  *'dotfiles status --json'*) printf '{"files":[{"state":"differs","source":"~/cfg/home/zshrc","target":"~/.zshrc"}],"edits":[]}\n' ;;
  *'dotfiles apply'*) printf 'after\n' >"$TARGET"; exec sleep 5 ;;
esac
`
	if err := os.WriteFile(mise, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	manager.Env.Mise = mise
	manager.Env.List = []string{"TARGET=" + target, "PATH=/usr/bin:/bin"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if code := manager.Apply(ctx); code == 0 {
		t.Fatal("cancelled apply succeeded")
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "before\n" {
		t.Fatalf("cancelled apply left %q", contents)
	}
}

func testManager(t *testing.T) Manager {
	t.Helper()
	base := t.TempDir()
	env := platform.Environment{
		Root: filepath.Join(base, "root"), Home: filepath.Join(base, "home"),
		Data: filepath.Join(base, "data"), Cache: filepath.Join(base, "cache"), State: filepath.Join(base, "state"),
		Values: map[string]string{},
	}
	for _, directory := range []string{env.Root, env.Home, env.Data, env.Cache, env.State} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return Manager{Env: env}
}
