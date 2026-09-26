package userland

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestRealmCommandRequiresExplicitArguments(t *testing.T) {
	tests := []struct {
		args    []string
		message string
	}{
		{[]string{"realm"}, "realm expects list, add, or remove"},
		{[]string{"realm", "add"}, "realm add expects a declared name, or a repository and mount path"},
		{[]string{"realm", "remove"}, "realm remove expects a name or mount path"},
		{[]string{"realm", "unknown"}, "realm expects list, add, or remove"},
	}
	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), Invocation{
			Args: test.args, Environ: []string{"USERLAND_UI_MODE=plain"},
			Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		})
		if code != ExitUsage || !strings.Contains(stderr.String(), test.message) {
			t.Fatalf("realm %v returned %d, stdout %q, stderr %q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestUsageIncludesRealmInterface(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Invocation{
		Args: []string{"help"}, Environ: []string{"USERLAND_UI_MODE=plain"},
		Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("help returned %d: %q", code, stderr.String())
	}
	for _, expected := range []string{
		"sync --non-interactive",
		"realm list",
		"realm add <name>",
		"realm add <repository> <path>",
		"realm remove <name-or-path>",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("help omitted %q: %q", expected, stdout.String())
		}
	}
}

func TestNonInteractiveSyncIsAccepted(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Invocation{
		Args: []string{"sync", "--non-interactive"}, Environ: []string{"USERLAND_ROOT=/missing", "USERLAND_UI_MODE=rich"},
		Stdin: strings.NewReader("should not be read"), Stdout: &stdout, Stderr: &stderr,
	})
	if code == ExitUsage {
		t.Fatalf("sync --non-interactive was rejected: %q", stderr.String())
	}
}

func TestNonInteractiveSyncDisablesEveryPromptChannel(t *testing.T) {
	env := platform.NewEnvironment(nonInteractiveEnvironment([]string{
		"USERLAND_ASSUME_YES=0",
		"USERLAND_NO_TTY=0",
		"USERLAND_UI_MODE=rich",
	}))
	for _, name := range []string{"USERLAND_NON_INTERACTIVE", "USERLAND_ASSUME_YES", "USERLAND_NO_TTY"} {
		if !env.Bool(name) {
			t.Fatalf("%s was not enabled", name)
		}
	}
	if env.Get("USERLAND_UI_MODE") != "plain" {
		t.Fatalf("automation UI mode = %q, want plain", env.Get("USERLAND_UI_MODE"))
	}
}
