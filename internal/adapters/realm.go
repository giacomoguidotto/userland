package adapters

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/giacomoguidotto/userland/internal/csvfile"
	"github.com/giacomoguidotto/userland/internal/platform"
	realmstate "github.com/giacomoguidotto/userland/internal/realm"
)

func realmSelection(c *Context, action Action) int {
	manager := realmstate.New(c.Env)
	options, err := manager.Options()
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	if len(options) == 0 {
		hasAttachments, attachmentErr := manager.HasAttachments()
		if attachmentErr != nil {
			c.Log(Attention, attachmentErr.Error())
			return 1
		}
		if !hasAttachments {
			return 0
		}
		if action == Plan {
			c.Log(Change, "disabled realm attachments will be removed")
			return 0
		}
		if action == Doctor {
			c.Log(Attention, "disabled realm attachments remain; run sync to remove them")
			return 2
		}
		if err := manager.Purge(c.Context); err != nil {
			c.Log(Attention, err.Error())
			return 1
		}
		c.Log(Changed, "removed disabled realm attachments and projections")
		return 0
	}
	if !manager.SelectionPending() {
		return 0
	}
	if action == Plan {
		c.Log(Current, "optional realms are disabled by default; use userland realm add to attach one")
		return 0
	}
	if action == Doctor {
		return 0
	}
	if err := manager.BeginSelection(); err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	if err := manager.RecordSelection(); err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	c.Log(Current, "optional realms disabled by default; use userland realm add to attach one")
	return 0
}

func realmsEnabled(env platform.Environment) bool {
	options, optionsErr := realmstate.New(env).Options()
	if optionsErr != nil || len(options) == 0 {
		return false
	}
	rows, err := csvfile.Read(filepath.Join(env.State, "realms.csv"), []string{"name", "path"})
	return err == nil && len(rows) != 0 || err != nil && !errors.Is(err, os.ErrNotExist)
}

func realms(c *Context, action Action) int {
	manager := realmstate.New(c.Env)
	var (
		findings []realmstate.Finding
		err      error
	)
	if action == Apply {
		findings, err = manager.Reconcile(c.Context)
	} else {
		findings, err = manager.Inspect(c.Context)
	}
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	code := 0
	for _, finding := range findings {
		switch finding.State {
		case realmstate.Current:
			c.Log(Current, finding.Message)
		case realmstate.Change:
			if action == Doctor {
				c.Log(Attention, finding.Message)
				code = 2
			} else if action == Apply {
				c.Log(Changed, finding.Message)
			} else {
				c.Log(Change, finding.Message)
			}
		case realmstate.Attention:
			c.Log(Attention, finding.Message)
			code = 2
		}
	}
	return code
}
