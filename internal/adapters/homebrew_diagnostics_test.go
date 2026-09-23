package adapters

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giacomoguidotto/userland/internal/platform"
)

type brewTestWriter func([]byte) (int, error)

func (f brewTestWriter) Write(b []byte) (int, error) { return f(b) }

func TestBrewOutputTracksNestedPhasesWithoutCountingStartsAsDone(t *testing.T) {
	var detail string
	var completed int
	c := &Context{Progress: func(current, total int, message string) { completed, detail = current, message }}
	p := newBrewProgress(c, []brewIssue{{"missing", "Cask", "helium-browser"}})
	observer := newBrewOutputProgress(p, map[string]bool{"helium-browser": true})
	for _, test := range []struct{ line, phase string }{
		{"Installing helium-browser cask. It is not currently installed.\n", "installing"},
		{"==> Downloading https://example.test/download.dmg\n", "downloading"},
		{"==> Installing Cask helium-browser\n", "installing"},
		{"==> Moving App 'Helium.app' to '/Applications/Helium.app'\n", "moving application"},
	} {
		// Subprocess output may split a line at any byte.
		for _, b := range []byte(test.line) {
			_, _ = observer.Write([]byte{b})
		}
		if completed != 0 || !strings.Contains(detail, "helium-browser · "+test.phase) {
			t.Fatalf("line %q: completed=%d detail=%q; want an active phase, not completion", test.line, completed, detail)
		}
	}
	p.Report("helium-browser")
	if completed != 1 {
		t.Fatalf("successful bundle was not counted: %d", completed)
	}
}

func TestBrewMutationLogsBeforeProcessExitsAndRetainsFailure(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "continue")
	brew := filepath.Join(root, "brew")
	script := fmt.Sprintf("#!/bin/sh\nprintf '==> Downloading archive\\n'\nwhile [ ! -e '%s' ]; do sleep 0.01; done\nprintf 'Error: download failed\\n' >&2\nexit 7\n", release)
	if err := os.WriteFile(brew, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c := &Context{Context: ctx, Env: platform.NewEnvironment([]string{"USERLAND_STATE_DIR=" + root, "PATH=/usr/bin:/bin"}), Output: io.Discard}
	var during string
	observer := brewTestWriter(func(b []byte) (int, error) {
		if strings.Contains(string(b), "Downloading") {
			contents, _ := os.ReadFile(filepath.Join(root, "last-run.log"))
			during = string(contents)
			_ = os.WriteFile(release, nil, 0600)
		}
		return len(b), nil
	})
	result := brewRunObserved(c, observer, brew, "bundle", "--verbose")
	if result.Code != 7 {
		t.Fatalf("exit=%d err=%v", result.Code, result.Err)
	}
	if !strings.Contains(during, "Downloading archive") || !strings.Contains(during, "bundle") {
		t.Fatalf("log unavailable while child was running: %q", during)
	}
	contents, err := os.ReadFile(filepath.Join(root, "last-run.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Error: download failed", "exit: 7"} {
		if !strings.Contains(string(contents), want) {
			t.Errorf("log missing %q: %s", want, contents)
		}
	}
	if len(c.Events) == 0 || !strings.Contains(c.Events[0].Message, "download failed") {
		t.Fatalf("failure reason lost: %#v", c.Events)
	}
}

func TestBrewSilenceReportsWaitingWithoutCompletingItem(t *testing.T) {
	var detail string
	var count int
	c := &Context{Progress: func(current, total int, message string) { count, detail = current, message }}
	p := newBrewProgress(c, []brewIssue{{"missing", "Cask", "helium-browser"}})
	observer := newBrewOutputProgress(p, map[string]bool{"helium-browser": true})
	now := time.Now()
	w := &brewActivity{log: io.Discard, observer: observer}
	_, _ = w.Write([]byte("Installing helium-browser cask. It is not currently installed.\n"))
	w.last = now
	w.reportSilence(now.Add(65 * time.Second))
	if count != 0 || !strings.Contains(detail, "helium-browser · no output for 1m5s") {
		t.Fatalf("silence: count=%d detail=%q", count, detail)
	}
	_, _ = w.Write([]byte("==> Downloading https://example.test/archive.dmg\n"))
	if !strings.Contains(detail, "helium-browser · downloading") {
		t.Fatalf("activity did not resume: %q", detail)
	}
}

func TestBrewActivitySurfacesNestedSudoPrompt(t *testing.T) {
	var prompt strings.Builder
	w := &brewActivity{log: io.Discard, interactive: &prompt, terminal: true}
	_, _ = w.Write([]byte("[sudo] pass"))
	_, _ = w.Write([]byte("word for giacomo: "))
	if !strings.Contains(prompt.String(), "[sudo] password for giacomo:") {
		t.Fatalf("nested sudo prompt was hidden: %q", prompt.String())
	}
}
