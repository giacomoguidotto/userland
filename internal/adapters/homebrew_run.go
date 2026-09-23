package adapters

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/giacomoguidotto/userland/internal/platform"
)

// Keep output on disk as it arrives, including before an interrupted command
// returns. Only the phase observer writes to the TUI.
func runBrewMutation(c *Context, environ []string, observer io.Writer, brew string, args ...string) platform.Result {
	path := filepath.Join(c.Env.State, "last-run.log")
	if err := os.MkdirAll(c.Env.State, 0700); err != nil {
		return brewLogFailure(c, err)
	}
	log, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return brewLogFailure(c, err)
	}
	defer log.Close()
	if err := log.Chmod(0600); err != nil {
		return brewLogFailure(c, err)
	}
	if _, err := fmt.Fprintf(log, "\n## Homebrew command %s\ncommand: %q %q\n", time.Now().UTC().Format(time.RFC3339), brew, args); err != nil {
		return brewLogFailure(c, err)
	}
	monitor := &brewActivity{log: log, observer: observer, last: time.Now()}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				monitor.reportSilence(now)
			case <-stop:
				return
			}
		}
	}()
	// Keep Homebrew attached to the terminal-backed input. Cask installs may
	// invoke sudo even after the initial bootstrap, and a nil stdin leaves the
	// child waiting indefinitely for a password nobody can enter.
	result := runWithObserved(c, environ, c.Stdin, monitor, brew, args...)
	close(stop)
	<-done
	fmt.Fprintf(log, "\nexit: %d\n", result.Code)
	if result.Err != nil {
		fmt.Fprintf(log, "error: %v\n", result.Err)
	}
	if c.Context.Err() != nil {
		fmt.Fprintf(log, "cancelled: %v\n", c.Context.Err())
	}
	if monitor.err != nil {
		return brewLogFailure(c, monitor.err)
	}
	if result.Code != 0 && c.Context.Err() == nil {
		detail := strings.TrimSpace(string(result.Output))
		lines := strings.Split(detail, "\n")
		if len(lines) > 4 {
			lines = lines[len(lines)-4:]
		}
		if result.Err != nil {
			lines = append(lines, result.Err.Error())
		}
		c.Log(Attention, "Homebrew command failed: "+strings.Join(lines, "\n"))
		c.Log(Attention, "Details: "+path)
	}
	return result
}

func brewLogFailure(c *Context, err error) platform.Result {
	c.Log(Attention, "Cannot write Homebrew diagnostics: "+err.Error())
	return platform.Result{Code: 1, Err: err}
}

type brewActivity struct {
	mu       sync.Mutex
	log      io.Writer
	observer io.Writer
	last     time.Time
	err      error
}

func (w *brewActivity) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.last = time.Now()
	if _, err := w.log.Write(b); err != nil && w.err == nil {
		w.err = err
	}
	if w.observer != nil {
		return w.observer.Write(b)
	}
	return len(b), nil
}

func (w *brewActivity) reportSilence(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	quiet := now.Sub(w.last).Truncate(time.Second)
	if quiet < time.Minute {
		return
	}
	// Silence is not proof of a hang. Keep the last known package visible and
	// point to the live log rather than claiming progress or killing installers.
	if observer, ok := w.observer.(*brewOutputProgress); ok {
		name := observer.active
		if name == "" {
			name = "Homebrew"
		}
		observer.progress.Update(name, fmt.Sprintf("no output for %s; see last-run.log", quiet))
	}
}
