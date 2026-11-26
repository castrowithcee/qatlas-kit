package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func choiceSpec(width int) FrameSpec {
	spec := sampleSpec(width, 6)
	spec.Nav = NavHeaderChoice
	spec.Workspace = "body"
	return spec
}

// The choice field takes the place of the tab line: the header holds "Area ▾", the sub-tab line stays where
// the area has sub-tabs, and the row of the tab line goes to the workspace.
func TestHeaderChoiceLeavesOnlyTheSubTabLine(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	tabs := choiceSpec(80)
	tabs.Nav = NavTabs
	for area, subs := range map[AreaID]bool{"settings": true, "items": false} {
		spec := choiceSpec(80)
		spec.ActiveArea = area
		tabs.ActiveArea = area
		if got, want := f.Workspace(spec).Height, f.Workspace(tabs).Height+1; got != want {
			t.Errorf("%s: workspace %d rows, want %d", area, got, want)
		}
		rows := plainRows(first(f.Render(spec)))
		if !strings.Contains(rows[0], strings.ToUpper(string(area[:1]))+string(area[1:])+" ▾") || strings.Contains(rows[0], "General") {
			t.Errorf("%s: header %q", area, rows[0])
		}
		if subs != strings.Contains(rows[1], "General") || strings.Contains(rows[1], "Overview") {
			t.Errorf("%s: row 1 %q", area, rows[1])
		}
		if !subs && strings.TrimSpace(rows[1]) != strings.Repeat("─", 80) {
			t.Errorf("%s: no rule after the header: %q", area, rows[1])
		}
	}
}

// The field is the zone ZoneTabs: one target that carries the active area, no target for the tab.
func TestHeaderChoiceTargetAndFocus(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	spec := choiceSpec(100)
	spec.Focus = ZoneTabs
	out, targets := f.Render(spec)
	if row := plainRows(out)[0]; !strings.Contains(row, "[Settings ▾]") {
		t.Errorf("focused header %q", row)
	}
	var field []Target
	for _, tg := range targets {
		if tg.Zone == ZoneTabs || tg.Zone == ZonePath {
			field = append(field, tg)
		}
	}
	if len(field) != 1 || field[0].Zone != ZoneTabs || field[0].Area != "settings" || field[0].Tab != "" || field[0].Rect.Y != 0 {
		t.Errorf("field targets %+v", field)
	}
	spec.Focus = ZoneSubtabs
	if row := plainRows(first(f.Render(spec)))[0]; strings.Contains(row, "[Settings") {
		t.Errorf("unfocused header %q", row)
	}
}

// At every width the field stays and a click on it carries the area; a mode tab is no target.
func TestHeaderChoiceKeepsTheFieldAtEveryWidth(t *testing.T) {
	for look, st := range looks(t) {
		f, _ := sampleFrame(t, st, i18n.English)
		for width := 8; width <= 100; width++ {
			out, targets := f.Render(choiceSpec(width))
			n := 0
			for _, tg := range targets {
				if tg.Zone == ZoneTabs {
					n++
					if tg.Area != "settings" || tg.Rect.Y != 0 {
						t.Errorf("%s %d: target %+v", look, width, tg)
					}
				}
			}
			if n != 1 {
				t.Errorf("%s %d: %d field targets in %q", look, width, n, plainRows(out)[0])
			}
		}
	}
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	spec := choiceSpec(80)
	spec.ModeTab = "ex.setup"
	out, targets := f.Render(spec)
	if row := plainRows(out)[0]; strings.Contains(row, "▾") || strings.Contains(row, "Settings") {
		t.Errorf("mode header %q", row)
	}
	for _, tg := range targets {
		if tg.Zone == ZoneTabs {
			t.Errorf("mode text is a target: %+v", tg)
		}
	}
}

// The icon of the area stands before its title, as on the tab line, and goes in ASCII.
func TestHeaderChoiceIcon(t *testing.T) {
	spec := iconSpec(100)
	spec.Nav = NavHeaderChoice
	spec.ActiveArea = "settings"
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	if row := plainRows(first(f.Render(spec)))[0]; !strings.Contains(row, "✓ Settings") {
		t.Errorf("header %q", row)
	}
	f, _ = sampleFrame(t, looks(t)["noneascii"], i18n.English)
	if row := plainRows(first(f.Render(spec)))[0]; strings.Contains(row, "✓") || !strings.Contains(row, "Settings v") {
		t.Errorf("ASCII header %q", row)
	}
}

func TestHeaderChoiceCheckFrame(t *testing.T) {
	spec := choiceSpec(80)
	if err := CheckFrame(spec); err != nil {
		t.Fatalf("CheckFrame() = %v", err)
	}
	spec.Areas = append([]Area{{ID: "x"}}, spec.Areas...)
	if CheckFrame(spec) == nil {
		t.Error("CheckFrame accepts an area without title")
	}
}

// ↓ ↑ walk header field, sub-tab line, work area; a missing sub-tab line is skipped.
func TestHeaderChoiceZoneLevels(t *testing.T) {
	with := []FocusZone{ZoneTabs, ZoneSubtabs, ZoneRows}
	without := []FocusZone{ZoneTabs, ZoneRows}
	for _, c := range []struct {
		zones []FocusZone
		cur   FocusZone
		step  int
		want  FocusZone
	}{
		{with, ZoneTabs, 1, ZoneSubtabs}, {with, ZoneSubtabs, 1, ZoneRows}, {with, ZoneRows, -1, ZoneSubtabs},
		{with, ZoneSubtabs, -1, ZoneTabs}, {without, ZoneTabs, 1, ZoneRows}, {without, ZoneRows, -1, ZoneTabs},
	} {
		if got := StepZoneLevel(c.zones, c.cur, c.step); got != c.want {
			t.Errorf("StepZoneLevel(%v, %s, %d) = %s, want %s", c.zones, c.cur, c.step, got, c.want)
		}
	}
}

func TestGoldenHeaderChoice(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		lang       i18n.Lang
		width      int
		area       AreaID
		focus      FocusZone
		icons      bool
	}{
		{"frame.headerchoice.wide.golden", "mono", i18n.English, 100, "settings", ZoneTabs, false},
		{"frame.headerchoice.single.golden", "mono", i18n.English, 100, "items", ZoneRows, false},
		{"frame.headerchoice.subtabs.golden", "mono", i18n.English, 100, "settings", ZoneSubtabs, false},
		{"frame.headerchoice.narrow.golden", "mono", i18n.English, 32, "settings", ZoneTabs, false},
		{"frame.headerchoice.wide.dark.golden", "dark", i18n.English, 100, "settings", ZoneTabs, false},
		{"frame.headerchoice.wide.de.golden", "mono", i18n.German, 100, "settings", ZoneRows, false},
		{"frame.headerchoice.noneascii.golden", "noneascii", i18n.English, 100, "settings", ZoneTabs, true},
		{"frame.headerchoice.icons.golden", "mono", i18n.English, 100, "settings", ZoneRows, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := sampleFrame(t, looks(t)[tt.look], tt.lang)
			spec := choiceSpec(tt.width)
			if tt.icons {
				spec = iconSpec(tt.width)
				spec.Nav, spec.Height, spec.Workspace = NavHeaderChoice, 6, "body"
			}
			spec.ActiveArea, spec.Focus = tt.area, tt.focus
			if tt.icons {
				spec.ActiveTab = "general"
			}
			checkGolden(t, tt.name, first(f.Render(spec))+"\n")
		})
	}
}

func TestPathNavOpenAreas(t *testing.T) {
	spec := choiceSpec(80)
	spec.ActiveArea = "items"
	p := samplePathNav(t, i18n.English)
	if !p.OpenAreas(spec) {
		t.Fatal("did not open")
	}
	// The active area is marked and chosen by enter; areas stand without their tabs.
	view, targets, _ := p.Overlay(looks(t)["noneascii"], sampleScreen(80, 20), 80, 20, pathNavRoom(80, 20))
	if v := strings.Join(plainRows(view), "\n"); strings.Contains(v, "General") || !strings.Contains(v, "Settings") {
		t.Errorf("overlay:\n%s", v)
	}
	if c, h := p.Update(press("enter")); c != (PathChoice{Area: "items"}) || !h || p.Open() {
		t.Errorf("enter chose %+v %v", c, h)
	}
	p.OpenAreas(spec)
	p.Update(press("down"))
	if c, _ := p.Update(press("enter")); c != (PathChoice{Area: "settings"}) {
		t.Errorf("down, enter chose %+v", c)
	}
	p.OpenAreas(spec)
	if c, h := p.Update(press("esc")); c != (PathChoice{}) || !h || p.Open() {
		t.Errorf("esc: %+v %v", c, h)
	}
	p.OpenAreas(spec)
	_, targets, _ = p.Overlay(looks(t)["noneascii"], sampleScreen(80, 20), 80, 20, pathNavRoom(80, 20))
	view, _, _ = p.Overlay(looks(t)["noneascii"], sampleScreen(80, 20), 80, 20, pathNavRoom(80, 20))
	rows := plainRows(view)
	for y, r := range rows {
		if x := strings.Index(r, "Overview"); x >= 0 {
			if c, h := p.Click(clickAt(NewMouseMap(targets, true), x, y)); c != (PathChoice{Area: "overview"}) || !h || p.Open() {
				t.Errorf("click chose %+v", c)
			}
			break
		}
	}
	spec.ModeTab = "ex.setup"
	if p.OpenAreas(spec) || p.Open() {
		t.Error("opened for the mode text")
	}
	spec.ModeTab, spec.Areas = "", nil
	if p.OpenAreas(spec) {
		t.Error("opened without areas")
	}
}

func TestGoldenPathNavAreas(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		w          int
	}{
		{"pathnav.areas.wide.golden", "noneascii", 100},
		{"pathnav.areas.wide.dark.golden", "dark", 100},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := samplePathNav(t, i18n.English)
			if !p.OpenAreas(choiceSpec(tt.w)) {
				t.Fatal("did not open")
			}
			view, _, ok := p.Overlay(looks(t)[tt.look], sampleScreen(tt.w, 20), tt.w, 20, pathNavRoom(tt.w, 20))
			if !ok {
				t.Fatal("no overlay")
			}
			checkGolden(t, tt.name, view+"\n")
		})
	}
}
