package sync

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	stdsync "sync"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
	"github.com/giacomoguidotto/userland/internal/tui"
)

type lockedBuffer struct {
	mu stdsync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(value)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestMiseUpgradeTaskReportsCurrentPackageProgress(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	mise := filepath.Join(root, "mise")
	script := `#!/bin/sh
printf '%s\n' 'mise brew:ffmpeg download ffmpeg.tar.gz'
printf '%s\n' 'mise brew:ffmpeg ✓ 9.0.1'
printf '%s\n' 'mise brew:yazi download yazi.tar.gz'
sleep 0.15
printf '%s\n' 'mise brew:yazi ✓ 26.8.15'
`
	if err := os.WriteFile(mise, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	environ := []string{
		"USERLAND_ROOT=" + root,
		"USERLAND_HOME=" + root,
		"USERLAND_STATE_DIR=" + state,
		"USERLAND_MISE=" + mise,
		"USERLAND_UI_MODE=rich",
		"USERLAND_UNICODE=1",
		"CLICOLOR_FORCE=1",
		"TERM=xterm-256color",
	}
	env := platform.NewEnvironment(environ)
	var output lockedBuffer
	render := tui.New(&output, environ)
	runLog := filepath.Join(state, "last-run.log")
	if err := os.WriteFile(runLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	code := miseTask(context.Background(), env, render, &output, runLog,
		"Upgrade installed rolling packages",
		"bootstrap", "packages", "upgrade", "--yes", "brew:ffmpeg", "brew:yazi",
	)

	if code != 0 {
		t.Fatalf("miseTask returned %d", code)
	}
	if expected := " · 2/2 · yazi"; !bytes.Contains([]byte(output.String()), []byte(expected)) {
		t.Fatalf("upgrade progress omitted %q: %q", expected, output.String())
	}
}
