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
	ZoneTabs    FocusZone = "tabs"
	ZoneSubtabs FocusZone = "subtabs"
	ZoneRows    FocusZone = "rows"
	ZoneForm    FocusZone = "form"
	ZoneDetail  FocusZone = "detail"
	ZoneFilters FocusZone = "filters"
)

var zoneOrder = [...]FocusZone{ZoneTabs, ZoneSubtabs, ZoneRows, ZoneForm, ZoneDetail, ZoneFilters}

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

// CheckAreas reports every area that the tab line cannot draw as one digit and a name: none at all, more
// than nine, an empty or repeated area ID, an empty title, and a sub-tab with an empty or repeated ID or an
// empty title. Applications run it in their tests or at start.
func CheckAreas(areas []Area) error {
	var errs []error
	if len(areas) == 0 {
		errs = append(errs, errors.New("the tab line holds no area"))
	}
	if len(areas) > maxAreas {
		errs = append(errs, fmt.Errorf("%d areas; a digit names at most %d", len(areas), maxAreas))
	}
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
