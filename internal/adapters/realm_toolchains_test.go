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

func TestRealmToolchainsInstallBeforeAuthenticationWithProgress(t *testing.T) {
	base := t.TempDir()
	root, home := filepath.Join(base, "root"), filepath.Join(base, "home")
	state, cache := filepath.Join(base, "state"), filepath.Join(base, "cache")
	realmRoot := filepath.Join(base, "danfoss")
	for _, directory := range []string{filepath.Join(root, "cfg"), state, cache, realmRoot} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "cfg", "realms.csv"), []byte(
		"name,repository,configuration_path,default_path,branch,mode\n"+
			"danfoss,git@example.test:private/danfoss.git,"+realmRoot+","+realmRoot+",main,optional\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "realms.csv"), []byte("name,path\ndanfoss,"+realmRoot+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realmRoot, "mise.toml"), []byte("[tools]\nazure-cli = \"latest\"\ncoder = \"latest\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mise := filepath.Join(base, "mise")
	script := `#!/bin/sh
case "$*" in
  *'ls --missing --json') printf '%s\n' '{"azure-cli": [{}], "coder": [{}]}' ;;
  *'install --yes')
    printf '%s\n' 'mise azure-cli@2.89.1 [1/3] install'
    printf '%s\n' 'mise coder@2.36.3 [1/3] install'
    ;;
esac
`
	if err := os.WriteFile(mise, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{
		"USERLAND_ROOT=" + root, "USERLAND_HOME=" + home, "USERLAND_CACHE_DIR=" + cache, "USERLAND_STATE_DIR=" + state,
		"USERLAND_MISE=" + mise, "PATH=/usr/bin:/bin",
	})
	value := plan.New()
	if code := realmToolchains(&Context{Context: context.Background(), Env: env, Plan: value}, Plan); code != 0 {
		t.Fatalf("plan returned %d", code)
	}
	assertPlanItem(t, value.Items(), "azure-cli", "install the danfoss realm tool")
	assertPlanItem(t, value.Items(), "coder", "install the danfoss realm tool")
	var progress []string
	apply := &Context{Context: context.Background(), Env: env, Progress: func(current, total int, detail string) {
		progress = append(progress, fmt.Sprintf("%d/%d:%s", current, total, detail))
	}}
	if code := realmToolchains(apply, Apply); code != 0 {
		t.Fatalf("apply returned %d", code)
	}
	expected := "1/2:danfoss · azure-cli\n2/2:danfoss · coder"
	if strings.Join(progress, "\n") != expected {
		t.Fatalf("realm tool progress = %q, want %q", strings.Join(progress, "\n"), expected)
	}
}

func TestRealmConvergencePreparesApplicationsAndToolsBeforeAuthentication(t *testing.T) {
	positions := make(map[string]int)
	for index, label := range Labels() {
		positions[label] = index
	}
	for _, prerequisite := range []string{"Realms", "Realm applications", "Realm toolchains"} {
		if positions[prerequisite] >= positions["Realm authentication"] {
			t.Fatalf("%s runs after realm authentication: %#v", prerequisite, positions)
		}
	}
}
