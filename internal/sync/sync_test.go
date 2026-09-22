package sync

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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

	code := miseTask(context.Background(), env, render, &output, runLog, nil,
		"Upgrade installed rolling packages",
		[]string{"ffmpeg", "yazi"},
		"bootstrap", "packages", "upgrade", "--yes", "brew:ffmpeg", "brew:yazi",
	)

	if code != 0 {
		t.Fatalf("miseTask returned %d", code)
	}
	if expected := " · 2/2 · yazi"; !bytes.Contains([]byte(output.String()), []byte(expected)) {
		t.Fatalf("upgrade progress omitted %q: %q", expected, output.String())
	}
}

func TestMiseToolTaskHidesInstallerOutputAndReportsEachTool(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	mise := filepath.Join(root, "mise")
	script := `#!/bin/sh
printf '%s\n' 'mise azure-cli@2.89.1 [1/3] install'
printf '%s\n' 'Downloading noisy vendor archive'
printf '%s\n' 'mise coder@2.36.3 [1/3] install'
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
	code := miseTask(context.Background(), env, render, &output, runLog, nil,
		"Install pinned development tools", []string{"azure-cli", "coder"}, "install", "--yes")
	if code != 0 {
		t.Fatalf("miseTask returned %d", code)
	}
	visible := output.String()
	if !strings.Contains(visible, " · 2/2 · coder") {
		t.Fatalf("tool progress omitted final item: %q", visible)
	}
	if strings.Contains(visible, "Downloading noisy vendor archive") {
		t.Fatalf("tool installer noise leaked into the CLI: %q", visible)
	}
	log, err := os.ReadFile(runLog)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(log, []byte("Downloading noisy vendor archive")) {
		t.Fatalf("hidden installer output was not retained in the diagnostic log: %q", log)
	}
}

func TestMiseTaskForwardsInteractiveInput(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	captured := filepath.Join(root, "stdin")
	mise := filepath.Join(root, "mise")
	script := `#!/bin/sh
IFS= read -r line
printf '%s\n' "$line" >"$MISE_STDIN_CAPTURE"
`
	if err := os.WriteFile(mise, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	environ := []string{
		"USERLAND_ROOT=" + root,
		"USERLAND_HOME=" + root,
		"USERLAND_STATE_DIR=" + state,
		"USERLAND_MISE=" + mise,
		"MISE_STDIN_CAPTURE=" + captured,
		"USERLAND_UI_MODE=plain",
	}
	env := platform.NewEnvironment(environ)
	var output bytes.Buffer
	render := tui.New(&output, environ)
	runLog := filepath.Join(state, "last-run.log")
	if err := os.WriteFile(runLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := miseTask(context.Background(), env, render, &output, runLog, strings.NewReader("sudo-password\n"), "Install packages", nil, "bootstrap", "packages", "apply"); code != 0 {
		t.Fatalf("miseTask returned %d", code)
	}
	value, err := os.ReadFile(captured)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(value)) != "sudo-password" {
		t.Fatalf("Mise did not receive interactive input: %q", value)
	}
}

func TestPackageTaskInputDisablesPromptsWhenNoTTYRequested(t *testing.T) {
	env := platform.NewEnvironment([]string{"USERLAND_UNAME=Darwin", "USERLAND_NO_TTY=1"})
	input, closeInput := packageTaskInput(env, strings.NewReader("password\n"))
	defer closeInput()
	if input != nil {
		t.Fatal("packageTaskInput returned interactive input with USERLAND_NO_TTY=1")
	}
}

func TestNativeTaskDoesNotReportCancellationAsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	environ := []string{"USERLAND_UI_MODE=plain"}
	var output bytes.Buffer
	render := tui.New(&output, environ)
	if code := nativeTask(ctx, render, "Homebrew applications", func() int { return -1 }); code != 130 {
		t.Fatalf("nativeTask returned %d, want cancellation 130", code)
	}
	if strings.Contains(output.String(), "failed") {
		t.Fatalf("cancellation was rendered as failure: %q", output.String())
	}
}
