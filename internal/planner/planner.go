// Package planner collects a read-only machine plan and renders its stable CLI contract.
package planner

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
	"runtime"
	"strings"

	"github.com/giacomoguidotto/userland/internal/adapters"
	"github.com/giacomoguidotto/userland/internal/managedfiles"
	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
	"github.com/giacomoguidotto/userland/internal/repository"
	"github.com/giacomoguidotto/userland/internal/tui"
)

type runner struct {
	ctx     context.Context
	environ []string
	env     map[string]string
	out     io.Writer
	render  tui.Renderer
	root    string
	cache   string
	state   string
	runLog  string
	machine platform.Environment
}

// Run collects and renders a standalone plan. It never applies machine changes.
func Run(ctx context.Context, environ []string, out io.Writer) error {
	return execute(ctx, environ, out, true, nil)
}

// Embedded collects and renders the approved sync plan without a second title
// or terminal summary.
func Embedded(ctx context.Context, environ []string, out io.Writer) (*plan.Plan, string, error) {
	var value *plan.Plan
	var runLog string
	err := execute(ctx, environ, out, false, func(collected *plan.Plan, details string) {
		value, runLog = collected, details
	})
	return value, runLog, err
}

func execute(ctx context.Context, environ []string, out io.Writer, standalone bool, collected func(*plan.Plan, string)) error {
	env := environment(environ)
	r := &runner{
		ctx: ctx, environ: environ, env: env, out: out,
		render: tui.New(out, environ),
		root:   env["USERLAND_ROOT"], cache: env["USERLAND_CACHE_DIR"], state: env["USERLAND_STATE_DIR"],
		machine: platform.NewEnvironment(environ),
	}
	if r.root == "" || r.cache == "" || r.state == "" {
		return errors.New("Userland paths are not configured")
	}
	if contents, err := os.ReadFile(filepath.Join(r.root, "cfg", "schema-version")); err != nil || strings.TrimSpace(string(contents)) != "2" {
		return errors.New("state schema is missing")
	}
	mise := env["USERLAND_MISE"]
	if info, err := os.Stat(mise); mise == "" || err != nil || info.Mode()&0o111 == 0 {
		return errors.New("mise is missing; run the public bootstrap command first")
	}
	if err := os.MkdirAll(r.cache, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(r.state, 0o700); err != nil {
		return err
	}
	r.runLog = r.state + "/last-run.log"
	if err := os.WriteFile(r.runLog, nil, 0o600); err != nil {
		return err
	}
	if standalone {
		r.render.Command("plan", "Preview declared state without applying it.")
		if r.render.Rich() {
			r.render.Spacer()
		}
	}
	if err := r.nativeTask("Refreshing repository index", func() ([]byte, error) {
		message, err := repository.RefreshSnapshot(ctx, r.machine)
		if err != nil {
			return nil, err
		}
		return []byte("[ok] " + message + "\n"), nil
	}); err != nil {
		return err
	}

	value := plan.New()
	output, err := r.miseOutput("Inspecting applications and resources", "bootstrap", "plan", "--json")
	if err != nil {
		return err
	}
	if err := importResources(value, output); err != nil {
		return errors.New("mise returned an unreadable plan; no approval was requested")
	}
	output, err = r.miseOutput("Inspecting rolling package upgrades", "bootstrap", "packages", "upgrade", "--dry-run", "--yes", "--jobs", jobs(env))
	if err != nil {
		return err
	}
	importRolling(value, output)
	output, err = r.miseOutput("Inspecting managed files", "bootstrap", "dotfiles", "status", "--json")
	if err != nil {
		return err
	}
	if err := importDotfiles(value, output); err != nil {
		return errors.New("mise returned unreadable managed-path status; no approval was requested")
	}
	if isMacOS(env) {
		output, err = r.miseOutput("Inspecting macOS settings", "bootstrap", "macos", "defaults", "status", "--json")
		if err != nil {
			return err
		}
		if err := importDefaults(value, output); err != nil {
			return errors.New("mise returned unreadable macOS-defaults status; no approval was requested")
		}
	}
	managedfiles.Manager{Env: r.machine}.PlanLegacy(value)
	if err := r.nativeTask("Inspecting personal state", func() ([]byte, error) {
		result := adapters.Run(ctx, r.machine, adapters.Plan, nil, false, value)
		if result.Code != 0 {
			return nil, fmt.Errorf("personal state inspection failed (exit %d)", result.Code)
		}
		return nil, nil
	}); err != nil {
		return err
	}
	r.render.Plan(value, r.runLog)
	if collected != nil {
		collected(value, r.runLog)
	}
	if standalone {
		r.render.SummaryOK("Plan complete. No changes were applied.")
	}
	return nil
}

func (r *runner) nativeTask(label string, operation func() ([]byte, error)) error {
	if !r.render.Rich() {
		r.render.Status(tui.StatusInfo, label)
	} else {
		r.render.BeginTask(label)
	}
	output, err := operation()
	r.render.ClearTask()
	if !r.render.Rich() && len(output) != 0 {
		_, _ = r.out.Write(output)
	}
	r.appendLog(label, output)
	if err != nil {
		return fmt.Errorf("%s failed: %w", label, err)
	}
	if r.render.Rich() {
		r.render.TaskSuccess(label)
	}
	return nil
}

func isMacOS(env map[string]string) bool {
	if name := env["USERLAND_UNAME"]; name != "" {
		return name == "Darwin"
	}
	return runtime.GOOS == "darwin"
}

func (r *runner) task(label string, command *exec.Cmd) error {
	if !r.render.Rich() {
		r.render.Status(tui.StatusInfo, label)
	} else {
		r.render.BeginTask(label)
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	r.render.ClearTask()
	if !r.render.Rich() && output.Len() > 0 {
		_, _ = r.out.Write(output.Bytes())
	}
	r.appendLog(label, output.Bytes())
	if err != nil {
		return fmt.Errorf("%s failed: %w", label, err)
	}
	if r.render.Rich() {
		r.render.TaskSuccess(label)
	}
	return nil
}

func (r *runner) miseOutput(label string, args ...string) ([]byte, error) {
	invocation := r.machine.MiseInvocation(args...)
	command := exec.CommandContext(r.ctx, invocation.Name, invocation.Args...)
	command.Env = invocation.Environ
	if !r.render.Rich() {
		r.render.Status(tui.StatusInfo, label)
	} else {
		r.render.BeginTask(label)
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	r.render.ClearTask()
	r.appendLog(label, output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", label, err)
	}
	if r.render.Rich() {
		r.render.TaskSuccess(label)
	}
	return output.Bytes(), nil
}

func (r *runner) appendLog(label string, output []byte) {
	file, err := os.OpenFile(r.runLog, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "\n## %s\n", label)
	_, _ = file.Write(output)
}

func importResources(value *plan.Plan, encoded []byte) error {
	var document struct {
		Resources []struct {
			ID               struct{ Kind, Name string } `json:"id"`
			Current, Desired any
			Action           string
		} `json:"resources"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		return err
	}
	for _, resource := range document.Resources {
		current, desired, proof := fmt.Sprint(resource.Current), fmt.Sprint(resource.Desired), "mise:"+resource.ID.Kind+":"+resource.ID.Name
		switch resource.Action {
		case "noop", "unchanged":
		case "remove":
			_ = value.Add(plan.Item{Area: plan.AreaCleanup, Action: "remove", Handling: plan.Automatic, Ownership: "declared", Target: resource.ID.Name, Detail: current + " to absent", Proof: proof})
		case "create", "update":
			if resource.ID.Kind == "package" {
				action, detail := plan.Action("upgrade"), "upgrade with Homebrew"
				if resource.Action == "create" {
					action, detail = "install", "migrate to Homebrew"
				}
				_ = value.Add(plan.Item{Area: plan.AreaApps, Action: action, Handling: plan.Automatic, Ownership: "declared", Target: strings.TrimPrefix(resource.ID.Name, "brew:"), Detail: detail, Proof: proof})
			} else if resource.ID.Kind == "file" || resource.ID.Kind == "directory" {
				_ = value.Add(plan.Item{Area: plan.AreaFS, Action: plan.Action(resource.Action), Handling: plan.Automatic, Ownership: "declared", Target: resource.ID.Name, Detail: current + " to " + desired, Proof: proof})
			} else {
				_ = value.Add(plan.Item{Area: plan.AreaOS, Action: "set", Handling: plan.Automatic, Ownership: "declared", Target: resource.ID.Name, Detail: current + " to " + desired, Proof: proof})
			}
		default:
			_ = value.Add(plan.Item{Area: plan.AreaOS, Action: "review", Handling: plan.Blocked, Ownership: "declared", Target: resource.ID.Name, Detail: "mise reported unknown action: " + resource.Action, Proof: proof})
		}
	}
	return nil
}
func importRolling(value *plan.Plan, output []byte) {
	seen := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || (f[0] != "repair" && f[0] != "pour" && f[0] != "build") || (f[0] != "repair" && !strings.Contains(line, "(requested")) {
			continue
		}
		artifact := strings.TrimSuffix(f[1], ":")
		i := strings.LastIndex(artifact, "/")
		if i < 1 || seen[artifact[:i]] {
			continue
		}
		name, version := artifact[:i], artifact[i+1:]
		seen[name] = true
		_ = value.Add(plan.Item{Area: plan.AreaApps, Action: "upgrade", Handling: plan.Automatic, Ownership: "declared", Target: name, Detail: "upgrade installed rolling package to " + version, Proof: "mise:rolling-upgrade:brew:" + name})
	}
}
func importDotfiles(value *plan.Plan, encoded []byte) error {
	var document struct {
		Files []struct{ State, Source, Target string } `json:"files"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		return err
	}
	for _, file := range document.Files {
		switch file.State {
		case "applied":
		case "missing", "differs":
			_ = value.Add(plan.Item{Area: plan.AreaFS, Action: "link", Handling: plan.Automatic, Ownership: "declared", Target: file.Target, Detail: "from " + file.Source, Proof: "dotfile:" + file.Target})
		case "source_missing":
			_ = value.Add(plan.Item{Area: plan.AreaFS, Action: "review", Handling: plan.Blocked, Ownership: "declared", Target: file.Target, Detail: "managed source is missing: " + file.Source, Proof: "dotfile:" + file.Target})
		default:
			_ = value.Add(plan.Item{Area: plan.AreaFS, Action: "review", Handling: plan.Blocked, Ownership: "declared", Target: file.Target, Detail: "unknown managed-path state: " + file.State, Proof: "dotfile:" + file.Target})
		}
	}
	return nil
}

func importDefaults(value *plan.Plan, encoded []byte) error {
	var document struct {
		Defaults struct {
			Available bool `json:"available"`
			Entries   []struct {
				State   string `json:"state"`
				Domain  string `json:"domain"`
				Key     string `json:"key"`
				Current any    `json:"current"`
				Value   any    `json:"value"`
			} `json:"entries"`
		} `json:"macos_defaults"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		return err
	}
	if !document.Defaults.Available {
		return nil
	}
	for _, entry := range document.Defaults.Entries {
		switch entry.State {
		case "set":
			continue
		case "differs", "missing", "unset":
			if err := value.Add(plan.Item{
				Area: plan.AreaOS, Action: "set", Handling: plan.Automatic, Ownership: "declared",
				Target: defaultDomain(entry.Domain) + " · " + entry.Key,
				Detail: fmt.Sprint(entry.Current) + " to " + fmt.Sprint(entry.Value),
				Proof:  "macos-default:" + entry.Domain + ":" + entry.Key,
			}); err != nil {
				return err
			}
		default:
			if err := value.Add(plan.Item{
				Area: plan.AreaOS, Action: "review", Handling: plan.Blocked, Ownership: "declared",
				Target: "macOS defaults", Detail: "unknown defaults state: " + entry.State, Proof: "macos-defaults-status",
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func defaultDomain(domain string) string {
	switch domain {
	case "NSGlobalDomain":
		return "Global"
	case "com.apple.finder":
		return "Finder"
	case "com.apple.dock":
		return "Dock"
	case "com.apple.spaces":
		return "Spaces"
	case "com.apple.AppleMultitouchTrackpad":
		return "Trackpad"
	case "com.apple.HIToolbox":
		return "Keyboard"
	default:
		return domain
	}
}
func jobs(env map[string]string) string {
	if env["USERLAND_JOBS"] != "" {
		return env["USERLAND_JOBS"]
	}
	return "4"
}
func environment(environ []string) map[string]string {
	values := map[string]string{}
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}
