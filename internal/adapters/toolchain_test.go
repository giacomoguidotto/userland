package adapters

import (
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

func TestShippedConfigMakesHomebrewDockerComposeDiscoverable(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	plugin := filepath.Join(root, "cfg", "docker", "cli-plugins", "docker-compose")
	target, err := os.Readlink(plugin)
	if err != nil {
		t.Fatalf("Docker Compose plugin is not a managed symlink: %v", err)
	}
	if expected := "/opt/homebrew/lib/docker/cli-plugins/docker-compose"; target != expected {
		t.Fatalf("Docker Compose plugin target = %q, want %q", target, expected)
	}
	contents, err := os.ReadFile(filepath.Join(root, "cfg", "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := `"~/.docker/cli-plugins" = { source = "docker/cli-plugins", mode = "symlink-each" }`
	if !strings.Contains(string(contents), declaration) {
		t.Fatalf("mise config does not manage Docker CLI plugins with %q", declaration)
	}
}
