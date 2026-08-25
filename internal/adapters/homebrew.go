package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/platform"
	"github.com/giacomoguidotto/userland/plan"
)

const (
	homebrewCommit    = "cced90146ea6d3057c03a636b668fef177415eb3"
	homebrewInstaller = "https://raw.githubusercontent.com/Homebrew/install/" + homebrewCommit + "/install.sh"
	homebrewSHA256    = "12479a24be3f5307eecac7cde670fad7118640f031229e964f544b1367b52a41"
)

type brewIssue struct{ state, kind, name string }

func homebrew(c *Context, action Action) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	brew, present := brewCommand(c)
	if present {
		present = brewRun(c, brew, "--version").Code == 0
	}
	declarations, err := brewDeclarations(filepath.Join(c.Env.Root, "cfg", "brewfile"))
	if err != nil {
		return 1
	}
	if action == Plan && !present {
		addPlan(c.Plan, plan.Item{Area: plan.AreaApps, Action: "install", Handling: plan.Automatic, Ownership: "declared", Target: "Homebrew", Detail: "install from the pinned Homebrew installer", Proof: "homebrew:missing:Homebrew"})
		for _, declaration := range declarations {
			kind := map[string]string{"tap": "Tap", "brew": "Formula", "cask": "Cask", "mas": "App"}[declaration[0]]
			brewPlan(c, brewIssue{"missing", kind, declaration[1]})
		}
		return 0
	}
	if action == Doctor && !present {
		c.Log(Attention, "Homebrew is missing")
		return 2
	}
	if action == Apply && !present {
		if code := installHomebrew(c); code != 0 {
			return code
		}
		brew, present = brewCommand(c)
		if !present {
			return 1
		}
	}
	issues := collectBrewIssues(c, brew, declarations)
	if action == Plan {
		if len(issues) == 0 {
			c.Log(Current, "declared Homebrew applications are installed and current")
		} else {
			for _, issue := range issues {
				brewPlan(c, issue)
			}
		}
		return 0
	}
	if action == Doctor {
		if len(issues) == 0 {
			c.Log(Healthy, "declared Homebrew applications are installed and current")
			return 0
		}
		for _, issue := range issues {
			c.Log(Attention, issue.name+" is "+issue.state)
		}
		return 2
	}
	brewfile := filepath.Join(c.Env.Root, "cfg", "brewfile")
	if result := brewRun(c, brew, "bundle", "--file", brewfile, "--no-upgrade"); result.Code != 0 {
		return result.Code
	}
	var formulae, casks []string
	for _, issue := range issues {
		switch issue.state + ":" + issue.kind {
		case "outdated:brew":
			formulae = append(formulae, issue.name)
		case "outdated:cask":
			casks = append(casks, issue.name)
		case "untrusted-unused:Tap":
			if !containsBrewIssue(collectBrewTapIssues(c, brew, declarations), issue) {
				c.Log(Attention, "refusing to untap "+issue.name+" because its ownership changed")
				return 1
			}
			if result := brewRun(c, brew, "untap", issue.name); result.Code != 0 {
				return result.Code
			}
		}
	}
	if len(formulae) != 0 {
		if result := brewRun(c, brew, append([]string{"upgrade"}, formulae...)...); result.Code != 0 {
			return result.Code
		}
	}
	if len(casks) != 0 {
		if result := brewRun(c, brew, append([]string{"upgrade", "--cask"}, casks...)...); result.Code != 0 {
			return result.Code
		}
	}
	return 0
}

func brewCommand(c *Context) (string, bool) {
	if value := c.Env.Get("USERLAND_BREW"); value != "" && executable(value) {
		return value, true
	}
	if executable("/opt/homebrew/bin/brew") {
		return "/opt/homebrew/bin/brew", true
	}
	return commandPath(c, "", "brew")
}

func brewRun(c *Context, brew string, args ...string) platform.Result {
	environ := c.Env.With("HOMEBREW_NO_AUTO_UPDATE", "1", "HOMEBREW_NO_ANALYTICS", "1", "HOMEBREW_NO_ENV_HINTS", "1", "HOMEBREW_NO_COLOR", "1")
	return runWith(c, environ, nil, brew, args...)
}

func brewDeclarations(path string) ([][2]string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result [][2]string
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kind, _, ok := strings.Cut(line, " ")
		if !ok || (kind != "tap" && kind != "brew" && kind != "cask" && kind != "mas") {
			return nil, fmt.Errorf("invalid Brewfile declaration")
		}
		start := strings.IndexByte(line, '"')
		end := -1
		if start >= 0 {
			end = strings.IndexByte(line[start+1:], '"')
		}
		if start < 0 || end < 0 {
			return nil, fmt.Errorf("invalid Brewfile declaration")
		}
		result = append(result, [2]string{kind, line[start+1 : start+1+end]})
	}
	return result, nil
}

func collectBrewIssues(c *Context, brew string, declarations [][2]string) []brewIssue {
	var issues []brewIssue
	brewfile := filepath.Join(c.Env.Root, "cfg", "brewfile")
	result := brewRun(c, brew, "bundle", "check", "--file", brewfile, "--no-upgrade", "--verbose")
	if result.Code != 0 {
		for _, line := range strings.Split(string(result.Output), "\n") {
			line = strings.TrimPrefix(strings.TrimPrefix(line, "→ "), "-> ")
			suffix := " needs to be installed."
			if strings.HasSuffix(line, " needs to be tapped.") {
				suffix = " needs to be tapped."
			}
			if !strings.HasSuffix(line, suffix) {
				continue
			}
			entry := strings.TrimSuffix(line, suffix)
			kind, name, ok := strings.Cut(entry, " ")
			if !ok {
				continue
			}
			state := "missing"
			if kind == "Cask" && brewCaskAdoptable(c, brew, name) {
				state = "adoptable"
			}
			issues = append(issues, brewIssue{state, kind, name})
		}
	}
	issues = append(issues, brewOutdated(c, brew, "brew", "--formula")...)
	issues = append(issues, brewOutdated(c, brew, "cask", "--cask")...)
	issues = append(issues, collectBrewTapIssues(c, brew, declarations)...)
	return issues
}

func brewCaskAdoptable(c *Context, brew, name string) bool {
	result := brewRun(c, brew, "info", "--json=v2", "--cask", name)
	if result.Code != 0 {
		return false
	}
	var document struct {
		Casks []struct {
			Artifacts []struct {
				App    []string `json:"app"`
				Target string   `json:"target"`
			} `json:"artifacts"`
		} `json:"casks"`
	}
	if json.Unmarshal(result.Output, &document) != nil || len(document.Casks) == 0 {
		return false
	}
	for _, artifact := range document.Casks[0].Artifacts {
		target := artifact.Target
		if target == "" && len(artifact.App) != 0 {
			target = filepath.Join("/Applications", artifact.App[0])
		}
		if target != "" && exists(target) {
			return true
		}
	}
	return false
}

func brewOutdated(c *Context, brew, kind, flag string) []brewIssue {
	result := brewRun(c, brew, "outdated", flag, "--json=v2")
	if result.Code != 0 || len(result.Output) == 0 {
		return nil
	}
	var document struct {
		Formulae []struct{ Name, FullName string } `json:"formulae"`
		Casks    []struct{ Name, FullName string } `json:"casks"`
	}
	if json.Unmarshal(result.Output, &document) != nil {
		return nil
	}
	var resultIssues []brewIssue
	items := document.Formulae
	if kind == "cask" {
		items = document.Casks
	}
	for _, item := range items {
		name := item.FullName
		if name == "" {
			name = item.Name
		}
		resultIssues = append(resultIssues, brewIssue{"outdated", kind, name})
	}
	return resultIssues
}

func collectBrewTapIssues(c *Context, brew string, declarations [][2]string) []brewIssue {
	trust := brewRun(c, brew, "trust", "--json=v1")
	formulae := brewRun(c, brew, "list", "--formula", "--full-name")
	casks := brewRun(c, brew, "list", "--cask", "--full-name")
	taps := brewRun(c, brew, "tap")
	if trust.Code != 0 || formulae.Code != 0 || casks.Code != 0 || taps.Code != 0 {
		return nil
	}
	var trusted struct {
		Taps []string `json:"taps"`
	}
	if json.Unmarshal(trust.Output, &trusted) != nil {
		return nil
	}
	trustedSet, installedTaps, declared := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, name := range trusted.Taps {
		trustedSet[name] = true
	}
	for _, name := range strings.Fields(string(taps.Output)) {
		installedTaps[name] = true
	}
	for _, declaration := range declarations {
		if declaration[0] == "tap" {
			declared[declaration[1]] = true
		}
	}
	var issues []brewIssue
	for name := range declared {
		if installedTaps[name] && !trustedSet[name] {
			issues = append(issues, brewIssue{"untrusted-declared", "Tap", name})
		}
	}
	installed := string(formulae.Output) + "\n" + string(casks.Output)
	for _, name := range strings.Fields(string(taps.Output)) {
		if !declared[name] && !trustedSet[name] && !strings.Contains(installed, name+"/") {
			issues = append(issues, brewIssue{"untrusted-unused", "Tap", name})
		}
	}
	return issues
}

func brewPlan(c *Context, issue brewIssue) {
	key := issue.state + ":" + issue.kind
	action, detail := plan.Action("install"), ""
	switch key {
	case "missing:Tap":
		detail = "add and trust the declared Homebrew tap"
	case "missing:Formula":
		detail = "install with Homebrew"
	case "missing:Cask":
		detail = "install the missing Homebrew cask"
	case "missing:App":
		detail = "install from Mac App Store"
	case "adoptable:Cask":
		detail = "adopt the existing application into Homebrew ownership"
	case "outdated:brew":
		action, detail = "upgrade", "upgrade the outdated installed Homebrew formula"
	case "outdated:cask":
		action, detail = "upgrade", "upgrade the outdated installed Homebrew cask"
	case "untrusted-declared:Tap":
		action, detail = "configure", "trust the declared Homebrew tap before loading its casks"
	case "untrusted-unused:Tap":
		addPlan(c.Plan, plan.Item{Area: plan.AreaCleanup, Action: "remove", Handling: plan.Automatic, Ownership: "userland", Target: issue.name, Detail: "untap after proving no installed formula or cask depends on it", Proof: "homebrew:unused-untrusted-tap:" + issue.name})
		return
	default:
		return
	}
	addPlan(c.Plan, plan.Item{Area: plan.AreaApps, Action: action, Handling: plan.Automatic, Ownership: "declared", Target: issue.name, Detail: detail, Proof: "homebrew:" + issue.state + ":" + issue.kind + ":" + issue.name})
}

func containsBrewIssue(issues []brewIssue, expected brewIssue) bool {
	for _, issue := range issues {
		if issue == expected {
			return true
		}
	}
	return false
}

func installHomebrew(c *Context) int {
	directory, err := os.MkdirTemp(c.Env.Get("TMPDIR"), "userland-homebrew.")
	if err != nil {
		return 1
	}
	defer os.RemoveAll(directory)
	installer := filepath.Join(directory, "install.sh")
	if result := run(c, "curl", "--proto", "=https", "--tlsv1.2", "-fsSL", homebrewInstaller, "-o", installer); result.Code != 0 {
		return result.Code
	}
	digest, err := fileSHA256(installer)
	if err != nil || digest != homebrewSHA256 {
		c.Log(Attention, "Homebrew installer checksum mismatch")
		return 1
	}
	c.Log(Changed, "installing Homebrew from pinned commit "+homebrewCommit)
	environ := c.Env.With("NONINTERACTIVE", "1")
	return runWith(c, environ, c.Stdin, "/bin/bash", installer).Code
}
