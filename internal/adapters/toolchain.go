package adapters

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
)

type toolProbe struct {
	id      string
	command string
	args    []string
}

type toolProblem struct {
	id      string
	repair  string
	command string
}

func toolchain(c *Context, action Action) int {
	if c.Env.Bool("USERLAND_TESTING") && !c.Env.Bool("TEST_TOOLCHAIN_HEALTH") {
		if action == Plan {
			c.Log(Current, "test toolchain fixture is healthy")
		} else if action == Doctor {
			c.Log(Healthy, "test toolchain fixture is healthy")
		}
		return 0
	}
	probes, complete := toolProbes(c)
	problems := toolProblems(c, probes, complete)
	if action == Plan {
		for _, problem := range problems {
			switch problem.repair {
			case "promote":
				addPlan(c.Plan, plan.Item{Area: plan.AreaFS, Action: "update", Handling: plan.Automatic, Ownership: "declared", Target: homePath(c, ".local", "bin", "mise"), Detail: "atomically promote the pinned Userland mise launcher", Proof: "toolchain:mise-launcher"})
			case "reinstall":
				addPlan(c.Plan, plan.Item{Area: plan.AreaApps, Action: "update", Handling: plan.Automatic, Ownership: "declared", Target: problem.command, Detail: "reinstall only the corrupted pinned tool " + problem.id, Proof: "toolchain:reinstall:" + problem.id})
			case "activate":
				addPlan(c.Plan, plan.Item{Area: plan.AreaFS, Action: "update", Handling: plan.Automatic, Ownership: "declared", Target: problem.command + " shim", Detail: "activate pinned " + problem.id + " in clean global shells", Proof: "toolchain:activate:" + problem.id})
			case "review":
				addPlan(c.Plan, plan.Item{Area: plan.AreaApps, Action: "review", Handling: plan.Blocked, Ownership: "declared", Target: "tool probe manifest", Detail: "add one executable probe for every pinned mise tool", Proof: "toolchain:probe-manifest"})
			}
		}
		if len(problems) == 0 {
			c.Log(Current, "every pinned tool executes and is active in clean global shells")
		}
		return 0
	}
	if action == Doctor {
		for _, problem := range problems {
			switch problem.repair {
			case "promote":
				c.Log(Attention, "public mise launcher is older than Userland pinned mise")
			case "reinstall":
				c.Log(Attention, problem.command+" cannot execute; pinned "+problem.id+" needs reinstall")
			case "activate":
				c.Log(Attention, problem.command+" is not executable through the clean global shim")
			case "review":
				c.Log(Attention, "tool probe manifest does not cover every pinned mise tool")
			}
		}
		if len(problems) == 0 {
			c.Log(Healthy, "every pinned tool executes and is active in clean global shells")
			return 0
		}
		return 2
	}
	if !toolPublicMiseCurrent(c) {
		if code := promoteMise(c); code != 0 {
			return code
		}
	}
	if !complete {
		c.Log(Attention, "tool probe manifest is incomplete")
		return 1
	}
	reinstalled := false
	for _, probe := range probes {
		if probePinned(c, probe) {
			continue
		}
		c.Log(Changed, "reinstalling affected pinned tool: "+probe.id)
		invocation := c.Env.MiseInvocation("install", "--force", "--yes", probe.id)
		invocation = invocation.WithEnvironment("MISE_QUIET", "true")
		if result := runInvocation(c, nil, invocation); result.Code != 0 {
			return result.Code
		}
		if !probePinned(c, probe) {
			c.Log(Attention, probe.command+" still cannot execute after targeted reinstall")
			return 1
		}
		reinstalled = true
	}
	if reinstalled {
		environ := c.Env.With("MISE_QUIET", "true")
		return runWith(c, environ, nil, c.Env.Mise, "reshim").Code
	}
	return 0
}

func toolProbes(c *Context) ([]toolProbe, bool) {
	rows, err := readCSV(filepath.Join(c.Env.Root, "cfg", "tool-probes.csv"), "mise_tool", "command", "version_probe_arguments")
	if err != nil {
		return nil, false
	}
	probes := make([]toolProbe, 0, len(rows))
	probed := make([]string, 0, len(rows))
	for _, row := range rows {
		probes = append(probes, toolProbe{row[0], row[1], strings.Fields(row[2])})
		probed = append(probed, row[0])
	}
	declared, err := declaredTools(filepath.Join(c.Env.Root, "cfg", "mise.toml"))
	if err != nil {
		return probes, false
	}
	sort.Strings(probed)
	sort.Strings(declared)
	return probes, strings.Join(unique(probed), "\n") == strings.Join(unique(declared), "\n")
}

func declaredTools(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	inside := false
	var result []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[tools]" {
			inside = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inside = false
		}
		if !inside || !strings.Contains(line, "=") {
			continue
		}
		key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		result = append(result, strings.Trim(key, `"`))
	}
	return result, scanner.Err()
}

func unique(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func toolProblems(c *Context, probes []toolProbe, complete bool) []toolProblem {
	var result []toolProblem
	globalReady := toolPublicMiseCurrent(c)
	if !globalReady {
		result = append(result, toolProblem{"mise", "promote", "launcher"})
	}
	if !complete {
		return append(result, toolProblem{"probe-manifest", "review", "tool-probes"})
	}
	problems := make([]*toolProblem, len(probes))
	parallelReadOnly(c, len(probes), func(index int) {
		probe := probes[index]
		if !probePinned(c, probe) {
			problem := toolProblem{probe.id, "reinstall", probe.command}
			problems[index] = &problem
		} else if globalReady && !probeGlobal(c, probe) {
			problem := toolProblem{probe.id, "activate", probe.command}
			problems[index] = &problem
		}
	})
	for _, problem := range problems {
		if problem != nil {
			result = append(result, *problem)
		}
	}
	return result
}

func toolPublicMiseCurrent(c *Context) bool {
	public := homePath(c, ".local", "bin", "mise")
	if !executable(public) || !executable(c.Env.Mise) {
		return false
	}
	results := make([]platform.Result, 2)
	parallelReadOnly(c, len(results), func(index int) {
		if index == 0 {
			results[index] = run(c, c.Env.Mise, "--version")
		} else {
			results[index] = run(c, public, "--version")
		}
	})
	pinned, current := results[0], results[1]
	return pinned.Code == 0 && current.Code == 0 && safeVersion(pinned.Output) != "" && safeVersion(pinned.Output) == safeVersion(current.Output)
}

func probePinned(c *Context, probe toolProbe) bool {
	args := append([]string{"exec", "--", probe.command}, probe.args...)
	invocation := c.Env.MiseInvocation(args...)
	invocation = invocation.WithEnvironment("MISE_QUIET", "true")
	return runInvocation(c, nil, invocation).Code == 0
}

func probeGlobal(c *Context, probe toolProbe) bool {
	shim := homePath(c, ".local", "share", "mise", "shims", probe.command)
	if !executable(shim) {
		return false
	}
	environ := c.Env.With(
		"HOME", c.Env.Home,
		"PATH", strings.Join([]string{homePath(c, ".local", "share", "mise", "shims"), homePath(c, ".local", "bin"), "/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/local/sbin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, ":"),
	)
	return runWith(c, environ, nil, shim, probe.args...).Code == 0
}

func promoteMise(c *Context) int {
	target := homePath(c, ".local", "bin", "mise")
	directory := filepath.Dir(target)
	if info, err := os.Lstat(directory); err == nil && info.Mode()&os.ModeSymlink != 0 {
		c.Log(Attention, "refusing to promote mise through a symlinked ~/.local/bin")
		return 1
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return 1
	}
	temporaryDirectory, err := os.MkdirTemp(directory, ".mise.userland.")
	if err != nil {
		return 1
	}
	defer os.RemoveAll(temporaryDirectory)
	temporary := filepath.Join(temporaryDirectory, "mise")
	if err := platform.CopyFile(c.Env.Mise, temporary, 0o755); err != nil {
		return 1
	}
	if run(c, temporary, "--version").Code != 0 {
		c.Log(Attention, "pinned mise launcher failed validation before promotion")
		return 1
	}
	if err := os.Rename(temporary, target); err != nil {
		return 1
	}
	c.Log(Changed, "promoted the pinned mise launcher atomically")
	return 0
}

func safeVersion(output []byte) string {
	fields := strings.Fields(firstLine(output))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func formatProblem(problem toolProblem) string {
	return fmt.Sprintf("%s:%s:%s", problem.id, problem.repair, problem.command)
}
