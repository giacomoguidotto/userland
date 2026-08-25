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

	"github.com/giacomoguidotto/userland/internal/adapters"
	"github.com/giacomoguidotto/userland/internal/doctor"
	"github.com/giacomoguidotto/userland/internal/managedfiles"
	"github.com/giacomoguidotto/userland/internal/planner"
	"github.com/giacomoguidotto/userland/internal/platform"
	"github.com/giacomoguidotto/userland/internal/repository"
	"github.com/giacomoguidotto/userland/internal/tui"
	"github.com/giacomoguidotto/userland/plan"
)

func Run(ctx context.Context, environ []string, stdin io.Reader, stdout, stderr io.Writer, terminal bool) int {
	env := platform.NewEnvironment(environ)
	render := tui.New(stdout, environ)
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
		render.Status(tui.StatusOK, "macOS, Apple silicon, and disk-space preflight passed")
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
	render.Section("Apply packages")
	if code := commandTask(ctx, env, render, stdout, runLog, "Install missing rolling packages", env.Mise, "-C", env.Root, "bootstrap", "packages", "apply", "--yes", "--jobs", env.Jobs()); code != 0 {
		return code
	}
	var upgrades []string
	for _, item := range approved.Items() {
		if item.Area == plan.AreaApps && item.Action == "upgrade" && strings.HasPrefix(item.Proof, "mise:rolling-upgrade:brew:") {
			upgrades = append(upgrades, "brew:"+item.Target)
		}
	}
	if len(upgrades) != 0 {
		args := append([]string{"-C", env.Root, "bootstrap", "packages", "upgrade", "--yes", "--jobs", env.Jobs()}, upgrades...)
		if code := commandTask(ctx, env, render, stdout, runLog, "Upgrade installed rolling packages", env.Mise, args...); code != 0 {
			return code
		}
	}
	render.Section("Apply machine state")
	if code := commandTask(ctx, env, render, stdout, runLog, "Install pinned development tools", env.Mise, "-C", env.Root, "bootstrap", "--yes", "--only", "tools", "--jobs", env.Jobs()); code != 0 {
		return code
	}
	if code := commandTask(ctx, env, render, stdout, runLog, "Apply macOS preferences", env.Mise, "-C", env.Root, "bootstrap", "macos", "defaults", "apply", "--yes"); code != 0 {
		return code
	}
	render.Section("Apply personal state")
	result := adapters.RunTasks(ctx, env, adapters.Apply, stdin, terminal,
		func(label string) {
			if adapters.DirectApply(label) {
				return
			}
			if render.Rich() {
				render.BeginTask(label)
			} else {
				render.Status(tui.StatusInfo, label)
			}
		},
		func(label string, events []adapters.Event, code int) {
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
			if !render.Rich() {
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
	if result.Code != 0 {
		render.Summary(tui.StatusError, "Stopped at the failed step. Fix it, then rerun sync.")
		return result.Code
	}
	render.Section("Apply managed files")
	if code := nativeTask(render, "Apply managed files transactionally", func() int { return manager.Apply(ctx) }); code != 0 {
		return code
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
	if !env.IsMacOS() {
		return errors.New("sync currently supports macOS only")
	}
	architecture := runtime.GOARCH
	if result := platform.Run(ctx, env.List, nil, "uname", "-m"); result.Code == 0 {
		architecture = strings.TrimSpace(string(result.Output))
	}
	if architecture != "arm64" {
		return errors.New("sync supports Apple silicon only; found " + architecture)
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
	if free < 31457280 {
		return errors.New("sync needs at least 30 GiB free before large application installs")
	}
	return nil
}

func commandTask(ctx context.Context, env platform.Environment, render tui.Renderer, out io.Writer, runLog, label, name string, args ...string) int {
	if render.Rich() {
		render.BeginTask(label)
	} else {
		render.Status(tui.StatusInfo, label)
	}
	result := platform.Run(ctx, env.List, nil, name, args...)
	if render.Rich() {
		render.ClearTask()
	} else if len(result.Output) != 0 {
		_, _ = out.Write(result.Output)
	}
	appendLog(runLog, label, result.Output)
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

func nativeTask(render tui.Renderer, label string, operation func() int) int {
	if render.Rich() {
		render.BeginTask(label)
	} else {
		render.Status(tui.StatusInfo, label)
	}
	code := operation()
	if render.Rich() {
		render.ClearTask()
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

func appendAdapterLog(path, label string, events []adapters.Event) {
	var output strings.Builder
	for _, event := range events {
		fmt.Fprintf(&output, "[%s] %s\n", event.Level, event.Message)
	}
	appendLog(path, label, []byte(output.String()))
}

func restart(ctx context.Context, env platform.Environment, stdin io.Reader, stdout, stderr io.Writer) int {
	executable, err := os.Executable()
	if err != nil {
		return 1
	}
	command := exec.CommandContext(ctx, executable, "sync")
	command.Env = env.With("USERLAND_REFRESHED", "1")
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
