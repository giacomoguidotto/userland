package tui

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBannerUsesInstalledVersionInsteadOfInheritedVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".userland-stage-version"), []byte("v0.10.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	render := New(io.Discard, []string{"USERLAND_ROOT=" + root, "USERLAND_VERSION=v0.10.21"})
	if got := render.version(); got != "v0.10.26" {
		t.Fatalf("banner version = %q, want installed v0.10.26", got)
	}
}
