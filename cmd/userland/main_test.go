package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeEnvironmentFindsCanonicalCheckoutForStandaloneCommand(t *testing.T) {
	home := t.TempDir()
	checkout := filepath.Join(home, ".userland")
	if err := os.MkdirAll(filepath.Join(checkout, "cfg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "cfg", "schema-version"), []byte("2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(home, ".local", "bin", "userland")

	values := environmentValues(runtimeEnvironmentFor([]string{"HOME=" + home, "PATH=/usr/bin:/bin"}, executable))
	if values["USERLAND_ROOT"] != checkout {
		t.Fatalf("USERLAND_ROOT = %q, want canonical checkout %q", values["USERLAND_ROOT"], checkout)
	}
}

func environmentValues(environ []string) map[string]string {
	values := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}
