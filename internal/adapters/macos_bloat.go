package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giacomoguidotto/userland/internal/plan"
)

// macosBloat removes only the explicitly declared optional application bundles
// in /Applications. It never scans the filesystem and never touches the
// protected /System/Applications tree.
func macosBloat(c *Context, action Action) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	rows, err := readCSV(filepath.Join(c.Env.Root, "cfg", "macos-bloat.csv"), "name", "application_path")
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	var present [][2]string
	for _, row := range rows {
		path := row[1]
		if err := validateBloatPath(path); err != nil {
			c.Log(Attention, row[0]+": "+err.Error())
			return 1
		}
		info, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			c.Log(Attention, row[0]+": "+statErr.Error())
			return 1
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			c.Log(Attention, row[0]+": refusing to remove a non-directory application path")
			return 1
		}
		present = append(present, [2]string{row[0], path})
	}
	if action == Plan {
		for _, item := range present {
			addPlan(c.Plan, plan.Item{
				Area: plan.AreaCleanup, Action: "remove", Handling: plan.Automatic,
				Ownership: "declared", Target: item[1],
				Detail: "remove declared optional macOS application " + item[0],
				Proof:  "macos-bloat:" + item[0],
			})
		}
		return 0
	}
	if action == Doctor {
		for _, item := range present {
			c.Log(Attention, item[0]+" is declared for removal")
		}
		if len(present) != 0 {
			return 2
		}
		c.Log(Healthy, "declared optional macOS applications are absent")
		return 0
	}
	for _, item := range present {
		if err := os.RemoveAll(item[1]); err != nil {
			c.Log(Attention, fmt.Sprintf("could not remove %s: %v", item[0], err))
			return 1
		}
		c.Log(Changed, "removed "+item[0])
	}
	return 0
}

func validateBloatPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Dir(path) != "/Applications" || !strings.HasSuffix(path, ".app") {
		return fmt.Errorf("refusing application path outside /Applications: %s", path)
	}
	return nil
}
