package nuke

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/giacomoguidotto/userland/internal/tui"
	"golang.org/x/term"
)

type invocation struct {
	Args    []string
	Environ []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

const (
	ExitSuccess = 0
	ExitFailure = 1
)

type ExitCode = int

func Run(ctx context.Context, environ []string, stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	invocation := invocation{Args: args, Environ: environ, Stdin: stdin, Stdout: stdout, Stderr: stderr}
	if _, _, err := parseNukeArgs(invocation.Args[1:]); err != nil {
		return usageError(invocation, err.Error())
	}
	account, err := user.LookupId(fmt.Sprint(os.Getuid()))
	if err != nil {
		tui.New(invocation.Stderr, invocation.Environ).Status(tui.StatusError, "could not resolve the current account: "+err.Error())
		return ExitFailure
	}
	return runNukeForAccount(ctx, invocation, account.HomeDir, os.Getuid())
}

// The account home comes from the OS, never from a command argument or an
// environment override. Tests supply an isolated temporary account directory.
func runNukeForAccount(ctx context.Context, in invocation, accountHome string, uid int) ExitCode {
	render := tui.New(in.Stdout, in.Environ)
	dry, yes, err := parseNukeArgs(in.Args[1:])
	if err != nil {
		return usageError(in, err.Error())
	}
	fail := func(err error) ExitCode { render.Status(tui.StatusError, err.Error()); return ExitFailure }
	home, err := safeNukeHome(accountHome, in.Environ, uid)
	if err != nil {
		return fail(err)
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return fail(err)
	}
	defer root.Close()
	original, err := root.Stat(".")
	if err != nil {
		return fail(err)
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return fail(err)
	}
	render.Command("nuke", "Erase the current account's home contents and recreate empty standard folders.")
	render.Status(tui.StatusWarning, "Target: "+home)
	render.Status(tui.StatusWarning, "Includes dev, personal files, hidden files, credentials, Library/app data, and Userland itself. No backup is created.")
	render.Status(tui.StatusInfo, "Applications and packages outside home remain installed. Quit other apps and disconnect cloud sync before erasing synced files.")
	for _, entry := range entries {
		message := "remove ~/" + entry.Name()
		if dry {
			message = "would " + message
		}
		render.Status(tui.StatusChange, message)
	}
	render.Status(tui.StatusInfo, "Recreate: Desktop, Documents, Downloads, Movies, Music, Pictures, Public, and Library")
	if ctx.Err() != nil {
		render.Summary(tui.StatusCancelled, "Nuke cancelled. No files were removed.")
		return 130
	}
	if err := nukePreflight(ctx, root, home); err != nil {
		if ctx.Err() != nil {
			return 130
		}
		return fail(fmt.Errorf("nuke preflight failed; no files removed: %w. On macOS, check Full Disk Access for the terminal and unmount volumes inside home", err))
	}
	if dry {
		render.Summary(tui.StatusOK, "Dry run complete. No files were removed.")
		return ExitSuccess
	}
	if !yes {
		file, ok := in.Stdin.(*os.File)
		if !ok || !term.IsTerminal(int(file.Fd())) {
			return fail(errors.New("nuke requires an interactive terminal, or explicit --yes after your backup completes"))
		}
		phrase := "NUKE " + home
		answer, code := (tui.Wizard{Render: render, Input: in.Stdin}).InputLine("Type " + phrase + " to permanently erase this home")
		if code != 0 || answer != phrase {
			render.Summary(tui.StatusCancelled, "Nuke cancelled. No files were removed.")
			if code == 130 {
				return 130
			}
			return 3
		}
	}
	if ctx.Err() != nil {
		return 130
	}
	current, err := os.Stat(home)
	if err != nil || !os.SameFile(original, current) {
		return fail(errors.New("home directory changed during confirmation; no files removed"))
	}
	// Recheck mounts and access after the interactive pause.
	if err := nukePreflight(ctx, root, home); err != nil {
		if ctx.Err() != nil {
			return 130
		}
		return fail(fmt.Errorf("nuke preflight failed; no files removed: %w", err))
	}
	// Store diagnostics outside the tree that is about to be erased. Ignore TMPDIR,
	// which can itself be under home.
	log, err := os.CreateTemp(nukeTempDir(), "userland-nuke-*.log")
	if err != nil {
		return fail(fmt.Errorf("cannot create reset log: %w", err))
	}
	defer log.Close()
	logPath := log.Name()
	render.Status(tui.StatusInfo, "Log: "+logPath)
	journal := json.NewEncoder(log)
	if err := journal.Encode(map[string]any{"event": "start", "home": home, "time": time.Now().UTC()}); err != nil {
		return fail(err)
	}
	var removed int
	render.BeginTask("Erase home contents")
	progress := func(path string) error {
		removed++
		render.UpdateTask(fmt.Sprintf("%d entries removed", removed))
		return journal.Encode(map[string]any{"event": "removed", "path": path})
	}
	err = eraseNukeHome(ctx, root, progress)
	render.ClearTask()
	if err != nil {
		_ = journal.Encode(map[string]any{"event": "incomplete", "removed": removed, "error": err.Error()})
		if ctx.Err() != nil {
			render.Summary(tui.StatusCancelled, "Nuke interrupted. Deleted files stay deleted. Log: "+logPath)
			return 130
		}
		render.Status(tui.StatusError, err.Error())
		render.Summary(tui.StatusError, "Home reset is incomplete. Log: "+logPath)
		return ExitFailure
	}
	if err := journal.Encode(map[string]any{"event": "complete", "removed": removed}); err != nil {
		return fail(err)
	}
	if err := log.Sync(); err != nil {
		return fail(err)
	}
	render.Summary(tui.StatusOK, "Home contents erased; empty standard folders recreated. Bootstrap Userland again after logging in, then run sync. Log: "+logPath)
	return ExitSuccess
}

func parseNukeArgs(args []string) (dryRun, yes bool, err error) {
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--yes":
			yes = true
		default:
			return false, false, errors.New("nuke accepts only --dry-run or --yes")
		}
	}
	if dryRun {
		yes = false
	}
	return
}

func safeNukeHome(accountHome string, environ []string, uid int) (string, error) {
	if uid == 0 {
		return "", errors.New("run nuke as the account being reset, never as root or through sudo")
	}
	if !filepath.IsAbs(accountHome) || filepath.Clean(accountHome) == "/" {
		return "", errors.New("refusing an unsafe account home path")
	}
	home, err := filepath.EvalSymlinks(accountHome)
	if err != nil {
		return "", err
	}
	// Reject shared/system roots even if an account is misconfigured.
	if home == "/" || filepath.Dir(home) == "/" || home == "/var/root" || home == "/private/var" || home == "/private/tmp" || home == "/Users/Shared" {
		return "", errors.New("refusing a shared or system directory as home")
	}
	for _, key := range []string{"HOME", "USERLAND_HOME"} {
		if value := environValue(environ, key); value != "" {
			if !filepath.IsAbs(value) {
				return "", fmt.Errorf("%s must match the account's absolute home path", key)
			}
			resolved, err := filepath.EvalSymlinks(value)
			if err != nil || resolved != home {
				return "", fmt.Errorf("%s does not match the current account home; refusing nuke", key)
			}
		}
	}
	info, err := os.Stat(home)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("account home is not a directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return "", errors.New("account does not own its home directory")
	}
	return home, nil
}

func nukeTempDir() string {
	if runtime.GOOS == "darwin" {
		return "/private/tmp"
	}
	return "/tmp"
}

func nukePreflight(ctx context.Context, root *os.Root, home string) error {
	mounts, err := nukeMounts()
	if err != nil {
		return err
	}
	for _, mount := range mounts {
		if strings.HasPrefix(filepath.Clean(mount), home+string(os.PathSeparator)) {
			return fmt.Errorf("mounted filesystem inside home: %s", mount)
		}
	}
	return fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		return nil
	})
}

var nukeFolders = []string{"Desktop", "Documents", "Downloads", "Movies", "Music", "Pictures", "Public", "Library"}

func eraseNukeHome(ctx context.Context, root *os.Root, removed func(string) error) error {
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	device, err := nukeDevice(info)
	if err != nil {
		return err
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := removeNukeEntry(ctx, root, entry.Name(), entry.Name(), device, removed); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Live applications can recreate state. Never report an empty home without
	// checking, and don't keep chasing newly-created files indefinitely.
	remaining, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("files were recreated during reset (%s); close running apps and retry", remaining[0].Name())
	}
	for _, name := range nukeFolders {
		mode := fs.FileMode(0700)
		if name == "Public" {
			mode = 0755
		}
		if err := root.Mkdir(name, mode); err != nil {
			return err
		}
	}
	return nil
}

// Each directory is opened relative to a retained parent descriptor. Links are
// unlinked as entries, never traversed. Device checks also guard mounted volumes.
func removeNukeEntry(ctx context.Context, parent *os.Root, name, display string, device uint64, removed func(string) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		child, err := parent.OpenRoot(name)
		if err != nil {
			return err
		}
		defer child.Close()
		opened, err := child.Stat(".")
		if err != nil {
			return err
		}
		openedDevice, err := nukeDevice(opened)
		if err != nil {
			return err
		}
		if !os.SameFile(info, opened) || openedDevice != device {
			return fmt.Errorf("refusing changed or mounted directory: %s", display)
		}
		entries, err := fs.ReadDir(child.FS(), ".")
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := removeNukeEntry(ctx, child, entry.Name(), display+"/"+entry.Name(), device, removed); err != nil {
				return err
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := parent.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", display, err)
	}
	return removed(display)
}

func nukeDevice(info fs.FileInfo) (uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("cannot inspect filesystem device")
	}
	return uint64(stat.Dev), nil
}

func environValue(environ []string, name string) string {
	for _, entry := range environ {
		if value, ok := strings.CutPrefix(entry, name+"="); ok {
			return value
		}
	}
	return ""
}
func usageError(in invocation, message string) int {
	tui.New(in.Stderr, in.Environ).Status(tui.StatusError, message)
	return 64
}
