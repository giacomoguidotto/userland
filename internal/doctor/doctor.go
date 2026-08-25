// Package doctor evaluates Userland health without changing machine state.
package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/giacomoguidotto/userland/internal/adapters"
	"github.com/giacomoguidotto/userland/internal/platform"
	"github.com/giacomoguidotto/userland/internal/tui"
)

var (
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type versionCheck struct {
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	Version *string `json:"version"`
}

type Report struct {
	SchemaVersion int   `json:"schema_version"`
	OK            bool  `json:"ok"`
	Checks        []any `json:"checks"`
}

// JSON returns the stable machine-readable health report and its overall state.
func JSON(ctx context.Context, environ []string) ([]byte, bool) {
	env := environment(environ)
	root := env["USERLAND_ROOT"]
	machine := platform.NewEnvironment(environ)

	versionStatus, version := probeVersion(ctx, root, env, environ)
	miseStatus, bootstrapStatus := probeMise(ctx, machine)
	adapterResult := adapters.Run(ctx, machine, adapters.Doctor, nil, false, nil)
	adapterStatus := "healthy"
	if adapterResult.Code != 0 {
		adapterStatus = "attention"
	}

	ok := (versionStatus == "current" || versionStatus == "ahead") &&
		miseStatus == "present" && bootstrapStatus == "healthy" && adapterStatus == "healthy"

	report := Report{
		SchemaVersion: 1,
		OK:            ok,
		Checks: []any{
			versionCheck{Name: "userland", Status: versionStatus, Version: version},
			Check{Name: "mise", Status: miseStatus},
			Check{Name: "bootstrap", Status: bootstrapStatus},
			Check{Name: "adapters", Status: adapterStatus},
		},
	}
	encoded, _ := json.Marshal(report)
	return append(encoded, '\n'), ok
}

func probeVersion(ctx context.Context, root string, env map[string]string, environ []string) (string, *string) {
	curl := env["USERLAND_CURL"]
	if curl == "" {
		curl = "curl"
	}
	endpoint := env["USERLAND_RELEASE_ENDPOINT"]
	if endpoint == "" {
		endpoint = "https://userland.guidotto.dev"
	}

	header, ok := run(ctx, environ, curl,
		"--proto", "=https", "--tlsv1.2", "--fail", "--location", "--silent",
		"--connect-timeout", "2", "--max-time", "5", "--range", "0-511", endpoint,
	)
	if !ok {
		return "unknown", nil
	}

	var tag, latest string
	for _, line := range strings.Split(string(header), "\n") {
		if strings.HasPrefix(line, "tag='") && strings.HasSuffix(line, "'") && tag == "" {
			tag = strings.TrimSuffix(strings.TrimPrefix(line, "tag='"), "'")
		}
		if strings.HasPrefix(line, "commit='") && strings.HasSuffix(line, "'") && latest == "" {
			latest = strings.TrimSuffix(strings.TrimPrefix(line, "commit='"), "'")
		}
	}
	if !versionPattern.MatchString(tag) || !commitPattern.MatchString(latest) {
		return "unknown", nil
	}

	local := localCommit(ctx, root, environ)
	if local == "" {
		return "unknown", nil
	}
	if local == latest {
		return "current", &tag
	}
	if _, ancestor := run(ctx, environ, "git", "-C", root, "merge-base", "--is-ancestor", latest, local); ancestor {
		return "ahead", &tag
	}
	return "outdated", &tag
}

func localCommit(ctx context.Context, root string, environ []string) string {
	if _, ok := run(ctx, environ, "git", "-C", root, "rev-parse", "--is-inside-work-tree"); ok {
		output, ok := run(ctx, environ, "git", "-C", root, "rev-parse", "HEAD^{commit}")
		if ok {
			return strings.TrimSpace(string(output))
		}
	}

	release := filepath.Join(root, ".userland-release")
	info, err := os.Lstat(release)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	contents, err := os.ReadFile(release)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(contents), "\n")
	return line
}

func probeMise(ctx context.Context, env platform.Environment) (string, string) {
	mise := env.Mise
	if mise == "" {
		return "missing", "unknown"
	}
	info, err := os.Stat(mise)
	if err != nil || info.Mode()&0o111 == 0 {
		return "missing", "unknown"
	}
	if result := env.RunMise(ctx, nil, "bootstrap", "status", "--missing"); result.Code == 0 {
		return "present", "healthy"
	}
	return "present", "drift"
}

// Human renders the stable interactive health report.
func Human(ctx context.Context, environ []string, out io.Writer, embedded bool) int {
	machine := platform.NewEnvironment(environ)
	render := tui.New(out, environ)
	if err := machine.Validate(); err != nil {
		render.Status(tui.StatusError, err.Error())
		return 1
	}
	if !embedded {
		render.Command("doctor", "Check drift and machine health. Nothing will be changed.")
	}
	_ = machine.Prepare()
	runLog := machine.State + "/last-run.log"
	if !embedded {
		_ = os.WriteFile(runLog, nil, 0o600)
	} else if _, err := os.Stat(runLog); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(runLog, nil, 0o600)
	}
	code := 0
	render.Section("Userland")
	state, version := probeVersion(ctx, machine.Root, machine.Values, environ)
	switch state {
	case "current":
		render.Status(tui.StatusOK, dereference(version)+" is current")
	case "ahead":
		render.Status(tui.StatusOK, "Userland includes changes after "+dereference(version))
	case "outdated":
		render.Status(tui.StatusAttention, "Userland is outdated; run userland sync")
		code = 1
	default:
		render.Status(tui.StatusWarning, "Could not check the latest Userland version")
		if !embedded {
			code = 1
		}
	}
	render.Section("System")
	if !miseTask(ctx, machine, render, "Toolchain", "doctor") {
		code = 1
	}
	if !miseTask(ctx, machine, render, "Machine state", "bootstrap", "status", "--missing") {
		code = 1
	}
	render.Section("Personal state")
	result := adapters.RunObserved(ctx, machine, adapters.Doctor, nil, false, func(label string, events []adapters.Event, adapterCode int) {
		if !render.Rich() {
			render.Status(tui.StatusInfo, label)
		}
		for _, event := range events {
			if !render.Rich() {
				render.Status(eventStatus(event.Level), event.Message)
			}
		}
		if adapterCode == 0 {
			render.TaskSuccess(label)
		} else if adapterCode == 1 || adapterCode == 2 {
			render.Status(tui.StatusAttention, label)
			for _, event := range events {
				render.Excerpt(eventStatus(event.Level), event.Message)
			}
			render.Status(tui.StatusInfo, "Log: "+runLog)
		} else {
			render.Status(tui.StatusError, fmt.Sprintf("%s failed (exit %d)", label, adapterCode))
		}
	})
	if result.Code != 0 {
		code = 1
	}
	if code == 0 {
		if embedded {
			render.Status(tui.StatusOK, "Userland matches the declaration")
		} else {
			render.Summary(tui.StatusOK, "Everything matches.")
		}
	} else if embedded {
		render.Status(tui.StatusAttention, "Userland found drift or a manual step")
	} else {
		render.Summary(tui.StatusAttention, "Needs attention. Review the items above.")
	}
	return code
}

func miseTask(ctx context.Context, env platform.Environment, render tui.Renderer, label string, args ...string) bool {
	if !render.Rich() {
		render.Status(tui.StatusInfo, label)
	}
	result := env.RunMise(ctx, nil, args...)
	file, _ := os.OpenFile(env.State+"/last-run.log", os.O_APPEND|os.O_WRONLY, 0o600)
	if file != nil {
		_, _ = fmt.Fprintf(file, "\n## %s\n", label)
		_, _ = file.Write(result.Output)
		_ = file.Close()
	}
	if result.Code == 0 {
		if !render.Rich() {
			for _, line := range outputLines(result.Output) {
				render.TaskLine(line)
			}
		}
		render.TaskSuccess(label)
		return true
	}
	render.Status(tui.StatusAttention, label)
	lines := outputLines(result.Output)
	if render.Rich() {
		var selected []string
		for _, line := range lines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "differs") || strings.Contains(lower, "missing") || strings.Contains(lower, "unknown") || strings.Contains(lower, "unavailable") || strings.Contains(lower, "failed") || strings.Contains(lower, "error") {
				selected = append(selected, line)
			}
		}
		if len(selected) != 0 {
			lines = selected
		}
	}
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	for _, line := range lines {
		render.TaskLine(line)
	}
	render.Status(tui.StatusInfo, "Log: "+env.State+"/last-run.log")
	return false
}

func outputLines(output []byte) []string {
	var result []string
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r", ""), "\n") {
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func eventStatus(level adapters.Level) tui.Status {
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

func dereference(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func run(ctx context.Context, environ []string, name string, args ...string) ([]byte, bool) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = environ
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &bytes.Buffer{}
	err := command.Run()
	return stdout.Bytes(), err == nil
}

func environment(environ []string) map[string]string {
	values := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}
