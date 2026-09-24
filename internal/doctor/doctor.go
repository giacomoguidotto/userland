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

		var details strings.Builder
		for _, event := range events {
			fmt.Fprintf(&details, "[%s] %s\n", event.Level, event.Message)
		}
		appendDoctorLog(runLog, label, []byte(details.String()))
		if adapterCode == 0 {
			if render.Rich() {
				render.TaskSuccess(label)
			} else {
				render.Status(tui.StatusOK, label)
			}
		} else {
			render.Status(tui.StatusAttention, label)
			seen := map[string]bool{}
			for _, event := range events {
				if event.Level == adapters.Healthy || event.Level == adapters.Current || event.Level == adapters.Changed {
					continue
				}
				summary := diagnosticSummary(label, event.Message)
				if !seen[summary] {
					render.Status(tui.StatusInfo, summary)
					seen[summary] = true
				}
			}
			render.Status(tui.StatusInfo, "Log: "+runLog)
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

	appendDoctorLog(env.State+"/last-run.log", label, result.Output)
	if result.Code == 0 {
		if render.Rich() {
			render.TaskSuccess(label)
		} else {
			render.Status(tui.StatusOK, label)
		}
		return true
	}
	render.Status(tui.StatusAttention, label)
	render.Status(tui.StatusInfo, diagnosticSummary(label, string(result.Output)))
	render.Status(tui.StatusInfo, "Log: "+env.State+"/last-run.log")
	return false
}

func appendDoctorLog(path, label string, output []byte) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "\n## %s\n", label)
	_, _ = file.Write(output)
}

// Command transcripts belong in the log. These summaries describe the action
// needed without exposing terminal banners or wide machine-readable tables.
func diagnosticSummary(label, output string) string {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "unprotected private key"), strings.Contains(lower, "life-auth.pub") && strings.Contains(lower, "bad permissions"):
		return "SSH could not sign with life/auth through 1Password. Unlock 1Password, check that this key is available to its SSH agent, then rerun sync."
	case strings.Contains(lower, "host key verification failed"):
		return "GitHub host verification failed. Rerun sync to reconcile the managed host key; details are in the log."
	case strings.Contains(lower, "signing failed"), strings.Contains(lower, "permission denied (publickey)"):
		return "GitHub SSH authentication failed. Check life/auth in the 1Password SSH agent and approve terminal access, then rerun sync."
	case strings.Contains(lower, "fail agent config"), strings.Contains(lower, "agent config is missing"), strings.Contains(lower, "does not select life/auth"):
		return "1Password SSH agent configuration does not select life/auth. Rerun userland sync to install the managed agent policy, then unlock 1Password."
	case strings.Contains(output, "com.apple.dock") && (strings.Contains(output, "persistent-apps") || strings.Contains(output, "persistent-others")):
		return "Dock still contains pinned items. Run userland sync to clear them; machine-state details are in the log."
	case strings.Contains(lower, "agent has no identities"), strings.Contains(lower, "fail agent inventory"):
		return "1Password SSH agent is available but has no identities. Enable life/auth in 1Password, then rerun userland sync."
	case strings.Contains(lower, "fail agent signing"):
		return "1Password offered life/auth but refused signing. Approve the terminal request in 1Password, then rerun userland sync."
	case label == "Toolchain", label == "Machine state":
		return label + " needs attention. Run userland sync; diagnostic details are in the log."
	}
	if len(output) <= 240 && !strings.ContainsAny(output, "\r\n\x1b") {
		return output
	}
	return label + " needs attention. Run userland sync; diagnostic details are in the log."
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
