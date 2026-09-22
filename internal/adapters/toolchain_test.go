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

func TestShippedToolProbesCoverPinnedUserTools(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	env := platform.NewEnvironment([]string{"USERLAND_ROOT=" + root})
	probes, complete := toolProbes(&Context{Env: env})
	if !complete {
		declared, err := declaredTools(filepath.Join(root, "cfg", "mise.toml"))
		if err != nil {
			t.Fatal(err)
		}
		probed := make([]string, 0, len(probes))
		for _, probe := range probes {
			probed = append(probed, probe.id)
		}
		t.Fatalf("shipped probe manifest does not match pinned user tools: declared=%v probed=%v", declared, probed)
	}
}

func TestShippedConfigDoesNotInstallDockerByDefault(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	contents, err := os.ReadFile(filepath.Join(root, "cfg", "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "docker") || strings.Contains(string(contents), "colima") {
		t.Fatalf("default mise config still contains Docker tooling: %q", contents)
	}
	if _, err := os.Stat(filepath.Join(root, "cfg", "docker")); !os.IsNotExist(err) {
		t.Fatalf("Docker configuration is still shipped")
	}
}

func TestToolProbeDistinguishesMissingFromBroken(t *testing.T) {
	base := t.TempDir()
	mise := filepath.Join(base, "mise")
	script := `#!/bin/sh
case "$*" in
  *where\ missing*) exit 1 ;;
  *where\ broken*|*where\ ready*) printf '%s\n' "$0"; exit 0 ;;
  *exec\ --\ broken*) exit 1 ;;
  *exec\ --\ ready*)
    [ "${MISE_AUTO_INSTALL:-}" = 0 ] || exit 1
    [ "${MISE_EXEC_AUTO_INSTALL:-}" = 0 ] || exit 1
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(mise, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{
		"USERLAND_ROOT=" + base,
		"USERLAND_HOME=" + filepath.Join(base, "home"),
		"USERLAND_MISE=" + mise,
		"PATH=/usr/bin:/bin",
	})
	c := &Context{Context: context.Background(), Env: env}
	for _, test := range []struct {
		id    string
		state toolProbeState
	}{
		{id: "missing", state: toolMissing},
		{id: "broken", state: toolBroken},
		{id: "ready", state: toolReady},
	} {
		result := env.RunMise(context.Background(), nil, "where", test.id)
		if state := probeState(c, toolProbe{id: test.id, command: test.id}); state != test.state {
			t.Fatalf("probe %s state = %q, want %q (where code=%d output=%q)", test.id, state, test.state, result.Code, result.Output)
		}
	}
}

func TestToolProbePlatformRestrictions(t *testing.T) {
	probe := toolProbe{id: "aqua:aristocratos/btop", command: "btop", platforms: []string{"linux"}}
	linux := &Context{Env: platform.NewEnvironment([]string{"USERLAND_UNAME=Linux"})}
	mac := &Context{Env: platform.NewEnvironment([]string{"USERLAND_UNAME=Darwin"})}
	if !probeEnabled(linux, probe) {
		t.Fatal("Linux-only probe was disabled on Linux")
	}
	if probeEnabled(mac, probe) {
		t.Fatal("Linux-only probe was enabled on macOS")
	}
}
