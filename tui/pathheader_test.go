package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func headerPathSpec(width int) FrameSpec {
	spec := sampleSpec(width, 10)
	spec.Nav = NavPathHeader
	spec.Workspace = "body"
	return spec
}

// With the path in the header no navigation line stands: the workspace takes the tab line and the sub-tab
// line, and the rule follows the header.
func TestPathHeaderLeavesNoNavigationLine(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	tabs := headerPathSpec(80)
	tabs.Nav = NavTabs
	for _, area := range []AreaID{"settings", "items"} {
		tabs.ActiveArea = area
		spec := headerPathSpec(80)
		spec.ActiveArea = area
		want := f.Workspace(tabs).Height + 1
		if _, ok := subTabs(tabs); ok {
			want++
		}
		if got := f.Workspace(spec).Height; got != want {
			t.Errorf("%s: workspace %d rows, want %d", area, got, want)
		}
		rows := plainRows(first(f.Render(spec)))
		if strings.TrimSpace(rows[1]) != strings.Repeat("─", 80) || !strings.Contains(rows[0], "Settings") && area == "settings" {
			t.Errorf("%s: rows %q", area, rows[:2])
		}
	}
	// Without areas there is no path and no row to give.
	spec := headerPathSpec(80)
	spec.Areas = nil
	if rows := plainRows(first(f.Render(spec))); strings.Contains(rows[0], "▾") || strings.Contains(rows[0], " v ") {
		t.Errorf("header without areas %q", rows[0])
	}
}

// The path stands after the product name and before the marks; at every width down to the smallest one the
// current section stays, drawn in the line, and a click on it carries the area and tab of the path line.
func TestPathHeaderKeepsTheCurrentSectionAtEveryWidth(t *testing.T) {
	for look, st := range looks(t) {
		f, _ := sampleFrame(t, st, i18n.English)
		for _, marks := range []bool{true, false} {
			for width := 8; width <= 100; width++ {
				spec := headerPathSpec(width)
				if !marks {
					spec.Status = nil
				}
				out, targets := f.Render(spec)
				row := plainRows(out)[0]
				var got []Target
				for _, tg := range targets {
					if tg.Zone == ZonePath {
						got = append(got, tg)
					}
				}
				if len(got) == 0 {
					t.Fatalf("%s %d marks=%v: no path target in %q", look, width, marks, row)
				}
				last := got[len(got)-1]
				cells := string([]rune(row)[last.Rect.X : last.Rect.X+last.Rect.Width])
				want := "G"
				if width >= 20 {
					want = "General"
				}
				if last.Tab != "general" || last.Area != "settings" || last.Rect.Y != 0 || !strings.Contains(cells, want) {
					t.Errorf("%s %d marks=%v: target %+v covers %q in %q", look, width, marks, last, cells, row)
				}
				if i := strings.Index(row, "example"); i < 0 && width >= 24 {
					t.Errorf("%s %d: no product name in %q", look, width, row)
				}
			}
		}
	}
}

// The ranks of the marks and the blocks hold: at room where the path is whole, the marks and the keys stand
// after it; short of room the path gives its widening to them first and keeps the current section.
func TestPathHeaderOrder(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	row := plainRows(first(f.Render(headerPathSpec(100))))[0]
	for _, part := range []string{"example", "Settings ▾", "General ▾", "store open", "session off", "[esc] back"} {
		if !strings.Contains(row, part) {
			t.Fatalf("wide header %q lacks %q", row, part)
		}
	}
	order := []string{"example", "Settings", "General", "store open", "session off", "[esc] back"}
	at := -1
	for _, part := range order {
		i := strings.Index(row, part)
		if i <= at {
			t.Errorf("%q stands out of order in %q", part, row)
		}
		at = i
	}
	narrow := plainRows(first(f.Render(headerPathSpec(30))))[0]
	if !strings.Contains(narrow, "General ▾") || strings.Contains(narrow, "Settings") {
		t.Errorf("narrow header %q", narrow)
	}
}

// The mode tab stands in the header as the only section, without mark and target.
func TestPathHeaderModeTab(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	spec := headerPathSpec(80)
	spec.ModeTab = "ex.setup"
	out, targets := f.Render(spec)
	row := plainRows(out)[0]
	if strings.Contains(row, "▾") || strings.Contains(row, "Settings") {
		t.Errorf("mode header %q", row)
	}
	for _, tg := range targets {
		if tg.Zone == ZonePath {
			t.Errorf("mode section is a target: %+v", tg)
		}
	}
}

// The focus at ZonePath draws the last section as the focused tab, the area section with PathFocusArea.
func TestPathHeaderFocus(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	spec := headerPathSpec(100)
	spec.Focus = ZonePath
	if row := plainRows(first(f.Render(spec)))[0]; !strings.Contains(row, "[General ▾]") {
		t.Errorf("focused header %q", row)
	}
	spec.PathFocusArea = true
	if row := plainRows(first(f.Render(spec)))[0]; !strings.Contains(row, "[Settings ▾]") {
		t.Errorf("area focus header %q", row)
	}
}

// The path of the header holds the areas to CheckAreas, like the path line.
func TestPathHeaderCheckFrame(t *testing.T) {
	spec := headerPathSpec(80)
	if err := CheckFrame(spec); err != nil {
		t.Fatalf("CheckFrame() = %v", err)
	}
	spec.Areas = append([]Area{{ID: "x"}}, spec.Areas...)
	if CheckFrame(spec) == nil {
		t.Error("CheckFrame accepts an area without title in the header path")
	}
}

func TestGoldenPathHeader(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		lang       i18n.Lang
		width      int
		marks      bool
		focus      FocusZone
	}{
		{"frame.pathheader.wide.golden", "mono", i18n.English, 100, true, ZonePath},
		{"frame.pathheader.wide.nomarks.golden", "mono", i18n.English, 100, false, ZoneRows},
		{"frame.pathheader.medium.golden", "mono", i18n.English, 60, true, ZoneRows},
		{"frame.pathheader.narrow.golden", "mono", i18n.English, 32, true, ZonePath},
		{"frame.pathheader.verynarrow.golden", "mono", i18n.English, 12, true, ZoneRows},
		{"frame.pathheader.wide.dark.golden", "dark", i18n.English, 100, true, ZonePath},
		{"frame.pathheader.wide.de.golden", "mono", i18n.German, 100, true, ZoneRows},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := sampleFrame(t, looks(t)[tt.look], tt.lang)
			spec := headerPathSpec(tt.width)
			spec.Height = 6
			spec.Focus = tt.focus
			if !tt.marks {
				spec.Status = nil
			}
			checkGolden(t, tt.name, first(f.Render(spec))+"\n")
		})
	}
}
