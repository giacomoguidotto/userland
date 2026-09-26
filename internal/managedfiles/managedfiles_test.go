package managedfiles

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestPlanLegacyRecordsOnlyOwnedLinks(t *testing.T) {
	manager := testManager(t)
	current := filepath.Join(manager.Env.Root, "cfg", "home", "zshrc")
	legacy := filepath.Join(manager.Env.Root, "config", "home", "zshenv")
	for _, source := range []string{current, legacy} {
		if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("managed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	currentLink := filepath.Join(manager.Env.Home, ".zshrc")
	if err := os.Symlink(current, currentLink); err != nil {
		t.Fatal(err)
	}
	legacyLink := filepath.Join(manager.Env.Home, ".zshenv")
	if err := os.Symlink(legacy, legacyLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp/unmanaged-hushlogin", filepath.Join(manager.Env.Home, ".hushlogin")); err != nil {
		t.Fatal(err)
	}

	value := plan.New()
	manager.PlanLegacy(value)
	items := value.Items()
	if len(items) != 1 || items[0].Target != legacyLink || items[0].Proof != "legacy-link:"+legacy {
		t.Fatalf("unexpected legacy plan: %#v", items)
	}
}

func TestEnsureComposableFilesPreservesExistingSetup(t *testing.T) {
	manager := testManager(t)
	if err := os.MkdirAll(filepath.Join(manager.Env.Root, "cfg/home/ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"cfg/home/zshrc", "cfg/home/ssh/config"} {
		if err := os.WriteFile(filepath.Join(manager.Env.Root, source), []byte("userland fragment\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(manager.Env.Home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manager.Env.Home, ".zshrc"), []byte("trellis shell setup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manager.Env.Home, ".ssh/config"), []byte("Host trellis-remote-dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.ensureComposableFiles(); err != nil {
		t.Fatal(err)
	}
	zshrc, _ := os.ReadFile(filepath.Join(manager.Env.Home, ".zshrc"))
	ssh, _ := os.ReadFile(filepath.Join(manager.Env.Home, ".ssh/config"))
	if !strings.Contains(string(zshrc), "trellis shell setup") || strings.Contains(string(zshrc), "userland/zshrc") {
		t.Fatalf("shell setup was not preserved: %q", zshrc)
	}
	if !strings.Contains(string(ssh), "Host trellis-remote-dev") || !strings.Contains(string(ssh), "userland/ssh/config") {
		t.Fatalf("ssh setup was not preserved: %q", ssh)
	}
}

func TestEnsureComposableFilesMigratesOwnedLinks(t *testing.T) {
	manager := testManager(t)
	for _, source := range []string{"cfg/home/zshrc", "cfg/home/ssh/config"} {
		path := filepath.Join(manager.Env.Root, source)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("managed fragment\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(manager.Env.Home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct {
		target string
		source string
	}{
		{filepath.Join(manager.Env.Home, ".zshrc"), filepath.Join(manager.Env.Root, "cfg/home/zshrc")},
		{filepath.Join(manager.Env.Home, ".ssh/config"), filepath.Join(manager.Env.Root, "cfg/home/ssh/config")},
	} {
		if err := os.Symlink(link.source, link.target); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.ensureComposableFiles(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{filepath.Join(manager.Env.Home, ".zshrc"), filepath.Join(manager.Env.Home, ".ssh/config")} {
		info, err := os.Lstat(target)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("owned link was not migrated: %s (%v)", target, err)
		}
	}
}

func TestEnsureComposableFilesRejectsUnmanagedLinks(t *testing.T) {
	manager := testManager(t)
	if err := os.Symlink(filepath.Join(t.TempDir(), "other.zshrc"), filepath.Join(manager.Env.Home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	if err := manager.ensureComposableFiles(); err == nil || !strings.Contains(err.Error(), "unmanaged symlink") {
		t.Fatalf("unmanaged link was not rejected: %v", err)
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
