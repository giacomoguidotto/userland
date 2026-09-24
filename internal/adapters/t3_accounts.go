package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func t3Accounts(c *Context, action Action) int {
	script := filepath.Join(c.Env.Root, "cfg", "t3-thread", "configure-accounts")
	if !executable(script) {
		return 0
	}
	args := []string{"--check"}
	if action == Apply {
		args = []string{}
	}
	result := platform.Run(c.Context, c.Env.List, nil, script, args...)
	if result.Code == 0 {
		if action == Apply {
			c.Log(Changed, "T3 provider accounts synchronized; authentication was not changed")
		} else {
			c.Log(Current, "T3 provider accounts are declared; authentication is separate")
		}
		return 0
	}
	if action == Plan {
		c.Log(Change, "T3 provider accounts will be synchronized after T3 initializes")
		return 0
	}
	if action == Doctor {
		c.Log(Attention, "T3 provider accounts need synchronization: "+lastOutputLine(string(result.Output)))
		return 2
	}
	c.Log(Attention, "T3 provider account synchronization failed: "+lastOutputLine(string(result.Output)))
	return 2
}

func declaredT3Accounts(root string) ([]string, error) {
	contents, err := os.ReadFile(filepath.Join(root, "cfg", "t3-thread", "profiles", "accounts.json"))
	if err != nil {
		return nil, err
	}
	var value struct {
		ProviderInstances map[string]json.RawMessage `json:"providerInstances"`
	}
	if err := json.Unmarshal(contents, &value); err != nil {
		return nil, err
	}
	accounts := make([]string, 0, len(value.ProviderInstances))
	for name := range value.ProviderInstances {
		accounts = append(accounts, name)
	}
	return accounts, nil
}

func lastOutputLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return "unknown error"
	}
	return strings.TrimSpace(lines[len(lines)-1])
}
