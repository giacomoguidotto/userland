package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/csvfile"
	"github.com/giacomoguidotto/userland/internal/plan"
	"github.com/giacomoguidotto/userland/internal/tui"
)

func main() {
	environ := os.Environ()
	root := env(environ, "USERLAND_ROOT")
	rows, err := csvfile.Read(filepath.Join(root, "tests", "compat", "plan.csv"), []string{"area", "action", "handling", "ownership", "target", "detail", "proof"})
	if err != nil {
		panic(err)
	}

	value := plan.New()
	for _, fields := range rows {
		if err := value.Add(plan.Item{
			Area: plan.Area(fields[0]), Action: plan.Action(fields[1]),
			Handling: plan.Handling(fields[2]), Ownership: plan.Ownership(fields[3]),
			Target: fields[4], Detail: fields[5], Proof: fields[6],
		}); err != nil {
			panic(err)
		}
	}

	tui.New(os.Stdout, environ).Plan(value, env(environ, "USERLAND_STATE_DIR")+"/last-run.log")
}

func env(environ []string, key string) string {
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}
