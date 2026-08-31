package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/giacomoguidotto/userland/internal/platform"
)

const (
	canonicalInspectTimeout = 10 * time.Second
	canonicalFetchTimeout   = 60 * time.Second
)

// CanonicalStatus describes whether a primary checkout matches its declaration.
type CanonicalStatus string

const (
	CanonicalCurrent   CanonicalStatus = "current"
	CanonicalChange    CanonicalStatus = "change"
	CanonicalAttention CanonicalStatus = "attention"
)

// CanonicalResult is the outcome of inspecting or reconciling one primary checkout.
type CanonicalResult struct {
	Status  CanonicalStatus
	Message string
}

// InspectCanonical compares a primary checkout with the declared remote branch.
// It does not fetch or change the checkout.
func InspectCanonical(ctx context.Context, env platform.Environment, target, repository, branch string) CanonicalResult {
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		return CanonicalResult{CanonicalChange, "repository is missing"}
	} else if err != nil {
		return CanonicalResult{CanonicalAttention, "repository cannot be inspected"}
	}
	if result := validateCanonicalCheckout(ctx, env, target, repository); result.Status != CanonicalCurrent {
		return result
	}
	remote, ok := remoteBranchRevision(ctx, env, target, branch)
	if !ok {
		return CanonicalResult{CanonicalAttention, "could not inspect origin/" + branch}
	}
	currentBranch := platform.Run(ctx, env.List, nil, "git", "-C", target, "branch", "--show-current")
	head := platform.Run(ctx, env.List, nil, "git", "-C", target, "rev-parse", "HEAD")
	state := platform.Run(ctx, env.List, nil, "git", "-C", target, "status", "--porcelain", "--untracked-files=all")
	if currentBranch.Code != 0 || head.Code != 0 || state.Code != 0 {
		return CanonicalResult{CanonicalAttention, "repository state could not be inspected"}
	}
	if strings.TrimSpace(string(currentBranch.Output)) != branch ||
		strings.TrimSpace(string(head.Output)) != remote || strings.TrimSpace(string(state.Output)) != "" {
		return CanonicalResult{CanonicalChange, "repository needs synchronization with origin/" + branch}
	}
	return CanonicalResult{CanonicalCurrent, "repository matches origin/" + branch}
}

// ReconcileCanonical makes a primary checkout an exact, clean copy of the
// declared remote branch. Ignored files, including local dotenv files, remain.
func ReconcileCanonical(ctx context.Context, env platform.Environment, target, repository, branch string) CanonicalResult {
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return CanonicalResult{CanonicalAttention, fmt.Sprintf("could not prepare repository: %v", err)}
		}
		clone := platform.Run(ctx, env.List, nil, "git", "clone", "--branch", branch, "--", repository, target)
		if clone.Code != 0 {
			return CanonicalResult{CanonicalAttention, "repository could not be cloned at origin/" + branch}
		}
		return CanonicalResult{CanonicalChange, "cloned repository at origin/" + branch}
	} else if err != nil {
		return CanonicalResult{CanonicalAttention, "repository cannot be inspected"}
	}
	if result := validateCanonicalCheckout(ctx, env, target, repository); result.Status != CanonicalCurrent {
		return result
	}
	tracking := "refs/remotes/origin/" + branch
	fetch := runCanonicalRemote(ctx, env, target, canonicalFetchTimeout, "fetch", "--quiet", "origin", "+refs/heads/"+branch+":"+tracking)
	if fetch.Code != 0 {
		return CanonicalResult{CanonicalAttention, "could not fetch origin/" + branch}
	}
	checkout := platform.Run(ctx, env.List, nil, "git", "-C", target, "checkout", "--quiet", "-f", "-B", branch, tracking)
	if checkout.Code != 0 {
		return CanonicalResult{CanonicalAttention, "could not make the primary checkout canonical on " + branch}
	}
	clean := platform.Run(ctx, env.List, nil, "git", "-C", target, "clean", "-fd", "--quiet")
	if clean.Code != 0 {
		return CanonicalResult{CanonicalAttention, "could not remove untracked primary-checkout files"}
	}
	upstream := platform.Run(ctx, env.List, nil, "git", "-C", target, "branch", "--set-upstream-to=origin/"+branch, branch)
	if upstream.Code != 0 {
		return CanonicalResult{CanonicalAttention, "could not configure the canonical upstream branch"}
	}
	return CanonicalResult{CanonicalChange, "synchronized repository with origin/" + branch}
}

func validateCanonicalCheckout(ctx context.Context, env platform.Environment, target, repository string) CanonicalResult {
	inside := platform.Run(ctx, env.List, nil, "git", "-C", target, "rev-parse", "--is-inside-work-tree")
	if inside.Code != 0 || strings.TrimSpace(string(inside.Output)) != "true" {
		return CanonicalResult{CanonicalAttention, "path exists and is not a Git checkout"}
	}
	origin := platform.Run(ctx, env.List, nil, "git", "-C", target, "config", "--local", "--get", "remote.origin.url")
	if origin.Code != 0 {
		return CanonicalResult{CanonicalAttention, "repository has no origin remote"}
	}
	if !sameRemote(string(origin.Output), repository) {
		return CanonicalResult{CanonicalAttention, "repository origin does not match its declaration"}
	}
	return CanonicalResult{CanonicalCurrent, "repository origin matches"}
}

func remoteBranchRevision(ctx context.Context, env platform.Environment, target, branch string) (string, bool) {
	result := runCanonicalRemote(ctx, env, target, canonicalInspectTimeout, "ls-remote", "--exit-code", "--refs", "origin", "refs/heads/"+branch)
	if result.Code != 0 {
		return "", false
	}
	return parseRemoteBranchRevision(result.Output, "refs/heads/"+branch)
}

func parseRemoteBranchRevision(output []byte, reference string) (string, bool) {
	for line := range strings.SplitSeq(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == reference && fields[0] != "" {
			return fields[0], true
		}
	}
	return "", false
}

func runCanonicalRemote(ctx context.Context, env platform.Environment, target string, timeout time.Duration, args ...string) platform.Result {
	remoteContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return platform.Run(remoteContext, env.With("GIT_TERMINAL_PROMPT", "0"), nil, "git", append([]string{"-C", target}, args...)...)
}
