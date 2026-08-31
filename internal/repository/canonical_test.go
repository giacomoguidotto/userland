package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestReconcileCanonicalResetsPrimaryCheckoutAndPreservesIgnoredEnvironment(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	runRepositoryGit(t, source, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(source, ".gitignore"), []byte(".env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "tracked.txt"), []byte("canonical\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runRepositoryGit(t, source, "add", ".gitignore", "tracked.txt")
	runRepositoryGit(t, source, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "initial")
	remote := filepath.Join(base, "remote.git")
	runRepositoryGit(t, base, "clone", "--bare", source, remote)
	target := filepath.Join(base, "checkout")
	runRepositoryGit(t, base, "clone", remote, target)
	runRepositoryGit(t, target, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(target, "tracked.txt"), []byte("incomplete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "scratch.txt"), []byte("discard\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".env"), []byte("KEEP=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := ReconcileCanonical(context.Background(), platform.NewEnvironment(os.Environ()), target, remote, "main")
	if result.Status != CanonicalChange {
		t.Fatalf("unexpected reconciliation: %#v", result)
	}
	if branch := repositoryGitOutput(t, target, "branch", "--show-current"); branch != "main" {
		t.Fatalf("branch = %q", branch)
	}
	if contents, err := os.ReadFile(filepath.Join(target, "tracked.txt")); err != nil || string(contents) != "canonical\n" {
		t.Fatalf("tracked file = %q, %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(target, "scratch.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untracked file remains: %v", err)
	}
	if contents, err := os.ReadFile(filepath.Join(target, ".env")); err != nil || string(contents) != "KEEP=secret\n" {
		t.Fatalf("ignored environment changed: %q, %v", contents, err)
	}
	if inspected := InspectCanonical(context.Background(), platform.NewEnvironment(os.Environ()), target, remote, "main"); inspected.Status != CanonicalCurrent {
		t.Fatalf("canonical checkout still drifts: %#v", inspected)
	}
}

func TestRefreshCheckoutRestoresCanonicalUserlandMain(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	runRepositoryGit(t, source, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(source, ".gitignore"), []byte(".env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "tracked.txt"), []byte("canonical\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runRepositoryGit(t, source, "add", ".gitignore", "tracked.txt")
	runRepositoryGit(t, source, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "initial")
	remote := filepath.Join(base, "remote.git")
	runRepositoryGit(t, base, "clone", "--bare", source, remote)
	root := filepath.Join(base, "userland")
	runRepositoryGit(t, base, "clone", remote, root)
	runRepositoryGit(t, root, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("incomplete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scratch.txt"), []byte("discard\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("KEEP=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	environ := append(os.Environ(), "USERLAND_ROOT="+root, "USERLAND_CACHE_DIR="+filepath.Join(base, "cache"))
	result := RefreshCheckout(context.Background(), platform.NewEnvironment(environ))
	if !result.Updated || result.Notice != "" {
		t.Fatalf("unexpected refresh: %#v", result)
	}
	if branch := repositoryGitOutput(t, root, "branch", "--show-current"); branch != "main" {
		t.Fatalf("branch = %q", branch)
	}
	if contents, err := os.ReadFile(filepath.Join(root, "tracked.txt")); err != nil || string(contents) != "canonical\n" {
		t.Fatalf("tracked file = %q, %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(root, "scratch.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untracked file remains: %v", err)
	}
	if contents, err := os.ReadFile(filepath.Join(root, ".env")); err != nil || string(contents) != "KEEP=secret\n" {
		t.Fatalf("ignored environment changed: %q, %v", contents, err)
	}
}

func TestCanonicalRemoteOperationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := runCanonicalRemote(ctx, platform.NewEnvironment(os.Environ()), t.TempDir(), "status")
	if !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("remote operation did not preserve cancellation: %#v", result)
	}
}

func TestRemoteBranchRevisionIgnoresSSHWarnings(t *testing.T) {
	output := []byte("warning: agent refused an unrelated identity\n232fe340cc0b4f760c51b8d90612965f79c09bc9\trefs/heads/main\n")

	revision, ok := parseRemoteBranchRevision(output, "refs/heads/main")
	if !ok || revision != "232fe340cc0b4f760c51b8d90612965f79c09bc9" {
		t.Fatalf("revision = %q, %v", revision, ok)
	}
}
