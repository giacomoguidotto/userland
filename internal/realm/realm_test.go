package realm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestAddAdoptsExistingCheckoutAndCreatesPathScopedActivation(t *testing.T) {
	fixture := newFixture(t)
	mount := filepath.Join(fixture.home, "dev", "work")
	initRepository(t, mount, fixture.repository)
	writeFile(t, filepath.Join(mount, "mise.toml"), "[env]\nKUBECONFIG = \"{{config_root}}/.kube/config\"\n", 0o600)
	writeFile(t, filepath.Join(mount, ".gitconfig"), "[user]\n\temail = work@example.test\n", 0o600)

	result, err := New(fixture.env()).Add(context.Background(), fixture.repository, mount)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Name != "work" || result.Mount != mount {
		t.Fatalf("unexpected add result: %#v", result)
	}

	catalog := readFile(t, filepath.Join(fixture.root, "cfg", "realms.csv"))
	wantDeclaration := "work," + fixture.repository + ",~/dev/work,optional\n"
	if !strings.Contains(catalog, wantDeclaration) {
		t.Fatalf("catalog omitted %q: %q", wantDeclaration, catalog)
	}
	attachments := readFile(t, filepath.Join(fixture.state, "realms.csv"))
	if attachments != "name,path\nwork,~/dev/work\n" {
		t.Fatalf("unexpected attachment map: %q", attachments)
	}

	link := filepath.Join(mount, ".envrc")
	info, err := os.Lstat(link)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("realm activation is not a regular generated file: %v", err)
	}
	activation := readFile(t, link)
	for _, expected := range []string{
		"export USERLAND_REALM='work'",
		"export USERLAND_REALM_ROOT='" + mount + "'",
		"watch_file '" + filepath.Join(mount, "mise.toml") + "'",
		"mise -C '" + mount + "' env -s bash",
	} {
		if !strings.Contains(activation, expected) {
			t.Fatalf("activation omitted %q: %q", expected, activation)
		}
	}
	if strings.Contains(activation, "mise activate") {
		t.Fatalf("activation restored the slow shell hook: %q", activation)
	}

	gitRealms := readFile(t, filepath.Join(fixture.home, ".config", "git", "userland-realms.gitconfig"))
	if !strings.Contains(gitRealms, `[includeIf "gitdir:`+mount+`/"]`) ||
		!strings.Contains(gitRealms, "path = "+filepath.Join(mount, ".gitconfig")) {
		t.Fatalf("Git realm include is incomplete: %q", gitRealms)
	}
	if calls := readFile(t, fixture.direnvCalls); calls != "allow\t"+link+"\n" {
		t.Fatalf("unexpected direnv calls: %q", calls)
	}
	if excluded := readFile(t, filepath.Join(mount, ".git", "info", "exclude")); !strings.Contains(excluded, ".envrc") {
		t.Fatalf("generated activation was not excluded: %q", excluded)
	}
	if remote := gitOutput(t, mount, "remote", "get-url", "origin"); remote != fixture.repository {
		t.Fatalf("existing checkout remote changed to %q", remote)
	}
}

func TestAddKeepsTheDeclaredRealmNameWhenRepositoryNameDiffers(t *testing.T) {
	fixture := newFixture(t)
	repository := "git@example.test:private/userland-work.git"
	mount := filepath.Join(fixture.home, "dev", "work")
	initRepository(t, mount, repository)
	writeFile(t, filepath.Join(mount, "mise.toml"), "[tools]\n", 0o600)
	writeFile(t, filepath.Join(fixture.root, "cfg", "realms.csv"),
		"name,repository,default_path,mode\nwork,"+repository+",~/dev/work,optional\n", 0o600)

	result, err := New(fixture.env()).Add(context.Background(), repository, mount)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "work" {
		t.Fatalf("realm name followed repository rename: %#v", result)
	}
	if got := readFile(t, filepath.Join(fixture.state, "realms.csv")); got != "name,path\nwork,~/dev/work\n" {
		t.Fatalf("attachment did not preserve logical name: %q", got)
	}
}

func TestAddClonesAMissingRealmWithoutPullingExistingOnes(t *testing.T) {
	fixture := newFixture(t)
	source := filepath.Join(fixture.base, "source")
	initRepository(t, source, "")
	writeFile(t, filepath.Join(source, "mise.toml"), "[tools]\n", 0o600)
	runGit(t, source, "add", "mise.toml")
	runGit(t, source, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "initial")
	bare := filepath.Join(fixture.base, "realm.git")
	runGit(t, fixture.base, "clone", "--bare", source, bare)
	mount := filepath.Join(fixture.home, "dev", "work")

	result, err := New(fixture.env()).Add(context.Background(), bare, mount)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || gitOutput(t, mount, "remote", "get-url", "origin") != bare {
		t.Fatalf("missing realm was not cloned: %#v", result)
	}
}

func TestAddRefusesToReplaceAnUnmanagedEnvrc(t *testing.T) {
	fixture := newFixture(t)
	mount := filepath.Join(fixture.home, "dev", "work")
	initRepository(t, mount, fixture.repository)
	writeFile(t, filepath.Join(mount, "mise.toml"), "[tools]\n", 0o600)
	writeFile(t, filepath.Join(mount, ".envrc"), "export KEEP_ME=1\n", 0o600)

	_, err := New(fixture.env()).Add(context.Background(), fixture.repository, mount)
	if err == nil || !strings.Contains(err.Error(), "unmanaged .envrc") {
		t.Fatalf("expected unmanaged .envrc refusal, got %v", err)
	}
	if got := readFile(t, filepath.Join(mount, ".envrc")); got != "export KEEP_ME=1\n" {
		t.Fatalf("unmanaged .envrc changed: %q", got)
	}
}

func TestAddDoesNotOptInThisMachineWhenDirenvAuthorizationFails(t *testing.T) {
	fixture := newFixture(t)
	mount := filepath.Join(fixture.home, "dev", "work")
	initRepository(t, mount, fixture.repository)
	writeFile(t, filepath.Join(mount, "mise.toml"), "[tools]\n", 0o600)
	writeFile(t, filepath.Join(fixture.bin, "direnv"), "#!/bin/sh\nexit 1\n", 0o755)

	_, err := New(fixture.env()).Add(context.Background(), fixture.repository, mount)
	if err == nil || !strings.Contains(err.Error(), "could not authorize") {
		t.Fatalf("expected direnv authorization failure, got %v", err)
	}
	attachments, readErr := os.ReadFile(filepath.Join(fixture.state, "realms.csv"))
	if readErr == nil && strings.TrimSpace(string(attachments)) != "" {
		t.Fatalf("failed add opted this machine in: %q", attachments)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
}

func TestRemoveDetachesWithoutDeletingCheckoutOrDeclaration(t *testing.T) {
	fixture := newFixture(t)
	mount := filepath.Join(fixture.home, "dev", "work")
	initRepository(t, mount, fixture.repository)
	writeFile(t, filepath.Join(mount, "mise.toml"), "[tools]\n", 0o600)
	manager := New(fixture.env())
	if _, err := manager.Add(context.Background(), fixture.repository, mount); err != nil {
		t.Fatal(err)
	}

	result, err := manager.Remove(context.Background(), mount)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Name != "work" {
		t.Fatalf("unexpected remove result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(mount, ".git")); err != nil {
		t.Fatalf("remove deleted the checkout: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(mount, ".envrc")); !os.IsNotExist(err) {
		t.Fatalf("activation still exists: %v", err)
	}
	if got := readFile(t, filepath.Join(fixture.state, "realms.csv")); got != "name,path\n" {
		t.Fatalf("attachment remains: %q", got)
	}
	if got := readFile(t, filepath.Join(fixture.root, "cfg", "realms.csv")); !strings.Contains(got, fixture.repository) {
		t.Fatalf("portable declaration was removed: %q", got)
	}
	if got := readFile(t, filepath.Join(fixture.home, ".config", "git", "userland-realms.gitconfig")); strings.Contains(got, mount) {
		t.Fatalf("Git realm remains active: %q", got)
	}
	if calls := readFile(t, fixture.direnvCalls); !strings.Contains(calls, "revoke\t"+filepath.Join(mount, ".envrc")+"\n") {
		t.Fatalf("direnv authorization was not revoked: %q", calls)
	}
}

func TestInspectIgnoresOptionalRealmsUntilThisMachineOptsIn(t *testing.T) {
	fixture := newFixture(t)
	writeFile(t, filepath.Join(fixture.root, "cfg", "realms.csv"),
		"name,repository,default_path,mode\nwork,"+fixture.repository+",~/dev/work,optional\n", 0o600)

	findings, err := New(fixture.env()).Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("optional unattached realm produced findings: %#v", findings)
	}
}

func TestInspectBlocksAnUnmanagedEnvrc(t *testing.T) {
	fixture := newFixture(t)
	mount := filepath.Join(fixture.home, "dev", "work")
	initRepository(t, mount, fixture.repository)
	writeFile(t, filepath.Join(mount, ".envrc"), "export KEEP_ME=1\n", 0o600)
	writeFile(t, filepath.Join(fixture.root, "cfg", "realms.csv"),
		"name,repository,default_path,mode\nwork,"+fixture.repository+",~/dev/work,optional\n", 0o600)
	writeFile(t, filepath.Join(fixture.state, "realms.csv"), "name,path\nwork,~/dev/work\n", 0o600)

	findings, err := New(fixture.env()).Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 || findings[0].State != Attention || !strings.Contains(findings[0].Message, "unmanaged .envrc") {
		t.Fatalf("unmanaged activation was not blocked: %#v", findings)
	}
}

func TestInspectReportsRepositoryTaxonomyDriftWithoutTouchingTheCheckout(t *testing.T) {
	fixture := newFixture(t)
	mount := filepath.Join(fixture.home, "dev", "work")
	initRepository(t, mount, fixture.repository)
	writeFile(t, filepath.Join(mount, "mise.toml"), "[tools]\n", 0o600)
	writeFile(t, filepath.Join(mount, ".userland", "repositories.csv"),
		"repository,path\ngit@example.test:private/missing.git,services/missing\n", 0o600)
	manager := New(fixture.env())
	if _, err := manager.Add(context.Background(), fixture.repository, mount); err != nil {
		t.Fatal(err)
	}

	findings, err := manager.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	var missing, exclusions bool
	for _, finding := range findings {
		missing = missing || finding.State == Change && strings.Contains(finding.Message, "services/missing repository is missing")
		exclusions = exclusions || finding.State == Change && strings.Contains(finding.Message, "repository exclusions need regeneration")
	}
	if !missing || !exclusions {
		t.Fatalf("repository taxonomy drift was not reported: %#v", findings)
	}
	if _, err := os.Stat(filepath.Join(mount, "services", "missing")); !os.IsNotExist(err) {
		t.Fatalf("inspect changed the missing checkout: %v", err)
	}
}

type fixture struct {
	base, root, home, state, bin string
	repository, direnvCalls      string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	base := t.TempDir()
	value := fixture{
		base:        base,
		root:        filepath.Join(base, "userland"),
		home:        filepath.Join(base, "home"),
		state:       filepath.Join(base, "state"),
		bin:         filepath.Join(base, "bin"),
		repository:  "git@example.test:private/work.git",
		direnvCalls: filepath.Join(base, "direnv-calls"),
	}
	for _, directory := range []string{filepath.Join(value.root, "cfg"), value.home, value.state, value.bin} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\nprintf '%s\\t%s\\n' \"$1\" \"$2\" >> " + shellQuote(value.direnvCalls) + "\n"
	writeFile(t, filepath.Join(value.bin, "direnv"), script, 0o755)
	writeFile(t, value.direnvCalls, "", 0o600)
	return value
}

func (f fixture) env() platform.Environment {
	return platform.NewEnvironment([]string{
		"USERLAND_ROOT=" + f.root,
		"USERLAND_HOME=" + f.home,
		"USERLAND_STATE_DIR=" + f.state,
		"PATH=" + f.bin + ":/usr/bin:/bin",
	})
}

func initRepository(t *testing.T, path, remote string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "init", "-q")
	if remote != "" {
		runGit(t, path, "remote", "add", "origin", remote)
	}
}

func runGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func gitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
