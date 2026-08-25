package tui

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/giacomoguidotto/userland/internal/plan"
)

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(value)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func richEnvironment(values ...string) []string {
	return append([]string{
		"USERLAND_UI_MODE=rich",
		"USERLAND_UNICODE=1",
		"CLICOLOR_FORCE=1",
		"TERM=xterm-256color",
		"USERLAND_HOME=/Users/example",
	}, values...)
}

func TestRichTaskSpinnerAdvancesFrames(t *testing.T) {
	var output synchronizedBuffer
	renderer := New(&output, richEnvironment())

	renderer.BeginTask("Inspecting personal state")
	time.Sleep(270 * time.Millisecond)
	renderer.ClearTask()

	result := output.String()
	if !strings.Contains(result, "⠋") {
		t.Fatalf("spinner omitted its initial frame: %q", result)
	}
	if !strings.ContainsAny(result, "⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("spinner never advanced past its initial frame: %q", result)
	}
}

func TestRichPlanUsesSemanticColorAndCurrentHeadings(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, richEnvironment())
	value := plan.New()
	if err := value.Add(plan.Item{
		Area: plan.AreaApps, Action: "upgrade", Handling: plan.Automatic,
		Ownership: "declared", Target: "ffmpeg", Detail: "upgrade installed rolling package",
	}); err != nil {
		t.Fatal(err)
	}

	renderer.Plan(value, "/Users/example/.local/state/userland/last-run.log")
	result := output.String()
	for _, expected := range []string{
		"\x1b[36m◆  Plan\x1b[0m",
		"\x1b[36m├─ \x1b[1mOS\x1b[0m",
		"\x1b[36m├─ \x1b[1mFilesystem\x1b[0m",
		"\x1b[36m├─ \x1b[1mApplications\x1b[0m",
		"\x1b[36m├─ \x1b[1mCleanup\x1b[0m",
		" │   \x1b[2mNo changes\x1b[0m",
		" │  \x1b[36m~\x1b[0m  ffmpeg",
		"\x1b[2mupgrade installed rolling package\x1b[0m",
		" │   \x1b[2mNo stale userland-owned items\x1b[0m",
		" \x1b[32m◇\x1b[0m  1 automatic · 0 attended · 0 cleanup",
		" │  \x1b[2mDetails ~/.local/state/userland/last-run.log\x1b[0m",
	} {
		if !strings.Contains(result, expected) {
			t.Errorf("rich plan omitted %q:\n%q", expected, result)
		}
	}
	for _, obsolete := range []string{"OS changes", "Filesystem changes", "Application additions"} {
		if strings.Contains(result, obsolete) {
			t.Errorf("rich plan retained obsolete heading %q: %q", obsolete, result)
		}
	}
}

func TestRichSectionKeepsItsAccentMarker(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, richEnvironment())

	renderer.Section("Preflight")

	if expected := " \x1b[36m◆\x1b[0m  Preflight"; !strings.Contains(output.String(), expected) {
		t.Fatalf("section omitted accent marker %q: %q", expected, output.String())
	}
}

func TestConfirmationDefaultsToYes(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		code   int
		choice string
	}{
		{name: "enter", answer: "", code: 0, choice: "Y"},
		{name: "yes", answer: "y", code: 0, choice: "Y"},
		{name: "no", answer: "n", code: 3, choice: "N"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			renderer := New(&output, richEnvironment(
				"USERLAND_TESTING=1",
				"USERLAND_UI_TEST_CONFIRMATION="+test.answer,
			))
			if code := renderer.Confirm(strings.NewReader(""), "Apply this plan?"); code != test.code {
				t.Fatalf("confirmation returned %d, want %d", code, test.code)
			}
			expected := "\x1b[2m[Y/n]\x1b[0m \x1b[36m›\x1b[0m " + test.choice
			if !strings.Contains(output.String(), expected) {
				t.Fatalf("confirmation omitted %q: %q", expected, output.String())
			}
		})
	}
}

func TestPlainInteractiveConfirmationUsesPlainPrompt(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, []string{
		"USERLAND_UI_MODE=plain",
		"NO_COLOR=1",
		"USERLAND_HOME=/Users/example",
	})

	renderer.confirmationPrompt("Apply this plan?")

	if expected := "Apply this plan? [Y/n] "; output.String() != expected {
		t.Fatalf("plain confirmation = %q, want %q", output.String(), expected)
	}
}
