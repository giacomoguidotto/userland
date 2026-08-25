// Package platform owns process execution and environment discovery for Userland.
package platform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Environment struct {
	Values map[string]string
	List   []string
	Root   string
	Home   string
	Data   string
	Cache  string
	State  string
	Mise   string
}

func NewEnvironment(environ []string) Environment {
	values := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return Environment{
		Values: values, List: append([]string(nil), environ...),
		Root: values["USERLAND_ROOT"], Home: values["USERLAND_HOME"],
		Data: values["USERLAND_DATA_DIR"], Cache: values["USERLAND_CACHE_DIR"],
		State: values["USERLAND_STATE_DIR"], Mise: values["USERLAND_MISE"],
	}
}

func (e Environment) Get(name string) string { return e.Values[name] }
func (e Environment) Bool(name string) bool  { return e.Values[name] == "1" }
func (e Environment) IsMacOS() bool {
	if value := e.Get("USERLAND_UNAME"); value != "" {
		return value == "Darwin"
	}
	return runtime.GOOS == "darwin"
}
func (e Environment) Jobs() string {
	if value := e.Get("USERLAND_JOBS"); value != "" {
		return value
	}
	return "4"
}
func (e Environment) RepositoryTTL() int64 {
	value, err := strconv.ParseInt(e.Get("USERLAND_REPOSITORY_TTL_SECONDS"), 10, 64)
	if err != nil || value < 0 {
		return 86400
	}
	return value
}
func (e Environment) With(values ...string) []string {
	result := append([]string(nil), e.List...)
	for i := 0; i < len(values); i += 2 {
		prefix := values[i] + "="
		filtered := result[:0]
		for _, entry := range result {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		result = append(filtered, prefix+values[i+1])
	}
	return result
}

func (e Environment) Prepare() error {
	for _, directory := range []string{e.Data, e.Cache, filepath.Join(e.State, "receipts")} {
		if directory == "" {
			return errors.New("Userland paths are not configured")
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (e Environment) Validate() error {
	contents, err := os.ReadFile(filepath.Join(e.Root, "cfg", "schema-version"))
	if err != nil {
		return errors.New("state schema is missing")
	}
	if strings.TrimSpace(string(contents)) != "1" {
		return errors.New("state schema " + strings.TrimSpace(string(contents)) + " requires a different userland release")
	}
	if info, err := os.Stat(e.Mise); e.Mise == "" || err != nil || info.Mode()&0o111 == 0 {
		return errors.New("mise is missing; run the public bootstrap command first")
	}
	return nil
}

type Result struct {
	Output []byte
	Code   int
	Err    error
}

func Run(ctx context.Context, environ []string, stdin io.Reader, name string, args ...string) Result {
	if !strings.ContainsRune(name, os.PathSeparator) {
		if resolved, ok := LookPath(environ, name); ok {
			name = resolved
		}
	}
	command := exec.CommandContext(ctx, name, args...)
	command.Env = environ
	command.Stdin = stdin
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	err := command.Run()
	if err == nil {
		return Result{Output: output.Bytes()}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return Result{Output: output.Bytes(), Code: exit.ExitCode()}
	}
	return Result{Output: output.Bytes(), Code: 1, Err: err}
}

func LookPath(environ []string, name string) (string, bool) {
	path := ""
	for _, entry := range environ {
		if strings.HasPrefix(entry, "PATH=") {
			path = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}
	for _, directory := range filepath.SplitList(path) {
		candidate := filepath.Join(directory, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, true
		}
	}
	return "", false
}

func CopyFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
