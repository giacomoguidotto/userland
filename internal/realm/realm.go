// Package realm manages optional, path-scoped private Userland configuration.
package realm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/giacomoguidotto/userland/internal/csvfile"
	"github.com/giacomoguidotto/userland/internal/platform"
	repositorycatalog "github.com/giacomoguidotto/userland/internal/repository"
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

type Option struct {
	Name, Repository, DefaultPath string
}

type AuthenticationScript struct {
	Name, Path, Mount, ConfigurationRoot string
}

type Configuration struct {
	Name, Mount, Root string
}

type Manager struct {
	Env platform.Environment
}

func New(env platform.Environment) Manager { return Manager{Env: env} }

func (m Manager) Options() ([]Option, error) {
	declarations, err := m.loadDeclarations()
	if err != nil {
		return nil, err
	}
	result := make([]Option, 0, len(declarations))
	for _, declared := range declarations {
		if declared.Mode == "optional" {
			result = append(result, Option{declared.Name, declared.Repository, declared.Path})
		}
	}
	return result, nil
}

func (m Manager) SelectionPending() bool {
	if _, err := os.Stat(m.selectionPath()); err == nil {
		return false
	}
	if _, err := os.Stat(m.selectionProgressPath()); err == nil {
		return true
	}
	_, err := os.Stat(m.attachmentsPath())
	return errors.Is(err, os.ErrNotExist)
}

func (m Manager) BeginSelection() error {
	return atomicWrite(m.selectionProgressPath(), []byte("in-progress\n"), 0o600)
}

func (m Manager) AddByName(ctx context.Context, name string) (Result, error) {
	declarations, err := m.loadDeclarations()
	if err != nil {
		return Result{}, err
	}
	for _, declared := range declarations {
		if declared.Name == name {
			return m.Add(ctx, declared.Repository, declared.Path)
		}
	}
	return Result{}, fmt.Errorf("realm is not declared: %s", name)
}

func (m Manager) RecordSelection() error {
	attachments, err := m.loadAttachments()
	if err != nil {
		return err
	}
	if err := m.writeAttachments(attachments); err != nil {
		return err
	}
	return atomicWrite(m.selectionPath(), []byte("complete\n"), 0o600)
}

func (m Manager) AuthenticationScripts() ([]AuthenticationScript, error) {
	configurations, err := m.Configurations()
	if err != nil {
		return nil, err
	}
	var result []AuthenticationScript
	for _, configuration := range configurations {
		script := filepath.Join(configuration.Root, ".userland", "auth-wizard")
		if info, statErr := os.Stat(script); statErr == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			result = append(result, AuthenticationScript{configuration.Name, script, configuration.Mount, configuration.Root})
		}
	}
	return result, nil
}

func (m Manager) Configurations() ([]Configuration, error) {
	attachments, err := m.loadAttachments()
	if err != nil {
		return nil, err
	}
	declarations, err := m.declarationsByName()
	if err != nil {
		return nil, err
	}
	var result []Configuration
	for _, attached := range attachments {
		declared, ok := declarations[attached.Name]
		if !ok {
			continue
		}
		mount, expandErr := m.expandPath(attached.Path)
		if expandErr != nil {
			return nil, expandErr
		}
		configurationRoot, expandErr := m.expandPath(declared.ConfigurationPath)
		if expandErr != nil {
			return nil, expandErr
		}
		result = append(result, Configuration{attached.Name, mount, configurationRoot})
	}
	return result, nil
}

type declaration struct {
	Name, Repository, ConfigurationPath, Path, Branch, Mode string
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
	declared, err := m.resolveDeclaration(repository, declaredPath)
	if err != nil {
		return Result{}, err
	}
	if err := m.validateDeclaration(declared); err != nil {
		return Result{}, err
	}
	configurationRoot, err := m.expandPath(declared.ConfigurationPath)
	if err != nil {
		return Result{}, err
	}
	if err := m.validateActivation(absMount, configurationRoot, declared.Name); err != nil {
		return Result{}, err
	}
	cloned, err := m.ensureCheckout(ctx, declared, configurationRoot)
	if err != nil {
		return Result{}, err
	}
	primaryChanged := false
	if configurationRoot != absMount {
		if _, statErr := os.Stat(absMount); errors.Is(statErr, os.ErrNotExist) {
			finding, found, reconcileErr := repositorycatalog.ReconcilePrimaryDeclaration(ctx, m.Env, configurationRoot, absMount)
			if reconcileErr != nil {
				return Result{}, reconcileErr
			}
			if !found {
				return Result{}, fmt.Errorf("%s realm configuration does not declare its primary checkout", declared.Name)
			}
			if finding.State == repositorycatalog.DeclarationAttention {
				return Result{}, fmt.Errorf("%s realm: %s", declared.Name, finding.Message)
			}
			primaryChanged = finding.State == repositorycatalog.DeclarationChange
		} else if statErr != nil {
			return Result{}, statErr
		}
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
		if existing.Name == declared.Name {
			if existing.Path != declaredPath {
				return Result{}, fmt.Errorf("realm %s is already attached at %s", declared.Name, existing.Path)
			}
			attachmentChanged = false
		}
	}
	if attachmentChanged {
		attachments = append(attachments, attachment{Name: declared.Name, Path: declaredPath})
	}
	activationChanged, err := m.ensureActivation(absMount, configurationRoot, declared.Name)
	if err != nil {
		return Result{}, err
	}
	if err := m.allow(ctx, filepath.Join(absMount, ".envrc")); err != nil {
		return Result{}, err
	}
	if _, err := m.reconcileFileProjections(configurationRoot, absMount, true); err != nil {
		return Result{}, err
	}
	if attachmentChanged {
		if err := m.writeAttachments(attachments); err != nil {
			return Result{}, err
		}
	}
	if err := m.writeRealmConfigurations(attachments); err != nil {
		return Result{}, err
	}
	return Result{
		Name: declared.Name, Repository: repository, Mount: absMount,
		Changed: cloned || primaryChanged || catalogChanged || attachmentChanged || activationChanged,
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
	configurationRoot := mount
	declarations, declarationErr := m.loadDeclarations()
	if declarationErr != nil {
		return Result{}, declarationErr
	}
	for _, declared := range declarations {
		if declared.Name == selected.Name && declared.Path == selected.Path {
			configurationRoot, err = m.expandPath(declared.ConfigurationPath)
			if err != nil {
				return Result{}, err
			}
			break
		}
	}
	link := filepath.Join(mount, ".envrc")
	removeActivation := false
	if info, statErr := os.Lstat(link); statErr == nil {
		if !info.Mode().IsRegular() {
			return Result{}, fmt.Errorf("refusing to remove unmanaged .envrc at %s", mount)
		}
		contents, readErr := os.ReadFile(link)
		if readErr != nil || !managedActivationContents(string(contents), selected.Name, mount, configurationRoot) {
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
	if err := m.writeRealmConfigurations(attachments); err != nil {
		return Result{}, err
	}
	return Result{Name: selected.Name, Mount: mount, Changed: true}, nil
}

func (m Manager) Inspect(ctx context.Context) ([]Finding, error) {
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
		configurationRoot, expandErr := m.expandPath(declared.ConfigurationPath)
		if expandErr != nil {
			return nil, expandErr
		}
		if err := m.checkCheckout(declared, configurationRoot); err != nil {
			findings = append(findings, Finding{Attention, err.Error()})
			continue
		}
		if err := m.checkActivation(mount, configurationRoot, attached.Name); err != nil {
			var conflict activationConflict
			if errors.As(err, &conflict) {
				findings = append(findings, Finding{Attention, err.Error()})
			} else {
				findings = append(findings, Finding{Change, err.Error()})
				active = append(active, attached)
			}
			continue
		}
		canonical := repositorycatalog.InspectCanonical(ctx, m.Env, configurationRoot, declared.Repository, declared.Branch)
		if canonical.Status == repositorycatalog.CanonicalAttention {
			findings = append(findings, Finding{Attention, attached.Name + " realm " + canonical.Message})
		}
		if canonical.Status == repositorycatalog.CanonicalChange {
			findings = append(findings, Finding{Change, attached.Name + " realm " + canonical.Message})
		}
		active = append(active, attached)
		repositories, repositoryErr := repositorycatalog.InspectDeclarations(ctx, m.Env, configurationRoot, mount)
		if repositoryErr != nil {
			findings = append(findings, Finding{Attention, attached.Name + " realm repository taxonomy is invalid: " + repositoryErr.Error()})
			continue
		}
		findings = appendRealmRepositories(findings, attached.Name, m.portablePath(mount), repositories)
		projectionFindings, projectionErr := m.reconcileFileProjections(configurationRoot, mount, false)
		if projectionErr != nil {
			findings = append(findings, Finding{Attention, attached.Name + " realm file projections are invalid: " + projectionErr.Error()})
			continue
		}
		findings = append(findings, prefixFindings(attached.Name+" realm: ", projectionFindings)...)
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
	sshExpected, err := m.sshRealmsContents(active)
	if err != nil {
		return nil, err
	}
	sshActual, sshReadErr := os.ReadFile(m.sshRealmsPath())
	if sshReadErr != nil && !errors.Is(sshReadErr, os.ErrNotExist) {
		return nil, sshReadErr
	}
	if string(sshActual) != sshExpected {
		findings = append(findings, Finding{Change, "realm SSH configuration needs regeneration"})
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
		configurationRoot, expandErr := m.expandPath(declared.ConfigurationPath)
		if expandErr != nil {
			return nil, expandErr
		}
		cloned, checkoutErr := m.ensureCheckout(ctx, declared, configurationRoot)
		if checkoutErr != nil {
			findings = append(findings, Finding{Attention, checkoutErr.Error()})
			continue
		}
		canonical := repositorycatalog.ReconcileCanonical(ctx, m.Env, configurationRoot, declared.Repository, declared.Branch)
		if canonical.Status == repositorycatalog.CanonicalAttention {
			findings = append(findings, Finding{Attention, attached.Name + " realm " + canonical.Message})
			continue
		}
		changed, activationErr := m.ensureActivation(mount, configurationRoot, attached.Name)
		if activationErr != nil {
			findings = append(findings, Finding{Attention, activationErr.Error()})
			continue
		}
		activationChanged := changed || cloned || canonical.Status == repositorycatalog.CanonicalChange
		if activationChanged {
			if err := m.allow(ctx, filepath.Join(mount, ".envrc")); err != nil {
				findings = append(findings, Finding{Attention, err.Error()})
				continue
			}
			findings = append(findings, Finding{Change, attached.Name + " realm activation was refreshed"})
		}
		repositories, repositoryErr := repositorycatalog.ReconcileDeclarations(ctx, m.Env, configurationRoot, mount)
		if repositoryErr != nil {
			findings = append(findings, Finding{Attention, attached.Name + " realm repository taxonomy could not be reconciled: " + repositoryErr.Error()})
			continue
		}
		if activationChanged {
			findings = appendRepositoryFindings(findings, attached.Name, repositories)
		} else {
			findings = appendRealmRepositories(findings, attached.Name, m.portablePath(mount), repositories)
		}
		projectionFindings, projectionErr := m.reconcileFileProjections(configurationRoot, mount, true)
		if projectionErr != nil {
			findings = append(findings, Finding{Attention, attached.Name + " realm file projections could not be reconciled: " + projectionErr.Error()})
			continue
		}
		findings = append(findings, prefixFindings(attached.Name+" realm: ", projectionFindings)...)
		active = append(active, attached)
	}
	if err := m.writeRealmConfigurations(active); err != nil {
		return findings, err
	}
	return findings, nil
}

func prefixFindings(prefix string, findings []Finding) []Finding {
	for index := range findings {
		findings[index].Message = prefix + findings[index].Message
	}
	return findings
}

func appendRealmRepositories(findings []Finding, name, mount string, repositories []repositorycatalog.DeclarationFinding) []Finding {
	if len(repositories) == 1 && repositories[0].State == repositorycatalog.DeclarationCurrent {
		return append(findings, Finding{Current, name + " realm is active at " + mount + " and its repository taxonomy matches"})
	}
	findings = append(findings, Finding{Current, name + " realm is active at " + mount})
	return appendRepositoryFindings(findings, name, repositories)
}

func appendRepositoryFindings(findings []Finding, name string, repositories []repositorycatalog.DeclarationFinding) []Finding {
	for _, item := range repositories {
		state := Current
		switch item.State {
		case repositorycatalog.DeclarationChange:
			state = Change
		case repositorycatalog.DeclarationAttention:
			state = Attention
		}
		findings = append(findings, Finding{state, name + " realm: " + item.Message})
	}
	return findings
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
		if existing.ConfigurationPath == candidate.ConfigurationPath && existing.Name != candidate.Name {
			return fmt.Errorf("realm configuration path %s is already declared as %s", candidate.ConfigurationPath, existing.Name)
		}
		if existing.Path == candidate.Path && existing.Name != candidate.Name {
			return fmt.Errorf("realm path %s is already declared as %s", candidate.Path, existing.Name)
		}
	}
	return nil
}

func (m Manager) validateActivation(mount, configurationRoot, name string) error {
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
	if err != nil || !managedActivationContents(string(contents), name, mount, configurationRoot) {
		return activationConflict{fmt.Sprintf("refusing to replace unmanaged .envrc at %s", mount)}
	}
	return nil
}

func (m Manager) ensureCheckout(ctx context.Context, declared declaration, configurationRoot string) (bool, error) {
	if _, err := os.Stat(configurationRoot); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(configurationRoot), 0o700); err != nil {
			return false, err
		}
		result := platform.Run(ctx, m.Env.List, nil, "git", "clone", "--branch", declared.Branch, "--", declared.Repository, configurationRoot)
		if result.Code != 0 {
			return false, fmt.Errorf("could not clone %s realm", declared.Name)
		}
		return true, nil
	} else if err != nil {
		return false, err
	}
	if err := m.checkCheckout(declared, configurationRoot); err != nil {
		return false, err
	}
	return false, nil
}

func (m Manager) checkCheckout(declared declaration, configurationRoot string) error {
	info, err := os.Stat(filepath.Join(configurationRoot, ".git"))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("realm configuration path is not a Git checkout: %s", m.portablePath(configurationRoot))
	}
	result := platform.Run(context.Background(), m.Env.List, nil, "git", "-C", configurationRoot, "config", "--local", "--get", "remote.origin.url")
	if result.Code != 0 {
		return fmt.Errorf("%s realm has no origin remote", declared.Name)
	}
	actual := strings.TrimSpace(string(result.Output))
	if !sameRepository(actual, declared.Repository) {
		return fmt.Errorf("%s realm origin does not match its declaration", declared.Name)
	}
	return nil
}

func (m Manager) ensureActivation(mount, configurationRoot, name string) (bool, error) {
	if err := m.validateActivation(mount, configurationRoot, name); err != nil {
		return false, err
	}
	expected := activationContents(name, mount, configurationRoot)
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

func (m Manager) checkActivation(mount, configurationRoot, name string) error {
	if err := m.validateActivation(mount, configurationRoot, name); err != nil {
		return err
	}
	actual, err := os.ReadFile(filepath.Join(mount, ".envrc"))
	if err != nil || string(actual) != activationContents(name, mount, configurationRoot) {
		return fmt.Errorf("%s realm activation needs regeneration", name)
	}
	return nil
}

func activationContents(name, mount, configurationRoot string) string {
	custom := filepath.Join(configurationRoot, ".userland", "envrc")
	contents := "# Generated by userland realm. Do not edit.\n" +
		"export USERLAND_REALM=" + shellQuote(name) + "\n" +
		"export USERLAND_REALM_ROOT=" + shellQuote(mount) + "\n" +
		"export USERLAND_REALM_CONFIG_ROOT=" + shellQuote(configurationRoot) + "\n"
	contents += miseActivation(configurationRoot)
	if mount != configurationRoot {
		contents += miseActivation(mount)
	}
	return contents +
		"if [[ -f " + shellQuote(custom) + " ]]; then\n" +
		"  watch_file " + shellQuote(custom) + "\n" +
		"  source_env " + shellQuote(custom) + "\n" +
		"fi\n"
}

func managedActivationContents(contents, name, mount, configurationRoot string) bool {
	return contents == activationContents(name, mount, configurationRoot) ||
		contents == legacyActivationContents(name, mount)
}

func legacyActivationContents(name, mount string) string {
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

func miseActivation(root string) string {
	mise := filepath.Join(root, "mise.toml")
	return "if [[ -f " + shellQuote(mise) + " ]]; then\n" +
		"  watch_file " + shellQuote(mise) + "\n" +
		"  if command -v mise >/dev/null 2>&1; then\n" +
		"    eval \"$(mise -C " + shellQuote(root) + " env -s bash)\"\n" +
		"  fi\n" +
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
	declarations = append(declarations, candidate)
	rows := make([][]string, 0, len(declarations))
	for _, item := range declarations {
		rows = append(rows, []string{item.Name, item.Repository, item.ConfigurationPath, item.Path, item.Branch, item.Mode})
	}
	return true, csvfile.Write(m.catalogPath(), realmHeader, rows, 0o600)
}

func (m Manager) loadDeclarations() ([]declaration, error) {
	rows, err := csvfile.Read(m.catalogPath(), realmHeader)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]declaration, 0, len(rows))
	for _, row := range rows {
		if !realmName.MatchString(row[0]) || !validBranch(row[4]) || row[5] != "optional" {
			return nil, fmt.Errorf("invalid realm declaration in %s", m.catalogPath())
		}
		result = append(result, declaration{
			Name: row[0], Repository: row[1], ConfigurationPath: row[2], Path: row[3], Branch: row[4], Mode: row[5],
		})
	}
	return result, nil
}

func (m Manager) loadAttachments() ([]attachment, error) {
	rows, err := csvfile.Read(m.attachmentsPath(), attachmentHeader)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
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
	rows := make([][]string, 0, len(attachments))
	for _, item := range attachments {
		rows = append(rows, []string{item.Name, item.Path})
	}
	return csvfile.Write(m.attachmentsPath(), attachmentHeader, rows, 0o600)
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

func (m Manager) writeRealmConfigurations(attachments []attachment) error {
	if err := m.writeGitRealms(attachments); err != nil {
		return err
	}
	return m.writeSSHRealms(attachments)
}

func (m Manager) writeSSHRealms(attachments []attachment) error {
	contents, err := m.sshRealmsContents(attachments)
	if err != nil {
		return err
	}
	if current, err := os.ReadFile(m.sshRealmsPath()); err == nil && string(current) == contents {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWrite(m.sshRealmsPath(), []byte(contents), 0o600)
}

func (m Manager) gitRealmsContents(attachments []attachment) (string, error) {
	declarations, err := m.declarationsByName()
	if err != nil {
		return "", err
	}
	copyOf := append([]attachment(nil), attachments...)
	sort.Slice(copyOf, func(i, j int) bool { return copyOf[i].Name < copyOf[j].Name })
	var contents strings.Builder
	contents.WriteString("# Generated by userland realm. Do not edit.\n")
	for _, item := range copyOf {
		declared, ok := declarations[item.Name]
		if !ok || declared.Path != item.Path {
			continue
		}
		mount, err := m.expandPath(item.Path)
		if err != nil {
			return "", err
		}
		configurationRoot, err := m.expandPath(declared.ConfigurationPath)
		if err != nil {
			return "", err
		}
		config := filepath.Join(configurationRoot, ".gitconfig")
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

func (m Manager) sshRealmsContents(attachments []attachment) (string, error) {
	declarations, err := m.declarationsByName()
	if err != nil {
		return "", err
	}
	copyOf := append([]attachment(nil), attachments...)
	sort.Slice(copyOf, func(i, j int) bool { return copyOf[i].Name < copyOf[j].Name })
	var contents strings.Builder
	contents.WriteString("# Generated by userland realm. Do not edit.\n")
	for _, item := range copyOf {
		declared, ok := declarations[item.Name]
		if !ok || declared.Path != item.Path {
			continue
		}
		configurationRoot, err := m.expandPath(declared.ConfigurationPath)
		if err != nil {
			return "", err
		}
		source := filepath.Join(configurationRoot, "ssh.config")
		template, err := os.ReadFile(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		mount, err := m.expandPath(item.Path)
		if err != nil {
			return "", err
		}
		rendered := strings.ReplaceAll(string(template), "{{USERLAND_REALM_ROOT}}", mount)
		rendered = strings.ReplaceAll(rendered, "{{USERLAND_REALM_CONFIG_ROOT}}", configurationRoot)
		rendered = strings.ReplaceAll(rendered, "{{USERLAND_HOME}}", m.Env.Home)
		if strings.Contains(rendered, "{{USERLAND_") {
			return "", fmt.Errorf("unsupported placeholder in %s realm SSH configuration", item.Name)
		}
		fmt.Fprintf(&contents, "\n# Realm: %s\n%s", item.Name, rendered)
		if !strings.HasSuffix(rendered, "\n") {
			contents.WriteByte('\n')
		}
	}
	return contents.String(), nil
}

func (m Manager) declarationsByName() (map[string]declaration, error) {
	declarations, err := m.loadDeclarations()
	if err != nil {
		return nil, err
	}
	result := make(map[string]declaration, len(declarations))
	for _, declared := range declarations {
		result[declared.Name] = declared
	}
	return result, nil
}

func (m Manager) catalogPath() string {
	if path := m.Env.Get("USERLAND_REALMS"); path != "" {
		return path
	}
	return filepath.Join(m.Env.Root, "cfg", "realms.csv")
}

func (m Manager) attachmentsPath() string { return filepath.Join(m.Env.State, "realms.csv") }

func (m Manager) selectionPath() string { return filepath.Join(m.Env.State, "realm-selection") }

func (m Manager) selectionProgressPath() string {
	return filepath.Join(m.Env.State, "realm-selection-in-progress")
}
func (m Manager) gitRealmsPath() string {
	if path := m.Env.Get("USERLAND_GIT_REALMS"); path != "" {
		return path
	}
	return filepath.Join(m.Env.Home, ".config", "git", "userland-realms.gitconfig")
}
func (m Manager) sshRealmsPath() string {
	if path := m.Env.Get("USERLAND_SSH_REALMS"); path != "" {
		return path
	}
	return filepath.Join(m.Env.Home, ".ssh", "userland-realms.config")
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

func (m Manager) resolveDeclaration(repository, path string) (declaration, error) {
	declarations, err := m.loadDeclarations()
	if err != nil {
		return declaration{}, err
	}
	for _, declared := range declarations {
		if declared.Path == path && sameRepository(declared.Repository, repository) {
			return declared, nil
		}
	}
	name, err := repositoryName(repository)
	if err != nil {
		return declaration{}, err
	}
	return declaration{
		Name: name, Repository: repository, ConfigurationPath: path, Path: path, Branch: "main", Mode: "optional",
	}, nil
}

func validBranch(branch string) bool {
	return branch != "" && !strings.HasPrefix(branch, "-") && !strings.ContainsAny(branch, " ~^:?*[\\") &&
		!strings.Contains(branch, "..") && !strings.Contains(branch, "//") && !strings.HasSuffix(branch, "/") &&
		!strings.HasSuffix(branch, ".") && !strings.HasSuffix(branch, ".lock")
}

func sameRepository(left, right string) bool {
	normalize := func(value string) string {
		return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(value), "/"), ".git")
	}
	return normalize(left) == normalize(right)
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

var realmHeader = []string{"name", "repository", "configuration_path", "default_path", "branch", "mode"}
var attachmentHeader = []string{"name", "path"}
