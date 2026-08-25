package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

type Mode uint8

const (
	ModePlain Mode = iota
	ModeRich
)

type Status uint8

const (
	StatusError Status = iota
	StatusInfo
	StatusOK
	StatusChange
	StatusAttention
	StatusManual
	StatusWarning
	StatusCancelled
)

type Renderer struct {
	out     io.Writer
	env     map[string]string
	mode    Mode
	unicode bool
	reset   string
	bold    string
	dim     string
	green   string
	yellow  string
	red     string
	cyan    string
	task    *taskAnimation
}

type taskAnimation struct {
	mu      sync.Mutex
	writeMu sync.Mutex
	stop    chan struct{}
	done    chan struct{}
}

func New(out io.Writer, environ []string) Renderer {
	env := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}

	mode := ModePlain
	requestedMode := env["USERLAND_UI_MODE"]
	if requestedMode == "rich" {
		mode = ModeRich
	} else if requestedMode == "auto" || requestedMode == "" {
		if file, ok := out.(*os.File); ok && term.IsTerminal(int(file.Fd())) && env["TERM"] != "dumb" && env["CI"] == "" {
			mode = ModeRich
		}
	}

	unicode := false
	if value, exists := env["USERLAND_UNICODE"]; exists {
		unicode = value != "0"
	} else {
		locale := env["LC_ALL"]
		if locale == "" {
			locale = env["LC_CTYPE"]
		}
		if locale == "" {
			locale = env["LANG"]
		}
		locale = strings.ToLower(locale)
		unicode = strings.Contains(locale, "utf-8") || strings.Contains(locale, "utf8")
	}

	color := env["NO_COLOR"] == "" && env["CLICOLOR"] != "0" && env["TERM"] != "dumb" &&
		(mode == ModeRich || env["CLICOLOR_FORCE"] != "" && env["CLICOLOR_FORCE"] != "0")
	renderer := Renderer{out: out, env: env, mode: mode, unicode: unicode, task: &taskAnimation{}}
	if color {
		renderer.reset = "\x1b[0m"
		renderer.bold = "\x1b[1m"
		renderer.dim = "\x1b[2m"
		renderer.green = "\x1b[32m"
		renderer.yellow = "\x1b[33m"
		renderer.red = "\x1b[31m"
		renderer.cyan = "\x1b[36m"
	}
	return renderer
}

func (r Renderer) Usage() {
	margin := ""
	if r.mode == ModeRich {
		margin = " "
		r.wordmark()
		fmt.Fprintln(r.out)
		fmt.Fprintf(r.out, " %sPersonal macOS state, kept in sync.%s\n", r.dim, r.reset)
		fmt.Fprintln(r.out)
		fmt.Fprintf(r.out, " %sUsage%s\n", r.bold, r.reset)
		fmt.Fprintln(r.out, "   userland <command>")
		fmt.Fprintln(r.out)
		fmt.Fprintf(r.out, " %sCommands%s\n", r.bold, r.reset)
	} else {
		fmt.Fprintln(r.out, "userland")
		fmt.Fprintln(r.out, "Personal macOS state, kept in sync.")
		fmt.Fprintln(r.out)
		fmt.Fprintln(r.out, "Usage")
		fmt.Fprintln(r.out, "  userland <command>")
		fmt.Fprintln(r.out)
		fmt.Fprintln(r.out, "Commands")
	}

	fmt.Fprintf(r.out, "%s  plan      Preview what would change\n", margin)
	fmt.Fprintf(r.out, "%s  sync      Update, apply, and verify declared state\n", margin)
	fmt.Fprintf(r.out, "%s  doctor    Check drift and machine health\n\n", margin)
	fmt.Fprintf(r.out, "%s  completions <shell>\n", margin)
	fmt.Fprintf(r.out, "%s            Print Bash, Fish, Nushell, or Zsh completions\n\n", margin)
	fmt.Fprintf(r.out, "%sAutomation\n", margin)
	fmt.Fprintf(r.out, "%s  userland doctor --json\n", margin)
}

func (r Renderer) Status(status Status, message string) {
	plain := "error"
	symbol := "x"
	tint := r.red
	if status == StatusInfo {
		plain, symbol, tint = "info", "-", r.dim
	} else if status == StatusOK {
		plain, symbol, tint = "ok", "ok", r.green
	} else if status == StatusChange {
		plain, symbol, tint = "change", "+", r.cyan
	} else if status == StatusAttention {
		plain, symbol, tint = "attention", "!", r.yellow
	} else if status == StatusManual {
		plain, symbol, tint = "manual", "!", r.yellow
	} else if status == StatusWarning {
		plain, symbol, tint = "warning", "!", r.yellow
	} else if status == StatusCancelled {
		plain, symbol, tint = "cancelled", "-", r.dim
	}
	switch r.mode {
	case ModeRich:
		if r.unicode {
			switch status {
			case StatusError:
				symbol = "×"
			case StatusInfo:
				symbol = "·"
			case StatusOK:
				symbol = "✓"
			case StatusCancelled:
				symbol = "–"
			}
		}
		fmt.Fprintf(r.out, " %s%s%s  %s\n", tint, symbol, r.reset, r.redact(message))
	default:
		fmt.Fprintf(r.out, "[%s] %s\n", plain, r.redact(message))
	}
}

func (r Renderer) Section(title string) {
	if r.mode == ModePlain {
		fmt.Fprintf(r.out, "== %s\n", r.redact(title))
		return
	}
	fmt.Fprintf(r.out, "%s\n %s%s%s  %s\n%s\n", r.rail(), r.cyan, r.sectionSymbol(), r.reset, r.redact(title), r.rail())
}

func (r Renderer) Summary(status Status, message string) {
	if r.mode == ModePlain {
		fmt.Fprintln(r.out)
		r.Status(status, message+" (<1s)")
		return
	}
	tint := r.green
	if status == StatusAttention || status == StatusManual || status == StatusWarning {
		tint = r.yellow
	} else if status == StatusError {
		tint = r.red
	} else if status == StatusCancelled {
		tint = ""
	}
	fmt.Fprintln(r.out, r.rail())
	fmt.Fprintf(r.out, " %s%s%s  %s\n", tint, r.closeSymbol(), r.reset, r.redact(message))
	fmt.Fprintf(r.out, "    %s<1s%s\n", r.dim, r.reset)
}

func (r Renderer) Confirm(in io.Reader, prompt string) int {
	if r.env["USERLAND_ASSUME_YES"] == "1" {
		if r.mode == ModePlain {
			r.Status(StatusInfo, prompt+" yes")
		} else {
			fmt.Fprintf(r.out, " %s?%s  %s %s[Y/n]%s %s›%s Y\n", r.cyan, r.reset, r.redact(prompt), r.dim, r.reset, r.cyan, r.reset)
		}
		return 0
	}
	answer, testConfirmation := r.env["USERLAND_UI_TEST_CONFIRMATION"]
	answered := true
	if !(r.env["USERLAND_TESTING"] == "1" && testConfirmation) {
		file, ok := in.(*os.File)
		if !ok || !term.IsTerminal(int(file.Fd())) {
			r.Status(StatusError, prompt+" requires an interactive terminal")
			return 1
		}
		r.confirmationPrompt(prompt)
		line, err := bufio.NewReader(in).ReadString('\n')
		answered = err == nil
		answer = strings.TrimSpace(line)
	}
	accepted := answered && (answer == "" || strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"))
	if r.mode == ModeRich && r.env["USERLAND_TESTING"] == "1" && testConfirmation {
		value := "N"
		if accepted {
			value = "Y"
		}
		fmt.Fprintf(r.out, " %s?%s  %s %s[Y/n]%s %s›%s %s\n", r.cyan, r.reset, r.redact(prompt), r.dim, r.reset, r.cyan, r.reset, value)
	} else if r.mode == ModePlain && r.env["USERLAND_TESTING"] == "1" && testConfirmation {
		value := "N"
		if accepted {
			value = "Y"
		}
		fmt.Fprintf(r.out, "%s [Y/n] %s\n", r.redact(prompt), value)
	}
	if accepted {
		return 0
	}
	return 3
}

func (r Renderer) confirmationPrompt(prompt string) {
	if r.mode == ModePlain {
		fmt.Fprintf(r.out, "%s [Y/n] ", r.redact(prompt))
		return
	}
	fmt.Fprintf(r.out, " %s?%s  %s %s[Y/n]%s %s›%s ", r.cyan, r.reset, r.redact(prompt), r.dim, r.reset, r.cyan, r.reset)
}

func (r Renderer) closeSymbol() string {
	if r.unicode {
		return "└"
	}
	return "`"
}

func (r Renderer) Rich() bool { return r.mode == ModeRich }

func (r Renderer) Command(command, description string) {
	if r.mode == ModePlain {
		fmt.Fprintf(r.out, "userland %s: %s\n", command, description)
		return
	}
	r.wordmark()
	width := 40
	if version := r.version(); version != "" {
		width += 2 + len(version)
	}
	open, rail, rule := "+", "|", "-"
	if r.unicode {
		open, rail, rule = "┌", "│", "─"
	}
	fmt.Fprintf(r.out, " %s%s\n", open, strings.Repeat(rule, width))
	fmt.Fprintf(r.out, " %s  %s%s%s\n", rail, r.bold, command, r.reset)
	fmt.Fprintf(r.out, " %s  %s%s%s\n", rail, r.dim, description, r.reset)
}

func (r Renderer) BeginTask(label string) {
	if r.mode != ModeRich {
		return
	}
	if r.task == nil {
		return
	}
	r.ClearTask()
	stop := make(chan struct{})
	done := make(chan struct{})
	r.task.mu.Lock()
	r.task.stop = stop
	r.task.done = done
	r.task.mu.Unlock()
	started := time.Now()
	r.taskFrame(label, 0, started)
	go func() {
		defer close(done)
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		frame := 1
		for {
			select {
			case <-ticker.C:
				r.taskFrame(label, frame, started)
				frame++
			case <-stop:
				return
			}
		}
	}()
}

func (r Renderer) ClearTask() {
	if r.mode != ModeRich || r.task == nil {
		return
	}
	r.task.mu.Lock()
	stop, done := r.task.stop, r.task.done
	r.task.stop, r.task.done = nil, nil
	r.task.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
	r.task.writeMu.Lock()
	fmt.Fprint(r.out, "\r\x1b[2K")
	r.task.writeMu.Unlock()
}

func (r Renderer) taskFrame(label string, frame int, started time.Time) {
	glyphs := []string{"-", "\\", "|", "/"}
	if r.unicode {
		glyphs = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	}
	seconds := int(time.Since(started).Seconds())
	duration := "<1s"
	if seconds >= 3600 {
		duration = fmt.Sprintf("%dh %dm", seconds/3600, seconds%3600/60)
	} else if seconds >= 60 {
		duration = fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
	} else if seconds > 0 {
		duration = fmt.Sprintf("%ds", seconds)
	}
	r.task.writeMu.Lock()
	fmt.Fprintf(r.out, "\r\x1b[2K %s%s%s  %s… %s%s%s", r.cyan, glyphs[frame%len(glyphs)], r.reset, r.redact(label), r.dim, duration, r.reset)
	r.task.writeMu.Unlock()
}

func (r Renderer) TaskSuccess(label string) {
	if r.mode != ModeRich {
		return
	}
	glyph := "o"
	if r.unicode {
		glyph = "◇"
	}
	fmt.Fprintf(r.out, " %s%s%s  %s\n", r.green, glyph, r.reset, r.redact(label))
}

func (r Renderer) Excerpt(status Status, message string) {
	plain := "error"
	if status == StatusAttention {
		plain = "attention"
	} else if status == StatusManual {
		plain = "manual"
	} else if status == StatusWarning {
		plain = "warning"
	}
	rail := "|"
	if r.mode == ModeRich {
		rail = r.rail()
	}
	fmt.Fprintf(r.out, "%s  [%s] %s\n", rail, plain, r.redact(message))
}

func (r Renderer) TaskLine(line string) {
	if r.mode == ModeRich {
		fmt.Fprintf(r.out, "%s  %s\n", r.rail(), r.redact(line))
	} else {
		fmt.Fprintln(r.out, r.redact(line))
	}
}

func (r Renderer) Spacer() {
	if r.mode == ModeRich {
		fmt.Fprintln(r.out, r.rail())
	} else {
		fmt.Fprintln(r.out)
	}
}

func (r Renderer) SummaryOK(message string) {
	if r.mode == ModePlain {
		fmt.Fprintln(r.out)
		fmt.Fprintf(r.out, "[ok] %s (<1s)\n", message)
		return
	}
	close := "`"
	if r.unicode {
		close = "└"
	}
	fmt.Fprintln(r.out, r.rail())
	fmt.Fprintf(r.out, " %s%s%s  %s\n", r.green, close, r.reset, message)
	fmt.Fprintf(r.out, "    %s<1s%s\n", r.dim, r.reset)
}

func (r Renderer) wordmark() {
	if r.unicode {
		fmt.Fprintf(r.out, " %s▗▖ ▗▖ ▗▄▄▖▗▄▄▄▖▗▄▄▖ ▗▖    ▗▄▖ ▗▖  ▗▖▗▄▄▄%s\n", r.cyan, r.reset)
		fmt.Fprintf(r.out, " %s▐▌ ▐▌▐▌   ▐▌   ▐▌ ▐▌▐▌   ▐▌ ▐▌▐▛▚▖▐▌▐▌  █%s\n", r.cyan, r.reset)
		fmt.Fprintf(r.out, " %s▐▌ ▐▌ ▝▀▚▖▐▛▀▀▘▐▛▀▚▖▐▌   ▐▛▀▜▌▐▌ ▝▜▌▐▌  █%s\n", r.cyan, r.reset)
		fmt.Fprintf(r.out, " %s▝▚▄▞▘▗▄▄▞▘▐▙▄▄▖▐▌ ▐▌▐▙▄▄▖▐▌ ▐▌▐▌  ▐▌▐▙▄▄▀%s", r.cyan, r.reset)
	} else {
		fmt.Fprintf(r.out, " %s+-- USERLAND --+%s", r.cyan, r.reset)
	}
	if version := r.version(); version != "" {
		fmt.Fprintf(r.out, "  %s%s%s", r.dim, version, r.reset)
	}
	fmt.Fprintln(r.out)
}

var semanticVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func (r Renderer) version() string {
	if version := r.env["USERLAND_VERSION"]; semanticVersion.MatchString(version) {
		return version
	}
	root := r.env["USERLAND_ROOT"]
	if root == "" {
		return ""
	}
	stage := filepath.Join(root, ".userland-stage-version")
	if info, err := os.Lstat(stage); err == nil && info.Mode()&os.ModeSymlink == 0 {
		if contents, err := os.ReadFile(stage); err == nil {
			if version := strings.TrimSpace(strings.SplitN(string(contents), "\n", 2)[0]); semanticVersion.MatchString(version) {
				return version
			}
		}
	}
	command := exec.Command("git", "-C", root, "describe", "--tags", "--exact-match", "HEAD")
	command.Env = mapToEnvironment(r.env)
	if output, err := command.Output(); err == nil {
		if version := strings.TrimSpace(string(output)); semanticVersion.MatchString(version) {
			return version
		}
	}
	if version := filepath.Base(root); semanticVersion.MatchString(version) && filepath.Base(filepath.Dir(root)) == "releases" {
		return version
	}
	return ""
}

func mapToEnvironment(values map[string]string) []string {
	environ := make([]string, 0, len(values))
	for key, value := range values {
		environ = append(environ, key+"="+value)
	}
	return environ
}
