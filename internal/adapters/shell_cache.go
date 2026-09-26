package adapters

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/giacomoguidotto/userland/internal/platform"
)

var shellEnvironmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type miseShellEnvironment struct {
	BinPaths []string
	Values   map[string]string
}

func shellCache(c *Context, action Action) int {
	environmentPath := filepath.Join(c.Env.Cache, "zsh", "mise-env.zsh")
	if !exists(environmentPath) {
		if action == Plan {
			c.Log(Change, "static Zsh environment cache will be generated")
			return 0
		}
		if action == Doctor {
			c.Log(Attention, "static Zsh environment cache is missing or stale")
			return 2
		}
	}
	environment, err := globalMiseEnvironment(c)
	if err != nil {
		// Planning runs before Toolchain health installs newly pinned tools. A
		// missing tool therefore makes the cache look unavailable even though
		// the apply phase can generate it after installation.
		if action == Plan {
			c.Log(Change, "static Zsh environment cache will be generated after pinned tools are installed")
			return 0
		}
		c.Log(Attention, "static global tool environment could not be generated: "+err.Error())
		if action == Doctor {
			return 2
		}
		return 1
	}
	fingerprint := shellFingerprint(c, environment)
	current := shellCacheCurrent(environmentPath, fingerprint)
	if action == Plan {
		if current {
			c.Log(Current, "static Zsh environment cache is current")
		} else {
			c.Log(Change, "static Zsh environment cache will be generated")
		}
		return 0
	}
	if action == Doctor {
		if current {
			c.Log(Healthy, "static Zsh environment cache is current")
			return 0
		}
		c.Log(Attention, "static Zsh environment cache is missing or stale")
		return 2
	}
	if err := os.MkdirAll(filepath.Dir(environmentPath), 0o755); err != nil {
		return 1
	}
	environmentContents := miseEnvironmentContents(fingerprint, environment)
	if err := writeShellCache(environmentPath, environmentContents); err != nil {
		return 1
	}
	c.Log(Changed, "regenerated the static Zsh environment cache")
	return 0
}

func shellFingerprint(c *Context, environment miseShellEnvironment) string {
	hash := sha256.New()
	hash.Write([]byte("userland-shell-environment-v7\n"))
	for _, path := range environment.BinPaths {
		fmt.Fprintf(hash, "path\t%s\n", path)
	}
	keys := make([]string, 0, len(environment.Values))
	for key := range environment.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(hash, "env\t%s\t%s\n", key, environment.Values[key])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func shellToolPath(c *Context, environment miseShellEnvironment, name string) (string, bool) {
	paths := append([]string(nil), environment.BinPaths...)
	for _, entry := range c.Env.List {
		if strings.HasPrefix(entry, "PATH=") {
			paths = append(paths, strings.TrimPrefix(entry, "PATH="))
			break
		}
	}
	return platform.LookPath(c.Env.With("PATH", strings.Join(paths, string(os.PathListSeparator))), name)
}

func shellCacheCurrent(path, fingerprint string) bool {
	contents, err := os.ReadFile(path)
	return err == nil && firstLine(contents) == "# userland-shell-cache: "+fingerprint
}

func writeShellCache(path string, contents []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, contents, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func globalMiseEnvironment(c *Context) (miseShellEnvironment, error) {
	binPaths, err := miseBinPaths(c)
	if err != nil {
		return miseShellEnvironment{}, err
	}
	result := runMise(c, "env", "--json")
	if result.Code != 0 {
		return miseShellEnvironment{}, fmt.Errorf("mise env failed: %s", strings.TrimSpace(string(result.Output)))
	}
	values := make(map[string]string)
	if err := json.Unmarshal(result.Output, &values); err != nil {
		return miseShellEnvironment{}, fmt.Errorf("mise env returned invalid JSON: %w", err)
	}
	delete(values, "PATH")
	for key := range values {
		if !shellEnvironmentName.MatchString(key) {
			return miseShellEnvironment{}, fmt.Errorf("mise env returned invalid variable name %q", key)
		}
	}
	return miseShellEnvironment{BinPaths: binPaths, Values: values}, nil
}

func miseBinPaths(c *Context) ([]string, error) {
	result := runMise(c, "bin-paths")
	if result.Code != 0 {
		return nil, fmt.Errorf("mise bin-paths failed: %s", strings.TrimSpace(string(result.Output)))
	}
	seen := make(map[string]bool)
	var paths []string
	for _, line := range strings.Split(string(result.Output), "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("mise bin-paths returned non-absolute path %q", path)
		}
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func miseEnvironmentContents(fingerprint string, environment miseShellEnvironment) []byte {
	var output bytes.Buffer
	fmt.Fprintf(&output, "# userland-shell-cache: %s\n", fingerprint)
	output.WriteString("# Generated by userland sync. Do not edit.\n")
	keys := make([]string, 0, len(environment.Values))
	for key := range environment.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&output, "export %s=%s\n", key, zshQuote(environment.Values[key]))
	}
	output.WriteString("typeset -gU path PATH\npath=(\n")
	for _, path := range environment.BinPaths {
		fmt.Fprintf(&output, "  %s\n", zshQuote(path))
	}
	output.WriteString("  $path\n)\nexport PATH\n")
	return output.Bytes()
}

func zshQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func platformCommand(c *Context, name string) (string, bool) {
	path, ok := commandPath(c, "", name)
	return strings.TrimSpace(path), ok
}
