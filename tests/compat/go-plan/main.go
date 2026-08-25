package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/tui"
	"github.com/giacomoguidotto/userland/plan"
)

func main() {
	environ := os.Environ()
	root := env(environ, "USERLAND_ROOT")
	file, err := os.Open(filepath.Join(root, "tests", "compat", "plan.tsv"))
	if err != nil {
		panic(err)
	}
	defer file.Close()

	value := plan.New()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 7 {
			panic("invalid plan fixture")
		}
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
