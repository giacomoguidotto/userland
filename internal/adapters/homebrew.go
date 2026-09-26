package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/platform"
	realmstate "github.com/giacomoguidotto/userland/internal/realm"
)

const (
	homebrewCommit    = "cced90146ea6d3057c03a636b668fef177415eb3"
	homebrewInstaller = "https://raw.githubusercontent.com/Homebrew/install/" + homebrewCommit + "/install.sh"
	homebrewSHA256    = "12479a24be3f5307eecac7cde670fad7118640f031229e964f544b1367b52a41"
)

type brewIssue struct{ state, kind, name string }

type brewSource struct {
	owner string
	path  string
}

func homebrew(c *Context, action Action) int {
	source := brewSource{owner: "personal", path: filepath.Join(c.Env.Root, "cfg", "brewfile")}
	return reconcileHomebrew(c, action, []brewSource{source}, true)
}

func realmHomebrew(c *Context, action Action) int {
	sources, err := realmBrewSources(c)
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	if len(sources) == 0 {
		if action == Plan {
			c.Log(Current, "selected realms declare no Homebrew applications")
		} else if action == Doctor {
			c.Log(Healthy, "selected realms declare no Homebrew applications")
		}
		return 0
	}
	return reconcileHomebrew(c, action, sources, false)
}

func reconcileHomebrew(c *Context, action Action, sources []brewSource, installManager bool) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	brew, present := brewCommand(c)
	declarations, err := brewSourceDeclarations(sources)
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	if err := validateBrewOwnership(c); err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	if action == Plan && !present {
		if installManager {
			addPlan(c.Plan, plan.Item{Area: plan.AreaApps, Action: "install", Handling: plan.Automatic, Ownership: "declared", Target: "Homebrew", Detail: "install from the pinned Homebrew installer", Proof: "homebrew:missing:Homebrew"})
		}
		for _, declaration := range declarations {
			kind := map[string]string{"tap": "Tap", "brew": "Formula", "cask": "Cask", "mas": "App"}[declaration[0]]
			brewPlan(c, brewIssue{"missing", kind, declaration[1]})
		}
		return 0
	}
	if action == Doctor && !present {
		if installManager {
			c.Log(Attention, "Homebrew is missing")
		} else {
			c.Log(Attention, "realm applications require Homebrew")
		}
		return 2
	}
	if action == Apply && !present {
		if !installManager {
			c.Log(Attention, "realm applications require Homebrew")
			return 1
		}
		if code := installHomebrew(c); code != 0 {
			return code
		}
		brew, present = brewCommand(c)
		if !present {
			return 1
		}
	}
	issues := collectBrewIssues(c, brew, declarations, sources)
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
	progress := newBrewProgress(c, issues)
	for _, source := range sources {
		bundleNames := brewBundleProgressNames(issues, source.path)
		observer := newBrewOutputProgress(progress, bundleNames)
		result := brewRunObserved(c, observer, brew, "bundle", "--file", source.path, "--no-upgrade")
		observer.Flush()
		if result.Code != 0 {
			return result.Code
		}
		for name := range bundleNames {
			progress.Report(name)
		}
	}
	for _, issue := range issues {
		switch issue.state + ":" + issue.kind {
		case "outdated:brew":
			progress.Report(issue.name)
			if result := brewRunObserved(c, newBrewOutputProgress(progress, map[string]bool{issue.name: true}), brew, "upgrade", issue.name); result.Code != 0 {
				return result.Code
			}
		case "outdated:cask":
			progress.Report(issue.name)
			if result := brewRunObserved(c, newBrewOutputProgress(progress, map[string]bool{issue.name: true}), brew, "upgrade", "--cask", issue.name); result.Code != 0 {
				return result.Code
			}
		case "untrusted-unused:Tap":
			if !containsBrewIssue(collectBrewTapIssues(c, brew, declarations), issue) {
				c.Log(Attention, "refusing to untap "+issue.name+" because its ownership changed")
				return 1
			}
			progress.Report(issue.name)
			if result := brewRunObserved(c, nil, brew, "untap", issue.name); result.Code != 0 {
				return result.Code
			}
		}
	}
	return 0
}

func realmBrewSources(c *Context) ([]brewSource, error) {
	configurations, err := realmstate.New(c.Env).Configurations()
	if err != nil {
		return nil, err
	}
	var sources []brewSource
	for _, configuration := range configurations {
		path := filepath.Join(configuration.Root, ".userland", "brewfile")
		info, statErr := os.Stat(path)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s realm Homebrew declaration is not a regular file", configuration.Name)
		}
		sources = append(sources, brewSource{owner: configuration.Name + " realm", path: path})
	}
	return sources, nil
}

func brewSourceDeclarations(sources []brewSource) ([][2]string, error) {
	var declarations [][2]string
	for _, source := range sources {
		items, err := brewDeclarations(source.path)
		if err != nil {
			return nil, fmt.Errorf("invalid %s Homebrew declarations: %w", source.owner, err)
		}
		declarations = append(declarations, items...)
	}
	return declarations, nil
}

func validateBrewOwnership(c *Context) error {
	sources := []brewSource{{owner: "personal", path: filepath.Join(c.Env.Root, "cfg", "brewfile")}}
	realmSources, err := realmBrewSources(c)
	if err != nil {
		return err
	}
	sources = append(sources, realmSources...)
	owners := make(map[string]string)
	for _, source := range sources {
		declarations, err := brewDeclarations(source.path)
		if err != nil {
			return fmt.Errorf("invalid %s Homebrew declarations: %w", source.owner, err)
		}
		for _, declaration := range declarations {
			key := declaration[0] + ":" + declaration[1]
			if owner, exists := owners[key]; exists {
				return fmt.Errorf("Homebrew declaration %s is owned by both %s and %s", declaration[1], owner, source.owner)
			}
			owners[key] = source.owner
		}
	}
	return nil
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

func brewRunObserved(c *Context, observer io.Writer, brew string, args ...string) platform.Result {
	environ := c.Env.With("HOMEBREW_NO_AUTO_UPDATE", "1", "HOMEBREW_NO_ANALYTICS", "1", "HOMEBREW_NO_ENV_HINTS", "1", "HOMEBREW_NO_COLOR", "1")
	return runBrewMutation(c, environ, observer, brew, args...)
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

func collectBrewIssues(c *Context, brew string, declarations [][2]string, sources []brewSource) []brewIssue {
	bundles := make([]platform.Result, len(sources))
	var outdated platform.Result
	var tapIssues []brewIssue
	parallelReadOnly(c, len(sources)+2, func(index int) {
		switch index {
		case len(sources):
			outdated = brewRun(c, brew, "outdated", "--json=v2")
		case len(sources) + 1:
			tapIssues = collectBrewTapIssues(c, brew, declarations)
		default:
			bundles[index] = brewRun(c, brew, "bundle", "check", "--file", sources[index].path, "--no-upgrade", "--verbose")
		}
	})

	var issues []brewIssue
	for _, bundle := range bundles {
		if bundle.Code == 0 {
			continue
		}
		for _, line := range strings.Split(string(bundle.Output), "\n") {
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
	issues = append(issues, brewOutdated(outdated.Output, declarations)...)
	issues = append(issues, tapIssues...)
	return issues
}

type brewProgress struct {
	context *Context
	total   int
	current int
	pending map[string]bool
}

func newBrewProgress(c *Context, issues []brewIssue) *brewProgress {
	pending := make(map[string]bool, len(issues))
	for _, issue := range issues {
		pending[issue.name] = true
	}
	return &brewProgress{context: c, total: len(pending), pending: pending}
}

func (p *brewProgress) Report(name string) {
	if p == nil || !p.pending[name] {
		return
	}
	delete(p.pending, name)
	p.current++
	p.context.ReportProgress(p.current, p.total, name)
}

func (p *brewProgress) Update(name, phase string) {
	if p == nil {
		return
	}
	p.context.ReportProgress(p.current, p.total, name+" · "+phase)
}

type brewOutputProgress struct {
	progress *brewProgress
	allowed  map[string]bool
	pending  string
	active   string
}

func newBrewOutputProgress(progress *brewProgress, allowed map[string]bool) *brewOutputProgress {
	return &brewOutputProgress{progress: progress, allowed: allowed}
}

func (p *brewOutputProgress) Write(value []byte) (int, error) {
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

func (p *brewOutputProgress) Flush() {
	if p.pending != "" {
		p.observe(p.pending)
		p.pending = ""
	}
}

func (p *brewOutputProgress) observe(line string) {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "==>"))
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	name := p.findName(line)
	if name != "" {
		p.active = name
	}
	if p.active == "" {
		return
	}
	switch fields[0] {
	case "Using":
		if name != "" {
			p.progress.Report(name)
		}
	case "Installing", "Upgrading", "Downloading", "Fetching":
		p.progress.Update(p.active, strings.ToLower(fields[0]))
	case "Moving":
		p.progress.Update(p.active, "moving application")
	case "Verifying":
		p.progress.Update(p.active, "verifying download")
	case "Extracting", "Unpacking":
		p.progress.Update(p.active, "extracting archive")
	}
}

func (p *brewOutputProgress) findName(line string) string {
	// Match tokens exactly: e.g. 'foo' must not steal 'foo-bar' progress.
	for _, field := range strings.Fields(line) {
		name := strings.Trim(field, "`'\".,:!")
		if p.allowed[name] {
			return name
		}
	}
	return ""
}

func brewBundleProgressNames(issues []brewIssue, path string) map[string]bool {
	declared, err := brewDeclarations(path)
	if err != nil {
		return nil
	}
	owned := make(map[string]bool, len(declared))
	for _, declaration := range declared {
		owned[declaration[1]] = true
	}
	result := make(map[string]bool)
	for _, issue := range issues {
		if owned[issue.name] && (issue.state == "missing" || issue.state == "adoptable") {
			result[issue.name] = true
		}
	}
	return result
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

func brewOutdated(output []byte, declarations [][2]string) []brewIssue {
	if len(output) == 0 {
		return nil
	}
	var document struct {
		Formulae []struct{ Name, FullName string } `json:"formulae"`
		Casks    []struct{ Name, FullName string } `json:"casks"`
	}
	if json.Unmarshal(output, &document) != nil {
		return nil
	}
	owned := map[string]map[string]bool{"brew": {}, "cask": {}}
	for _, declaration := range declarations {
		if names := owned[declaration[0]]; names != nil {
			names[declaration[1]] = true
		}
	}
	var resultIssues []brewIssue
	for _, item := range document.Formulae {
		name := item.FullName
		if name == "" {
			name = item.Name
		}
		if owned["brew"][name] || owned["brew"][item.Name] {
			resultIssues = append(resultIssues, brewIssue{"outdated", "brew", name})
		}
	}
	for _, item := range document.Casks {
		name := item.FullName
		if name == "" {
			name = item.Name
		}
		if owned["cask"][name] || owned["cask"][item.Name] {
			resultIssues = append(resultIssues, brewIssue{"outdated", "cask", name})
		}
	}
	return resultIssues
}

func collectBrewTapIssues(c *Context, brew string, declarations [][2]string) []brewIssue {
	results := make([]platform.Result, 3)
	parallelReadOnly(c, len(results), func(index int) {
		switch index {
		case 0:
			results[index] = brewRun(c, brew, "trust", "--json=v1")
		case 1:
			results[index] = brewRun(c, brew, "list", "--full-name")
		case 2:
			results[index] = brewRun(c, brew, "tap")
		}
	})
	trust, installed, taps := results[0], results[1], results[2]
	if trust.Code != 0 || installed.Code != 0 || taps.Code != 0 {
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
	for _, name := range strings.Fields(string(taps.Output)) {
		if !declared[name] && !trustedSet[name] && !strings.Contains(string(installed.Output), name+"/") {
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
	if result := authenticateHomebrew(c); result.Code != 0 {
		return result.Code
	}
	return installHomebrewAfterAuth(c, newBrewOutputProgress(newBrewProgress(c, nil), nil)).Code
}

func authenticateHomebrew(c *Context) platform.Result {
	if !c.Env.IsMacOS() {
		return platform.Result{}
	}
	if len(c.SudoPassword) == 0 {
		if !c.Terminal || c.Env.Bool("USERLAND_NON_INTERACTIVE") {
			return runWithObserved(c, c.Env.List, nil, c.Output, "/usr/bin/sudo", "-n", "-v")
		}
		return runWithObserved(c, c.Env.List, c.Stdin, c.Output, "/usr/bin/sudo", "-v")
	}
	input := append(append([]byte(nil), c.SudoPassword...), '\n')
	defer clearSecretBytes(input)
	return runWithObserved(c, c.Env.List, bytes.NewReader(input), c.Output, "/usr/bin/sudo", "-S", "-p", "", "-v")
}

func runPrivileged(c *Context, args ...string) platform.Result {
	if len(c.SudoPassword) == 0 {
		return runWith(c, c.Env.List, c.Stdin, "/usr/bin/sudo", append([]string{"-n"}, args...)...)
	}
	input := append(append([]byte(nil), c.SudoPassword...), '\n')
	defer clearSecretBytes(input)
	command := append([]string{"-S", "-p", ""}, args...)
	return runWith(c, c.Env.List, bytes.NewReader(input), "/usr/bin/sudo", command...)
}

func clearSecretBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

// AuthenticateHomebrew obtains the administrator authorization that the
// pinned Homebrew installer needs for its native prefix.
func AuthenticateHomebrew(ctx context.Context, env platform.Environment, password []byte, output io.Writer, terminal bool) platform.Result {
	if !env.IsMacOS() {
		return platform.Result{}
	}
	c := &Context{Context: ctx, Env: env, Output: output, Terminal: terminal, SudoPassword: password}
	return authenticateHomebrew(c)
}

func installHomebrewAfterAuth(c *Context, progress io.Writer) platform.Result {
	directory, err := os.MkdirTemp(c.Env.Get("TMPDIR"), "userland-homebrew.")
	if err != nil {
		return platform.Result{Code: 1, Err: err}
	}
	defer os.RemoveAll(directory)
	installer := filepath.Join(directory, "install.sh")
	if result := run(c, "curl", "--proto", "=https", "--tlsv1.2", "-fsSL", homebrewInstaller, "-o", installer); result.Code != 0 {
		return result
	}
	digest, err := fileSHA256(installer)
	if err != nil || digest != homebrewSHA256 {
		c.Log(Attention, "Homebrew installer checksum mismatch")
		return platform.Result{Code: 1, Err: errors.New("Homebrew installer checksum mismatch")}
	}
	c.Log(Changed, "installing Homebrew from pinned commit "+homebrewCommit)
	// The caller provides a progress parser, never the terminal writer. Keep
	// Homebrew's confirmation prompt disabled after the user approved the plan.
	environ := c.Env.With("NONINTERACTIVE", "1")
	input := c.Stdin
	var passwordInput []byte
	if len(c.SudoPassword) != 0 {
		passwordInput = append(append([]byte(nil), c.SudoPassword...), '\n')
		defer clearSecretBytes(passwordInput)
		input = bytes.NewReader(passwordInput)
	}
	// Keep installer output in the run result and diagnostic log. Rendering raw
	// Homebrew output here tears up the Userland task view.
	return runWithObserved(c, environ, input, progress, "/bin/bash", installer)
}

// PrepareHomebrew ensures the manager used by Mise's brew backend exists
// after administrator access has been authenticated. Formula declarations and
// installation still go through Mise; this only bootstraps the native manager
// that Mise needs on a fresh macOS account.
func PrepareHomebrew(ctx context.Context, env platform.Environment, stdin io.Reader, password []byte, output io.Writer, terminal bool) platform.Result {
	if !env.IsMacOS() {
		return platform.Result{}
	}
	c := &Context{Context: ctx, Env: env, Stdin: stdin, Output: output, Terminal: terminal, SudoPassword: password}
	if _, present := brewCommand(c); present {
		return platform.Result{}
	}
	return installHomebrewAfterAuth(c, output)
}
