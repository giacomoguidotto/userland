package adapters

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/giacomoguidotto/userland/internal/plan"
)

type powerSetting struct {
	profile string
	key     string
	want    int
	label   string
	detail  string
}

var desiredPowerSettings = []powerSetting{
	{profile: "battery power", key: "displaysleep", want: 20, label: "Battery display sleep", detail: "20 minutes"},
	{profile: "ac power", key: "displaysleep", want: 0, label: "AC display sleep", detail: "never"},
	{profile: "battery power", key: "sleep", want: 0, label: "Battery system sleep", detail: "disabled"},
	{profile: "ac power", key: "sleep", want: 0, label: "AC system sleep", detail: "disabled"},
}

func powerManagement(c *Context, action Action) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	pmset := c.Env.Get("USERLAND_PMSET")
	if pmset == "" {
		pmset = "/usr/bin/pmset"
	}
	current := run(c, pmset, "-g", "custom")
	if current.Code != 0 {
		c.Log(Attention, "could not inspect power settings: "+firstLine(current.Output))
		return 1
	}
	values, err := parsePMSetCustom(current.Output)
	if err != nil {
		c.Log(Attention, "could not parse power settings: "+err.Error())
		return 1
	}
	var pending []powerSetting
	for _, setting := range desiredPowerSettings {
		if values[setting.profile][setting.key] != setting.want {
			pending = append(pending, setting)
		}
	}
	if action == Plan {
		for _, setting := range pending {
			_ = c.Plan.Add(plan.Item{
				Area: plan.AreaOS, Action: "set", Handling: plan.Automatic, Ownership: "declared",
				Target: setting.label, Detail: setting.detail, Proof: "power-management:" + setting.profile + ":" + setting.key,
			})
		}
		return 0
	}
	if action == Doctor {
		if len(pending) == 0 {
			c.Log(Healthy, "display and system sleep settings match the declaration")
			return 0
		}
		for _, setting := range pending {
			c.Log(Attention, setting.label+" needs configuration")
		}
		return 2
	}
	if len(pending) == 0 {
		return 0
	}
	for _, profile := range []string{"-b", "-c"} {
		args := []string{profile}
		for _, setting := range pending {
			if (profile == "-b" && setting.profile != "battery power") || (profile == "-c" && setting.profile != "ac power") {
				continue
			}
			args = append(args, setting.key, strconv.Itoa(setting.want))
		}
		if len(args) == 1 {
			continue
		}
		command := append([]string{pmset}, args...)
		result := runPrivileged(c, command...)
		if result.Code != 0 {
			c.Log(Attention, fmt.Sprintf("could not apply %s power settings: %s", strings.TrimPrefix(profile, "-"), firstLine(result.Output)))
			return 1
		}
	}
	for _, setting := range pending {
		c.Log(Changed, setting.label+" set to "+setting.detail)
	}
	return 0
}

func parsePMSetCustom(output []byte) (map[string]map[string]int, error) {
	profiles := make(map[string]map[string]int)
	var profile string
	for _, raw := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, ":") {
			profile = strings.ToLower(strings.TrimSuffix(line, ":"))
			profiles[profile] = make(map[string]int)
			continue
		}
		fields := strings.Fields(line)
		if profile == "" || len(fields) < 2 {
			continue
		}
		value, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		profiles[profile][strings.ToLower(fields[0])] = value
	}
	for _, setting := range desiredPowerSettings {
		if _, ok := profiles[setting.profile][setting.key]; !ok {
			return nil, fmt.Errorf("missing %s %s", setting.profile, setting.key)
		}
	}
	return profiles, nil
}
