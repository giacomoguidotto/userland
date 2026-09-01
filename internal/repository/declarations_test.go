package repository

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/csvfile"
	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestReconcileDeclarationsMakesDeclaredCheckoutsCanonical(t *testing.T) {
	fixture := newDeclarationFixture(t)
	existingRemote := fixture.bareRepository(t, "existing")
	missingRemote := fixture.bareRepository(t, "missing")
	existing := filepath.Join(fixture.root, "existing")
	runRepositoryGit(t, fixture.root, "clone", existingRemote, existing)
	runRepositoryGit(t, existing, "checkout", "-b", "keep-branch")
	if err := os.WriteFile(filepath.Join(existing, "dirty.txt"), []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.writeManifest(t, [][]string{{existingRemote, "existing", "main"}, {missingRemote, "nested/missing", "main"}})

	findings, err := ReconcileDeclarations(context.Background(), fixture.env, fixture.root, fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 || findings[0].State != DeclarationChange || findings[1].State != DeclarationChange ||
		!strings.Contains(findings[0].Message, "synchronized") || !strings.Contains(findings[1].Message, "cloned") {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if branch := repositoryGitOutput(t, existing, "branch", "--show-current"); branch != "main" {
		t.Fatalf("existing branch changed to %q", branch)
	}
	if _, err := os.Stat(filepath.Join(existing, "dirty.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untracked primary-checkout file remains: %v", err)
	}
	if origin := repositoryGitOutput(t, filepath.Join(fixture.root, "nested", "missing"), "config", "--local", "--get", "remote.origin.url"); origin != missingRemote {
		t.Fatalf("missing repository origin = %q", origin)
	}
	exclude, err := os.ReadFile(filepath.Join(fixture.root, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(exclude), "/existing/\n") || !strings.Contains(string(exclude), "/nested/missing/\n") {
		t.Fatalf("repository exclusions are incomplete: %q, %v", exclude, err)
	}
}

func TestInspectDeclarationsReportsConflictsWithoutChangingThem(t *testing.T) {
	fixture := newDeclarationFixture(t)
	target := filepath.Join(fixture.root, "conflict")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.writeManifest(t, [][]string{{"git@example.test:team/conflict.git", "conflict", "main"}})

	findings, err := InspectDeclarations(context.Background(), fixture.env, fixture.root, fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 || findings[0].State != DeclarationAttention || findings[1].State != DeclarationChange {
		t.Fatalf("unexpected conflict findings: %#v", findings)
	}
	if contents, err := os.ReadFile(filepath.Join(target, "keep.txt")); err != nil || string(contents) != "keep\n" {
		t.Fatalf("conflicting directory changed: %q, %v", contents, err)
	}
}

func TestReconcileDeclarationsNeverDeletesAnUndeclaredCheckout(t *testing.T) {
	fixture := newDeclarationFixture(t)
	checkout := filepath.Join(fixture.root, "keep")
	runRepositoryGit(t, fixture.root, "init", checkout)
	fixture.writeManifest(t, nil)

	if _, err := ReconcileDeclarations(context.Background(), fixture.env, fixture.root, fixture.root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(checkout, ".git")); err != nil {
		t.Fatalf("undeclared checkout was deleted: %v", err)
	}
}

func TestInspectDeclarationsRejectsPathsOutsideTheRealm(t *testing.T) {
	fixture := newDeclarationFixture(t)
	fixture.writeManifest(t, [][]string{{"git@example.test:team/repo.git", "../outside", "main"}})

	_, err := InspectDeclarations(context.Background(), fixture.env, fixture.root, fixture.root)
	if err == nil || !strings.Contains(err.Error(), "invalid repository declaration") {
		t.Fatalf("unsafe path was accepted: %v", err)
	}
}

func TestReconcileDeclarationsReadsTaxonomyFromASeparateConfigurationRoot(t *testing.T) {
	fixture := newDeclarationFixture(t)
	configurationRoot := filepath.Join(fixture.base, "configuration")
	remote := fixture.bareRepository(t, "separate")
	if err := csvfile.Write(filepath.Join(configurationRoot, ".userland", "repositories.csv"),
		repositoryDeclarationHeader, [][]string{{remote, "separate", "main"}}, 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := ReconcileDeclarations(context.Background(), fixture.env, configurationRoot, fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].State != DeclarationChange || !strings.Contains(findings[0].Message, "cloned") {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if got := repositoryGitOutput(t, filepath.Join(fixture.root, "separate"), "remote", "get-url", "origin"); got != remote {
		t.Fatalf("separate declaration target remote = %q", got)
	}
	exclude, err := os.ReadFile(filepath.Join(fixture.root, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(exclude), "/separate/\n") {
		t.Fatalf("checkout root exclusions are incomplete: %q, %v", exclude, err)
	}
}

func TestReconcileDeclarationsAllowsASeparateConfigurationRootToOwnThePrimaryCheckout(t *testing.T) {
	fixture := newDeclarationFixture(t)
	configurationRoot := filepath.Join(fixture.base, "configuration")
	checkoutRoot := filepath.Join(fixture.base, "product")
	remote := fixture.bareRepository(t, "primary")
	if err := csvfile.Write(filepath.Join(configurationRoot, ".userland", "repositories.csv"),
		repositoryDeclarationHeader, [][]string{{remote, ".", "main"}}, 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := ReconcileDeclarations(context.Background(), fixture.env, configurationRoot, checkoutRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].State != DeclarationChange ||
		!strings.Contains(findings[0].Message, "primary checkout cloned") {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if got := repositoryGitOutput(t, checkoutRoot, "remote", "get-url", "origin"); got != remote {
		t.Fatalf("primary checkout remote = %q", got)
	}
	if branch := repositoryGitOutput(t, checkoutRoot, "branch", "--show-current"); branch != "main" {
		t.Fatalf("primary checkout branch = %q", branch)
	}
	exclude, err := os.ReadFile(filepath.Join(checkoutRoot, ".git", "info", "exclude"))
	if err != nil || strings.Contains(string(exclude), exclusionsBegin) {
		t.Fatalf("root declaration created an exclusion marker: %q, %v", exclude, err)
	}

	findings, err = InspectDeclarations(context.Background(), fixture.env, configurationRoot, checkoutRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].State != DeclarationCurrent {
		t.Fatalf("primary checkout did not become current: %#v", findings)
	}
}

func TestInspectDeclarationsRejectsARootPathForColocatedConfiguration(t *testing.T) {
	fixture := newDeclarationFixture(t)
	fixture.writeManifest(t, [][]string{{"git@example.test:team/repo.git", ".", "main"}})

	_, err := InspectDeclarations(context.Background(), fixture.env, fixture.root, fixture.root)
	if err == nil || !strings.Contains(err.Error(), "invalid repository declaration") {
		t.Fatalf("colocated root declaration was accepted: %v", err)
	}
}

type declarationFixture struct {
	base string
	root string
	env  platform.Environment
}

func newDeclarationFixture(t *testing.T) declarationFixture {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "realm")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	runRepositoryGit(t, root, "init")
	return declarationFixture{base: base, root: root, env: platform.NewEnvironment(os.Environ())}
}

func (f declarationFixture) bareRepository(t *testing.T, name string) string {
	t.Helper()
	source := filepath.Join(f.base, name+"-source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	runRepositoryGit(t, source, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runRepositoryGit(t, source, "add", "README.md")
	runRepositoryGit(t, source, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "initial")
	bare := filepath.Join(f.base, name+".git")
	runRepositoryGit(t, f.base, "clone", "--bare", source, bare)
	return bare
}

func (f declarationFixture) writeManifest(t *testing.T, rows [][]string) {
	t.Helper()
	if err := csvfile.Write(filepath.Join(f.root, ".userland", "repositories.csv"), repositoryDeclarationHeader, rows, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runRepositoryGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func repositoryGitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
