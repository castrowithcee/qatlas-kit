package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// restingRows tick the resting plants across the groups; oak is exempt. "old" matches only the exempt oak.
func restingRows(calls *int) []ListTickRow {
	count := func() {
		if calls != nil {
			*calls++
		}
	}
	return []ListTickRow{
		{Key: "rest", Title: "Resting", Match: func(it ListItem) bool { count(); return it.Cells[1] == "resting" },
			Exempt: func(it ListItem) bool { count(); return it.ID == "oak" }},
		{Key: "old", Title: "Old", Match: func(it ListItem) bool { return it.ID == "oak" },
			Exempt: func(ListItem) bool { return true }},
	}
}

func tickRowList(t *testing.T, edit func(*ListSpec)) *List {
	t.Helper()
	l := tickList(t, edit)
	if err := l.SetTickRows(restingRows(nil)); err != nil {
		t.Fatal(err)
	}
	return l
}

// rowNames names the shown rows: "*" all, "<key>" a tick row, "[group]" and entry IDs.
func rowNames(l *List) string {
	var out []string
	for _, r := range l.rows {
		switch {
		case r.all:
			out = append(out, "*")
		case r.isTick:
			out = append(out, "<"+r.tick+">")
		case r.item < 0:
			out = append(out, "["+r.group+"]")
		default:
			out = append(out, string(l.items[r.item].ID))
		}
	}
	return strings.Join(out, ",")
}

func markTick(t *testing.T, l *List, key string) {
	t.Helper()
	if !l.reselect(listMark{tick: key, isTick: true, ok: true}) {
		t.Fatalf("tick row %q does not show: %s", key, rowNames(l))
	}
}

// boxOf is the tick box drawn in front of the title.
func boxOf(t *testing.T, l *List, title string) string {
	t.Helper()
	view, _ := l.View(looks(t)["noneascii"], 60, 16)
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if i := strings.Index(line, title); i >= 4 && strings.TrimSpace(line[:i]) != "" && line[i-1] == ' ' {
			return line[i-4 : i-1]
		}
	}
	t.Fatalf("no row %q:\n%s", title, ansi.Strip(view))
	return ""
}

func clickTickBox(t *testing.T, l *List, key string) {
	t.Helper()
	_, targets := l.View(looks(t)["noneascii"], 60, 16)
	for _, tg := range targets {
		if tg.Item == tickRef+tickRowRef+key {
			if _, handled := l.Click(MouseResult{Handled: true, Zone: tg.Zone, Item: tg.Item, Action: tg.Action}); !handled {
				t.Fatal("the click on the box was not handled")
			}
			return
		}
	}
	t.Fatalf("no box target for %q", key)
}

func TestTickRowTicksMatchesAcrossGroupsNeverExempt(t *testing.T) {
	toggles := map[string]func(*testing.T, *List){
		"key":   func(_ *testing.T, l *List) { keysTo(l, "space") },
		"click": func(t *testing.T, l *List) { clickTickBox(t, l, "rest") },
		"act":   func(_ *testing.T, l *List) { l.Act("toggle") },
	}
	for name, toggle := range toggles {
		t.Run(name, func(t *testing.T) {
			l := tickRowList(t, nil)
			markTick(t, l, "rest")
			toggle(t, l)
			if got := ticks(l); got != "mint,fern" {
				t.Fatalf("ticked %q, want mint,fern (not oak)", got)
			}
			toggle(t, l)
			if got := ticks(l); got != "" {
				t.Fatalf("ticked %q after the second toggle", got)
			}
			// An exempt entry ticked on its own stays on the first toggle and goes with unticking.
			l.SetTicked([]ItemID{"oak"})
			toggle(t, l)
			if got := ticks(l); got != "mint,oak,fern" {
				t.Fatalf("ticked %q, want mint,oak,fern", got)
			}
			toggle(t, l)
			if got := ticks(l); got != "" {
				t.Fatalf("unticking left %q, the exempt entry goes too", got)
			}
		})
	}
}

func TestTickRowCycleDoesNotRestoreExemptEntry(t *testing.T) {
	l := tickRowList(t, nil)
	markTick(t, l, "rest")
	l.SetTicked([]ItemID{"mint", "oak"})
	want := []string{"mint,oak,fern", "", "mint", "mint,fern"}
	for i, w := range want {
		keysTo(l, "space")
		if got := ticks(l); got != w {
			t.Fatalf("toggle %d: ticked %q, want %q", i+1, got, w)
		}
	}
	// A single toggle of a member forgets the memory; the row then ticks all.
	l.SetTicked([]ItemID{"mint"})
	keysTo(l, "space") // remembers mint, ticks all
	keysTo(l, "space") // none
	l.reselect(listMark{id: "mint", ok: true})
	keysTo(l, "space") // single toggle of mint
	markTick(t, l, "rest")
	keysTo(l, "space")
	if got := ticks(l); got != "mint,fern" {
		t.Fatalf("a stale memory was restored: %q", got)
	}
}

func TestTickRowMemoryOverlapsOtherScopes(t *testing.T) {
	l := tickRowList(t, nil)
	l.SetTicked([]ItemID{"mint"})
	markTick(t, l, "rest")
	keysTo(l, "space") // remembers mint, ticks mint and fern
	if _, ok := l.memory[tickScope{row: "rest", isRow: true}]; !ok {
		t.Fatal("no memory after the partial step")
	}
	l.reselect(listMark{group: "herbs", isGroup: true, ok: true})
	keysTo(l, "space")
	if _, ok := l.memory[tickScope{row: "rest", isRow: true}]; ok {
		t.Error("toggling a group kept the memory of an overlapping tick row")
	}
}

func TestTickRowFollowsTheFilter(t *testing.T) {
	l := tickRowList(t, nil)
	l.SetFilter("shade")
	if got := rowNames(l); got != "<rest>,[],fern" {
		t.Fatalf("rows = %s", got)
	}
	if got := boxOf(t, l, "Resting"); got != "[ ]" {
		t.Errorf("box %q", got)
	}
	view, _ := l.View(looks(t)["noneascii"], 60, 10)
	if !strings.Contains(ansi.Strip(view), "Resting (3)") {
		t.Errorf("the count is of all matching entries, not of the shown:\n%s", ansi.Strip(view))
	}
	markTick(t, l, "rest")
	keysTo(l, "space")
	if got := ticks(l); got != "fern" {
		t.Errorf("a filtered toggle ticked %q", got)
	}
	if got := boxOf(t, l, "Resting"); got != "[x]" {
		t.Errorf("box %q, want [x] where the only shown target is ticked", got)
	}
	l.SetFilter("blooming")
	if strings.Contains(rowNames(l), "<") {
		t.Errorf("a tick row without shown match stands: %s", rowNames(l))
	}
	l.SetFilter("planted") // only the exempt oak
	if got := rowNames(l); got != "<rest>,<old>,[trees],oak" {
		t.Fatalf("rows = %s", got)
	}
	l.SetTicked(nil)
	markTick(t, l, "rest")
	keysTo(l, "space")
	if got := ticks(l); got != "" {
		t.Errorf("a row whose shown targets are all exempt ticked %q", got)
	}
}

func TestTickRowBoxStates(t *testing.T) {
	l := tickRowList(t, nil)
	for _, c := range []struct {
		ticked []ItemID
		rest   string
		old    string
	}{
		{nil, "[ ]", "[ ]"},
		{[]ItemID{"mint"}, "[-]", "[ ]"},
		{[]ItemID{"mint", "fern"}, "[x]", "[ ]"},
		{[]ItemID{"mint", "fern", "oak"}, "[x]", "[x]"},
		{[]ItemID{"oak"}, "[-]", "[x]"},
	} {
		l.SetTicked(c.ticked)
		if got := boxOf(t, l, "Resting"); got != c.rest {
			t.Errorf("%v: Resting box %q, want %q", c.ticked, got, c.rest)
		}
		if got := boxOf(t, l, "Old"); got != c.old {
			t.Errorf("%v: Old box %q, want %q", c.ticked, got, c.old)
		}
	}
}

func TestSetTickRowsRejectsAndChangesNothing(t *testing.T) {
	l := tickRowList(t, nil)
	before := rowNames(l)
	match := func(ListItem) bool { return true }
	for name, rows := range map[string][]ListTickRow{
		"empty key":    {{Key: "", Title: "A", Match: match}},
		"control key":  {{Key: "a\x1bb", Title: "A", Match: match}},
		"duplicate":    {{Key: "a", Title: "A", Match: match}, {Key: "a", Title: "B", Match: match}},
		"empty title":  {{Key: "a", Match: match}},
		"nil Match":    {{Key: "a", Title: "A"}},
		"one bad of 2": {{Key: "a", Title: "A", Match: match}, {Key: "b", Title: "B"}},
	} {
		if err := l.SetTickRows(rows); err == nil {
			t.Errorf("%s was accepted", name)
		}
		if got := rowNames(l); got != before || len(l.tickRows) != 2 {
			t.Errorf("%s changed the rows: %s", name, got)
		}
	}
	plain := filledList(t, nil)
	if err := plain.SetTickRows(restingRows(nil)); err == nil {
		t.Error("tick rows were accepted without tick boxes")
	}
}

func TestTickRowSelectionPositionAndEnter(t *testing.T) {
	l := tickRowList(t, nil)
	l.SetTicked(nil)
	if got := rowNames(l); got != "<rest>,<old>,[herbs],basil,mint,thyme,[trees],oak,[],fern" {
		t.Fatalf("rows = %s", got)
	}
	markTick(t, l, "old")
	if key, ok := l.SelectedTickRow(); !ok || key != "old" {
		t.Errorf("SelectedTickRow = %q, %v", key, ok)
	}
	if _, ok := l.Selected(); ok {
		t.Error("Selected names an entry on a tick row")
	}
	if _, ok := l.Current(); ok {
		t.Error("Current names an entry on a tick row")
	}
	if _, ok := l.SelectedGroup(); ok {
		t.Error("SelectedGroup names a group on a tick row")
	}
	if got := l.Position(); got != "1/5" {
		t.Errorf("Position = %q, tick rows do not count", got)
	}
	for _, k := range []string{"enter", "left", "right"} {
		if open, _ := keysTo(l, k); open != "" || rowNames(l) != "<rest>,<old>,[herbs],basil,mint,thyme,[trees],oak,[],fern" {
			t.Errorf("%s on a tick row opened %q or folded: %s", k, open, rowNames(l))
		}
	}
	l.reselect(listMark{id: "basil", ok: true})
	if _, ok := l.SelectedTickRow(); ok {
		t.Error("SelectedTickRow on an entry")
	}
}

func TestTickRowsSitUnderAllAndAboveGroupsFlatToo(t *testing.T) {
	l := tickRowList(t, func(s *ListSpec) { s.AllRow = true })
	if got := rowNames(l); !strings.HasPrefix(got, "*,<rest>,<old>,[herbs]") {
		t.Errorf("grouped rows = %s", got)
	}
	l.SetGrouped(false)
	if got := rowNames(l); got != "*,<rest>,<old>,basil,mint,oak,thyme,fern" {
		t.Errorf("flat rows = %s", got)
	}
	// The row of all still ticks the exempt entry.
	l.reselect(listMark{all: true, ok: true})
	keysTo(l, "space")
	if got := ticks(l); got != "basil,mint,oak,thyme,fern" {
		t.Errorf("all ticked %q", got)
	}
	if got, want := l.wanted(80, Styles{}), 1+2+5+1+2; got != want {
		t.Errorf("wanted = %d, want %d", got, want)
	}
}

func TestTickRowsEscapeTheTitleAndCallTheToolOnlyOnRebuild(t *testing.T) {
	calls := 0
	l := tickList(t, nil)
	rows := restingRows(&calls)
	rows[0].Title = "Rest\x1b[31mred"
	if err := l.SetTickRows(rows); err != nil {
		t.Fatal(err)
	}
	after := calls
	for range 5 {
		view, _ := l.View(looks(t)["noneascii"], 60, 16)
		if strings.Contains(view, "\x1b[31m") {
			t.Fatal("the title reached the screen unmasked")
		}
	}
	markTick(t, l, "rest")
	keysTo(l, "space", "space")
	l.SetFilter("sh")
	if calls != after {
		t.Errorf("Match and Exempt ran %d times while drawing, toggling and filtering", calls-after)
	}
}

func TestWithoutTickRowsTheOutputIsUnchanged(t *testing.T) {
	st := looks(t)["noneascii"]
	a := tickList(t, func(s *ListSpec) { s.AllRow = true })
	b := tickList(t, func(s *ListSpec) { s.AllRow = true })
	if err := b.SetTickRows([]ListTickRow{}); err != nil {
		t.Fatal(err)
	}
	va, _ := a.View(st, 60, 16)
	vb, _ := b.View(st, 60, 16)
	if va != vb {
		t.Errorf("an empty set of tick rows changed the view:\n%s\n%s", va, vb)
	}
}
