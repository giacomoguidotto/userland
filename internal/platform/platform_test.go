package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestRunInteractivePTYHelper(t *testing.T) {
	if os.Getenv("USERLAND_PLATFORM_PTY_HELPER") != "1" {
		return
	}
	result := RunInteractive(context.Background(), []string{"PATH=/bin:/usr/bin"}, "/bin/sh", "-c", "test -t 0 && ! test -t 1 && ! test -t 2 && printf 'stdout\\n' && printf 'stderr\\n' >&2")
	if string(result.Output) != "stdout\nstderr\n" {
		fmt.Fprintf(os.Stdout, "OUTPUT:%q\n", result.Output)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "RESULT:%d\n", result.Code)
	os.Exit(result.Code)
}

func TestRunInteractiveAttachesControllingTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("controlling terminals use /dev/tty")
	}
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
import fcntl, os, pty, select, subprocess, sys, termios, time
master, slave = pty.openpty()
def controlling_terminal():
    os.setsid()
    fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
proc = subprocess.Popen([sys.argv[1], '-test.run=^TestRunInteractivePTYHelper$'], stdin=slave, stdout=slave, stderr=slave,
                        env=dict(os.environ, USERLAND_PLATFORM_PTY_HELPER='1'), preexec_fn=controlling_terminal)
data = b''
try:
    deadline = time.monotonic() + 5
    while b'RESULT:' not in data:
        assert time.monotonic() < deadline, repr(data)
        if select.select([master], [], [], .05)[0]: data += os.read(master, 4096)
    proc.wait(timeout=2)
    assert b'RESULT:0' in data, data
finally:
    if proc.poll() is None: proc.kill(); proc.wait()
    os.close(master); os.close(slave)
`
	cmd := exec.CommandContext(ctx, python, "-c", script, binary)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("PTY regression: %v\n%s", err, output)
	}
}
