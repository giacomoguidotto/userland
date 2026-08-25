package tui

import (
	"fmt"
	"strings"

	"github.com/giacomoguidotto/userland/internal/plan"
)

// Plan renders the public plan model using the same stable terminal contract as
// the legacy implementation.
func (r Renderer) Plan(value *plan.Plan, runLog string) {
	sections := value.Sections()
	summary := value.Summary()
	width := 24
	for _, section := range sections {
		for _, item := range section.Items {
			if len([]rune(r.redact(item.Target))) > width {
				width = len([]rune(r.redact(item.Target)))
			}
		}
	}

	if r.mode == ModeRich {
		rail := r.rail()
		fmt.Fprintf(r.out, "%s\n %s  Plan\n%s\n", rail, r.sectionSymbol(), rail)
		for index, section := range sections {
			if index > 0 {
				fmt.Fprintln(r.out, rail)
			}
			fmt.Fprintf(r.out, " ├─ %s\n", section.Title)
			if len(section.Items) == 0 {
				if section.Area == plan.AreaCleanup {
					fmt.Fprintf(r.out, "%s   No stale userland-owned items\n", rail)
				} else {
					fmt.Fprintf(r.out, "%s   No changes\n", rail)
				}
				continue
			}
			for _, item := range section.Items {
				target := r.redact(item.Target)
				detail := r.redact(item.Detail)
				fmt.Fprintf(r.out, "%s  %s  %s", rail, plan.Glyph(item), target)
				if detail != "" {
					fmt.Fprintf(r.out, "%*s%s", width-len([]rune(target))+2, "", detail)
				}
				fmt.Fprintln(r.out)
			}
		}
		fmt.Fprintf(r.out, "%s\n %s  %d automatic · %d attended · %d cleanup", rail, r.doneSymbol(), summary.Automatic, summary.Attended, summary.Cleanup)
		if summary.Blocked != 0 {
			fmt.Fprintf(r.out, " · %d blocked", summary.Blocked)
		}
		fmt.Fprintf(r.out, "\n%s\n%s  Details %s\n%s\n", rail, rail, r.redact(runLog), rail)
		return
	}

	for _, section := range sections {
		fmt.Fprintf(r.out, "== %s\n", section.Title)
		if len(section.Items) == 0 {
			if section.Area == plan.AreaCleanup {
				fmt.Fprintln(r.out, "[ok] No stale userland-owned items")
			} else {
				fmt.Fprintln(r.out, "[ok] No changes")
			}
			continue
		}
		for _, item := range section.Items {
			state := "change"
			if item.Handling == plan.Blocked {
				state = "error"
			} else if item.Handling == plan.Attended {
				state = "manual"
			}
			fmt.Fprintf(r.out, "[%s] %s", state, r.redact(item.Target))
			if item.Detail != "" {
				fmt.Fprintf(r.out, ": %s", r.redact(item.Detail))
			}
			fmt.Fprintln(r.out)
		}
	}
	fmt.Fprintf(r.out, "[info] %d automatic; %d attended; %d cleanup; %d blocked\n", summary.Automatic, summary.Attended, summary.Cleanup, summary.Blocked)
	fmt.Fprintf(r.out, "[info] Details: %s\n", r.redact(runLog))
}

func (r Renderer) redact(value string) string {
	home := r.env["USERLAND_HOME"]
	if home == "" {
		return value
	}
	return strings.ReplaceAll(value, home, "~")
}

func (r Renderer) rail() string {
	if r.unicode {
		return " │"
	}
	return " |"
}

func (r Renderer) sectionSymbol() string {
	if r.unicode {
		return "◆"
	}
	return "*"
}

func (r Renderer) doneSymbol() string {
	if r.unicode {
		return "◇"
	}
	return "o"
}
