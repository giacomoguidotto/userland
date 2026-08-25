// Package managedfiles applies declared links inside a recoverable transaction.
package managedfiles

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/giacomoguidotto/userland/internal/platform"
	"github.com/giacomoguidotto/userland/plan"
)

const recoveryFormat = "dotfiles-v1"

type Log func(level, message string)

type Manager struct {
	Env platform.Environment
	Log Log
}

type status struct {
	Files []struct {
		State  string `json:"state"`
		Source string `json:"source"`
		Target string `json:"target"`
	} `json:"files"`
	Edits []struct {
		State string `json:"state"`
	} `json:"edits"`
}

type manifestEntry struct {
	Index    int
	Path     string
	Presence string
}

func (m Manager) PlanLegacy(value *plan.Plan) {
	for _, path := range m.legacyPaths() {
		m.walkLegacy(path, func(link, source string) {
			_ = value.Add(plan.Item{Area: plan.AreaCleanup, Action: "release", Handling: plan.Automatic, Ownership: "userland", Target: link, Detail: "release legacy workspace link", Proof: "legacy-link:" + source})
		})
	}
	checkout := filepath.Join(m.Env.Data, "repo")
	info, err := os.Lstat(checkout)
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || !isDirectory(filepath.Join(checkout, ".git"))) {
		_ = value.Add(plan.Item{Area: plan.AreaCleanup, Action: "review", Handling: plan.Blocked, Ownership: "userland", Target: checkout, Detail: "unrecognized legacy checkout requires review", Proof: "legacy-checkout:" + checkout})
	} else if isDirectory(filepath.Join(checkout, ".git")) {
		_ = value.Add(plan.Item{Area: plan.AreaCleanup, Action: "release", Handling: plan.Automatic, Ownership: "userland", Target: checkout, Detail: "move to Trash after managed links migrate and doctor passes", Proof: "legacy-checkout:" + checkout})
	}
}

func (m Manager) Recover() error {
	active := filepath.Join(m.Env.State, "recovery", "active")
	contents, err := os.ReadFile(active)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	id := strings.TrimSpace(string(contents))
	if id == "" || strings.ContainsAny(id, `/\`) {
		return errors.New("invalid managed-file recovery id")
	}
	directory := filepath.Join(m.Env.State, "recovery", id)
	if info, err := os.Lstat(directory); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid managed-file recovery directory")
	}
	state, err := readLine(filepath.Join(directory, "state"))
	if err != nil {
		return err
	}
	switch state {
	case "committed", "rolled-back":
		return m.clearActive(id)
	case "preparing", "applying", "rolling-back", "rollback-failed":
		return m.rollback(directory)
	default:
		return errors.New("unknown managed-file recovery state")
	}
}

func (m Manager) Prune() error {
	root := filepath.Join(m.Env.State, "recovery")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, entry := range entries {
		directory := filepath.Join(root, entry.Name())
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "cutover-") || validRecovery(directory) != nil {
			continue
		}
		state, _ := readLine(filepath.Join(directory, "state"))
		created, err := readInteger(filepath.Join(directory, "created-at"))
		if state == "committed" && err == nil && now-created >= 86400 {
			if err := os.RemoveAll(directory); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m Manager) WindowOpen() bool {
	entries, _ := os.ReadDir(filepath.Join(m.Env.State, "recovery"))
	now := time.Now().Unix()
	for _, entry := range entries {
		directory := filepath.Join(m.Env.State, "recovery", entry.Name())
		state, _ := readLine(filepath.Join(directory, "state"))
		created, err := readInteger(filepath.Join(directory, "created-at"))
		if entry.IsDir() && state == "committed" && err == nil && now-created < 86400 {
			return true
		}
	}
	return false
}

func (m Manager) Apply(ctx context.Context) int {
	if err := m.Recover(); err != nil || m.Prune() != nil {
		return 1
	}
	result := m.Env.RunMise(ctx, nil, "bootstrap", "dotfiles", "status", "--json")
	if result.Code != 0 {
		return result.Code
	}
	var before status
	if json.Unmarshal(result.Output, &before) != nil {
		return 1
	}
	if statusApplied(before) {
		return 0
	}
	directory, id, err := m.begin(before, result.Output)
	if err != nil {
		return 1
	}
	code := 0
	if err := m.prepareLegacy(); err != nil {
		code = 1
	}
	if code == 0 {
		result = m.Env.RunMise(ctx, nil, "bootstrap", "dotfiles", "apply", "--yes", "--force")
		code = result.Code
	}
	if code == 0 {
		result = m.Env.RunMise(ctx, nil, "bootstrap", "dotfiles", "status", "--json")
		var after status
		if result.Code != 0 || json.Unmarshal(result.Output, &after) != nil || !statusApplied(after) {
			code = result.Code
			if code == 0 {
				code = 1
			}
		}
	}
	if code != 0 {
		if m.rollback(directory) != nil {
			return 1
		}
		return code
	}
	if m.writeState(directory, "committed") != nil || m.clearActive(id) != nil {
		return 1
	}
	return 0
}

func statusApplied(value status) bool {
	for _, file := range value.Files {
		if file.State != "applied" {
			return false
		}
	}
	for _, edit := range value.Edits {
		if edit.State != "applied" && edit.State != "present" {
			return false
		}
	}
	return true
}

func (m Manager) begin(value status, encoded []byte) (string, string, error) {
	recovery := filepath.Join(m.Env.State, "recovery")
	if err := os.MkdirAll(recovery, 0o700); err != nil {
		return "", "", err
	}
	if _, err := os.Lstat(filepath.Join(recovery, "active")); err == nil {
		return "", "", errors.New("a managed-file transaction is active")
	}
	created := time.Now().Unix()
	id := fmt.Sprintf("cutover-%d-%d", created, os.Getpid())
	directory := filepath.Join(recovery, id)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return "", "", err
	}
	if err := os.Mkdir(filepath.Join(directory, "before"), 0o700); err != nil {
		return "", "", err
	}
	for path, contents := range map[string][]byte{
		".userland-recovery": []byte(recoveryFormat + "\n"),
		"created-at":         []byte(strconv.FormatInt(created, 10) + "\n"),
		"manifest":           nil,
		"status-before.json": encoded,
	} {
		if err := os.WriteFile(filepath.Join(directory, path), contents, 0o600); err != nil {
			return "", "", err
		}
	}
	if err := m.writeState(directory, "preparing"); err != nil {
		return "", "", err
	}
	seen := map[string]bool{}
	for _, file := range value.Files {
		target, err := m.expandTarget(file.Target)
		if err != nil {
			return "", "", err
		}
		if !seen[target] {
			if err := snapshot(directory, len(seen)+1, target); err != nil {
				return "", "", err
			}
			seen[target] = true
		}
	}
	for _, target := range []string{filepath.Join(m.Env.Home, ".codex", "AGENTS.md"), filepath.Join(m.Env.Home, ".config", "opencode", "AGENTS.md")} {
		if exists(target) && !seen[target] {
			if err := snapshot(directory, len(seen)+1, target); err != nil {
				return "", "", err
			}
			seen[target] = true
		}
	}
	if err := atomicWrite(filepath.Join(recovery, "active"), []byte(id+"\n"), 0o600); err != nil {
		return "", "", err
	}
	if err := m.writeState(directory, "applying"); err != nil {
		return "", "", err
	}
	return directory, id, nil
}

func snapshot(directory string, index int, target string) error {
	presence := "absent"
	if exists(target) {
		presence = "present"
		if err := copyPath(target, filepath.Join(directory, "before", strconv.Itoa(index))); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(filepath.Join(directory, "manifest"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "%d\t%s\t%s\n", index, target, presence)
	return err
}

func (m Manager) rollback(directory string) error {
	if err := validRecovery(directory); err != nil {
		return err
	}
	if err := m.writeState(directory, "rolling-back"); err != nil {
		return err
	}
	after := filepath.Join(directory, fmt.Sprintf("after-%d-%d", time.Now().Unix(), os.Getpid()))
	if err := os.Mkdir(after, 0o700); err != nil {
		return err
	}
	entries, err := readManifest(filepath.Join(directory, "manifest"))
	if err != nil {
		return err
	}
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if !within(m.Env.Home, entry.Path) {
			_ = m.writeState(directory, "rollback-failed")
			return errors.New("rollback target escaped home")
		}
		if exists(entry.Path) {
			if err := os.Rename(entry.Path, filepath.Join(after, strconv.Itoa(entry.Index))); err != nil {
				_ = m.writeState(directory, "rollback-failed")
				return err
			}
		}
		if entry.Presence == "present" {
			if err := os.MkdirAll(filepath.Dir(entry.Path), 0o755); err != nil {
				return err
			}
			if err := copyPath(filepath.Join(directory, "before", strconv.Itoa(entry.Index)), entry.Path); err != nil {
				_ = m.writeState(directory, "rollback-failed")
				return err
			}
		}
	}
	if err := m.writeState(directory, "rolled-back"); err != nil {
		return err
	}
	if err := m.clearActive(filepath.Base(directory)); err != nil {
		return err
	}
	m.log("changed", "Restored the managed-file state from before this sync")
	return nil
}

func (m Manager) prepareLegacy() error {
	m.log("sync", "checking legacy workspace links")
	for _, root := range m.legacyPaths() {
		var links []string
		m.walkLegacy(root, func(link, _ string) { links = append(links, link) })
		sort.Strings(links)
		for _, link := range links {
			if err := m.migrateLegacy(link); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m Manager) migrateLegacy(link string) error {
	source, err := resolvedLink(link)
	if err != nil || !m.ownedLegacy(source) {
		return nil
	}
	suffix := legacySuffix(source, m.Env.Data)
	switch suffix {
	case "home/agents/AGENT.md", "home/agents/opencode/AGENTS.md":
		if err := os.Remove(link); err != nil {
			return err
		}
		m.log("changed", "removed retired agent instructions at "+link)
		return nil
	case "home/agents/skills", "agents/skills":
		suffix = "agents/skills"
	case "home/claude/settings.json", "agents/claude/settings.json":
		suffix = "agents/claude/settings.json"
	}
	replacement := filepath.Join(m.Env.Root, "cfg", suffix)
	if !exists(replacement) {
		return nil
	}
	if isDirectory(source) {
		temporary := fmt.Sprintf("%s.userland-migrate.%d", link, os.Getpid())
		if err := os.MkdirAll(temporary, 0o755); err != nil {
			return err
		}
		entries, _ := os.ReadDir(source)
		for _, entry := range entries {
			if entry.Name() == ".git" || exists(filepath.Join(replacement, entry.Name())) {
				continue
			}
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(temporary, entry.Name())); err != nil {
				return err
			}
			m.log("preserved", filepath.Join(temporary, entry.Name()))
		}
		if err := os.Remove(link); err != nil {
			return err
		}
		if err := os.Rename(temporary, link); err != nil {
			return err
		}
	} else if err := os.Remove(link); err != nil {
		return err
	}
	m.log("changed", "released legacy workspace ownership of "+link)
	return nil
}

func (m Manager) TrashLegacy(ctx context.Context) int {
	checkout := filepath.Join(m.Env.Data, "repo")
	if !exists(checkout) {
		return 0
	}
	info, _ := os.Lstat(checkout)
	if info.Mode()&os.ModeSymlink != 0 || !isDirectory(filepath.Join(checkout, ".git")) || isSymlink(filepath.Join(checkout, ".git")) {
		m.log("attention", "preserved unrecognized legacy checkout at "+checkout)
		return 2
	}
	gitEnv := m.Env.With("GIT_CONFIG_GLOBAL", "/dev/null", "GIT_CONFIG_NOSYSTEM", "1", "GIT_OPTIONAL_LOCKS", "0")
	if platform.Run(ctx, gitEnv, nil, "git", "-C", checkout, "config", "--local", "--no-includes", "--get", "core.worktree").Code == 0 {
		m.log("attention", "preserved external-worktree checkout at "+checkout)
		return 2
	}
	origin := platform.Run(ctx, gitEnv, nil, "git", "-C", checkout, "config", "--local", "--no-includes", "--get", "remote.origin.url")
	if origin.Code != 0 {
		m.log("attention", "preserved legacy checkout without an origin at "+checkout)
		return 2
	}
	if strings.TrimSpace(string(origin.Output)) != "https://github.com/giacomoguidotto/userland.git" {
		m.log("attention", "preserved legacy checkout with an unexpected origin at "+checkout)
		return 2
	}
	status := platform.Run(ctx, gitEnv, nil, "git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-C", checkout, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if status.Code != 0 {
		m.log("attention", "could not inspect legacy checkout at "+checkout)
		return 2
	}
	if strings.TrimSpace(string(status.Output)) != "" || m.checkoutHasLinks(checkout) {
		m.log("attention", "preserved legacy checkout with local state or active links at "+checkout)
		return 2
	}
	trash := m.Env.Get("USERLAND_TRASH_DIR")
	if trash == "" {
		trash = filepath.Join(m.Env.Home, ".Trash")
	}
	if info, err := os.Lstat(trash); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		m.log("attention", "preserved legacy checkout because Trash is unavailable at "+trash)
		return 2
	}
	if err := os.MkdirAll(trash, 0o755); err != nil {
		m.log("attention", "preserved legacy checkout because Trash could not be created at "+trash)
		return 2
	}
	bundle, err := os.MkdirTemp(trash, "userland-legacy.")
	if err != nil || os.Rename(checkout, filepath.Join(bundle, "checkout")) != nil {
		m.log("attention", "preserved legacy checkout because it could not be moved to Trash")
		return 2
	}
	m.log("changed", "moved legacy userland checkout to Trash at "+filepath.Join(bundle, "checkout"))
	return 0
}

func (m Manager) legacyPaths() []string {
	paths := []string{
		filepath.Join(m.Env.Home, ".zshrc"), filepath.Join(m.Env.Home, ".zshenv"),
		filepath.Join(m.Env.Home, ".hushlogin"), filepath.Join(m.Env.Home, ".ssh", "config"),
		filepath.Join(m.Env.Home, ".agents", "skills"), filepath.Join(m.Env.Home, ".claude", "skills"),
		filepath.Join(m.Env.Home, ".claude", "settings.json"), filepath.Join(m.Env.Home, ".codex", "AGENTS.md"),
	}
	config, _ := filepath.Glob(filepath.Join(m.Env.Home, ".config", "*"))
	return append(paths, config...)
}

func (m Manager) walkLegacy(root string, visit func(link, source string)) {
	info, err := os.Lstat(root)
	if err != nil {
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if source, err := resolvedLink(root); err == nil && m.ownedLegacy(source) {
			visit(root, source)
		}
		return
	}
	if !info.IsDir() {
		return
	}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type()&os.ModeSymlink != 0 {
			if source, err := resolvedLink(path); err == nil && m.ownedLegacy(source) {
				visit(path, source)
			}
			return filepath.SkipDir
		}
		return nil
	})
}

func (m Manager) ownedLegacy(source string) bool {
	prefixes := []string{
		filepath.Join(m.Env.Root, "cfg") + string(os.PathSeparator), filepath.Join(m.Env.Root, "agents") + string(os.PathSeparator),
		filepath.Join(m.Env.Data, "repo", "cfg") + string(os.PathSeparator), filepath.Join(m.Env.Data, "repo", "config") + string(os.PathSeparator), filepath.Join(m.Env.Data, "repo", "agents") + string(os.PathSeparator),
	}
	if strings.Contains(source, string(os.PathSeparator)+"workspace"+string(os.PathSeparator)+"cfg"+string(os.PathSeparator)) {
		return true
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(source, prefix) {
			return true
		}
	}
	for _, layout := range []string{"cfg", "config", "agents"} {
		prefix := filepath.Join(m.Env.Data, "releases") + string(os.PathSeparator)
		if strings.HasPrefix(source, prefix) && strings.Contains(strings.TrimPrefix(source, prefix), string(os.PathSeparator)+layout+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func legacySuffix(source, data string) string {
	for _, marker := range []string{string(os.PathSeparator) + "cfg" + string(os.PathSeparator), string(os.PathSeparator) + "config" + string(os.PathSeparator)} {
		if index := strings.LastIndex(source, marker); index >= 0 {
			return source[index+len(marker):]
		}
	}
	if index := strings.LastIndex(source, string(os.PathSeparator)+"agents"+string(os.PathSeparator)); index >= 0 {
		return filepath.Join("agents", source[index+len("/agents/"):])
	}
	return strings.TrimPrefix(source, data)
}

func (m Manager) checkoutHasLinks(checkout string) bool {
	found := false
	for _, root := range m.legacyPaths() {
		m.walkLegacy(root, func(_ string, source string) {
			if strings.HasPrefix(source, checkout+string(os.PathSeparator)) {
				found = true
			}
		})
	}
	return found
}

func (m Manager) expandTarget(target string) (string, error) {
	if strings.HasPrefix(target, "~/") {
		target = filepath.Join(m.Env.Home, strings.TrimPrefix(target, "~/"))
	}
	if !filepath.IsAbs(target) || !within(m.Env.Home, target) {
		return "", errors.New("managed target escaped home")
	}
	return target, nil
}

func (m Manager) writeState(directory, state string) error {
	return atomicWrite(filepath.Join(directory, "state"), []byte(state+"\n"), 0o600)
}

func (m Manager) clearActive(id string) error {
	path := filepath.Join(m.Env.State, "recovery", "active")
	current, err := readLine(path)
	if errors.Is(err, os.ErrNotExist) || current != id {
		return nil
	}
	return os.Remove(path)
}

func (m Manager) log(level, message string) {
	if m.Log != nil {
		m.Log(level, message)
	}
}

func validRecovery(directory string) error {
	value, err := readLine(filepath.Join(directory, ".userland-recovery"))
	if err != nil || value != recoveryFormat {
		return errors.New("invalid recovery directory")
	}
	return nil
}

func readManifest(path string) ([]manifestEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var result []manifestEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 3 {
			return nil, errors.New("invalid recovery manifest")
		}
		index, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, err
		}
		result = append(result, manifestEntry{index, fields[1], fields[2]})
	}
	return result, scanner.Err()
}

func copyPath(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		value, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(value, target)
	}
	if info.IsDir() {
		if err := os.Mkdir(target, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return platform.CopyFile(source, target, info.Mode().Perm())
}

func resolvedLink(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target), nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func atomicWrite(path string, contents []byte, mode os.FileMode) error {
	temporary := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(temporary, contents, mode); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func readLine(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(contents), "\n")
	return strings.TrimSpace(line), nil
}
func readInteger(path string) (int64, error) {
	value, err := readLine(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(value, 10, 64)
}
func exists(path string) bool      { _, err := os.Lstat(path); return err == nil }
func isDirectory(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}
