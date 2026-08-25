// Package plan models the deterministic set of proposed Userland changes.
package plan

import (
	"errors"
	"strings"
)

type Area string
type Action string
type Handling string
type Ownership string

const (
	AreaOS      Area = "os"
	AreaFS      Area = "fs"
	AreaApps    Area = "apps"
	AreaCleanup Area = "cleanup"

	Automatic Handling = "automatic"
	Attended  Handling = "attended"
	Blocked   Handling = "blocked"
)

var areaOrder = []Area{AreaOS, AreaFS, AreaApps, AreaCleanup}

type Item struct {
	Area      Area
	Action    Action
	Handling  Handling
	Ownership Ownership
	Target    string
	Detail    string
	Proof     string
}

type Summary struct {
	Automatic int
	Attended  int
	Blocked   int
	Cleanup   int
}

// Plan owns validation, de-duplication, and stable presentation order.
type Plan struct {
	items []Item
	seen  map[string]struct{}
}

func New() *Plan {
	return &Plan{seen: make(map[string]struct{})}
}

func (p *Plan) Add(item Item) error {
	if !validArea(item.Area) || !validAction(item.Action) || !validHandling(item.Handling) || !validOwnership(item.Ownership) {
		return errors.New("invalid plan item")
	}
	if !validText(item.Target, true) || !validText(item.Detail, false) || !validText(item.Proof, false) {
		return errors.New("plan text contains a tab or newline")
	}
	if item.Area == AreaCleanup && item.Ownership != "declared" && item.Ownership != "userland" {
		return errors.New("cleanup must be owned by Userland")
	}
	if item.Area == AreaCleanup && item.Proof == "" {
		return errors.New("cleanup requires proof")
	}

	key := string(item.Area) + "\x00" + string(item.Action) + "\x00" + item.Target
	if _, exists := p.seen[key]; exists {
		return nil
	}
	p.seen[key] = struct{}{}
	p.items = append(p.items, item)
	return nil
}

func (p *Plan) Sections() []Section {
	sections := make([]Section, 0, len(areaOrder))
	for _, area := range areaOrder {
		section := Section{Area: area, Title: areaTitle(area)}
		for _, risky := range []bool{true, false} {
			for _, item := range p.items {
				if item.Area != area {
					continue
				}
				itemRisky := item.Handling != Automatic || item.Area == AreaCleanup
				if itemRisky == risky {
					section.Items = append(section.Items, item)
				}
			}
		}
		sections = append(sections, section)
	}
	return sections
}

type Section struct {
	Area  Area
	Title string
	Items []Item
}

func (p *Plan) Summary() Summary {
	var summary Summary
	for _, item := range p.items {
		switch item.Handling {
		case Automatic:
			if item.Area != AreaCleanup {
				summary.Automatic++
			}
		case Attended:
			summary.Attended++
		case Blocked:
			summary.Blocked++
		}
		if item.Area == AreaCleanup {
			summary.Cleanup++
		}
	}
	return summary
}

// Items returns an immutable snapshot in approval order.
func (p *Plan) Items() []Item {
	return append([]Item(nil), p.items...)
}

func Glyph(item Item) string {
	if item.Handling != Automatic {
		return "!"
	}
	switch item.Action {
	case "remove", "release":
		return "-"
	case "create", "link", "clone", "install":
		return "+"
	default:
		return "~"
	}
}

func areaTitle(area Area) string {
	switch area {
	case AreaOS:
		return "OS changes"
	case AreaFS:
		return "Filesystem changes"
	case AreaApps:
		return "Application additions"
	default:
		return "Cleanup"
	}
}

func validText(value string, required bool) bool {
	return (!required || value != "") && !strings.ContainsAny(value, "\t\r\n")
}

func validArea(value Area) bool {
	return value == AreaOS || value == AreaFS || value == AreaApps || value == AreaCleanup
}

func validAction(value Action) bool {
	switch value {
	case "set", "create", "link", "clone", "update", "install", "upgrade", "configure", "remove", "release", "review":
		return true
	default:
		return false
	}
}

func validHandling(value Handling) bool {
	return value == Automatic || value == Attended || value == Blocked
}

func validOwnership(value Ownership) bool {
	switch value {
	case "declared", "userland", "external", "unmanaged":
		return true
	default:
		return false
	}
}
