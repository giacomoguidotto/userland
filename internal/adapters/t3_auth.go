package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/giacomoguidotto/userland/internal/platform"
	"github.com/giacomoguidotto/userland/internal/tui"
)

type t3AuthAccount struct {
	ID          string
	Driver      string `json:"driver"`
	DisplayName string `json:"displayName"`
	Enabled     bool   `json:"enabled"`
	Config      struct {
		BinaryPath     string `json:"binaryPath"`
		HomePath       string `json:"homePath"`
		ShadowHomePath string `json:"shadowHomePath"`
	} `json:"config"`
	home string
}

func t3AuthAccounts(c *Context) ([]t3AuthAccount, error) {
	data, err := os.ReadFile(filepath.Join(c.Env.Root, "cfg/t3-thread/profiles/accounts.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var declared struct {
		ProviderInstances map[string]t3AuthAccount `json:"providerInstances"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return nil, err
	}
	var accounts []t3AuthAccount
	seen := map[string]bool{}
	for id, a := range declared.ProviderInstances {
		if !a.Enabled {
			continue
		}
		a.ID = id
		path := a.Config.HomePath
		if a.Driver == "codex" {
			path = a.Config.ShadowHomePath
		}
		if a.Driver != "codex" && a.Driver != "claudeAgent" {
			return nil, fmt.Errorf("unsupported authentication driver for %s", id)
		}
		// A missing or shared home must never silently log in to a different profile.
		if !strings.HasPrefix(path, "~/") {
			return nil, fmt.Errorf("%s needs an account-specific home under ~/", id)
		}
		a.home = filepath.Join(c.Env.Home, strings.TrimPrefix(path, "~/"))
		relative, err := filepath.Rel(c.Env.Home, a.home)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || seen[a.home] {
			return nil, fmt.Errorf("invalid or shared authentication home for %s", id)
		}
		seen[a.home] = true
		if a.Config.BinaryPath == "" {
			return nil, fmt.Errorf("missing CLI for %s", id)
		}
		accounts = append(accounts, a)
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Driver != accounts[j].Driver {
			return accounts[i].Driver == "codex"
		}
		order := map[string]int{"alpha": 0, "beta": 1, "gamma": 2, "delta": 3}
		if order[accounts[i].DisplayName] != order[accounts[j].DisplayName] {
			return order[accounts[i].DisplayName] < order[accounts[j].DisplayName]
		}
		return accounts[i].ID < accounts[j].ID
	})
	return accounts, nil
}

func (a t3AuthAccount) label() string {
	provider := "Codex"
	if a.Driver == "claudeAgent" {
		provider = "Claude"
	}
	return provider + " " + a.DisplayName
}

func (a t3AuthAccount) environ(c *Context) []string {
	var clean []string
	for _, entry := range c.Env.List {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "CODEX_HOME", "CLAUDE_CONFIG_DIR", "OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY":
			continue
		}
		clean = append(clean, entry)
	}
	if a.Driver == "codex" {
		return append(clean, "CODEX_HOME="+a.home)
	}
	return append(clean, "CLAUDE_CONFIG_DIR="+a.home)
}

func (a t3AuthAccount) check(c *Context) bool {
	if info, err := os.Stat(a.home); err != nil || !info.IsDir() {
		return false
	}
	args := []string{"auth", "status", "--json"}
	if a.Driver == "codex" {
		args = []string{"-c", `cli_auth_credentials_store="keyring"`, "login", "status"}
	}
	ctx, cancel := context.WithTimeout(c.Context, 15*time.Second)
	defer cancel()
	result := limitedRun(c, func() platform.Result { return platform.Run(ctx, a.environ(c), nil, a.Config.BinaryPath, args...) })
	if result.Code != 0 {
		return false
	}
	if a.Driver == "codex" {
		return result.Code == 0
	}
	return result.Code == 0
}

func (a t3AuthAccount) prepare(c *Context) error {
	if err := os.MkdirAll(a.home, 0700); err != nil {
		return err
	}
	if a.Driver != "codex" {
		return nil
	}
	// T3 shares config with the main Codex home but keeps auth in the shadow
	// home. Use exactly that layout, without copying or reading credentials.
	shared := filepath.Join(c.Env.Home, strings.TrimPrefix(a.Config.HomePath, "~/"), "config.toml")
	if !strings.HasPrefix(a.Config.HomePath, "~/") {
		return fmt.Errorf("invalid shared Codex home")
	}
	link := filepath.Join(a.home, "config.toml")
	if _, err := os.Lstat(link); os.IsNotExist(err) {
		return os.Symlink(shared, link)
	} else if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(link)
	desired, wantedErr := filepath.EvalSymlinks(shared)
	if err != nil || wantedErr != nil || actual != desired {
		return fmt.Errorf("%s has a different config.toml; preserved it", a.label())
	}
	return nil
}

func t3Authentication(c *Context, action Action) int {
	accounts, err := t3AuthAccounts(c)
	if err != nil {
		c.Log(Attention, "Cannot load T3 authentication profiles: "+err.Error())
		return 2
	}
	w := tui.Wizard{Render: tui.New(c.Output, c.Env.List), Input: c.Stdin}
	incomplete := false
	for index, a := range accounts {
		if c.Context.Err() != nil {
			return 130
		}
		if a.check(c) {
			c.Log(Current, a.label()+" is authenticated")
			continue
		}
		if c.Context.Err() != nil {
			return 130
		}
		if action == Plan {
			c.Log(Manual, a.label()+" needs account login")
			continue
		}
		if action == Doctor || !c.Terminal {
			c.Log(Attention, a.label()+" needs login; run userland sync in a terminal")
			incomplete = true
			continue
		}
		w.Stage(index+1, len(accounts), "T3 · "+a.label())
		if _, ok := platform.LookPath(a.environ(c), a.Config.BinaryPath); !ok {
			c.Log(Attention, a.label()+" CLI is unavailable: "+a.Config.BinaryPath)
			incomplete = true
			continue
		}
		w.Info("Use the browser account intended for " + a.label() + ". Switch accounts in the browser if needed. This login is saved only for this T3 profile.")
		yes, code := w.ConfirmDoneYes("Sign in to " + a.label() + " now?")
		if code != 0 {
			return code
		}
		if !yes {
			c.Log(Attention, a.label()+" login skipped")
			incomplete = true
			continue
		}
		if err := a.prepare(c); err != nil {
			c.Log(Attention, err.Error())
			incomplete = true
			continue
		}
		args := []string{"auth", "login", "--claudeai"}
		if a.Driver == "codex" {
			args = []string{"-c", `cli_auth_credentials_store="keyring"`, "login"}
		}
		output := &tui.CommandOutput{Wizard: w}
		result := runWithObserved(c, a.environ(c), c.Stdin, output, a.Config.BinaryPath, args...)
		output.Flush()
		if c.Context.Err() != nil || result.Code == 130 {
			return 130
		}
		// No receipt or successful command alone can stand in for a profile check.
		if result.Code != 0 || !a.check(c) {
			c.Log(Attention, fmt.Sprintf("%s login was not verified (exit %d); rerun sync to retry this profile", a.label(), result.Code))
			incomplete = true
			continue
		}
		w.Render.Status(tui.StatusOK, a.label()+" is authenticated")
		c.Log(Changed, a.label()+" authentication verified")
	}
	if incomplete {
		return 2
	}
	return 0
}
