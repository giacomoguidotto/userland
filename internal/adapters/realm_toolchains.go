package adapters

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
	realmstate "github.com/giacomoguidotto/userland/internal/realm"
)

func realmToolchains(c *Context, action Action) int {
	configurations, err := realmstate.New(c.Env).Configurations()
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	missingByRealm := make(map[string][]string, len(configurations))
	for _, configuration := range configurations {
		missing, inspectErr := realmMissingTools(c, configuration.Root)
		if inspectErr != nil {
			c.Log(Attention, configuration.Name+" realm toolchain could not be inspected: "+inspectErr.Error())
			return 1
		}
		missingByRealm[configuration.Name] = missing
	}

	switch action {
	case Plan:
		for _, configuration := range configurations {
			for _, tool := range missingByRealm[configuration.Name] {
				addPlan(c.Plan, plan.Item{
					Area: plan.AreaApps, Action: "install", Handling: plan.Automatic, Ownership: "declared",
					Target: tool, Detail: "install the " + configuration.Name + " realm tool", Proof: "realm-tool:" + configuration.Name + ":" + tool,
				})
			}
		}
		if realmMissingCount(missingByRealm) == 0 {
			c.Log(Current, "selected realm toolchains are installed")
		}
		return 0
	case Doctor:
		for _, configuration := range configurations {
			for _, tool := range missingByRealm[configuration.Name] {
				c.Log(Attention, configuration.Name+" realm tool is missing: "+tool)
			}
		}
		if realmMissingCount(missingByRealm) == 0 {
			c.Log(Healthy, "selected realm toolchains are installed")
			return 0
		}
		return 2
	}

	total := realmMissingCount(missingByRealm)
	current := 0
	for _, configuration := range configurations {
		missing := missingByRealm[configuration.Name]
		if len(missing) == 0 {
			continue
		}
		progress := newRealmToolProgress(missing, func(tool string) {
			current++
			c.ReportProgress(current, total, configuration.Name+" · "+tool)
		})
		invocation := realmMiseInvocation(c, configuration.Root, "install", "--yes")
		result := runInvocationObserved(c, nil, progress, invocation)
		progress.Flush()
		if result.Code != 0 {
			c.Log(Attention, configuration.Name+" realm tool installation failed: "+firstLine(result.Output))
			return result.Code
		}
		for _, tool := range missing {
			progress.Report(tool)
		}
	}
	return 0
}

func realmMissingTools(c *Context, root string) ([]string, error) {
	declared, err := declaredTools(filepath.Join(root, "mise.toml"))
	if err != nil {
		return nil, err
	}
	result := runInvocation(c, nil, realmMiseInvocation(c, root, "ls", "--missing", "--json"))
	if result.Code != 0 {
		return nil, fmt.Errorf("mise exited with status %d", result.Code)
	}
	missing := make(map[string]json.RawMessage)
	if err := json.Unmarshal(result.Output, &missing); err != nil {
		return nil, fmt.Errorf("mise returned unreadable missing-tool state")
	}
	var ordered []string
	for _, tool := range declared {
		if _, ok := missing[tool]; ok {
			ordered = append(ordered, tool)
		}
	}
	return ordered, nil
}

func realmMiseInvocation(c *Context, root string, args ...string) platform.Invocation {
	return platform.Invocation{
		Environ: c.Env.With("MISE_OVERRIDE_CONFIG_FILENAMES", "mise.toml", "MISE_AUTO_INSTALL", "0"),
		Name:    c.Env.Mise,
		Args:    append([]string{"-C", root}, args...),
	}
}

func realmMissingCount(values map[string][]string) int {
	total := 0
	for _, tools := range values {
		total += len(tools)
	}
	return total
}

type realmToolProgress struct {
	pending string
	wanted  map[string]bool
	report  func(string)
}

func newRealmToolProgress(tools []string, report func(string)) *realmToolProgress {
	wanted := make(map[string]bool, len(tools))
	for _, tool := range tools {
		wanted[tool] = true
	}
	return &realmToolProgress{wanted: wanted, report: report}
}

func (p *realmToolProgress) Write(value []byte) (int, error) {
	p.pending += strings.ReplaceAll(string(value), "\r", "\n")
	for {
		line, rest, found := strings.Cut(p.pending, "\n")
		if !found {
			break
		}
		p.observe(line)
		p.pending = rest
	}
	return len(value), nil
}

func (p *realmToolProgress) Flush() {
	if p.pending != "" {
		p.observe(p.pending)
		p.pending = ""
	}
}

func (p *realmToolProgress) Report(tool string) {
	if !p.wanted[tool] {
		return
	}
	delete(p.wanted, tool)
	p.report(tool)
}

func (p *realmToolProgress) observe(line string) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "mise" {
		return
	}
	tool := fields[1]
	if marker := strings.LastIndexByte(tool, '@'); marker > 0 {
		tool = tool[:marker]
	}
	p.Report(tool)
}

var _ io.Writer = (*realmToolProgress)(nil)
