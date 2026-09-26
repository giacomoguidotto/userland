package tui

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/term"
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
	if file, ok := w.Input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return w.inputTerminal(file, prompt)
	}
	w.writePrompt(prompt)
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

func (w Wizard) writePrompt(prompt string) {
	w.Render.ClearTask()
	if w.Render.Rich() {
		fmt.Fprintf(w.Render.out, " %s?%s  %s %s›%s ", w.Render.cyan, w.Render.reset, w.Render.redact(prompt), w.Render.cyan, w.Render.reset)
	} else {
		fmt.Fprintf(w.Render.out, "%s: ", w.Render.redact(prompt))
	}
}

func (w Wizard) inputTerminal(file *os.File, prompt string) (string, int) {
	state, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return "", 1
	}
	w.writePrompt(prompt)
	defer func() {
		_ = term.Restore(int(file.Fd()), state)
		fmt.Fprintln(w.Render.out)
	}()
	var answer []byte
	var one [1]byte
	for {
		n, err := file.Read(one[:])
		if n != 0 {
			switch one[0] {
			case '\r', '\n':
				return strings.TrimSpace(string(answer)), 0
			case 3:
				return "", 130
			case 4:
				return "", 3
			case 8, 127:
				if len(answer) > 0 {
					answer = answer[:len(answer)-1]
					fmt.Fprint(w.Render.out, "\b \b")
				}
			default:
				if len(answer) >= 4096 {
					return "", 1
				}
				answer = append(answer, one[0])
				_, _ = w.Render.out.Write(one[:])
			}
		}
		if err != nil {
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

func (w Wizard) ConfirmDoneYes(prompt string) (bool, int) {
	answer, code := w.InputLine(prompt + " [Y/n]")
	return code == 0 && (answer == "" || strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")), code
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
