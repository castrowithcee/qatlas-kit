package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func pathSpec(width int) FrameSpec {
	spec := sampleSpec(width, 10)
	spec.Nav = NavPathLine
	spec.Focus = ZoneRows
	spec.Workspace = "body"
	return spec
}

// The zero form draws the tab line exactly as before; the path line takes one row where the tab line and the
// sub-tab line took two, and the row goes to the workspace.
func TestPathLineReplacesTheTabLines(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	tabs := pathSpec(80)
	tabs.Nav = NavTabs
	if tabs.Nav != 0 {
		t.Fatal("the tab line is not the zero form")
	}
	path := pathSpec(80)
	if got, want := f.Workspace(path).Height, f.Workspace(tabs).Height+1; got != want {
		t.Errorf("workspace %d rows, want %d", got, want)
	}
	rows := plainRows(first(f.Render(path)))
	if !strings.Contains(rows[1], "Settings v") && !strings.Contains(rows[1], "Settings ▾") {
		t.Errorf("path line %q", rows[1])
	}
	if strings.Contains(rows[1], "Overview") || strings.TrimSpace(rows[2]) != strings.Repeat("─", 80) {
		t.Errorf("rows %q", rows[:3])
	}
	// An area without sub-tabs takes the same single row.
	path.ActiveArea = "items"
	if got, want := f.Workspace(path).Height, f.Workspace(tabs).Height+1; got != want {
		t.Errorf("workspace of an area without sub-tabs %d rows, want %d", got, want)
	}
	tabs.ActiveArea = "items"
	if f.Workspace(path).Height != f.Workspace(tabs).Height {
		t.Error("an area without sub-tabs gains no row")
	}
}

// Each section is a ZonePath target that carries the area, and the tab for the last; the cells of a target
// hold its text. Narrow lines keep the last section only, and the targets follow.
func TestPathLineTargetsAndShortening(t *testing.T) {
	for look, st := range looks(t) {
		f, _ := sampleFrame(t, st, i18n.English)
		for _, tt := range []struct {
			width int
			want  []string
		}{
			{80, []string{"Settings", "General"}},
			{26, []string{"Settings", "General"}},
			{20, []string{"General"}},
			{13, []string{"General"}},
			{9, []string{"Ge" + st.Glyph(GlyphEllipsis)}},
		} {
			out, targets := f.Render(pathSpec(tt.width))
			rows := plainRows(out)
			var got []Target
			for _, tg := range targets {
				if tg.Zone == ZonePath {
					got = append(got, tg)
				}
				if tg.Zone == ZoneTabs || tg.Zone == ZoneSubtabs {
					t.Errorf("%s %d: tab target %+v", look, tt.width, tg)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("%s %d: %d targets in %q", look, tt.width, len(got), rows[1])
			}
			for i, tg := range got {
				cells := []rune(rows[tg.Rect.Y])[tg.Rect.X : tg.Rect.X+tg.Rect.Width]
				if !strings.Contains(string(cells), tt.want[i]) || tg.Area != "settings" || tg.Rect.Y != 1 {
					t.Errorf("%s %d: target %+v covers %q, want %q", look, tt.width, tg, string(cells), tt.want[i])
				}
				if (tg.Tab != "") != (i == len(tt.want)-1) {
					t.Errorf("%s %d: target %d has tab %q", look, tt.width, i, tg.Tab)
				}
			}
			if tt.width < 20 && strings.Contains(rows[1], "Settings") {
				t.Errorf("%s %d: %q keeps the area", look, tt.width, rows[1])
			}
		}
	}
}

// Without sub-tabs the path holds the area alone; the ASCII form uses v and >.
func TestPathLineWithoutSubTabsAndInASCII(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := pathSpec(60)
	spec.ActiveArea = "items"
	if row := plainRows(first(f.Render(spec)))[1]; strings.TrimSpace(row) != "Items v" {
		t.Errorf("path line %q", row)
	}
	spec.ActiveArea = "settings"
	if row := plainRows(first(f.Render(spec)))[1]; strings.TrimSpace(row) != "Settings v > General v" {
		t.Errorf("path line %q", row)
	}
}

// The focus mark and the focused section follow Focus and PathFocusArea; the last section is the default.
func TestPathLineFocus(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	spec := pathSpec(60)
	if row := plainRows(first(f.Render(spec)))[1]; strings.Contains(row, ">") || strings.Contains(row, "[") {
		t.Errorf("unfocused %q", row)
	}
	spec.Focus = ZonePath
	if row := plainRows(first(f.Render(spec)))[1]; !strings.HasPrefix(row, "> ") || !strings.Contains(row, "[General ▾]") {
		t.Errorf("focused %q", row)
	}
	spec.PathFocusArea = true
	if row := plainRows(first(f.Render(spec)))[1]; !strings.Contains(row, "[Settings ▾]") || strings.Contains(row, "[General") {
		t.Errorf("area focused %q", row)
	}
}

// The mode text is the only section, without target and without mark, escaped.
func TestPathLineModeTab(t *testing.T) {
	for look, st := range looks(t) {
		f, _ := sampleFrame(t, st, i18n.English)
		spec := pathSpec(60)
		spec.ModeTab = "ex.setup"
		out, targets := f.Render(spec)
		row := plainRows(out)[1]
		if !strings.Contains(row, "Guided setup") || strings.Contains(row, "Settings") || strings.ContainsAny(row, "▾›") || strings.Contains(row, " v ") {
			t.Errorf("%s: mode path line %q", look, row)
		}
		for _, tg := range targets {
			if tg.Zone == ZonePath {
				t.Errorf("%s: the mode text is a target: %+v", look, tg)
			}
		}
	}
	st := looks(t)["dark"]
	keys := sampleKeymap(t, st.Charset())
	cat := frameCatalogs()
	cat[i18n.English]["ex.hostile"] = "set\x1b[31mup\x07"
	f := styledFrame{NewFrame(keys, newTranslator(t, i18n.English, cat, keys)), st}
	spec := pathSpec(60)
	spec.ModeTab = "ex.hostile"
	if out := plainRows(first(f.Render(spec)))[1]; strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("control characters reach the terminal: %q", out)
	}
	spec.ModeTab = ""
	cat[i18n.English]["ex.settings"] = "Set\x1b[31mtings\x07"
	if out := plainRows(first(f.Render(spec)))[1]; strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("area name unescaped: %q", out)
	}
}

func TestCheckFrameNavigation(t *testing.T) {
	spec := pathSpec(60)
	if err := CheckFrame(spec); err != nil {
		t.Errorf("CheckFrame: %v", err)
	}
	spec.Nav = NavForm(9)
	if err := CheckFrame(spec); err == nil {
		t.Error("CheckFrame accepts an unknown navigation form")
	}
	spec.Nav = NavPathLine
	spec.Areas = []Area{{ID: "a", Title: ""}}
	if err := CheckFrame(spec); err == nil {
		t.Error("CheckFrame accepts an area without title on the path line")
	}
	spec.Areas = sampleAreas()
	spec.Areas[0].Icon = "\x1b"
	if err := CheckFrame(spec); err == nil {
		t.Error("CheckFrame accepts a hostile icon on the path line")
	}
}

func TestZonePathFollowsSubtabs(t *testing.T) {
	zones := []FocusZone{ZoneRows, ZonePath, ZoneForm}
	if got := CycleZone(zones, ZonePath, 1); got != ZoneRows {
		t.Errorf("after the path line: %q", got)
	}
	if got := CycleZone(zones, ZoneRows, -1); got != ZonePath {
		t.Errorf("before the rows: %q", got)
	}
	if got := CycleZone([]FocusZone{ZoneSubtabs, ZonePath, ZoneRows}, ZoneSubtabs, 1); got != ZonePath {
		t.Errorf("after the sub-tabs: %q", got)
	}
}

func TestGoldenPathLine(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		lang       i18n.Lang
		width      int
		area       AreaID
		focus      FocusZone
	}{
		{"frame.pathline.wide.golden", "mono", i18n.English, 80, "settings", ZonePath},
		{"frame.pathline.narrow.golden", "mono", i18n.English, 20, "settings", ZonePath},
		{"frame.pathline.wide.dark.golden", "dark", i18n.English, 80, "settings", ZonePath},
		{"frame.pathline.wide.de.golden", "mono", i18n.German, 80, "settings", ZoneRows},
		{"frame.pathline.single.golden", "mono", i18n.English, 80, "items", ZonePath},
		{"frame.pathline.noneascii.golden", "noneascii", i18n.English, 80, "settings", ZonePath},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := sampleFrame(t, looks(t)[tt.look], tt.lang)
			spec := pathSpec(tt.width)
			spec.ActiveArea, spec.Focus = tt.area, tt.focus
			spec.Height = 6
			checkGolden(t, tt.name, first(f.Render(spec))+"\n")
		})
	}
}
