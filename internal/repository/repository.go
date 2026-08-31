// Package repository owns checkout refresh and observational repository discovery.
package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/giacomoguidotto/userland/internal/csvfile"
	"github.com/giacomoguidotto/userland/internal/platform"
)

func SnapshotFresh(env platform.Environment) bool {
	roots := repositoryRoots(env)
	meta, err := os.ReadFile(filepath.Join(env.Cache, "repositories.meta"))
	if err != nil || firstLine(meta) != "v2 "+strings.Join(roots, ":") {
		return false
	}
	info, err := os.Stat(filepath.Join(env.Cache, "repositories.csv"))
	return err == nil && time.Since(info.ModTime()) < time.Duration(env.RepositoryTTL())*time.Second
}

func RefreshSnapshot(ctx context.Context, env platform.Environment) (string, error) {
	if err := env.Prepare(); err != nil {
		return "", err
	}
	if SnapshotFresh(env) {
		return "repository snapshot is younger than 24 hours", nil
	}
	var repositories []string
	for _, root := range repositoryRoots(env) {
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "node_modules", ".next", ".venv", "target", "vendor":
					return filepath.SkipDir
				}
			}
			if entry.Name() != ".git" {
				return nil
			}
			repository := filepath.Dir(path)
			result := platform.Run(ctx, env.List, nil, "git", "-C", repository, "rev-parse", "--show-superproject-working-tree")
			if result.Code == 0 && strings.TrimSpace(string(result.Output)) != "" {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if relative, err := filepath.Rel(filepath.Clean(root), repository); err == nil {
				repository = strings.TrimSuffix(root, string(os.PathSeparator)) + string(os.PathSeparator) + relative
			}
			repositories = append(repositories, repository)
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(repositories)
	repositories = unique(repositories)
	snapshot := filepath.Join(env.Cache, "repositories.csv")
	meta := filepath.Join(env.Cache, "repositories.meta")
	rows := make([][]string, 0, len(repositories))
	for _, path := range repositories {
		rows = append(rows, []string{path})
	}
	if err := csvfile.Write(snapshot, []string{"path"}, rows, 0o600); err != nil {
		return "", err
	}
	if err := atomicWrite(meta, []byte("v2 "+strings.Join(repositoryRoots(env), ":")+"\n"), 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("refreshed the 24-hour repository snapshot (%d repositories)", len(repositories)), nil
}

type RefreshResult struct {
	Updated bool
	Notice  string
}

func RefreshCheckout(ctx context.Context, env platform.Environment) RefreshResult {
	if !exists(filepath.Join(env.Root, ".git")) {
		return RefreshResult{}
	}
	if _, ok := platform.LookPath(env.List, "git"); !ok {
		return RefreshResult{Notice: "Git is unavailable; skipped the userland checkout refresh"}
	}
	logPath := filepath.Join(env.Cache, "repository-refresh.log")
	_ = os.MkdirAll(env.Cache, 0o700)
	_ = os.WriteFile(logPath, nil, 0o600)
	run := func(args ...string) platform.Result {
		result := platform.Run(ctx, env.List, nil, "git", append([]string{"-C", env.Root}, args...)...)
		if len(result.Output) != 0 {
			file, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
			if file != nil {
				_, _ = file.Write(result.Output)
				_ = file.Close()
			}
		}
		return result
	}
	status := run("status", "--porcelain")
	if status.Code != 0 {
		return RefreshResult{Notice: "Could not inspect the userland checkout; continuing with the local version"}
	}
	branch := run("branch", "--show-current")
	if branch.Code != 0 {
		return RefreshResult{Notice: "Could not inspect the userland branch; continuing with the local version"}
	}
	branchName := strings.TrimSpace(string(branch.Output))
	if run("fetch", "--quiet", "--tags", "origin", "+refs/heads/main:refs/remotes/origin/main").Code != 0 {
		return RefreshResult{Notice: "Could not reach origin; continuing with the local checkout"}
	}
	head, remote := run("rev-parse", "HEAD"), run("rev-parse", "origin/main")
	if head.Code != 0 || remote.Code != 0 {
		return RefreshResult{Notice: "Could not compare the userland checkout; continuing with the local version"}
	}
	refreshSubmodules := func() string {
		if run("submodule", "sync", "--quiet", "--recursive").Code != 0 || run("submodule", "update", "--quiet", "--init", "--recursive").Code != 0 {
			return "Could not refresh userland submodules; continuing with their local versions"
		}
		_ = os.Remove(logPath)
		return ""
	}
	needsUpdate := branchName != "main" || strings.TrimSpace(string(status.Output)) != "" ||
		strings.TrimSpace(string(head.Output)) != strings.TrimSpace(string(remote.Output))
	if !needsUpdate {
		return RefreshResult{Notice: refreshSubmodules()}
	}
	if run("checkout", "--quiet", "-f", "-B", "main", "origin/main").Code != 0 {
		return RefreshResult{Notice: "Could not restore the canonical userland main checkout"}
	}
	if run("clean", "-fd", "--quiet").Code != 0 {
		return RefreshResult{Notice: "Could not clean the canonical userland checkout"}
	}
	notice := refreshSubmodules()
	if notice != "" {
		notice = "Updated userland, but its submodules need attention"
	}
	return RefreshResult{Updated: true, Notice: notice}
}

func repositoryRoots(env platform.Environment) []string {
	if value := env.Get("USERLAND_REPO_ROOTS"); value != "" {
		return filepath.SplitList(value)
	}
	return []string{filepath.Join(env.Home, "dev", "life"), filepath.Join(env.Home, "dev", "research")}
}

func atomicWrite(path string, contents []byte, mode os.FileMode) error {
	temporary := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(temporary, contents, mode); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func firstLine(value []byte) string {
	line, _, _ := strings.Cut(string(value), "\n")
	return strings.TrimSpace(line)
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }
func unique(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
