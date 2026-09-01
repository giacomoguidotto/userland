package adapters

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/platform"
	realmstate "github.com/giacomoguidotto/userland/internal/realm"
)

func personalAuthentication(c *Context, action Action) int {
	return authenticationScript(c, action, "personal", filepath.Join(c.Env.Root, "cfg", "auth-wizard"), c.Env.List)
}

func realmAuthentication(c *Context, action Action) int {
	scripts, err := realmstate.New(c.Env).AuthenticationScripts()
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	code := 0
	for _, script := range scripts {
		environ := c.Env.With(
			"USERLAND_REALM", script.Name,
			"USERLAND_REALM_ROOT", script.Mount,
			"USERLAND_REALM_CONFIG_ROOT", script.ConfigurationRoot,
		)
		if result := authenticationScript(c, action, script.Name+" realm", script.Path, environ); result != 0 {
			code = result
			if result == 1 {
				return result
			}
		}
	}
	return code
}

func authenticationScript(c *Context, action Action, label, script string, environ []string) int {
	if !executable(script) {
		return 0
	}
	check := platform.Run(c.Context, environ, nil, script, "--check")
	if check.Code == 0 {
		c.Log(Current, label+" authentication is ready")
		return 0
	}
	detail := strings.TrimSpace(string(check.Output))
	if detail == "" {
		detail = label + " authentication needs setup"
	}
	if action == Plan {
		c.Log(Manual, detail)
		return 0
	}
	if action == Doctor {
		c.Log(Attention, detail)
		return 2
	}
	if !c.Terminal {
		c.Log(Attention, label+" authentication needs an interactive terminal")
		return 2
	}
	result := platform.RunObserved(c.Context, environ, c.Stdin, c.Output, script)
	if result.Code != 0 {
		c.Log(Attention, fmt.Sprintf("%s authentication wizard exited with status %d", label, result.Code))
		return 1
	}
	check = platform.Run(c.Context, environ, nil, script, "--check")
	if check.Code != 0 {
		c.Log(Attention, label+" authentication is still incomplete")
		return 2
	}
	c.Log(Changed, label+" authentication completed")
	return 0
}
