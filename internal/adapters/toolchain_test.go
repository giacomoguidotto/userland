package adapters

import (
	"path/filepath"
	"runtime"
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
