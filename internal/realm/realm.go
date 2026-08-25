// Package realm manages optional, path-scoped private Userland configuration.
package realm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/giacomoguidotto/userland/internal/platform"
)

var realmName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type State string

const (
	Current   State = "current"
	Change    State = "change"
	Attention State = "attention"
)

type Finding struct {
	State   State
	Message string
}

type Result struct {
	Name       string
	Repository string
	Mount      string
	Changed    bool
}

type Manager struct {
	Env platform.Environment
}

func New(env platform.Environment) Manager { return Manager{Env: env} }

type declaration struct {
	Name, Repository, Path, Mode string
}

type attachment struct {
	Name, Path string
}

type activationConflict struct{ message string }

func (e activationConflict) Error() string { return e.message }

func (m Manager) Add(ctx context.Context, repository, mount string) (Result, error) {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return Result{}, errors.New("realm repository cannot be empty")
	}
	absMount, err := m.expandPath(mount)
	if err != nil {
		return Result{}, err
	}
	declaredPath := m.portablePath(absMount)
	name, err := m.resolveName(repository, declaredPath)
	if err != nil {
		return Result{}, err
	}
	declared := declaration{Name: name, Repository: repository, Path: declaredPath, Mode: "optional"}
	if err := m.validateDeclaration(declared); err != nil {
		return Result{}, err
	}
	if err := m.validateActivation(absMount, name); err != nil {
		return Result{}, err
	}
	cloned, err := m.ensureCheckout(ctx, declared, absMount)
	if err != nil {
		return Result{}, err
	}
	catalogChanged, err := m.ensureDeclaration(declared)
	if err != nil {
		return Result{}, err
	}
	attachments, err := m.loadAttachments()
	if err != nil {
		return Result{}, err
	}
	attachmentChanged := true
	for _, existing := range attachments {
		if existing.Name == name {
			if existing.Path != declaredPath {
				return Result{}, fmt.Errorf("realm %s is already attached at %s", name, existing.Path)
			}
			attachmentChanged = false
		}
	}
	if attachmentChanged {
		attachments = append(attachments, attachment{Name: name, Path: declaredPath})
	}
	activationChanged, err := m.ensureActivation(absMount, name)
	if err != nil {
		return Result{}, err
	}
	if err := m.allow(ctx, filepath.Join(absMount, ".envrc")); err != nil {
		return Result{}, err
	}
	if attachmentChanged {
		if err := m.writeAttachments(attachments); err != nil {
			return Result{}, err
		}
	}
	if err := m.writeGitRealms(attachments); err != nil {
		return Result{}, err
	}
	return Result{
		Name: name, Repository: repository, Mount: absMount,
		Changed: cloned || catalogChanged || attachmentChanged || activationChanged,
	}, nil
}

func (m Manager) Remove(ctx context.Context, target string) (Result, error) {
	attachments, err := m.loadAttachments()
	if err != nil {
		return Result{}, err
	}
	absTarget, pathErr := m.expandPath(target)
	index := -1
	var selected attachment
	for position, candidate := range attachments {
		mount, expandErr := m.expandPath(candidate.Path)
		if expandErr != nil {
			return Result{}, expandErr
		}
		if candidate.Name == target || pathErr == nil && mount == absTarget {
			index, selected = position, candidate
			break
		}
	}
	if index < 0 {
		return Result{}, fmt.Errorf("realm is not attached: %s", target)
	}
	mount, err := m.expandPath(selected.Path)
	if err != nil {
		return Result{}, err
	}
	link := filepath.Join(mount, ".envrc")
	removeActivation := false
	if info, statErr := os.Lstat(link); statErr == nil {
		if !info.Mode().IsRegular() {
			return Result{}, fmt.Errorf("refusing to remove unmanaged .envrc at %s", mount)
		}
		contents, readErr := os.ReadFile(link)
		if readErr != nil || string(contents) != activationContents(selected.Name, mount) {
			return Result{}, fmt.Errorf("refusing to remove unmanaged .envrc at %s", mount)
		}
		removeActivation = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Result{}, statErr
	}
	if err := m.revoke(ctx, link); err != nil {
		return Result{}, err
	}
	if removeActivation {
		if err := os.Remove(link); err != nil {
			return Result{}, err
		}
	}
	attachments = append(attachments[:index], attachments[index+1:]...)
	if err := m.writeAttachments(attachments); err != nil {
		return Result{}, err
	}
	if err := m.writeGitRealms(attachments); err != nil {
		return Result{}, err
	}
	return Result{Name: selected.Name, Mount: mount, Changed: true}, nil
}

func (m Manager) Inspect() ([]Finding, error) {
	attachments, err := m.loadAttachments()
	if err != nil {
		return nil, err
	}
	if len(attachments) == 0 {
		return nil, nil
	}
	declarations, err := m.loadDeclarations()
	if err != nil {
		return nil, err
	}
	byName := make(map[string]declaration, len(declarations))
	for _, item := range declarations {
		byName[item.Name] = item
	}
	var findings []Finding
	var active []attachment
	for _, attached := range attachments {
		declared, ok := byName[attached.Name]
		if !ok || declared.Path != attached.Path {
			findings = append(findings, Finding{Attention, attached.Name + " realm attachment has no matching declaration"})
			continue
		}
		mount, expandErr := m.expandPath(attached.Path)
		if expandErr != nil {
			return nil, expandErr
		}
		if err := m.checkCheckout(declared, mount); err != nil {
			findings = append(findings, Finding{Attention, err.Error()})
			continue
		}
		if err := m.checkActivation(mount, attached.Name); err != nil {
			var conflict activationConflict
			if errors.As(err, &conflict) {
				findings = append(findings, Finding{Attention, err.Error()})
			} else {
				findings = append(findings, Finding{Change, err.Error()})
				active = append(active, attached)
			}
			continue
		}
		active = append(active, attached)
		findings = append(findings, Finding{Current, attached.Name + " realm is active at " + m.portablePath(mount)})
	}
	expected, err := m.gitRealmsContents(active)
	if err != nil {
		return nil, err
	}
	actual, readErr := os.ReadFile(m.gitRealmsPath())
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	if string(actual) != expected {
		findings = append(findings, Finding{Change, "realm Git configuration needs regeneration"})
	}
	return findings, nil
}

func (m Manager) Reconcile(ctx context.Context) ([]Finding, error) {
	attachments, err := m.loadAttachments()
	if err != nil {
		return nil, err
	}
	if len(attachments) == 0 {
		return nil, nil
	}
	declarations, err := m.loadDeclarations()
	if err != nil {
		return nil, err
	}
	byName := make(map[string]declaration, len(declarations))
	for _, item := range declarations {
		byName[item.Name] = item
	}
	var findings []Finding
	var active []attachment
	for _, attached := range attachments {
		declared, ok := byName[attached.Name]
		if !ok || declared.Path != attached.Path {
			findings = append(findings, Finding{Attention, attached.Name + " realm attachment has no matching declaration"})
			continue
		}
		mount, expandErr := m.expandPath(attached.Path)
		if expandErr != nil {
			return nil, expandErr
		}
		cloned, checkoutErr := m.ensureCheckout(ctx, declared, mount)
		if checkoutErr != nil {
			findings = append(findings, Finding{Attention, checkoutErr.Error()})
			continue
		}
		changed, activationErr := m.ensureActivation(mount, attached.Name)
		if activationErr != nil {
			findings = append(findings, Finding{Attention, activationErr.Error()})
			continue
		}
		if changed || cloned {
			if err := m.allow(ctx, filepath.Join(mount, ".envrc")); err != nil {
				findings = append(findings, Finding{Attention, err.Error()})
				continue
			}
			findings = append(findings, Finding{Change, attached.Name + " realm activation was refreshed"})
		} else {
			findings = append(findings, Finding{Current, attached.Name + " realm is active at " + m.portablePath(mount)})
		}
		active = append(active, attached)
	}
	if err := m.writeGitRealms(active); err != nil {
		return findings, err
	}
	return findings, nil
}

func (m Manager) validateDeclaration(candidate declaration) error {
	declarations, err := m.loadDeclarations()
	if err != nil {
		return err
	}
	for _, existing := range declarations {
		if existing.Name == candidate.Name && existing != candidate {
			return fmt.Errorf("realm %s is already declared with different settings", candidate.Name)
		}
		if existing.Path == candidate.Path && existing.Name != candidate.Name {
			return fmt.Errorf("realm path %s is already declared as %s", candidate.Path, existing.Name)
		}
	}
	return nil
}

func (m Manager) validateActivation(mount, name string) error {
	link := filepath.Join(mount, ".envrc")
	info, err := os.Lstat(link)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return activationConflict{fmt.Sprintf("refusing to replace unmanaged .envrc at %s", mount)}
	}
	contents, err := os.ReadFile(link)
	if err != nil || string(contents) != activationContents(name, mount) {
		return activationConflict{fmt.Sprintf("refusing to replace unmanaged .envrc at %s", mount)}
	}
	return nil
}

func (m Manager) ensureCheckout(ctx context.Context, declared declaration, mount string) (bool, error) {
	if _, err := os.Stat(mount); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(mount), 0o700); err != nil {
			return false, err
		}
		result := platform.Run(ctx, m.Env.List, nil, "git", "clone", "--", declared.Repository, mount)
		if result.Code != 0 {
			return false, fmt.Errorf("could not clone %s realm", declared.Name)
		}
		return true, nil
	} else if err != nil {
		return false, err
	}
	if err := m.checkCheckout(declared, mount); err != nil {
		return false, err
	}
	return false, nil
}

func (m Manager) checkCheckout(declared declaration, mount string) error {
	info, err := os.Stat(filepath.Join(mount, ".git"))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("realm path is not a Git checkout: %s", m.portablePath(mount))
	}
	result := platform.Run(context.Background(), m.Env.List, nil, "git", "-C", mount, "config", "--local", "--get", "remote.origin.url")
	if result.Code != 0 {
		return fmt.Errorf("%s realm has no origin remote", declared.Name)
	}
	actual := strings.TrimSpace(string(result.Output))
	if !sameRepository(actual, declared.Repository) {
		return fmt.Errorf("%s realm origin does not match its declaration", declared.Name)
	}
	return nil
}

func (m Manager) ensureActivation(mount, name string) (bool, error) {
	if err := m.validateActivation(mount, name); err != nil {
		return false, err
	}
	expected := activationContents(name, mount)
	changed := false
	link := filepath.Join(mount, ".envrc")
	if actual, err := os.ReadFile(link); err != nil || string(actual) != expected {
		if err := atomicWrite(link, []byte(expected), 0o600); err != nil {
			return false, err
		}
		changed = true
	}
	if err := excludeEnvrc(mount); err != nil {
		return false, err
	}
	return changed, nil
}

func (m Manager) checkActivation(mount, name string) error {
	if err := m.validateActivation(mount, name); err != nil {
		return err
	}
	actual, err := os.ReadFile(filepath.Join(mount, ".envrc"))
	if err != nil || string(actual) != activationContents(name, mount) {
		return fmt.Errorf("%s realm activation needs regeneration", name)
	}
	return nil
}

func activationContents(name, mount string) string {
	mise := filepath.Join(mount, "mise.toml")
	custom := filepath.Join(mount, ".userland", "envrc")
	return "# Generated by userland realm. Do not edit.\n" +
		"export USERLAND_REALM=" + shellQuote(name) + "\n" +
		"export USERLAND_REALM_ROOT=" + shellQuote(mount) + "\n" +
		"if [[ -f " + shellQuote(mise) + " ]]; then\n" +
		"  watch_file " + shellQuote(mise) + "\n" +
		"  if command -v mise >/dev/null 2>&1; then\n" +
		"    eval \"$(mise -C " + shellQuote(mount) + " env -s bash)\"\n" +
		"  fi\n" +
		"fi\n" +
		"if [[ -f " + shellQuote(custom) + " ]]; then\n" +
		"  watch_file " + shellQuote(custom) + "\n" +
		"  source_env " + shellQuote(custom) + "\n" +
		"fi\n"
}

func (m Manager) allow(ctx context.Context, envrc string) error {
	result := platform.Run(ctx, m.Env.List, nil, "direnv", "allow", envrc)
	if result.Code != 0 {
		return fmt.Errorf("could not authorize realm activation with direnv: %s", strings.TrimSpace(string(result.Output)))
	}
	return nil
}

func (m Manager) revoke(ctx context.Context, envrc string) error {
	if _, ok := platform.LookPath(m.Env.List, "direnv"); !ok {
		return nil
	}
	result := platform.Run(ctx, m.Env.List, nil, "direnv", "revoke", envrc)
	if result.Code != 0 {
		return errors.New("could not revoke realm activation from direnv")
	}
	return nil
}

func (m Manager) ensureDeclaration(candidate declaration) (bool, error) {
	declarations, err := m.loadDeclarations()
	if err != nil {
		return false, err
	}
	for _, existing := range declarations {
		if existing == candidate {
			return false, nil
		}
	}
	path := m.catalogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	contents, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if len(contents) == 0 {
		contents = []byte("# name\trepository\tdefault_path\tmode\n")
	} else if contents[len(contents)-1] != '\n' {
		contents = append(contents, '\n')
	}
	contents = append(contents, []byte(strings.Join([]string{candidate.Name, candidate.Repository, candidate.Path, candidate.Mode}, "\t")+"\n")...)
	return true, atomicWrite(path, contents, 0o600)
}

func (m Manager) loadDeclarations() ([]declaration, error) {
	rows, err := readRows(m.catalogPath(), 4)
	if err != nil {
		return nil, err
	}
	result := make([]declaration, 0, len(rows))
	for _, row := range rows {
		if !realmName.MatchString(row[0]) || row[3] != "optional" {
			return nil, fmt.Errorf("invalid realm declaration in %s", m.catalogPath())
		}
		result = append(result, declaration{Name: row[0], Repository: row[1], Path: row[2], Mode: row[3]})
	}
	return result, nil
}

func (m Manager) loadAttachments() ([]attachment, error) {
	rows, err := readRows(m.attachmentsPath(), 2)
	if err != nil {
		return nil, err
	}
	result := make([]attachment, 0, len(rows))
	for _, row := range rows {
		if !realmName.MatchString(row[0]) {
			return nil, fmt.Errorf("invalid realm attachment in %s", m.attachmentsPath())
		}
		result = append(result, attachment{Name: row[0], Path: row[1]})
	}
	return result, nil
}

func (m Manager) writeAttachments(attachments []attachment) error {
	sort.Slice(attachments, func(i, j int) bool { return attachments[i].Name < attachments[j].Name })
	var contents strings.Builder
	for _, item := range attachments {
		fmt.Fprintf(&contents, "%s\t%s\n", item.Name, item.Path)
	}
	return atomicWrite(m.attachmentsPath(), []byte(contents.String()), 0o600)
}

func (m Manager) writeGitRealms(attachments []attachment) error {
	contents, err := m.gitRealmsContents(attachments)
	if err != nil {
		return err
	}
	if current, err := os.ReadFile(m.gitRealmsPath()); err == nil && string(current) == contents {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWrite(m.gitRealmsPath(), []byte(contents), 0o600)
}

func (m Manager) gitRealmsContents(attachments []attachment) (string, error) {
	copyOf := append([]attachment(nil), attachments...)
	sort.Slice(copyOf, func(i, j int) bool { return copyOf[i].Name < copyOf[j].Name })
	var contents strings.Builder
	contents.WriteString("# Generated by userland realm. Do not edit.\n")
	for _, item := range copyOf {
		mount, err := m.expandPath(item.Path)
		if err != nil {
			return "", err
		}
		config := filepath.Join(mount, ".gitconfig")
		if _, err := os.Stat(config); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", err
		}
		fmt.Fprintf(&contents, "\n[includeIf \"gitdir:%s/\"]\n\tpath = %s\n", escapeGit(mount), gitValue(config))
	}
	return contents.String(), nil
}

func (m Manager) catalogPath() string {
	if path := m.Env.Get("USERLAND_REALMS"); path != "" {
		return path
	}
	return filepath.Join(m.Env.Root, "cfg", "realms.tsv")
}

func (m Manager) attachmentsPath() string { return filepath.Join(m.Env.State, "realms.tsv") }
func (m Manager) gitRealmsPath() string {
	if path := m.Env.Get("USERLAND_GIT_REALMS"); path != "" {
		return path
	}
	return filepath.Join(m.Env.Home, ".config", "git", "userland-realms.gitconfig")
}

func (m Manager) expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" {
		path = m.Env.Home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(m.Env.Home, strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("realm mount path must be absolute or start with ~/")
	}
	return filepath.Clean(path), nil
}

func (m Manager) portablePath(path string) string {
	if path == m.Env.Home {
		return "~"
	}
	prefix := m.Env.Home + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return "~/" + strings.TrimPrefix(path, prefix)
	}
	return path
}

func repositoryName(repository string) (string, error) {
	trimmed := strings.TrimSuffix(strings.TrimRight(repository, "/"), ".git")
	index := strings.LastIndexAny(trimmed, "/:")
	name := trimmed[index+1:]
	if !realmName.MatchString(name) {
		return "", errors.New("could not derive a safe realm name from repository")
	}
	return name, nil
}

func (m Manager) resolveName(repository, path string) (string, error) {
	declarations, err := m.loadDeclarations()
	if err != nil {
		return "", err
	}
	for _, declared := range declarations {
		if declared.Path == path && sameRepository(declared.Repository, repository) {
			return declared.Name, nil
		}
	}
	return repositoryName(repository)
}

func sameRepository(left, right string) bool {
	normalize := func(value string) string {
		return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(value), "/"), ".git")
	}
	return normalize(left) == normalize(right)
}

func readRows(path string, fields int) ([][]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var rows [][]string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		values := strings.Split(line, "\t")
		if len(values) != fields {
			return nil, fmt.Errorf("invalid declaration in %s", path)
		}
		rows = append(rows, values)
	}
	return rows, scanner.Err()
}

func atomicWrite(path string, contents []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlink: %s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func excludeEnvrc(mount string) error {
	path := filepath.Join(mount, ".git", "info", "exclude")
	contents, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.TrimSpace(line) == "/.envrc" {
			return nil
		}
	}
	if len(contents) != 0 && contents[len(contents)-1] != '\n' {
		contents = append(contents, '\n')
	}
	contents = append(contents, []byte("# Userland realm activation\n/.envrc\n")...)
	return atomicWrite(path, contents, 0o600)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func escapeGit(value string) string  { return strings.ReplaceAll(value, `"`, `\"`) }
func gitValue(value string) string {
	if strings.ContainsAny(value, " \t#;\"") {
		return `"` + escapeGit(value) + `"`
	}
	return value
}
