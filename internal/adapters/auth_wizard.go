package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/tui"
)

type authenticationStage struct {
	id, title, instruction string
	browserLogin           bool
}

var personalAuthStages = []authenticationStage{
	{id: "agent", title: "1Password SSH agent", instruction: "In 1Password, sign in and open Settings > Developer. Enable the SSH agent."},
	{id: "ssh", title: "Personal GitHub SSH key", instruction: "Your public key is copied to the clipboard. Add it as an Authentication Key on the GitHub page that opens."},
	{id: "github-cli", title: "GitHub CLI", instruction: "Complete the browser login using the device code below, then return here.", browserLogin: true},
	{id: "codex", title: "Codex CLI", instruction: "Complete the browser login, then return here. Credentials must be stored in the system keyring.", browserLogin: true},
}

func personalAuthWizard(c *Context, script string) int {
	w := tui.Wizard{Render: tui.New(c.Output, c.Env.List), Input: c.Stdin}
	total := len(personalAuthStages) + 1
	for index, stage := range personalAuthStages {
		if c.Context.Err() != nil {
			return 130
		}
		w.Stage(index+1, total, stage.title)
		if run(c, script, "--check-stage", stage.id).Code == 0 {
			w.Render.Status(tui.StatusOK, stage.title+" is ready")
			continue
		}
		w.Info(stage.instruction)
		// Browser authentication owns no terminal input. All prompts remain in
		// Userland; account credentials are entered in the browser or native app.
		output := &tui.CommandOutput{Wizard: w}
		result := runWithObserved(c, c.Env.List, nil, output, script, "--apply-stage", stage.id)
		output.Flush()
		if c.Context.Err() != nil {
			return 130
		}
		if result.Code != 0 {
			c.Log(Attention, fmt.Sprintf("%s setup failed (exit %d); rerun sync to retry", stage.title, result.Code))
			return 1
		}
		if !stage.browserLogin {
			if code := w.Continue("Complete this step, then continue"); code != 0 {
				return code
			}
		}
		if run(c, script, "--check-stage", stage.id).Code != 0 {
			c.Log(Attention, stage.title+" is still incomplete; rerun sync to retry")
			return 2
		}
		w.Render.Status(tui.StatusOK, stage.title+" is ready")
	}
	w.Stage(total, total, "Optional 1Password references")
	w.Info("These are op:// references, not token values. Existing references are kept unless you replace them.")
	wanted, code := w.ConfirmDone("Configure optional token references?")
	if code != 0 {
		return code
	}
	if wanted {
		for _, key := range []string{"GH_TOKEN", "OPENAI_API_KEY"} {
			value, code := w.InputLine(key + " reference (op://…; Enter keeps current)")
			if code != 0 {
				return code
			}
			if value == "" {
				continue
			}
			if err := saveSecretReference(c, key, value); err != nil {
				c.Log(Attention, err.Error())
				return 1
			}
			w.Render.Status(tui.StatusOK, "Saved "+key+" reference")
		}
	}
	c.Log(Changed, "personal authentication completed")
	return 0
}

func saveSecretReference(c *Context, key, value string) error {
	if (key != "GH_TOKEN" && key != "OPENAI_API_KEY") || !strings.HasPrefix(value, "op://") || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("%s must be an op:// reference; no value was saved", key)
	}
	config := c.Env.Get("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(c.Env.Home, ".config")
	}
	path := filepath.Join(config, "userland", "secret-refs.env")
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(string(old), "\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, key+"=") {
			lines = append(lines, line)
		}
	}
	lines = append(lines, key+"="+value)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".secret-refs-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
