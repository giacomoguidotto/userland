package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestWizardKeepsPromptsInRendererAndDoesNotConsumeNextAnswer(t *testing.T) {
	var output bytes.Buffer
	w := Wizard{Render: New(&output, []string{"USERLAND_UI_MODE=rich", "USERLAND_UNICODE=1", "NO_COLOR=1", "USERLAND_YES=1"}), Input: strings.NewReader("\nyes\n")}
	w.Stage(1, 2, "Authentication")
	if confirmed, code := w.ConfirmDone("Import completed?"); confirmed || code != 0 {
		t.Fatalf("empty answer auto-confirmed: %v %d", confirmed, code)
	}
	if confirmed, code := w.ConfirmDone("Import completed?"); !confirmed || code != 0 {
		t.Fatalf("next answer lost: %v %d", confirmed, code)
	}
	if !strings.Contains(output.String(), "◆  1/2 · Authentication") || !strings.Contains(output.String(), "?  Import completed? [y/N] ›") {
		t.Fatalf("prompts left TUI: %q", output.String())
	}
}

func TestWizardStopsOnEOFOrCtrlC(t *testing.T) {
	for _, tc := range []struct {
		input string
		code  int
	}{{"", 3}, {"\x03", 130}} {
		var output bytes.Buffer
		w := Wizard{Render: New(&output, nil), Input: strings.NewReader(tc.input)}
		if confirmed, code := w.ConfirmDone("Done?"); confirmed || code != tc.code {
			t.Fatalf("got %v %d", confirmed, code)
		}
	}
}

func TestWizardConfirmDoneYesDefaultsToYes(t *testing.T) {
	var output bytes.Buffer
	w := Wizard{Render: New(&output, []string{"USERLAND_UI_MODE=plain"}), Input: strings.NewReader("\nn\n")}
	if confirmed, code := w.ConfirmDoneYes("Sign in?"); !confirmed || code != 0 {
		t.Fatalf("empty answer should confirm: %v %d", confirmed, code)
	}
	if confirmed, code := w.ConfirmDoneYes("Sign in?"); confirmed || code != 0 {
		t.Fatalf("n should decline: %v %d", confirmed, code)
	}
	if !strings.Contains(output.String(), "Sign in? [Y/n]: ") {
		t.Fatalf("missing default-yes prompt: %q", output.String())
	}
}

func TestWizardCommandOutputRendersDeviceInstructionsWithoutANSI(t *testing.T) {
	var output bytes.Buffer
	o := CommandOutput{Wizard: Wizard{Render: New(&output, []string{"USERLAND_UI_MODE=plain"})}}
	for _, chunk := range []string{"\x1b[31mCopy AB", "CD-1234\x1b[0m\n", "Visit https://example.test/login"} {
		_, _ = o.Write([]byte(chunk))
	}
	o.Flush()
	if got, want := output.String(), "[info] Copy ABCD-1234\n[info] Visit https://example.test/login\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
