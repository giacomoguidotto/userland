package adapters

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	if len(options) == 0 || !manager.SelectionPending() {
		return 0
	}
	if action == Plan {
		for _, option := range options {
			c.Log(Manual, "choose whether to attach the "+option.Name+" realm at "+option.DefaultPath)
		}
		return 0
	}
	if action == Doctor {
		c.Log(Attention, "realm selection has not been recorded; run userland sync")
		return 2
	}
	if !c.Terminal {
		c.Log(Attention, "realm selection needs an interactive terminal")
		return 2
	}
	reader := bufio.NewReader(c.Stdin)
	var selected []string
	for _, option := range options {
		fmt.Fprintf(c.Output, "Attach the %s realm at %s? [y/N] ", option.Name, option.DefaultPath)
		reply, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			c.Log(Attention, readErr.Error())
			return 1
		}
		if strings.EqualFold(strings.TrimSpace(reply), "y") || strings.EqualFold(strings.TrimSpace(reply), "yes") {
			selected = append(selected, option.Name)
		}
	}
	if err := manager.BeginSelection(); err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	for _, name := range selected {
		result, addErr := manager.AddByName(c.Context, name)
		if addErr != nil {
			c.Log(Attention, addErr.Error())
			return 1
		}
		c.Log(Changed, result.Name+" realm attached")
	}
	if err := manager.RecordSelection(); err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	return 0
}

func realmsEnabled(env platform.Environment) bool {
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
