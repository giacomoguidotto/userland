package adapters

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestBrowserReadyRecordsAttendedHeliumSetupAndIsIdempotent(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	root := filepath.Join(base, "root")
	state := filepath.Join(base, "state")
	bin := filepath.Join(base, "bin")
	for _, directory := range []string{filepath.Join(home, "Applications", "Helium.app"), root, filepath.Join(state, "receipts"), bin} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	opened := filepath.Join(base, "opened")
	if err := os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >>"+shellSingleQuote(opened)+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{
		"HOME=" + home, "USERLAND_HOME=" + home, "USERLAND_ROOT=" + root,
		"USERLAND_STATE_DIR=" + state, "USERLAND_UNAME=Darwin", "PATH=" + bin,
		"USERLAND_UI_MODE=plain",
	})
	var output bytes.Buffer
	c := &Context{Context: context.Background(), Env: env, Stdin: strings.NewReader("\n"), Output: &output, Terminal: true}
	if code := browserReady(c, Apply); code != 0 {
		t.Fatalf("first browser setup returned %d: %#v", code, c.Events)
	}
	if !receiptReady(filepath.Join(state, "receipts", heliumReadyReceipt)) {
		t.Fatal("browser readiness receipt was not written")
	}
	if data, err := os.ReadFile(opened); err != nil || !strings.Contains(string(data), "-a Helium") {
		t.Fatalf("Helium was not opened for setup: %q %v", data, err)
	}
	output.Reset()
	c.Stdin = strings.NewReader("")
	if code := browserReady(c, Apply); code != 0 {
		t.Fatalf("second browser setup returned %d", code)
	}
	data, _ := os.ReadFile(opened)
	if strings.Count(string(data), "-a Helium") != 1 {
		t.Fatalf("completed browser setup reopened Helium: %q", data)
	}
}
