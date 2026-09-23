package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWizardTerminalHelper(t *testing.T) {
	if os.Getenv("USERLAND_WIZARD_PTY_HELPER") != "1" {
		return
	}
	w := Wizard{Render: New(os.Stdout, []string{"USERLAND_UI_MODE=plain"}), Input: os.Stdin}
	answer, code := w.InputLine("Ready")
	fmt.Printf("RESULT:%d:%s\n", code, answer)
	os.Exit(0)
}

func TestWizardRealTerminalRestoresModeAndColumn(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	script := `
import os, pty, select, subprocess, sys, termios, time
for value, expected in [(b'yes\r', b'RESULT:0:yes'), (b'\x03', b'RESULT:130:'), (b'\x04', b'RESULT:3:')]:
    master, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    proc = subprocess.Popen([sys.argv[1], '-test.run=^TestWizardTerminalHelper$'], stdin=slave, stdout=slave, stderr=slave, env=dict(os.environ, USERLAND_WIZARD_PTY_HELPER='1'))
    data = b''
    try:
        deadline = time.monotonic() + 3
        while b'Ready: ' not in data:
            assert time.monotonic() < deadline, 'prompt not displayed'
            if select.select([master], [], [], .05)[0]: data += os.read(master, 4096)
        os.write(master, value)
        while expected not in data:
            assert time.monotonic() < deadline, repr(data)
            if select.select([master], [], [], .05)[0]: data += os.read(master, 4096)
        proc.wait(timeout=2)
        assert b'\r\n' + expected in data, ('next line did not return to column zero', data)
        assert termios.tcgetattr(slave) == before, 'terminal mode was not restored'
    finally:
        if proc.poll() is None: proc.kill(); proc.wait()
        os.close(master); os.close(slave)
`
	cmd := exec.CommandContext(ctx, python, "-c", script, binary)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("PTY regression: %v\n%s", err, output)
	}
}
