package nuke

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNukeDryRunPreservesHomeContents(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "dev"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".secret"), []byte("keep for dry run"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runNukeForAccount(context.Background(), invocation{
		Args: []string{"nuke", "--dry-run"}, Environ: []string{"HOME=" + home, "USERLAND_HOME=" + home, "USERLAND_UI_MODE=plain"},
		Stdout: &stdout, Stderr: &stderr,
	}, home, os.Getuid())
	if code != ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("dry run returned %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".secret")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "would remove ~/dev") || !strings.Contains(stdout.String(), "Dry run complete") {
		t.Fatalf("dry run omitted preview: %q", stdout.String())
	}
}

func TestNukeYesRemovesHiddenAndVisibleEntries(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"dev", ".config", "notes.txt"} {
		path := filepath.Join(home, name)
		if strings.Contains(name, ".") && !strings.HasPrefix(name, ".") {
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "credential"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	code := runNukeForAccount(context.Background(), invocation{
		Args: []string{"nuke", "--yes"}, Environ: []string{"HOME=" + home, "USERLAND_HOME=" + home, "USERLAND_UI_MODE=plain"},
		Stdout: &output, Stderr: &output,
	}, home, os.Getuid())
	if code != ExitSuccess {
		t.Fatalf("nuke returned %d: %q", code, output.String())
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(nukeFolders) {
		t.Fatalf("unexpected entries: %v", entries)
	}
	for _, entry := range entries {
		children, err := os.ReadDir(filepath.Join(home, entry.Name()))
		if err != nil || len(children) != 0 {
			t.Fatalf("folder is not empty: %s %v", entry.Name(), err)
		}
	}
}

func TestNukeRejectsFilesystemRoot(t *testing.T) {
	if _, err := safeNukeHome("/", nil, os.Getuid()); err == nil {
		t.Fatal("accepted filesystem root")
	}
}
