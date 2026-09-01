// Package adapters reconciles personal resources that mise does not own.
package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/giacomoguidotto/userland/internal/csvfile"
	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
)

type Action uint8

const (
	Plan Action = iota
	Apply
	Doctor
)

type Level string

const (
	Healthy   Level = "healthy"
	Current   Level = "current"
	Changed   Level = "changed"
	Change    Level = "change"
	Manual    Level = "manual"
	Attention Level = "attention"
)

type Event struct {
	Level   Level
	Message string
}

type Context struct {
	Context  context.Context
	Env      platform.Environment
	Stdin    io.Reader
	Output   io.Writer
	Events   []Event
	Plan     *plan.Plan
	Terminal bool
	commands chan struct{}
}

func (c *Context) Log(level Level, message string) {
	c.Events = append(c.Events, Event{Level: level, Message: message})
}

type adapter struct {
	name        string
	label       string
	area        plan.Area
	action      plan.Action
	attention   plan.Handling
	run         func(*Context, Action) int
	enabled     func(platform.Environment) bool
	directApply bool
}

var registry = []adapter{
	{name: "toolchain-health", label: "Toolchain health", area: plan.AreaApps, action: "install", attention: plan.Blocked, run: toolchain},
	{name: "homebrew-apps", label: "Homebrew applications", area: plan.AreaApps, action: "install", attention: plan.Blocked, run: homebrew},
	{name: "android-sdk", label: "Android development tools", area: plan.AreaApps, action: "install", attention: plan.Blocked, run: androidSDK, directApply: true},
	{name: "personal-auth", label: "Personal authentication", area: plan.AreaOS, action: "configure", attention: plan.Attended, run: personalAuthentication, enabled: machineClosureEnabled, directApply: true},
	{name: "personal-repos", label: "Personal repositories", area: plan.AreaFS, action: "clone", attention: plan.Blocked, run: personalRepositories},
	{name: "realm-selection", label: "Realm selection", area: plan.AreaFS, action: "configure", attention: plan.Attended, run: realmSelection, enabled: machineClosureEnabled, directApply: true},
	{name: "realms", label: "Realms", area: plan.AreaFS, action: "update", attention: plan.Blocked, run: realms, enabled: realmsEnabled},
	{name: "realm-auth", label: "Realm authentication", area: plan.AreaOS, action: "configure", attention: plan.Attended, run: realmAuthentication, enabled: realmAuthenticationEnabled, directApply: true},
	{name: "browser-extensions", label: "Browser extensions", area: plan.AreaApps, action: "install", attention: plan.Blocked, run: browserExtensions},
	{name: "file-handlers", label: "File handlers", area: plan.AreaOS, action: "set", attention: plan.Automatic, run: fileHandlers},
	{name: "login-items", label: "Login items", area: plan.AreaOS, action: "set", attention: plan.Automatic, run: loginItems, enabled: machineClosureEnabled},
	{name: "raycast", label: "Raycast configuration", area: plan.AreaApps, action: "configure", attention: plan.Blocked, run: raycast, directApply: true},
	{name: "shell-cache", label: "Shell cache", area: plan.AreaFS, action: "update", attention: plan.Blocked, run: shellCache},
	{name: "manual-apps", label: "Manual applications", area: plan.AreaApps, action: "install", attention: plan.Blocked, run: manualApps},
	{name: "repository-snapshot", label: "Repository snapshot", area: plan.AreaFS, action: "update", attention: plan.Blocked, run: repositorySnapshot},
	{name: "security-health", label: "Security health", area: plan.AreaOS, action: "set", attention: plan.Blocked, run: securityHealth},
}

func machineClosureEnabled(env platform.Environment) bool { return !env.Bool("USERLAND_TESTING") }

func realmAuthenticationEnabled(env platform.Environment) bool {
	return machineClosureEnabled(env) && realmsEnabled(env)
}

type Result struct {
	Events    []Event
	Attention bool
	Code      int
}

func Run(ctx context.Context, env platform.Environment, action Action, stdin io.Reader, terminal bool, value *plan.Plan) Result {
	return runRegistry(ctx, env, action, stdin, terminal, value, nil, nil)
}

type Observer func(label string, events []Event, code int)

func RunObserved(ctx context.Context, env platform.Environment, action Action, stdin io.Reader, terminal bool, observer Observer) Result {
	return runRegistry(ctx, env, action, stdin, terminal, nil, nil, observer)
}

func RunTasks(ctx context.Context, env platform.Environment, action Action, stdin io.Reader, output io.Writer, terminal bool, begin func(string), end Observer) Result {
	return runRegistryOutput(ctx, env, action, stdin, output, terminal, nil, begin, end)
}

func runRegistry(ctx context.Context, env platform.Environment, action Action, stdin io.Reader, terminal bool, value *plan.Plan, begin func(string), observer Observer) Result {
	return runRegistryOutput(ctx, env, action, stdin, io.Discard, terminal, value, begin, observer)
}

func runRegistryOutput(ctx context.Context, env platform.Environment, action Action, stdin io.Reader, output io.Writer, terminal bool, value *plan.Plan, begin func(string), observer Observer) Result {
	if action != Apply {
		return runReadOnlyRegistry(ctx, env, action, stdin, output, terminal, value, begin, observer)
	}
	return runSerialRegistry(ctx, env, action, stdin, output, terminal, value, begin, observer)
}

func runSerialRegistry(ctx context.Context, env platform.Environment, action Action, stdin io.Reader, output io.Writer, terminal bool, value *plan.Plan, begin func(string), observer Observer) Result {
	result := Result{}
	for _, item := range registry {
		if item.enabled != nil && !item.enabled(env) {
			continue
		}
		if begin != nil {
			begin(item.label)
		}
		invocation := &Context{Context: ctx, Env: env, Stdin: stdin, Output: output, Plan: value, Terminal: terminal}
		code := item.run(invocation, action)
		if observer != nil {
			observer(item.label, invocation.Events, code)
		}
		if action == Plan {
			capturePlan(value, item, invocation.Events)
		}
		result.Events = append(result.Events, invocation.Events...)
		if code == 2 {
			result.Attention = true
			continue
		}
		if code != 0 {
			result.Code = code
			if action == Apply {
				return result
			}
		}
	}
	return finishResult(result, action)
}

type adapterExecution struct {
	item       adapter
	invocation *Context
	code       int
}

func runReadOnlyRegistry(ctx context.Context, env platform.Environment, action Action, stdin io.Reader, output io.Writer, terminal bool, value *plan.Plan, begin func(string), observer Observer) Result {
	items := make([]adapter, 0, len(registry))
	for _, item := range registry {
		if item.enabled == nil || item.enabled(env) {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return Result{}
	}

	const parallelism = 4
	commands := make(chan struct{}, parallelism)
	executions := make([]adapterExecution, len(items))
	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := min(parallelism, len(items))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				item := items[index]
				var localPlan *plan.Plan
				if action == Plan && value != nil {
					localPlan = plan.New()
				}
				invocation := &Context{
					Context: ctx, Env: env, Stdin: stdin, Output: output, Plan: localPlan,
					Terminal: terminal, commands: commands,
				}
				executions[index] = adapterExecution{item: item, invocation: invocation, code: item.run(invocation, action)}
			}
		}()
	}
	for index := range items {
		jobs <- index
	}
	close(jobs)
	workers.Wait()

	result := Result{}
	for _, execution := range executions {
		if begin != nil {
			begin(execution.item.label)
		}
		if observer != nil {
			observer(execution.item.label, execution.invocation.Events, execution.code)
		}
		if action == Plan {
			if execution.invocation.Plan != nil {
				for _, item := range execution.invocation.Plan.Items() {
					_ = value.Add(item)
				}
			}
			capturePlan(value, execution.item, execution.invocation.Events)
		}
		result.Events = append(result.Events, execution.invocation.Events...)
		if execution.code == 2 {
			result.Attention = true
		} else if execution.code != 0 {
			result.Code = execution.code
		}
	}
	return finishResult(result, action)
}

func finishResult(result Result, action Action) Result {
	if result.Code != 0 {
		return result
	}
	if action == Doctor && result.Attention {
		result.Code = 2
	}
	return result
}

func Labels() []string {
	labels := make([]string, 0, len(registry))
	for _, item := range registry {
		labels = append(labels, item.label)
	}
	return labels
}

func DirectApply(label string) bool {
	for _, item := range registry {
		if item.label == label {
			return item.directApply
		}
	}
	return false
}

func capturePlan(value *plan.Plan, item adapter, events []Event) {
	if value == nil {
		return
	}
	for _, event := range events {
		var handling plan.Handling
		switch event.Level {
		case Change, Changed:
			handling = plan.Automatic
		case Manual:
			handling = plan.Attended
		case Attention:
			handling = item.attention
		default:
			continue
		}
		_ = value.Add(plan.Item{Area: item.area, Action: item.action, Handling: handling, Ownership: "declared", Target: event.Message})
	}
}

func run(c *Context, name string, args ...string) platform.Result {
	return limitedRun(c, func() platform.Result {
		return platform.Run(c.Context, c.Env.List, nil, name, args...)
	})
}

func runMise(c *Context, args ...string) platform.Result {
	return limitedRun(c, func() platform.Result {
		return c.Env.RunMise(c.Context, nil, args...)
	})
}

func runWith(c *Context, environ []string, stdin io.Reader, name string, args ...string) platform.Result {
	return limitedRun(c, func() platform.Result {
		return platform.Run(c.Context, environ, stdin, name, args...)
	})
}

func runInvocation(c *Context, stdin io.Reader, invocation platform.Invocation) platform.Result {
	return limitedRun(c, func() platform.Result {
		return platform.RunInvocation(c.Context, stdin, invocation)
	})
}

func limitedRun(c *Context, operation func() platform.Result) platform.Result {
	if c.commands == nil {
		return operation()
	}
	select {
	case c.commands <- struct{}{}:
		defer func() { <-c.commands }()
		return operation()
	case <-c.Context.Done():
		return platform.Result{Code: 1, Err: c.Context.Err()}
	}
}

func parallelReadOnly(c *Context, count int, operation func(int)) {
	if c.commands == nil {
		for index := range count {
			operation(index)
		}
		return
	}
	var group sync.WaitGroup
	for index := range count {
		group.Add(1)
		go func() {
			defer group.Done()
			operation(index)
		}()
	}
	group.Wait()
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func readCSV(path string, header ...string) ([][]string, error) { return csvfile.Read(path, header) }

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func firstLine(value []byte) string {
	line, _, _ := strings.Cut(string(value), "\n")
	return strings.TrimSpace(line)
}

func commandPath(c *Context, override, name string) (string, bool) {
	if value := c.Env.Get(override); value != "" && executable(value) {
		return value, true
	}
	return platform.LookPath(c.Env.List, name)
}

var errAttention = errors.New("attention required")

func addPlan(value *plan.Plan, item plan.Item) {
	if value != nil {
		_ = value.Add(item)
	}
}

func homePath(c *Context, parts ...string) string {
	return filepath.Join(append([]string{c.Env.Home}, parts...)...)
}
