package adapters

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestGlobalMiseEnvironmentContainsOnlyDeclaredToolPaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "userland")
	home := filepath.Join(base, "home")
	mise := filepath.Join(base, "mise")
	if err := os.MkdirAll(filepath.Join(root, "cfg"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
case "$*" in
  *" bin-paths")
    printf '%s\n' '/tools/node/bin' '/tools/python/bin'
    ;;
  *" env --json")
    printf '%s\n' '{"JAVA_HOME":"/tools/java","PATH":"/tools/node/bin:/global/shims:/project/gcloud/bin"}'
    ;;
  *)
    exit 64
    ;;
esac
`
	if err := os.WriteFile(mise, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{
		"USERLAND_ROOT=" + root,
		"USERLAND_HOME=" + home,
		"USERLAND_MISE=" + mise,
		"PATH=/global/shims:/project/gcloud/bin:/usr/bin:/bin",
	})
	value, err := globalMiseEnvironment(&Context{Context: context.Background(), Env: env})
	if err != nil {
		t.Fatal(err)
	}
	contents := string(miseEnvironmentContents("fingerprint", value))
	for _, expected := range []string{"'/tools/node/bin'", "'/tools/python/bin'", "export JAVA_HOME='/tools/java'"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("static environment omitted %q: %q", expected, contents)
		}
	}
	for _, leaked := range []string{"/global/shims", "/project/gcloud", "export PATH='/tools"} {
		if strings.Contains(contents, leaked) {
			t.Fatalf("static environment leaked %q: %q", leaked, contents)
		}
	}
}

func TestShellToolPathFindsPinnedToolsInMiseBinPaths(t *testing.T) {
	base := t.TempDir()
	bin := filepath.Join(base, "starship-bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	starship := filepath.Join(bin, "starship")
	if err := os.WriteFile(starship, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{"PATH=/usr/bin:/bin"})
	path, ok := shellToolPath(&Context{Env: env}, miseShellEnvironment{BinPaths: []string{bin}}, "starship")
	if !ok || path != starship {
		t.Fatalf("shell cache did not resolve pinned tool: %q, %v", path, ok)
	}
}

func TestShippedShellScopesMiseToolsAndGcloudPrompt(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	zshenv, err := os.ReadFile(filepath.Join(root, "cfg", "home", "zshenv"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(zshenv), ".local/share/mise/shims") {
		t.Fatalf("zshenv still exposes all mise shims globally: %q", zshenv)
	}
	if !strings.Contains(string(zshenv), "userland/zsh/mise-env.zsh") {
		t.Fatalf("zshenv does not load the static global tool environment: %q", zshenv)
	}
	starship, err := os.ReadFile(filepath.Join(root, "cfg", "xdg", "starship", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(starship), "[gcloud]\ndetect_env_vars = ['CLOUDSDK_ROOT_DIR']") {
		t.Fatalf("Starship gcloud module is not scoped to the realm tool environment: %q", starship)
	}
}

// An existing cache must not prevent sync from installing a newly pinned tool.
func TestPlanExistingShellCacheBeforeToolUpgrade(t *testing.T) {
	base := t.TempDir()
	cache := filepath.Join(base, "cache")
	if err := os.MkdirAll(filepath.Join(cache, "zsh"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"init.zsh", "mise-env.zsh"} {
		if err := os.WriteFile(filepath.Join(cache, "zsh", name), []byte("old cache\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mise := filepath.Join(base, "mise")
	if err := os.WriteFile(mise, []byte("#!/bin/sh\necho 'mise ERROR Tool not installed: aqua:anthropics/claude-code@2.1.280' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{"USERLAND_ROOT=" + base, "USERLAND_CACHE_DIR=" + cache, "USERLAND_MISE=" + mise})
	original := registry
	t.Cleanup(func() { registry = original })
	registry = []adapter{{name: "shell-cache", label: "Shell cache", area: plan.AreaFS, action: "update", attention: plan.Blocked, run: shellCache}}
	value := plan.New()
	result := Run(context.Background(), env, Plan, nil, false, value)
	if result.Code != 0 {
		t.Fatalf("personal state inspection failed (exit %d): %#v", result.Code, result.Events)
	}
	if len(value.Items()) != 1 || value.Items()[0].Handling != plan.Automatic {
		t.Fatalf("expected cache rebuild in plan: %#v", value.Items())
	}
	for _, name := range []string{"init.zsh", "mise-env.zsh"} {
		data, err := os.ReadFile(filepath.Join(cache, "zsh", name))
		if err != nil || string(data) != "old cache\n" {
			t.Fatalf("planning changed cache: %q %v", data, err)
		}
	}
	for _, action := range []Action{Doctor, Apply} {
		c := &Context{Context: context.Background(), Env: env}
		if code := shellCache(c, action); code == 0 {
			t.Fatalf("%v concealed unavailable tool environment", action)
		}
	}
}
