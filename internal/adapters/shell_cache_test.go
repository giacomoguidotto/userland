package adapters

import (
	"context"
	"os"
	"os/exec"
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
	if strings.Contains(string(zshenv), "_userland_load_interactive") || strings.Contains(string(zshenv), "precmd") {
		t.Fatalf("zshenv contains interactive hook logic: %q", zshenv)
	}
	zshrc, err := os.ReadFile(filepath.Join(root, "cfg", "home", "zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`eval "$(starship init zsh)"`, `eval "$(atuin init zsh)"`} {
		if !strings.Contains(string(zshrc), fragment) {
			t.Fatalf("zshrc is missing %q: %q", fragment, zshrc)
		}
	}
	starship, err := os.ReadFile(filepath.Join(root, "cfg", "xdg", "starship", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(starship), "[gcloud]\ndetect_env_vars = ['CLOUDSDK_ROOT_DIR']") {
		t.Fatalf("Starship gcloud module is not scoped to the realm tool environment: %q", starship)
	}
}

func TestInteractiveStartupLoadsToolsBeforeFirstPrompt(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	config := filepath.Join(base, "config")
	cache := filepath.Join(base, "cache")
	bin := filepath.Join(base, "bin")
	for _, directory := range []string{home, filepath.Join(config, "userland"), filepath.Join(cache, "userland", "zsh"), bin} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"starship", "atuin", "fzf", "zoxide", "mise"} {
		script := "#!/bin/sh\n"
		switch tool {
		case "starship":
			script += `printf "PROMPT='STARSHIP_READY'\n"\n`
		case "atuin":
			script += `printf "_atuin_search() { :; }\n"\n`
		case "fzf":
			script += `printf "typeset -g FZF_READY=1\n"\n`
		case "zoxide":
			script += `printf "__zoxide_z() { :; }\n"\n`
		case "mise":
			script += `printf "typeset -g MISE_READY=1\n"\n`
		}
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cacheContents := "# userland-shell-cache: test\nexport PATH='" + bin + ":/usr/bin:/bin'\ntypeset -gU path PATH\npath=( '" + bin + "' /usr/bin /bin $path )\nexport PATH\n"
	if err := os.WriteFile(filepath.Join(cache, "userland/zsh/mise-env.zsh"), []byte(cacheContents), 0o600); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	zshenv, err := os.ReadFile(filepath.Join(root, "cfg/home/zshenv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zshenv"), zshenv, 0o600); err != nil {
		t.Fatal(err)
	}
	fragment, err := os.ReadFile(filepath.Join(root, "cfg/home/zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "userland/zshrc"), fragment, 0o600); err != nil {
		t.Fatal(err)
	}
	userZshrc := `PROMPT='MACOS_DEFAULT'
if [ -r "${XDG_CONFIG_HOME:-$HOME/.config}/userland/zshrc" ]; then source "${XDG_CONFIG_HOME:-$HOME/.config}/userland/zshrc"; fi
print -r -- "$PROMPT|$FZF_READY|$MISE_READY|$commands[starship]|$+functions[__zoxide_z]"
exit
`
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(userZshrc), 0o600); err != nil {
		t.Fatal(err)
	}
	environ := []string{"HOME=" + home, "XDG_CONFIG_HOME=" + config, "XDG_CACHE_HOME=" + cache, "PATH=" + bin + ":/usr/bin:/bin", "TERM=xterm-256color", "USER=tester", "LOGNAME=tester"}
	command := exec.Command(zsh, "-i", "-c", ":")
	command.Env = environ
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("interactive zsh failed: %v\n%s", err, output)
	}
	line := strings.TrimSpace(string(output))
	if !strings.Contains(line, "STARSHIP_READY|1|1|") || !strings.HasSuffix(line, "|1") {
		t.Fatalf("interactive tools were not loaded before the prompt: %q", line)
	}
	nonInteractive := exec.Command(zsh, "-c", `print -r -- "${PROMPT:-unset}|${FZF_READY:-0}"`)
	nonInteractive.Env = environ
	output, err = nonInteractive.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != "unset|0" {
		t.Fatalf("non-interactive shell loaded interactive setup: %q", output)
	}
}

// An existing cache must not prevent sync from installing a newly pinned tool.
func TestPlanExistingShellCacheBeforeToolUpgrade(t *testing.T) {
	base := t.TempDir()
	cache := filepath.Join(base, "cache")
	if err := os.MkdirAll(filepath.Join(cache, "zsh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "zsh", "mise-env.zsh"), []byte("old cache\n"), 0600); err != nil {
		t.Fatal(err)
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
	data, err := os.ReadFile(filepath.Join(cache, "zsh", "mise-env.zsh"))
	if err != nil || string(data) != "old cache\n" {
		t.Fatalf("planning changed cache: %q %v", data, err)
	}
	for _, action := range []Action{Doctor, Apply} {
		c := &Context{Context: context.Background(), Env: env}
		if code := shellCache(c, action); code == 0 {
			t.Fatalf("%v concealed unavailable tool environment", action)
		}
	}
}
