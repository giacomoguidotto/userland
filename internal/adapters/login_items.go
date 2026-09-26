package adapters

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	realmstate "github.com/giacomoguidotto/userland/internal/realm"
)

var inspectLoginItemScript = `on run argv
set wantedName to item 1 of argv
tell application "System Events"
  if exists login item wantedName then
    return "present"
  end if
end tell
return "missing"
end run`

var applyLoginItemScript = `on run argv
set wantedName to item 1 of argv
set wantedPath to item 2 of argv
set wantedHidden to (item 3 of argv is "true")
set wantedAlias to POSIX file wantedPath as alias
tell application "System Events"
  if not (exists login item wantedName) then
    make login item at end with properties {name:wantedName, path:wantedAlias, hidden:wantedHidden}
  end if
end tell
end run`

func loginItems(c *Context, action Action) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	rows, err := readCSV(filepath.Join(c.Env.Root, "cfg", "login-items.csv"), "name", "path", "hidden")
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	configurations, err := realmstate.New(c.Env).Configurations()
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	for _, configuration := range configurations {
		realmRows, readErr := readCSV(filepath.Join(configuration.Root, ".userland", "login-items.csv"), "name", "path", "hidden")
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			c.Log(Attention, configuration.Name+" realm login items: "+readErr.Error())
			return 1
		}
		rows = append(rows, realmRows...)
	}
	osascript := c.Env.Get("USERLAND_OSASCRIPT")
	if osascript == "" {
		osascript = "/usr/bin/osascript"
	}
	code := 0
	for _, row := range rows {
		name, path, hidden := row[0], row[1], row[2]
		if name == "" || !filepath.IsAbs(path) || hidden != "true" && hidden != "false" {
			c.Log(Attention, "invalid login item declaration for "+name)
			return 1
		}
		if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
			c.Log(Current, name+" login item skipped because its app is not installed")
			continue
		} else if statErr != nil {
			c.Log(Attention, "could not inspect "+name+" application")
			return 1
		}
		inspect := runWith(c, c.Env.List, nil, osascript, "-e", inspectLoginItemScript, "--", name)
		if inspect.Code == 0 && strings.TrimSpace(string(inspect.Output)) == "present" {
			c.Log(Current, name+" login item matches")
			if startsImmediately(name) && action == Apply {
				if code := startLoginApplication(c, name); code != 0 {
					return code
				}
			}
			continue
		}
		if action == Plan {
			c.Log(Change, name+" login item needs configuration")
			continue
		}
		if action == Doctor {
			c.Log(Attention, name+" login item does not match")
			code = 2
			continue
		}
		apply := runWith(c, c.Env.List, nil, osascript, "-e", applyLoginItemScript, "--", name, path, hidden)
		if apply.Code != 0 {
			c.Log(Attention, fmt.Sprintf("could not configure %s login item: %s", name, strings.TrimSpace(string(apply.Output))))
			return 1
		}
		c.Log(Changed, name+" login item configured")
		if startsImmediately(name) {
			if code := startLoginApplication(c, name); code != 0 {
				return code
			}
		}
	}
	return code
}

func startsImmediately(name string) bool {
	// Capture apps must be started by macOS at login. Opening them during sync
	// restores their last window and can put a screenshot or recording panel on
	// screen. Wispr Flow has no capture window, so it remains available now.
	return name == "Wispr Flow"
}

func startLoginApplication(c *Context, name string) int {
	launched := run(c, "open", "-gj", "-a", name)
	if launched.Code != 0 {
		c.Log(Attention, "could not start "+name+": "+strings.TrimSpace(string(launched.Output)))
		return 2
	}
	c.Log(Changed, "started "+name)
	return 0
}
