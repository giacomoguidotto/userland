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

type installOnContinue struct {
	path string
	done bool
}

func (r *installOnContinue) Read(p []byte) (int, error) {
	if !r.done {
		if err := os.MkdirAll(r.path, 0700); err != nil {
			return 0, err
		}
		r.done = true
	}
	return strings.NewReader("\n").Read(p)
}

func TestBrowserExtensionWaitsAndRechecksInstallation(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "not installed", true: "installed"}[installed], func(t *testing.T) {
			base := t.TempDir()
			for _, dir := range []string{"cfg", "bin", "Applications/Helium.app"} {
				if err := os.MkdirAll(filepath.Join(base, dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(base, "cfg/browser-extensions.csv"), []byte("browser,extension_id,name\nhelium,abc,Raycast Companion\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(base, "bin/open"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			c := &Context{Context: context.Background(), Env: platform.NewEnvironment([]string{"USERLAND_ROOT=" + base, "USERLAND_HOME=" + base, "USERLAND_UNAME=Darwin", "PATH=" + filepath.Join(base, "bin") + ":/usr/bin:/bin", "USERLAND_UI_MODE=plain"}), Output: &output, Terminal: true, Stdin: strings.NewReader("\n")}
			if installed {
				c.Stdin = &installOnContinue{path: filepath.Join(base, "Library/Application Support/net.imput.helium/Default/Extensions/abc")}
			}
			code := browserExtensions(c, Apply)
			want := 2
			if installed {
				want = 0
			}
			if code != want {
				t.Fatalf("got %d want %d: %s %#v", code, want, output.String(), c.Events)
			}
			if !strings.Contains(output.String(), "Add to Chrome") || !strings.Contains(output.String(), "Check installation") {
				t.Fatalf("missing attended instructions: %s", output.String())
			}
		})
	}
}

func TestBrowserExtensionDoesNotOpenHeliumBeforeInstallation(t *testing.T) {
	base := t.TempDir()
	for _, dir := range []string{"cfg", "bin"} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "cfg/browser-extensions.csv"), []byte("browser,extension_id,name\nhelium,abc,Raycast Companion\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(base, "calls")
	if err := os.WriteFile(filepath.Join(base, "bin/open"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >>"+shellSingleQuote(calls)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	c := &Context{
		Context: context.Background(),
		Env:     platform.NewEnvironment([]string{"USERLAND_ROOT=" + base, "USERLAND_HOME=" + base, "USERLAND_UNAME=Darwin", "PATH=" + filepath.Join(base, "bin") + ":/usr/bin:/bin", "USERLAND_UI_MODE=plain"}),
		Output:  &output, Stdin: strings.NewReader("\n"), Terminal: true,
	}
	if code := browserExtensions(c, Apply); code != 2 {
		t.Fatalf("got %d want dependency failure: %s %#v", code, output.String(), c.Events)
	}
	if opened, err := os.ReadFile(calls); err == nil && len(opened) != 0 {
		t.Fatalf("opened Helium before it was installed: %s", opened)
	}
	found := false
	for _, event := range c.Events {
		if strings.Contains(event.Message, "waiting for Homebrew") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing dependency message: %s %#v", output.String(), c.Events)
	}
}
