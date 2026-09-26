package adapters

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/tui"
)

const heliumReadyReceipt = "helium-ready"

// browserReady is the one attended browser gate. It runs before any login or
// extension step, and its receipt means only that the user confirmed Helium is
// open, signed in, and ready for the following steps.
func browserReady(c *Context, action Action) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	if !applicationInstalled(c, "Helium.app") {
		if action == Plan {
			c.Log(Manual, "Helium must be installed before browser setup")
		} else {
			c.Log(Attention, "Helium is not installed; install it before browser logins")
		}
		return 2
	}
	receipt := filepath.Join(c.Env.State, "receipts", heliumReadyReceipt)
	if receiptReady(receipt) {
		level := Current
		if action == Doctor {
			level = Healthy
		}
		c.Log(level, "Helium readiness was confirmed")
		return 0
	}
	if action == Plan {
		c.Log(Manual, "Helium will be opened for attended account setup")
		return 0
	}
	if action == Doctor || !c.Terminal {
		c.Log(Attention, "open Helium, sign in, then run userland sync in a terminal")
		return 2
	}
	wizard := tui.Wizard{Render: tui.New(c.Output, c.Env.List), Input: c.Stdin}
	wizard.Render.Section("Browser readiness")
	wizard.Info("Userland will use Helium for browser logins. Sign in to the required accounts, unlock the 1Password extension, and leave Helium ready before continuing.")
	opened := run(c, "open", "-a", "Helium")
	if opened.Code != 0 {
		c.Log(Attention, "could not open Helium: "+strings.TrimSpace(string(opened.Output)))
		return 2
	}
	if code := wizard.Continue("When Helium is ready, continue"); code != 0 {
		return code
	}
	if err := os.MkdirAll(filepath.Dir(receipt), 0o700); err != nil {
		return 1
	}
	if err := os.WriteFile(receipt, []byte("confirmed\n"), 0o600); err != nil {
		return 1
	}
	c.Log(Changed, "Helium readiness confirmed")
	return 0
}

func receiptReady(path string) bool {
	contents, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(contents)) == "confirmed"
}
