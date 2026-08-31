package adapters

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
