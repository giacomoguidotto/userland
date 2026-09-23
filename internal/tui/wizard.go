package tui

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// Wizard uses the same renderer as sync. Completion prompts deliberately ignore
// --yes: approving a plan is not evidence that a browser action was completed.
type Wizard struct {
	Render Renderer
	Input  io.Reader
}

func (w Wizard) Stage(index, total int, title string) {
	w.Render.ClearTask()
	w.Render.Section(fmt.Sprintf("%d/%d · %s", index, total, title))
}

func (w Wizard) Info(message string) { w.Render.Status(StatusInfo, message) }

func (w Wizard) InputLine(prompt string) (string, int) {
	w.Render.ClearTask()
	if w.Render.Rich() {
		fmt.Fprintf(w.Render.out, " %s?%s  %s %s›%s ", w.Render.cyan, w.Render.reset, w.Render.redact(prompt), w.Render.cyan, w.Render.reset)
	} else {
		fmt.Fprintf(w.Render.out, "%s: ", w.Render.redact(prompt))
	}
	if w.Input == nil {
		return "", 3
	}
	// Do not buffer past a prompt: subprocesses and later stages share this input.
	var answer strings.Builder
	var one [1]byte
	for {
		n, err := w.Input.Read(one[:])
		if n != 0 {
			if one[0] == '\n' {
				return strings.TrimSpace(answer.String()), 0
			}
			if one[0] == 3 {
				return "", 130
			}
			if answer.Len() >= 4096 {
				return "", 1
			}
			answer.WriteByte(one[0])
		}
		if err != nil {
			fmt.Fprintln(w.Render.out)
			return "", 3
		}
	}
}

func (w Wizard) Continue(prompt string) int {
	_, code := w.InputLine(prompt + " [Enter to continue, Ctrl-C to stop]")
	return code
}

func (w Wizard) ConfirmDone(prompt string) (bool, int) {
	answer, code := w.InputLine(prompt + " [y/N]")
	return code == 0 && (strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")), code
}

// CommandOutput presents browser-login instructions, including device codes,
// inside the TUI. It is not a diagnostic log and must not be used for secrets.
type CommandOutput struct {
	mu      sync.Mutex
	Wizard  Wizard
	pending string
}

func (o *CommandOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pending += string(p)
	for {
		line, rest, ok := strings.Cut(o.pending, "\n")
		if !ok {
			break
		}
		o.line(line)
		o.pending = rest
	}
	return len(p), nil
}

func (o *CommandOutput) Flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.pending != "" {
		o.line(o.pending)
		o.pending = ""
	}
}

var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

func (o *CommandOutput) line(s string) {
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if s = strings.TrimSpace(s); s != "" {
		o.Wizard.Info(s)
	}
}
