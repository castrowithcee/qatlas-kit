package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// NavForm is the form of the navigation between the areas of a screen. The zero value is the tab line.
type NavForm uint8

const (
	// NavTabs is the tab line with the sub-tab line below it.
	NavTabs NavForm = iota
	// NavPathLine is one line that names the current place as "Area ▾ › Tab ▾" and takes the place of the tab
	// line and the sub-tab line; the row they leave goes to the workspace.
	NavPathLine
)

// pathSection is one part of the path line. A section without target is the mode text.
type pathSection struct {
	name   string
	area   AreaID
	tab    TabID
	target bool
}

// pathSections are the sections of the path line: the active area, and its active tab where the area has
// more than one; with a mode tab the mode text alone, which is no target.
func (f *frameRun) pathSections(spec FrameSpec) []pathSection {
	if spec.ModeTab != "" {
		return []pathSection{{name: Escape(lookup(f.text, spec.ModeTab))}}
	}
	area, found := Area{}, false
	for _, a := range spec.Areas {
		if a.ID == spec.ActiveArea {
			area, found = a, true
			break
		}
	}
	if !found {
		return nil
	}
	out := []pathSection{{name: Escape(lookup(f.text, area.Title)), area: area.ID, target: true}}
	if len(area.Tabs) > 1 {
		for _, t := range area.Tabs {
			if t.ID == spec.ActiveTab {
				out = append(out, pathSection{name: Escape(lookup(f.text, t.Title)), area: area.ID, tab: t.ID, target: true})
			}
		}
	}
	return out
}

// pathLine is the path line: the sections as "Area ▾ › Tab ▾", the focused one as the focused tab of the tab
// line. Where the line does not fit, the sections but the last lose their mark, then only the last stands, and
// its name is cut. Each section with a target is a click target of ZonePath.
func (f *frameRun) pathLine(spec FrameSpec, width int) (string, []Target) {
	all := f.pathSections(spec)
	focused := spec.Focus == ZonePath
	sep := f.styles.Glyph(GlyphPathSeparator)
	arrow := " " + f.styles.Glyph(GlyphExpanded)
	mode := spec.ModeTab != ""
	// words are the chip texts of one level: every section, the last alone, or the last cut to room.
	type chip struct {
		sec  pathSection
		word string
	}
	build := func(level int) []chip {
		secs := all
		if level >= 2 && len(secs) > 1 {
			secs = secs[len(secs)-1:]
		}
		chips := make([]chip, len(secs))
		for i, s := range secs {
			chips[i] = chip{s, s.name}
			if !mode && (level == 0 || i == len(secs)-1) {
				chips[i].word += arrow
			}
		}
		if level >= 3 && len(chips) == 1 {
			c := &chips[0]
			tail := ""
			if !mode {
				tail = arrow
			}
			c.word = f.styles.Truncate(c.sec.name, width-2-2-lipgloss.Width(tail)) + tail
		}
		return chips
	}
	size := func(chips []chip) int {
		n := 2
		for i, c := range chips {
			n += 2 + lipgloss.Width(c.word)
			if i > 0 {
				n += lipgloss.Width(sep)
			}
		}
		return n
	}
	chips := build(0)
	for level := 1; level <= 3 && size(chips) > width; level++ {
		chips = build(level)
	}
	focus := len(chips) - 1
	if spec.PathFocusArea {
		focus = 0
	}
	plain := f.styles.profile.Colorless()
	var b strings.Builder
	b.WriteString(f.focusMark(focused))
	var targets []Target
	x := 2
	for i, c := range chips {
		if i > 0 {
			b.WriteString(f.styles.Render(StyleHint, sep))
			x += lipgloss.Width(sep)
		}
		text, style := " "+c.word+" ", f.styles.Style(StyleHint)
		switch {
		case focused && i == focus:
			style = f.activeTab(plain)
			if plain {
				text = "[" + c.word + "]"
			}
		case i == len(chips)-1:
			style = f.styles.Style(StyleActive)
		}
		b.WriteString(style.Render(text))
		cells := lipgloss.Width(text)
		if r, ok := clipRect(Rect{X: x, Width: cells, Height: 1}, width); ok && c.sec.target {
			targets = append(targets, Target{Rect: r, Zone: ZonePath, Area: c.sec.area, Tab: c.sec.tab})
		}
		x += cells
	}
	return b.String(), targets
}
