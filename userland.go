package userland

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/giacomoguidotto/userland/internal/doctor"
	"github.com/giacomoguidotto/userland/internal/planner"
	usersync "github.com/giacomoguidotto/userland/internal/sync"
	"github.com/giacomoguidotto/userland/internal/tui"
	"golang.org/x/term"
)

// ExitCode is the process status returned by a Userland invocation.
type ExitCode int

const (
	ExitSuccess ExitCode = 0
	ExitFailure ExitCode = 1
	ExitUsage   ExitCode = 64
)

// Invocation contains the complete process-facing Interface for one Userland run.
type Invocation struct {
	Args    []string
	Environ []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

// Run executes one Userland command.
func Run(ctx context.Context, invocation Invocation) ExitCode {
	renderer := tui.New(invocation.Stdout, invocation.Environ)

	if len(invocation.Args) == 0 {
		renderer.Usage()
		return ExitUsage
	}

	command := invocation.Args[0]
	switch command {
	case "plan":
		if len(invocation.Args) != 1 {
			return usageError(invocation, "plan does not accept arguments")
		}
		if err := planner.Run(ctx, invocation.Environ, invocation.Stdout); err != nil {
			tui.New(invocation.Stderr, invocation.Environ).Status(tui.StatusError, err.Error())
			return ExitFailure
		}
		return ExitSuccess
	case "sync":
		if len(invocation.Args) != 1 {
			return usageError(invocation, "sync does not accept arguments")
		}
		return runSync(ctx, invocation)
	case "doctor":
		if len(invocation.Args) > 2 || len(invocation.Args) == 2 && invocation.Args[1] != "--json" {
			return usageError(invocation, "doctor accepts only --json")
		}
		if len(invocation.Args) == 2 {
			return runDoctorJSON(ctx, invocation)
		}
		return ExitCode(doctor.Human(ctx, invocation.Environ, invocation.Stdout, false))
	case "help", "-h", "--help":
		renderer.Usage()
		return ExitSuccess
	case "completions":
		return runCompletions(invocation)
	default:
		renderer = tui.New(invocation.Stderr, invocation.Environ)
		renderer.Usage()
		renderer.Status(tui.StatusError, "unknown command: "+command)
		return ExitUsage
	}
}

func runDoctorJSON(ctx context.Context, invocation Invocation) ExitCode {
	encoded, healthy := doctor.JSON(ctx, invocation.Environ)
	if _, err := invocation.Stdout.Write(encoded); err != nil {
		return ExitFailure
	}
	if !healthy {
		return ExitFailure
	}
	return ExitSuccess
}

func runSync(ctx context.Context, invocation Invocation) ExitCode {
	terminal := false
	if file, ok := invocation.Stdin.(*os.File); ok {
		terminal = term.IsTerminal(int(file.Fd()))
	}
	return ExitCode(usersync.Run(ctx, invocation.Environ, invocation.Stdin, invocation.Stdout, invocation.Stderr, terminal))
}

func usageError(invocation Invocation, message string) ExitCode {
	tui.New(invocation.Stderr, invocation.Environ).Status(tui.StatusError, message)
	return ExitUsage
}

func runCompletions(invocation Invocation) ExitCode {
	renderer := tui.New(invocation.Stderr, invocation.Environ)
	if len(invocation.Args) != 2 {
		renderer.Status(tui.StatusError, "completions expects one shell: bash, fish, nushell, or zsh")
		return ExitUsage
	}

	shell := invocation.Args[1]
	switch shell {
	case "bash", "fish", "nushell", "zsh":
	default:
		renderer.Status(tui.StatusError, "completions supports bash, fish, nushell, or zsh")
		return ExitUsage
	}

	root := environValue(invocation.Environ, "USERLAND_ROOT")
	completion, err := os.ReadFile(filepath.Join(root, "completions", shell))
	if err != nil {
		renderer.Status(tui.StatusError, fmt.Sprintf("could not read %s completions", shell))
		return ExitFailure
	}
	if _, err := invocation.Stdout.Write(completion); err != nil {
		return ExitFailure
	}
	return ExitSuccess
}

func environValue(environ []string, name string) string {
	prefix := name + "="
	for _, entry := range environ {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			return entry[len(prefix):]
		}
	}
	return ""
}
