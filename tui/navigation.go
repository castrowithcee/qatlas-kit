package tui

import (
	"errors"
	"fmt"
)

// FocusZone is a part of a screen that holds the focus.
type FocusZone string

// The focus zones in their fixed order: tab and shift+tab walk through the zones a screen has in this
// order, with wrap-around.
const (
	// ZoneTabs is the tab line, or the choice field of NavHeaderChoice in the header.
	ZoneTabs    FocusZone = "tabs"
	ZoneSubtabs FocusZone = "subtabs"
	// ZonePath is the path line, which a screen reports instead of ZoneTabs and ZoneSubtabs.
	ZonePath    FocusZone = "path"
	ZoneRows    FocusZone = "rows"
	ZoneForm    FocusZone = "form"
	ZoneDetail  FocusZone = "detail"
	ZoneFilters FocusZone = "filters"
)

// ZoneOutside is what a click outside the frame of an open modal reports, for a modal whose spec turns on
// CloseOutside. It is no focus zone: it stands in no screen and not in the zone order.
const ZoneOutside FocusZone = "outside"

var zoneOrder = [...]FocusZone{ZoneTabs, ZoneSubtabs, ZonePath, ZoneRows, ZoneForm, ZoneDetail, ZoneFilters}

// workZones are the zones of the work area, below the navigation lines.
var workZones = zoneOrder[3:]

// CycleZone returns the zone step places after current among the zones a screen has, in the fixed order
// and with wrap-around; a negative step goes back. A current zone the screen does not have leads to its
// first zone. Zones outside the fixed order are ignored; without any zone the result is current.
func CycleZone(zones []FocusZone, current FocusZone, step int) FocusZone {
	var present []FocusZone
	for _, z := range zoneOrder {
		for _, have := range zones {
			if have == z {
				present = append(present, z)
				break
			}
		}
	}
	if len(present) == 0 {
		return current
	}
	for i, z := range present {
		if z == current {
			return present[wrap(i+step, len(present))]
		}
	}
	return present[0]
}

// StepZoneLevel moves vertically between the levels tabs, subtabs, path line and work area among the zones a
// screen has: a positive step goes down, a negative one up, by that many levels and without wrap-around. A
// level the screen lacks is skipped. Going down into the work area lands on its first zone in the fixed
// order; a current work zone counts as that level and stays put when going down. A current zone the screen
// does not have leads to the top level. Without any level the result is current.
func StepZoneLevel(zones []FocusZone, current FocusZone, step int) FocusZone {
	has := func(z FocusZone) bool {
		for _, have := range zones {
			if have == z {
				return true
			}
		}
		return false
	}
	var levels []FocusZone // the zone that enters each present level
	cur := -1
	if has(ZoneTabs) {
		levels = append(levels, ZoneTabs)
	}
	if has(ZoneSubtabs) {
		levels = append(levels, ZoneSubtabs)
	}
	if has(ZonePath) {
		levels = append(levels, ZonePath)
	}
	work := false
	for _, z := range workZones {
		if has(z) {
			levels = append(levels, z)
			work = true
			break
		}
	}
	if len(levels) == 0 {
		return current
	}
	for i, z := range levels {
		if z == current || (work && i == len(levels)-1 && isWorkZone(current) && has(current)) {
			cur = i
		}
	}
	if cur < 0 {
		return levels[0]
	}
	next := min(max(cur+step, 0), len(levels)-1)
	if next == cur {
		return current
	}
	return levels[next]
}

func isWorkZone(z FocusZone) bool {
	for _, w := range workZones {
		if w == z {
			return true
		}
	}
	return false
}

// StepArea returns the area step places after active, with wrap-around; a negative step goes back. An
// active area that is not one of areas leads to the first; without areas the result is active.
func StepArea(areas []Area, active AreaID, step int) AreaID {
	if len(areas) == 0 {
		return active
	}
	for i, a := range areas {
		if a.ID == active {
			return areas[wrap(i+step, len(areas))].ID
		}
	}
	return areas[0].ID
}

// StepTab is StepArea for the sub-tabs of area.
func StepTab(area Area, active TabID, step int) TabID {
	if len(area.Tabs) == 0 {
		return active
	}
	for i, t := range area.Tabs {
		if t.ID == active {
			return area.Tabs[wrap(i+step, len(area.Tabs))].ID
		}
	}
	return area.Tabs[0].ID
}

func wrap(i, n int) int { return ((i % n) + n) % n }

// maxAreas is the number of areas a digit can name on the tab line.
const maxAreas = 9

// checkAreaIcons reports the icons of areas that fail CheckIcon.
func checkAreaIcons(areas []Area) []error {
	var errs []error
	for i, a := range areas {
		if a.Icon == "" {
			continue
		}
		if err := CheckIcon(a.Icon); err != nil {
			errs = append(errs, fmt.Errorf("area %d: %w", i+1, err))
		}
	}
	return errs
}

// CheckAreas reports every area that the tab line cannot draw as one digit and a name: none at all, more
// than nine, an empty or repeated area ID, an empty title, an icon that fails CheckIcon, and a sub-tab with an
// empty or repeated ID or an empty title. Applications run it in their tests or at start.
func CheckAreas(areas []Area) error {
	var errs []error
	if len(areas) == 0 {
		errs = append(errs, errors.New("the tab line holds no area"))
	}
	if len(areas) > maxAreas {
		errs = append(errs, fmt.Errorf("%d areas; a digit names at most %d", len(areas), maxAreas))
	}
	errs = append(errs, checkAreaIcons(areas)...)
	errs = append(errs, checkAreaKeys(areas, nil)...)
	seen := map[AreaID]bool{}
	for i, a := range areas {
		switch {
		case a.ID == "":
			errs = append(errs, fmt.Errorf("area %d has no ID", i+1))
		case seen[a.ID]:
			errs = append(errs, fmt.Errorf("area %q is listed twice", a.ID))
		}
		seen[a.ID] = true
		if a.Title == "" {
			errs = append(errs, fmt.Errorf("area %q has no title", a.ID))
		}
		tabs := map[TabID]bool{}
		for j, t := range a.Tabs {
			switch {
			case t.ID == "":
				errs = append(errs, fmt.Errorf("area %q: tab %d has no ID", a.ID, j+1))
			case tabs[t.ID]:
				errs = append(errs, fmt.Errorf("area %q: tab %q is listed twice", a.ID, t.ID))
			}
			tabs[t.ID] = true
			if t.Title == "" {
				errs = append(errs, fmt.Errorf("area %q: tab %q has no title", a.ID, t.ID))
			}
		}
	}
	return errors.Join(errs...)
}
