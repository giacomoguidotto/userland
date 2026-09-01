package doctor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHumanGroupsMachineChecksWithoutRepeatedSections(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	state := filepath.Join(root, "state")
	commit := strings.Repeat("a", 40)
	for _, directory := range []string{filepath.Join(root, "cfg"), home, state, filepath.Join(root, "cache"), filepath.Join(root, "data"), filepath.Join(root, "bin")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "cfg", "schema-version"), []byte("3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".userland-release"), []byte(commit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repositories := filepath.Join(root, "repositories.csv")
	if err := os.WriteFile(repositories, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	mise := filepath.Join(root, "bin", "mise")
	if err := os.WriteFile(mise, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	curl := filepath.Join(root, "bin", "curl")
	curlOutput := "#!/bin/sh\nprintf \"tag='v9.9.9'\\ncommit='" + commit + "'\\n\"\n"
	if err := os.WriteFile(curl, []byte(curlOutput), 0o755); err != nil {
		t.Fatal(err)
	}
	environ := []string{
		"USERLAND_ROOT=" + root,
		"USERLAND_HOME=" + home,
		"USERLAND_STATE_DIR=" + state,
		"USERLAND_CACHE_DIR=" + filepath.Join(root, "cache"),
		"USERLAND_DATA_DIR=" + filepath.Join(root, "data"),
		"USERLAND_REPOSITORIES=" + repositories,
		"USERLAND_MISE=" + mise,
		"USERLAND_CURL=" + curl,
		"USERLAND_TESTING=1",
		"USERLAND_UNAME=Linux",
		"USERLAND_UI_MODE=rich",
		"USERLAND_UNICODE=1",
		"CLICOLOR_FORCE=1",
		"TERM=xterm-256color",
	}
	var output bytes.Buffer

	Human(context.Background(), environ, &output, false)
	result := output.String()
	expectedOrder := []string{"  Userland", "v9.9.9 is current", "  System", "  Toolchain", "  Machine state", "  Personal state"}
	position := 0
	for _, expected := range expectedOrder {
		next := strings.Index(result[position:], expected)
		if next < 0 {
			t.Fatalf("doctor omitted %q after byte %d: %q", expected, position, result)
		}
		position += next + len(expected)
	}
	for _, repeated := range []string{"◆\x1b[0m  Toolchain", "◆\x1b[0m  Machine state"} {
		if strings.Contains(result, repeated) {
			t.Fatalf("doctor retained repeated section %q: %q", repeated, result)
		}
	}
}
