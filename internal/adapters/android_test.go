package adapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestAndroidInstallsMissingPackagesSeriallyWithProgress(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	java := filepath.Join(base, "java")
	if err := os.MkdirAll(filepath.Join(java, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(java, "bin", "java"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mise := filepath.Join(base, "mise")
	if err := os.WriteFile(mise, []byte("#!/bin/sh\nprintf '%s\\n' "+java+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(base, "sdkmanager.calls")
	sdkmanager := filepath.Join(base, "sdkmanager")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >>%q\nexit 0\n", trace)
	if err := os.WriteFile(sdkmanager, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{
		"USERLAND_HOME=" + home,
		"USERLAND_UNAME=Darwin",
		"USERLAND_MISE=" + mise,
		"USERLAND_SDKMANAGER=" + sdkmanager,
		"ANDROID_HOME=" + filepath.Join(home, "Library", "Android", "sdk"),
		"PATH=/usr/bin:/bin",
	})
	var progress []string
	invocation := &Context{
		Context: context.Background(), Env: env, Terminal: true, Stdin: strings.NewReader("y\n"),
		Progress: func(current, total int, detail string) {
			progress = append(progress, fmt.Sprintf("%d/%d:%s", current, total, detail))
		},
	}
	if code := androidSDK(invocation, Apply); code != 0 {
		t.Fatalf("android apply returned %d", code)
	}
	wantProgress := []string{
		"1/4:platform-tools",
		"2/4:emulator",
		"3/4:platforms;android-36",
		"4/4:build-tools;36.0.0",
	}
	if strings.Join(progress, "\n") != strings.Join(wantProgress, "\n") {
		t.Fatalf("Android progress = %#v, want %#v", progress, wantProgress)
	}
	calls, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, sdkPackage := range androidPackages {
		if !containsLine(string(calls), sdkPackage) {
			t.Fatalf("Android package %q was not installed separately:\n%s", sdkPackage, calls)
		}
	}
}
