// Package sync orchestrates one approved convergence run.
package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/giacomoguidotto/userland/internal/adapters"
	"github.com/giacomoguidotto/userland/internal/doctor"
	"github.com/giacomoguidotto/userland/internal/managedfiles"
	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/planner"
	"github.com/giacomoguidotto/userland/internal/platform"
	"github.com/giacomoguidotto/userland/internal/repository"
	"github.com/giacomoguidotto/userland/internal/tui"
)

func Run(ctx context.Context, environ []string, stdin io.Reader, stdout, stderr io.Writer, terminal bool) int {
	env := platform.NewEnvironment(environ)
	started := syncStart(env, time.Now(), os.Getppid())
	environ = env.With("USERLAND_SYNC_STARTED_AT", strconv.FormatInt(started.UnixNano(), 10))
	env = platform.NewEnvironment(environ)
	render := tui.NewAt(stdout, environ, started)
	if err := env.Validate(); err != nil {
		tui.New(stderr, environ).Status(tui.StatusError, err.Error())
		return 1
	}
	if err := env.Prepare(); err != nil {
		tui.New(stderr, environ).Status(tui.StatusError, err.Error())
		return 1
	}
	if err := requireBootstrapAccess(env); err != nil {
		tui.New(stderr, environ).Status(tui.StatusError, err.Error())
		return 1
	}
	if env.Get("USERLAND_ARCHIVE") == "" && env.Get("USERLAND_REFRESHED") == "" {
		refresh := repository.RefreshCheckout(ctx, env)
		if refresh.Updated {
			return restart(ctx, env, stdin, stdout, stderr)
		}
		if refresh.Notice != "" {
			env.Values["USERLAND_REPOSITORY_REFRESH_NOTICE"] = refresh.Notice
		}
	}
	render.Command("sync", "Bring this Mac in line with the state declared in giacomoguidotto/userland.")
	render.Section("Preflight")
	if env.Bool("USERLAND_BOOTSTRAP_CREATED") {
		render.Status(tui.StatusOK, "Creating ~/.userland")
	}
	if env.Bool("USERLAND_BOOTSTRAP_REPOSITORY_PREPARED") {
		render.Status(tui.StatusOK, "Cloning giacomoguidotto/userland into ~/.userland")
	}
	manager := managedfiles.Manager{Env: env, Log: func(level, message string) { render.Status(logStatus(level), message) }}
	if manager.Recover() != nil {
		render.Status(tui.StatusError, "managed-file recovery needs attention before sync can continue")
		return 1
	}
	if manager.Prune() != nil {
		render.Status(tui.StatusError, "managed-file recovery cleanup failed")
		return 1
	}
	if err := preflight(ctx, env); err != nil {
		render.Status(tui.StatusError, err.Error())
		return 1
	}
	if !env.Bool("USERLAND_TESTING") {
		if env.IsMacOS() {
			render.Status(tui.StatusOK, "macOS, Apple silicon, and disk-space preflight passed")
		} else {
			render.Status(tui.StatusOK, "Linux architecture and disk-space preflight passed")
		}
	}
	if notice := env.Get("USERLAND_REPOSITORY_REFRESH_NOTICE"); notice != "" {
		render.Status(tui.StatusWarning, notice)
	}
	approved, runLog, err := planner.Embedded(ctx, environ, stdout)
	if err != nil {
		render.Status(tui.StatusError, err.Error())
		return 1
	}
	if approved.Summary().Blocked != 0 {
		render.Status(tui.StatusError, "Resolve the blocked plan items before syncing")
		return 2
	}
	if len(approved.Items()) == 0 {
		render.Summary(tui.StatusOK, "Done. This Mac matches userland. Run `userland doctor` to check the machine state")
		return 0
	}
	confirm := render.Confirm(stdin, "Apply this plan?")
	if confirm != 0 {
		if confirm == 3 && !env.Bool("USERLAND_BOOTSTRAP_CREATED") {
			render.Summary(tui.StatusCancelled, "Cancelled. No changes were applied.")
		}
		return confirm
	}
	if err := markApplyStarted(env); err != nil {
		render.Status(tui.StatusError, err.Error())
		return 1
	}
	taskStdin, closeTaskStdin := packageTaskInput(env, stdin)
	defer closeTaskStdin()
	var sudoPassword []byte
	defer func() { clearBytes(sudoPassword) }()
	render.Section("Apply packages")
	missingPackages := planTargets(approved, "mise:package:brew:", "install")
	// Homebrew and protected application cleanup run later with sudo. Authenticate
	// whenever either adapter has work, even when the rolling Mise packages were
	// already installed on an earlier run.
	if env.IsMacOS() && (len(missingPackages) != 0 || hasPrivilegedChanges(approved)) {
		var result platform.Result
		var passwordCode int
		sudoPassword, passwordCode = render.Secret(taskStdin, "Administrator password")
		if passwordCode != 0 {
			return passwordCode
		}
		code := nativeTask(ctx, render, "Authenticate macOS administrator access", func() int {
			result = adapters.AuthenticateHomebrew(ctx, env, sudoPassword, stdout, terminal)
			return result.Code
		})
		appendBootstrapLog(runLog, "Authenticate macOS administrator access", result, "sudo -v")
		if code != 0 {
			if detail := lastOutputLine(result.Output); detail != "" {
				render.Status(tui.StatusInfo, "sudo: "+detail)
			}
			render.Status(tui.StatusInfo, "Log: "+runLog)
			return code
		}
		progressOutput := homebrewProgressOutput{render: render}
		code = nativeTask(ctx, render, "Prepare Homebrew for Mise packages", func() int {
			result = adapters.PrepareHomebrew(ctx, env, taskStdin, sudoPassword, &progressOutput, terminal)
			return result.Code
		})
		appendBootstrapLog(runLog, "Prepare Homebrew for Mise packages", result, "pinned Homebrew installer")
		if code != 0 {
			if detail := lastOutputLine(result.Output); detail != "" {
				render.Status(tui.StatusInfo, "Homebrew: "+detail)
			}
			render.Status(tui.StatusInfo, "Log: "+runLog)
			return code
		}
	}
	if code := miseTask(ctx, env, render, runLog, taskStdin, "Install missing rolling packages", missingPackages, "bootstrap", "packages", "apply", "--yes", "--jobs", env.Jobs()); code != 0 {
		return code
	}
	render.Section("Apply machine state")
	if code := miseTask(ctx, env, render, runLog, taskStdin, "Install pinned development tools", planTargets(approved, "mise:tool:", ""), "bootstrap", "--yes", "--only", "tools", "--jobs", env.Jobs()); code != 0 {
		return code
	}
	if env.IsMacOS() {
		if code := miseTask(ctx, env, render, runLog, taskStdin, "Apply macOS preferences", nil, "bootstrap", "macos", "defaults", "apply", "--yes"); code != 0 {
			return code
		}
		if code := clearDock(ctx, env, render, runLog); code != 0 {
			return code
		}
	}
	if !env.Bool("USERLAND_TESTING") {
		render.Section("Apply managed files")
		if code := nativeTask(ctx, render, "Apply managed files transactionally", func() int { return manager.Apply(ctx) }); code != 0 {
			return code
		}
	}
	render.Section("Apply personal state")
	result := adapters.RunTasks(ctx, env, adapters.Apply, taskStdin, stdout, terminal, sudoPassword,
		func(label string) {
			if adapters.DirectApply(label) {
				return
			}
			if render.Rich() {
				render.BeginTask(label)
			} else {
				render.Status(tui.StatusInfo, label)
			}
		}, func(label string, current, total int, detail string) {
			message := fmt.Sprintf("%d/%d · %s", current, total, detail)
			if render.Rich() {
				render.UpdateTask(message)
			} else {
				render.Status(tui.StatusInfo, label+" "+message)
			}
		},
		func(label string, events []adapters.Event, code int) {
			if ctx.Err() != nil {
				if render.Rich() {
					render.ClearTask()
				}
				return
			}
			if adapters.DirectApply(label) {
				for _, event := range events {
					render.Status(adapterStatus(event.Level), event.Message)
				}
				return
			}
			if render.Rich() {
				render.ClearTask()
			}
			appendAdapterLog(runLog, label, events)
			if !render.Rich() || code != 0 {
				for _, event := range events {
					render.Status(adapterStatus(event.Level), event.Message)
				}
			}
			if code == 0 {
				render.TaskSuccess(label)
			} else if code == 2 {
				render.Status(tui.StatusAttention, label)
			} else {
				render.Status(tui.StatusError, fmt.Sprintf("%s failed (exit %d)", label, code))
			}
		})
	if ctx.Err() != nil {
		return 130
	}
	if result.Code == 3 || result.Code == 130 {
		render.Summary(tui.StatusCancelled, "Setup paused. Completed steps were preserved; rerun sync to continue.")
		return result.Code
	}
	if result.Code != 0 {
		render.Summary(tui.StatusError, "Stopped at the failed step. Fix it, then rerun sync.")
		return result.Code
	}
	if env.Bool("USERLAND_TESTING") {
		render.Section("Apply managed files")
		if code := nativeTask(ctx, render, "Apply managed files transactionally", func() int { return manager.Apply(ctx) }); code != 0 {
			return code
		}
	}
	render.Section("Verify")
	if doctor.Human(ctx, environ, stdout, true) == 0 {
		checkout := filepath.Join(env.Data, "repo")
		if exists(checkout) && manager.WindowOpen() {
			render.Status(tui.StatusOK, "Keeping the legacy checkout for the 24-hour recovery window")
		} else if manager.TrashLegacy(ctx) != 0 {
			render.Summary(tui.StatusAttention, "Sync complete, but a legacy checkout needs review.")
			return 2
		}
		render.Summary(tui.StatusOK, "Done. This Mac matches userland. Run `userland doctor` to check the machine state")
		return 0
	}
	render.Summary(tui.StatusAttention, "Done with steps that need attention.")
	return 2
}

// Mise versions have changed how the Dock app declaration is projected into
// defaults. Apply the two raw arrays explicitly and verify the write, so a
// stale pinned Dock cannot be mistaken for a successful sync.
func clearDock(ctx context.Context, env platform.Environment, render tui.Renderer, runLog string) int {
	for _, key := range []string{"persistent-apps", "persistent-others"} {
		result := platform.Run(ctx, env.List, nil, "defaults", "write", "com.apple.dock", key, "-array")
		appendLog(runLog, "Clear Dock "+key, result.Output)
		if result.Code != 0 {
			render.Status(tui.StatusError, "Dock "+key+" failed")
			render.Status(tui.StatusInfo, "Log: "+runLog)
			return result.Code
		}
		check := platform.Run(ctx, env.List, nil, "defaults", "read", "com.apple.dock", key)
		value := strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
				return -1
			}
			return r
		}, string(check.Output))
		if check.Code == 0 && value != "" && value != "()" {
			appendLog(runLog, "Verify Dock "+key, check.Output)
			render.Status(tui.StatusError, "Dock "+key+" was not cleared")
			render.Status(tui.StatusInfo, "Log: "+runLog)
			return 1
		}
	}
	result := platform.Run(ctx, env.List, nil, "killall", "Dock")
	appendLog(runLog, "Restart Dock", result.Output)
	if result.Code != 0 {
		// killall returns 1 when Dock is already restarting or not running. The
		// defaults writes above are still valid, so do not turn that into drift.
		if !strings.Contains(strings.ToLower(string(result.Output)), "no matching processes") {
			render.Status(tui.StatusError, "Dock restart failed")
			render.Status(tui.StatusInfo, "Log: "+runLog)
			return result.Code
		}
	}
	render.Status(tui.StatusOK, "Dock pinned items cleared")
	return 0
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func hasPrivilegedChanges(value *plan.Plan) bool {
	for _, item := range value.Items() {
		if strings.HasPrefix(item.Proof, "homebrew:") || strings.HasPrefix(item.Proof, "macos-bloat:") || strings.HasPrefix(item.Proof, "power-management:") || item.Target == "Homebrew" {
			return true
		}
	}
	return false
}

func packageTaskInput(env platform.Environment, stdin io.Reader) (io.Reader, func()) {
	if !env.IsMacOS() {
		return stdin, func() {}
	}
	if env.Bool("USERLAND_NO_TTY") {
		return nil, func() {}
	}
	if file, ok := stdin.(*os.File); ok {
		if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
			return nil, func() {}
		}
		return file, func() {}
	}
	return stdin, func() {}
}

func requireBootstrapAccess(env platform.Environment) error {
	lock := filepath.Join(env.Data, "bootstrap.lock")
	info, err := os.Lstat(lock)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("bootstrap lock is invalid: " + lock)
	}
	token := env.Get("USERLAND_BOOTSTRAP_TOKEN")
	if token == "" {
		return errors.New("bootstrap is preparing ~/.userland; finish or cancel that run first")
	}
	owner := filepath.Join(lock, "owner")
	if isSymlink(owner) {
		return errors.New("bootstrap lock owner is invalid")
	}
	value, err := os.ReadFile(owner)
	if err != nil {
		return errors.New("bootstrap lock owner is invalid")
	}
	if strings.TrimSpace(string(value)) != token {
		return errors.New("another userland bootstrap owns this checkout")
	}
	return nil
}

func markApplyStarted(env platform.Environment) error {
	control := env.Get("USERLAND_BOOTSTRAP_CONTROL")
	if control == "" {
		return nil
	}
	token := env.Get("USERLAND_BOOTSTRAP_TOKEN")
	if token == "" {
		return errors.New("bootstrap control token is missing")
	}
	info, err := os.Lstat(control)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("bootstrap control directory is invalid")
	}
	owner := filepath.Join(control, "owner")
	value, err := os.ReadFile(owner)
	if err != nil || isSymlink(owner) {
		return errors.New("bootstrap control owner is invalid")
	}
	if strings.TrimSpace(string(value)) != token {
		return errors.New("bootstrap control owner does not match")
	}
	temporary := filepath.Join(control, fmt.Sprintf(".apply-started.%d", os.Getpid()))
	if err := os.WriteFile(temporary, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(control, "apply-started"))
}

func preflight(ctx context.Context, env platform.Environment) error {
	if env.Bool("USERLAND_TESTING") {
		return nil
	}
	if !env.IsMacOS() && runtime.GOOS != "linux" {
		return errors.New("sync supports macOS and Linux")
	}
	architecture := runtime.GOARCH
	if result := platform.Run(ctx, env.List, nil, "uname", "-m"); result.Code == 0 {
		architecture = strings.TrimSpace(string(result.Output))
	}
	if architecture != "arm64" && architecture != "aarch64" && architecture != "x86_64" && architecture != "amd64" {
		return errors.New("sync supports arm64 and x86_64 Linux, or Apple silicon; found " + architecture)
	}
	result := platform.Run(ctx, env.List, nil, "df", "-Pk", "/")
	lines := strings.Split(strings.TrimSpace(string(result.Output)), "\n")
	if len(lines) < 2 {
		return errors.New("sync needs at least 30 GiB free before large application installs")
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return errors.New("sync needs at least 30 GiB free before large application installs")
	}
	free, _ := strconv.ParseInt(fields[3], 10, 64)
	minimum := int64(31457280)
	if !env.IsMacOS() {
		minimum = 1048576
	}
	if free < minimum {
		return errors.New("sync needs at least 30 GiB free before large application installs")
	}
	return nil
}

func miseTask(ctx context.Context, env platform.Environment, render tui.Renderer, runLog string, stdin io.Reader, label string, progressTargets []string, args ...string) int {
	// Command output is retained in runLog and surfaced only on failure. Keeping
	// it out of the terminal preserves the renderer's single live TUI surface.
	if render.Rich() {
		render.BeginTask(label)
	} else {
		render.Status(tui.StatusInfo, label)
	}
	progress := newPackageProgress(render, progressTargets)
	// Keep the terminal input attached. Mise may need one sudo prompt while
	// creating a native package prefix such as /opt/homebrew.
	result := env.RunMiseObserved(ctx, stdin, progress, args...)
	progress.Flush()
	if render.Rich() {
		render.ClearTask()
	}
	appendLog(runLog, label, result.Output)
	if ctx.Err() != nil {
		return 130
	}
	if result.Code == 0 {
		render.TaskSuccess(label)
		return 0
	}
	if result.Code == 2 {
		render.Status(tui.StatusAttention, label)
	} else {
		render.Status(tui.StatusError, fmt.Sprintf("%s failed (exit %d)", label, result.Code))
	}
	render.Status(tui.StatusInfo, "Log: "+runLog)
	return result.Code
}

type packageProgress struct {
	render   tui.Renderer
	position map[string]int
	total    int
	pending  string
}

func newPackageProgress(render tui.Renderer, targets []string) *packageProgress {
	positions := make(map[string]int)
	for _, name := range targets {
		if name == "" {
			continue
		}
		if _, exists := positions[name]; !exists {
			positions[name] = len(positions) + 1
		}
	}
	return &packageProgress{render: render, position: positions, total: len(positions)}
}

func (p *packageProgress) Write(value []byte) (int, error) {
	p.pending += strings.ReplaceAll(string(value), "\r", "\n")
	for {
		line, rest, found := strings.Cut(p.pending, "\n")
		if !found {
			break
		}
		p.observe(line)
		p.pending = rest
	}
	return len(value), nil
}

func (p *packageProgress) Flush() {
	if p.pending != "" {
		p.observe(p.pending)
		p.pending = ""
	}
}

func (p *packageProgress) observe(line string) {
	if p.total == 0 {
		return
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "mise" {
		return
	}
	name := strings.TrimPrefix(fields[1], "brew:")
	if marker := strings.LastIndexByte(name, '@'); marker > 0 {
		name = name[:marker]
	}
	position, declared := p.position[name]
	if !declared {
		return
	}
	p.render.UpdateTask(fmt.Sprintf("%d/%d · %s", position, p.total, name))
}

func planTargets(value *plan.Plan, proofPrefix string, action plan.Action) []string {
	var result []string
	for _, item := range value.Items() {
		if action != "" && item.Action != action {
			continue
		}
		name, ok := strings.CutPrefix(item.Proof, proofPrefix)
		if ok && name != "" {
			result = append(result, name)
		}
	}
	return result
}

func nativeTask(ctx context.Context, render tui.Renderer, label string, operation func() int) int {
	if render.Rich() {
		render.BeginTask(label)
	} else {
		render.Status(tui.StatusInfo, label)
	}
	code := operation()
	if render.Rich() {
		render.ClearTask()
	}
	if ctx.Err() != nil {
		return 130
	}
	if code == 0 {
		render.TaskSuccess(label)
	} else if code == 2 {
		render.Status(tui.StatusAttention, label)
	} else {
		render.Status(tui.StatusError, fmt.Sprintf("%s failed (exit %d)", label, code))
	}
	return code
}

func appendLog(path, label string, output []byte) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "\n## %s\n", label)
	_, _ = file.Write(output)
}

func appendBootstrapLog(path, label string, result platform.Result, command string) {
	var output strings.Builder
	fmt.Fprintf(&output, "command: %s\nexit: %d\n", command, result.Code)
	if result.Err != nil {
		fmt.Fprintf(&output, "error: %v\n", result.Err)
	}
	output.Write(result.Output)
	appendLog(path, label, []byte(output.String()))
}

type homebrewProgressOutput struct {
	render  tui.Renderer
	pending string
}

func (w *homebrewProgressOutput) Write(value []byte) (int, error) {
	w.pending += strings.ReplaceAll(string(value), "\r", "\n")
	for {
		line, rest, found := strings.Cut(w.pending, "\n")
		if !found {
			break
		}
		w.pending = rest
		if detail := homebrewProgressDetail(line); detail != "" {
			w.render.UpdateTask(detail)
		}
	}
	return len(value), nil
}

func homebrewProgressDetail(line string) string {
	line = strings.TrimSpace(strings.TrimPrefix(line, "==>"))
	switch {
	case line == "", strings.HasPrefix(line, "Warning:"), strings.HasPrefix(line, "This installation"), strings.HasPrefix(line, "Please create"), strings.HasPrefix(line, "You are responsible"), strings.HasPrefix(line, "/opt/homebrew"), strings.HasPrefix(line, "/etc/paths"):
		return ""
	case strings.HasPrefix(line, "Checking for"):
		return "checking Homebrew prerequisites"
	case strings.HasPrefix(line, "The Xcode Command Line Tools"):
		return "installing Xcode Command Line Tools"
	case strings.HasPrefix(line, "Downloading"):
		return "downloading Homebrew"
	case strings.HasPrefix(line, "Installing"):
		return "installing Homebrew"
	case strings.HasPrefix(line, "Running"):
		return line
	}
	return ""
}

func lastOutputLine(output []byte) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return ""
}

func clearInteractivePrompt(output io.Writer) {
	_, _ = io.WriteString(output, "\r\x1b[2K")
}

func appendAdapterLog(path, label string, events []adapters.Event) {
	var output strings.Builder
	for _, event := range events {
		fmt.Fprintf(&output, "[%s] %s\n", event.Level, event.Message)
	}
	appendLog(path, label, []byte(output.String()))
}

func syncStart(env platform.Environment, now time.Time, parentPID int) time.Time {
	if env.Get("USERLAND_SYNC_PARENT_PID") == strconv.Itoa(parentPID) {
		if nanoseconds, err := strconv.ParseInt(env.Get("USERLAND_SYNC_STARTED_AT"), 10, 64); err == nil {
			if started := time.Unix(0, nanoseconds); !started.After(now) {
				return started
			}
		}
	}
	return now
}

func restart(ctx context.Context, env platform.Environment, stdin io.Reader, stdout, stderr io.Writer) int {
	executable, err := os.Executable()
	if err != nil {
		return 1
	}
	command := exec.CommandContext(ctx, executable, "sync")
	command.Env = env.With("USERLAND_REFRESHED", "1", "USERLAND_SYNC_PARENT_PID", strconv.Itoa(os.Getpid()))
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, stderr
	if err := command.Run(); err == nil {
		return 0
	} else if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return 1
}

func adapterStatus(level adapters.Level) tui.Status {
	switch level {
	case adapters.Healthy, adapters.Current, adapters.Changed:
		return tui.StatusOK
	case adapters.Change:
		return tui.StatusChange
	case adapters.Manual:
		return tui.StatusManual
	default:
		return tui.StatusAttention
	}
}

func logStatus(level string) tui.Status {
	switch level {
	case "changed", "preserved", "healthy", "current":
		return tui.StatusOK
	case "change":
		return tui.StatusChange
	case "manual":
		return tui.StatusManual
	case "attention", "warning":
		return tui.StatusAttention
	case "error":
		return tui.StatusError
	default:
		return tui.StatusInfo
	}
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}
