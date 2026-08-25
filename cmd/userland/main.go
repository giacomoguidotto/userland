package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"

	userland "github.com/giacomoguidotto/userland"
)

func main() {
	environ := runtimeEnvironment(os.Environ())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(signals)
	var signalCode atomic.Int32
	go func() {
		received := <-signals
		switch received {
		case syscall.SIGHUP:
			signalCode.Store(129)
		case syscall.SIGTERM:
			signalCode.Store(143)
		default:
			signalCode.Store(130)
		}
		cancel()
	}()
	code := userland.Run(ctx, userland.Invocation{
		Args:    os.Args[1:],
		Environ: environ,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})
	if interrupted := signalCode.Load(); interrupted != 0 {
		code = userland.ExitCode(interrupted)
	}
	os.Exit(int(code))
}

func runtimeEnvironment(environ []string) []string {
	executablePath, _ := os.Executable()
	return runtimeEnvironmentFor(environ, executablePath)
}

func runtimeEnvironmentFor(environ []string, executablePath string) []string {
	values := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	home := values["HOME"]
	if executablePath != "" {
		if resolved, err := filepath.EvalSymlinks(executablePath); err == nil {
			executablePath = resolved
		}
		candidate := filepath.Dir(filepath.Dir(executablePath))
		if values["USERLAND_ROOT_EXPLICIT"] != "1" {
			if hasSchema(candidate) {
				values["USERLAND_ROOT"] = candidate
			} else if values["USERLAND_ROOT"] == "" && hasSchema(filepath.Join(home, ".userland")) {
				values["USERLAND_ROOT"] = filepath.Join(home, ".userland")
			} else if values["USERLAND_ROOT"] == "" {
				values["USERLAND_ROOT"] = candidate
			}
		} else if values["USERLAND_ROOT"] == "" {
			values["USERLAND_ROOT"] = candidate
		}
	}
	if values["USERLAND_HOME"] == "" {
		values["USERLAND_HOME"] = home
	}
	if values["USERLAND_ORIGINAL_PATH"] == "" {
		values["USERLAND_ORIGINAL_PATH"] = values["PATH"]
	}
	values["PATH"] = strings.Join([]string{
		filepath.Join(values["USERLAND_HOME"], ".local", "share", "mise", "shims"),
		filepath.Join(values["USERLAND_HOME"], ".local", "bin"),
		"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/local/sbin",
		"/usr/bin", "/bin", "/usr/sbin", "/sbin", values["PATH"],
	}, ":")
	if values["USERLAND_DATA_DIR"] == "" {
		values["USERLAND_DATA_DIR"] = first(values["XDG_DATA_HOME"], filepath.Join(home, ".local", "share")) + "/userland"
	}
	if values["USERLAND_CACHE_DIR"] == "" {
		values["USERLAND_CACHE_DIR"] = first(values["XDG_CACHE_HOME"], filepath.Join(home, ".cache")) + "/userland"
	}
	if values["USERLAND_STATE_DIR"] == "" {
		values["USERLAND_STATE_DIR"] = first(values["XDG_STATE_HOME"], filepath.Join(home, ".local", "state")) + "/userland"
	}
	values["USERLAND_MISE"] = resolveMise(values)
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func hasSchema(root string) bool {
	info, err := os.Stat(filepath.Join(root, "cfg", "schema-version"))
	return err == nil && !info.IsDir()
}

func resolveMise(values map[string]string) string {
	root, data := values["USERLAND_ROOT"], values["USERLAND_DATA_DIR"]
	for _, candidate := range []string{filepath.Join(root, "bin", "mise"), filepath.Join(data, "current", "bin", "mise")} {
		if executable(candidate) {
			return candidate
		}
	}
	releases, _ := filepath.Glob(filepath.Join(data, "releases", "v*", "bin", "mise"))
	sort.Strings(releases)
	for index := len(releases) - 1; index >= 0; index-- {
		if executable(releases[index]) {
			return releases[index]
		}
	}
	if executable(values["USERLAND_MISE"]) {
		return values["USERLAND_MISE"]
	}
	path, _ := exec.LookPath("mise")
	return path
}
func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode()&0o111 != 0
}
func first(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
