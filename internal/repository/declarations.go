package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/csvfile"
	"github.com/giacomoguidotto/userland/internal/platform"
)

type DeclarationState string

const (
	DeclarationCurrent   DeclarationState = "current"
	DeclarationChange    DeclarationState = "change"
	DeclarationAttention DeclarationState = "attention"
)

type DeclarationFinding struct {
	State   DeclarationState
	Message string
}

type checkoutDeclaration struct {
	repository string
	path       string
}

var repositoryDeclarationHeader = []string{"repository", "path"}

// InspectDeclarations reports drift in a realm's optional repository taxonomy.
func InspectDeclarations(ctx context.Context, env platform.Environment, root string) ([]DeclarationFinding, error) {
	declarations, exists, err := loadDeclarations(root)
	if err != nil || !exists {
		return nil, err
	}
	findings := inspectDeclarations(ctx, env, root, declarations)
	exclusionsCurrent, err := exclusionsMatch(ctx, env, root, declarations)
	if err != nil {
		return nil, err
	}
	if !exclusionsCurrent {
		findings = append(findings, DeclarationFinding{DeclarationChange, "repository exclusions need regeneration"})
	}
	return summarizeDeclarations(findings), nil
}

// ReconcileDeclarations clones missing repositories and refreshes local excludes.
// It never changes or deletes an existing checkout.
func ReconcileDeclarations(ctx context.Context, env platform.Environment, root string) ([]DeclarationFinding, error) {
	declarations, exists, err := loadDeclarations(root)
	if err != nil || !exists {
		return nil, err
	}
	findings := inspectDeclarations(ctx, env, root, declarations)
	for index, finding := range findings {
		if finding.State != DeclarationChange || !strings.HasSuffix(finding.Message, " repository is missing") {
			continue
		}
		declared := declarations[index]
		target := filepath.Join(root, filepath.FromSlash(declared.path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			findings[index] = DeclarationFinding{DeclarationAttention, fmt.Sprintf("could not prepare %s repository: %v", declared.path, err)}
			continue
		}
		result := platform.Run(ctx, env.List, nil, "git", "clone", "--", declared.repository, target)
		if result.Code != 0 {
			findings[index] = DeclarationFinding{DeclarationAttention, declared.path + " repository could not be cloned"}
			continue
		}
		findings[index] = DeclarationFinding{DeclarationChange, "cloned " + declared.path + " repository"}
	}
	if err := writeExclusions(ctx, env, root, declarations); err != nil {
		return findings, err
	}
	return summarizeDeclarations(findings), nil
}

func loadDeclarations(root string) ([]checkoutDeclaration, bool, error) {
	path := filepath.Join(root, ".userland", "repositories.csv")
	rows, err := csvfile.Read(path, repositoryDeclarationHeader)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	declarations := make([]checkoutDeclaration, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		repository := strings.TrimSpace(row[0])
		path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(row[1])))
		if repository == "" || path == "." || !filepath.IsLocal(filepath.FromSlash(path)) {
			return nil, true, fmt.Errorf("invalid repository declaration at record %d in %s", index+2, filepath.Join(root, ".userland", "repositories.csv"))
		}
		if _, exists := seen[path]; exists {
			return nil, true, fmt.Errorf("duplicate repository path %s", path)
		}
		seen[path] = struct{}{}
		declarations = append(declarations, checkoutDeclaration{repository: repository, path: path})
	}
	return declarations, true, nil
}

func inspectDeclarations(ctx context.Context, env platform.Environment, root string, declarations []checkoutDeclaration) []DeclarationFinding {
	findings := make([]DeclarationFinding, 0, len(declarations))
	for _, declared := range declarations {
		target := filepath.Join(root, filepath.FromSlash(declared.path))
		if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			findings = append(findings, DeclarationFinding{DeclarationChange, declared.path + " repository is missing"})
			continue
		} else if err != nil {
			findings = append(findings, DeclarationFinding{DeclarationAttention, declared.path + " repository cannot be inspected"})
			continue
		}
		inside := platform.Run(ctx, env.List, nil, "git", "-C", target, "rev-parse", "--is-inside-work-tree")
		if inside.Code != 0 || strings.TrimSpace(string(inside.Output)) != "true" {
			findings = append(findings, DeclarationFinding{DeclarationAttention, declared.path + " exists and is not a Git checkout"})
			continue
		}
		origin := platform.Run(ctx, env.List, nil, "git", "-C", target, "config", "--local", "--get", "remote.origin.url")
		if origin.Code != 0 {
			findings = append(findings, DeclarationFinding{DeclarationAttention, declared.path + " repository has no origin remote"})
			continue
		}
		if !sameRemote(string(origin.Output), declared.repository) {
			findings = append(findings, DeclarationFinding{DeclarationAttention, declared.path + " repository origin does not match its declaration"})
			continue
		}
		findings = append(findings, DeclarationFinding{DeclarationCurrent, declared.path + " repository matches"})
	}
	return findings
}

func summarizeDeclarations(findings []DeclarationFinding) []DeclarationFinding {
	for _, finding := range findings {
		if finding.State != DeclarationCurrent {
			result := findings[:0]
			for _, item := range findings {
				if item.State != DeclarationCurrent {
					result = append(result, item)
				}
			}
			return result
		}
	}
	if len(findings) == 0 {
		return nil
	}
	return []DeclarationFinding{{DeclarationCurrent, "repository taxonomy matches"}}
}

func sameRemote(left, right string) bool {
	normalize := func(value string) string {
		return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(value), "/"), ".git")
	}
	return normalize(left) == normalize(right)
}

const exclusionsBegin = "# BEGIN userland realm repositories"
const exclusionsEnd = "# END userland realm repositories"

func exclusionsMatch(ctx context.Context, env platform.Environment, root string, declarations []checkoutDeclaration) (bool, error) {
	path, err := exclusionsPath(ctx, env, root)
	if err != nil {
		return false, err
	}
	actual, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	expected, err := exclusionsContents(actual, declarations)
	return err == nil && string(actual) == string(expected), err
}

func writeExclusions(ctx context.Context, env platform.Environment, root string, declarations []checkoutDeclaration) error {
	path, err := exclusionsPath(ctx, env, root)
	if err != nil {
		return err
	}
	actual, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	expected, err := exclusionsContents(actual, declarations)
	if err != nil || string(actual) == string(expected) {
		return err
	}
	return atomicWrite(path, expected, 0o600)
}

func exclusionsPath(ctx context.Context, env platform.Environment, root string) (string, error) {
	result := platform.Run(ctx, env.List, nil, "git", "-C", root, "rev-parse", "--git-path", "info/exclude")
	if result.Code != 0 {
		return "", errors.New("realm repository exclusions require a Git checkout")
	}
	path := strings.TrimSpace(string(result.Output))
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Clean(path), nil
}

func exclusionsContents(current []byte, declarations []checkoutDeclaration) ([]byte, error) {
	text := string(current)
	start, finish := strings.Index(text, exclusionsBegin), strings.Index(text, exclusionsEnd)
	if start >= 0 != (finish >= 0) || start >= 0 && finish < start {
		return nil, errors.New("repository exclusion markers are malformed")
	}
	if start >= 0 {
		finish += len(exclusionsEnd)
		if finish < len(text) && text[finish] == '\n' {
			finish++
		}
		text = text[:start] + text[finish:]
	}
	text = strings.TrimRight(text, "\n")
	if text != "" {
		text += "\n"
	}
	if len(declarations) == 0 {
		return []byte(text), nil
	}
	text += exclusionsBegin + "\n"
	for _, declared := range declarations {
		text += "/" + escapeExcludePath(declared.path) + "/\n"
	}
	text += exclusionsEnd + "\n"
	return []byte(text), nil
}

func escapeExcludePath(path string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`, `#`, `\#`, `!`, `\!`, ` `, `\ `,
	)
	return replacer.Replace(path)
}
