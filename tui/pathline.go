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
	// NavPathHeader puts the path in the header after the product name and draws no navigation line: the
	// tab line, the sub-tab line and the path line all give their rows to the workspace.
	NavPathHeader
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

// pathChip is one section of the path as drawn: its text with the mark where it keeps one.
type pathChip struct {
	sec  pathSection
	word string
}

// pathChips are the chips of one level of shortening: 0 every section marked, 1 only the last marked, 2 the
// last alone, 3 the last alone with its name cut to room cells, the padding included.
func (f *frameRun) pathChips(all []pathSection, mode bool, level, room int) []pathChip {
	arrow := " " + f.styles.Glyph(GlyphExpanded)
	secs := all
	if level >= 2 && len(secs) > 1 {
		secs = secs[len(secs)-1:]
	}
	chips := make([]pathChip, len(secs))
	for i, s := range secs {
		chips[i] = pathChip{s, s.name}
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
		c.word = f.styles.Truncate(c.sec.name, room-2-lipgloss.Width(tail)) + tail
	}
	return chips
}

// pathChipsWidth is the cells chips take: the padding of each, and the separators between.
func (f *frameRun) pathChipsWidth(chips []pathChip) int {
	sep := lipgloss.Width(f.styles.Glyph(GlyphPathSeparator))
	n := 0
	for i, c := range chips {
		n += 2 + lipgloss.Width(c.word)
		if i > 0 {
			n += sep
		}
	}
	return n
}

// pathChipsFit are the chips of the first level that fits room cells, the last cut to room where none does.
func (f *frameRun) pathChipsFit(all []pathSection, mode bool, room int) []pathChip {
	chips := f.pathChips(all, mode, 0, room)
	for level := 1; level <= 3 && f.pathChipsWidth(chips) > room; level++ {
		chips = f.pathChips(all, mode, level, room)
	}
	return chips
}

// drawPathChips draws chips from column x, the focused one as the focused tab of the tab line, and returns
// the click targets of ZonePath of the sections with a target, clipped at width.
func (f *frameRun) drawPathChips(spec FrameSpec, chips []pathChip, x, width int) (string, []Target) {
	focused := spec.Focus == ZonePath
	sep := f.styles.Glyph(GlyphPathSeparator)
	focus := len(chips) - 1
	if spec.PathFocusArea {
		focus = 0
	}
	plain := f.styles.profile.Colorless()
	var b strings.Builder
	var targets []Target
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

// pathLine is the path line: the sections as "Area ▾ › Tab ▾", the focused one as the focused tab of the tab
// line. Where the line does not fit, the sections but the last lose their mark, then only the last stands,
// and its name is cut. Each section with a target is a click target of ZonePath.
func (f *frameRun) pathLine(spec FrameSpec, width int) (string, []Target) {
	chips := f.pathChipsFit(f.pathSections(spec), spec.ModeTab != "", width-2)
	line, targets := f.drawPathChips(spec, chips, 2, width)
	return f.focusMark(spec.Focus == ZonePath) + line, targets
}
