package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func wrapFooterSpec(width int) FrameSpec {
	spec := sampleSpec(width, 14)
	spec.Footer = []Hint{
		{Action: "help", Label: "ex.help", Kind: HintAlways},
		{Action: "enter", Label: "ex.open", Priority: 3},
		{Action: "save", Label: "ex.save", Priority: 4},
		{Action: "quit", Label: "ex.quit", Priority: 6},
		{Action: "info", Label: "ex.info", Priority: 700},
		{Action: "yes", Label: "ex.yes", Kind: HintAnswer},
		{Action: "no", Label: "ex.no", Kind: HintAnswer},
		{Action: "up", Label: "ex.move", Kind: HintNavigation},
	}
	return spec
}

// column is the cell of the first character of sub in row; -1 where row does not hold it.
func column(row, sub string) int {
	i := strings.Index(row, sub)
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(row[:i])
}

// The footer takes a second line before it drops a group; no group is broken over the lines, the groups at
// the right end stand at the right of the last line, and a click on a key in either line reaches its action.
func TestFooterWrapsBeforeItDrops(t *testing.T) {
	for _, look := range []string{"noneascii", "dark"} {
		f, _ := sampleFrame(t, looks(t)[look], i18n.English)
		for width := 20; width <= 160; width++ {
			spec := wrapFooterSpec(width)
			out, targets := f.Render(spec)
			rows := plainRows(out)
			ws := f.Workspace(spec)
			foot := rows[ws.Y+ws.Height:]
			if len(foot) < 1 || len(foot) > 2 || len(rows) != spec.Height {
				t.Fatalf("%s %d columns: %d footer rows of %d", look, width, len(foot), len(rows))
			}
			for _, r := range rows {
				if w := ansi.StringWidth(r); w != width {
					t.Fatalf("%s %d columns: row of %d cells: %q", look, width, w, r)
				}
			}
			groups := f.keys.Hints(spec.Footer, spec.Input, f.text)
			all := footerItems(groups, spec.Position)
			twoFit := fitsRows(all, width, 2)
			if oneFit := footerWidth(all) <= width; oneFit != (len(foot) == 1) {
				t.Errorf("%s %d columns: %d footer rows where one row fits is %v", look, width, len(foot), oneFit)
			}
			text := strings.Join(foot, "\n")
			if !strings.Contains(foot[0], "? help") {
				t.Errorf("%s %d columns: the help key is not in the first line: %q", look, width, foot)
			}
			for _, g := range groups {
				seen := -1
				for i, row := range foot {
					if strings.Contains(row, g.Text()) {
						seen = i
					}
				}
				if twoFit && seen < 0 {
					t.Errorf("%s %d columns: two lines fit, yet %q is missing:\n%s", look, width, g.Text(), text)
				}
				if g.Kind == HintNavigation && seen >= 0 && seen != len(foot)-1 {
					t.Errorf("%s %d columns: navigation is not in the last line:\n%s", look, width, text)
				}
			}
			if last := foot[len(foot)-1]; strings.Contains(text, "3/12") && !strings.HasSuffix(last, "3/12") {
				t.Errorf("%s %d columns: the position is not at the right end of the last line:\n%s", look, width, text)
			}
			// Every key group drawn in a line is a target of its first action in that line.
			for y, row := range foot {
				for _, g := range groups {
					if len(g.Keys) == 0 || g.Kind == HintAnswer && !strings.Contains(row, g.Text()) {
						continue
					}
					x := column(row, g.Text())
					if x < 0 {
						continue
					}
					mm := NewMouseMap(targets, true)
					got := mm.Handle(click(x, ws.Y+ws.Height+y))
					if !got.Handled || got.Action != g.Keys[0].Action {
						t.Errorf("%s %d columns: click on %q in footer line %d = %+v", look, width, g.Text(), y, got)
					}
					lastKey := g.Keys[len(g.Keys)-1]
					if lastKey.Action != g.Keys[0].Action {
						kx := x + ansi.StringWidth(g.keyText()) - ansi.StringWidth(lastKey.Keys)
						if got := mm.Handle(click(kx, ws.Y+ws.Height+y)); got.Action != lastKey.Action {
							t.Errorf("%s %d columns: click on the key %q = %+v", look, width, lastKey.Keys, got)
						}
					}
				}
			}
		}
	}
}

// Workspace takes the rows of the footer into account: the drawn workspace has exactly its height, the
// footer stands in the last rows and the frame is exactly Height lines tall.
func TestWorkspaceFollowsTheFooterHeight(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	for _, tc := range []struct{ width, footerRows int }{{120, 1}, {50, 2}} {
		spec := wrapFooterSpec(tc.width)
		spec.Message = nil
		out, targets := f.Render(spec)
		rows := plainRows(out)
		ws := f.Workspace(spec)
		if len(rows) != spec.Height {
			t.Fatalf("%d columns: %d rows", tc.width, len(rows))
		}
		if got := spec.Height - ws.Y - ws.Height; got != tc.footerRows {
			t.Errorf("%d columns: footer rows = %d, want %d", tc.width, got, tc.footerRows)
		}
		if !strings.HasPrefix(rows[ws.Y], "first line") || !strings.HasPrefix(rows[ws.Y+1], "second line") {
			t.Errorf("%d columns: workspace not drawn at its rect %+v", tc.width, ws)
		}
		for _, r := range rows[ws.Y+2 : ws.Y+ws.Height] {
			if strings.TrimSpace(r) != "" {
				t.Errorf("%d columns: workspace row is not empty: %q", tc.width, r)
			}
		}
		if !strings.HasPrefix(rows[ws.Y+ws.Height], "? help") || !strings.HasSuffix(rows[len(rows)-1], "3/12") {
			t.Errorf("%d columns: footer rows:\n%s", tc.width, strings.Join(rows[ws.Y+ws.Height:], "\n"))
		}
		for _, tg := range targets {
			if tg.Rect.Y >= spec.Height {
				t.Errorf("%d columns: target outside the frame: %+v", tc.width, tg)
			}
		}
	}
	// A terminal lower than the frame loses rows above the footer first, then the first footer line.
	spec := wrapFooterSpec(50)
	for height := 1; height <= 8; height++ {
		spec.Height = height
		out, targets := f.Render(spec)
		rows := plainRows(out)
		if len(rows) != height {
			t.Fatalf("height %d: %d rows", height, len(rows))
		}
		if !strings.HasSuffix(rows[height-1], "3/12") {
			t.Errorf("height %d: the last row is no footer: %q", height, rows[height-1])
		}
		for _, tg := range targets {
			if tg.Rect.Y >= height || tg.Rect.Y < 0 {
				t.Errorf("height %d: target outside the frame: %+v", height, tg)
			}
		}
	}
}
