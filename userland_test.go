package userland

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRealmCommandRequiresExplicitAddOrRemoveArguments(t *testing.T) {
	tests := []struct {
		args    []string
		message string
	}{
		{[]string{"realm"}, "realm expects add or remove"},
		{[]string{"realm", "add", "repository-only"}, "realm add expects a repository and mount path"},
		{[]string{"realm", "remove"}, "realm remove expects a name or mount path"},
		{[]string{"realm", "unknown"}, "realm expects add or remove"},
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
		"realm add <repository> <path>",
		"realm remove <name-or-path>",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("help omitted %q: %q", expected, stdout.String())
		}
	}
}
