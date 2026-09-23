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

func TestRaycastRequiresConfirmationAndSkipsCompletedImport(t *testing.T) {
	base := t.TempDir()
	for _, dir := range []string{"cfg", "state", "Applications/Raycast.app", "bin"} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	export := filepath.Join(base, "cfg/raycast.rayconfig")
	contents, err := os.ReadFile("../../cfg/raycast.rayconfig")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(export, contents, 0600); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(base, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >>" + shellSingleQuote(calls) + "\n"
	if err := os.WriteFile(filepath.Join(base, "bin/open"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	env := platform.NewEnvironment([]string{"USERLAND_ROOT=" + base, "USERLAND_HOME=" + base, "USERLAND_STATE_DIR=" + filepath.Join(base, "state"), "USERLAND_UNAME=Darwin", "PATH=" + filepath.Join(base, "bin") + ":/usr/bin:/bin", "USERLAND_UI_MODE=plain", "USERLAND_YES=1"})
	c := &Context{Context: context.Background(), Env: env, Output: &output, Stdin: strings.NewReader(""), Terminal: true}
	if code := raycast(c, Apply); code != 3 {
		t.Fatalf("EOF should pause import: %d", code)
	}
	receipt := filepath.Join(base, "state/receipts/raycast-import.sha256")
	if _, err := os.Stat(receipt); !os.IsNotExist(err) {
		t.Fatal("unconfirmed import recorded")
	}
	opened, _ := os.ReadFile(calls)
	if strings.Contains(string(opened), export) {
		t.Fatal("import opened before onboarding was completed")
	}
	if !strings.Contains(output.String(), "onboarding") {
		t.Fatalf("instructions missing before prompt: %s", output.String())
	}
	c.Stdin = strings.NewReader("yes\nyes\n")
	if code := raycast(c, Apply); code != 0 {
		t.Fatalf("confirmed import: %d %#v", code, c.Events)
	}
	if _, err := os.Stat(receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(calls); err != nil {
		t.Fatal(err)
	}
	c.Stdin = nil
	if code := raycast(c, Apply); code != 0 {
		t.Fatalf("resume: %d", code)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatalf("sync launched Raycast instead of relying on its login item")
	}
}
