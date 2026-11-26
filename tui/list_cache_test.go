package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// heads are the drawn rows of all and of the groups without the selection mark, joined by "|".
func heads(l *List) string {
	view, _ := l.View(Styles{}, 70, 20)
	var out []string
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		// The first two cells hold the mark of the selected row.
		line = strings.TrimSpace(line[2:])
		if strings.Contains(line, " all (") || strings.Contains(line, "# ") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "|")
}

// titleAt is the cell at which the column title stands in the drawn header.
func titleAt(t *testing.T, l *List, title string) int {
	t.Helper()
	view, _ := l.View(Styles{}, 120, 20)
	header := strings.Split(ansi.Strip(view), "\n")[0]
	at := strings.Index(header, title)
	if at < 0 {
		t.Fatalf("no %s in the header %q", title, header)
	}
	return at
}

// Boxes, the count of the row of all and the counts of the groups are right at once after every way that
// changes them.
func TestListBoxesAndCountsFollowEveryChange(t *testing.T) {
	l := tickList(t, func(s *ListSpec) { s.AllRow = true })
	step := func(name, want string) {
		t.Helper()
		if got := heads(l); got != want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, want)
		}
	}
	step("start", "[ ] all (5)|v [ ] # herbs (3)|v [ ] # trees (1)|v [ ] # unsorted (1)")

	if selected(t, l) != "basil" {
		t.Fatal("the mark is not on basil")
	}
	keysTo(l, "space")
	step("toggle key", "[-] all (5)|v [-] # herbs (3)|v [ ] # trees (1)|v [ ] # unsorted (1)")

	for _, id := range []string{"mint", "thyme"} {
		_, targets := l.View(Styles{}, 70, 20)
		var box Target
		for _, tg := range targets {
			if tg.Item == tickRef+id {
				box = tg
			}
		}
		l.Click(NewMouseMap(targets, true).Handle(click(box.Rect.X+1, box.Rect.Y)))
	}
	step("click on the boxes", "[-] all (5)|v [x] # herbs (3)|v [ ] # trees (1)|v [ ] # unsorted (1)")

	l.Select("basil")
	keysTo(l, "up", "space")
	step("toggle of the group", "[ ] all (5)|v [ ] # herbs (3)|v [ ] # trees (1)|v [ ] # unsorted (1)")
	keysTo(l, "up", "space")
	step("toggle of all", "[x] all (5)|v [x] # herbs (3)|v [x] # trees (1)|v [x] # unsorted (1)")

	if err := l.SetTicked([]ItemID{"oak", "thyme"}); err != nil {
		t.Fatal(err)
	}
	step("SetTicked", "[-] all (5)|v [-] # herbs (3)|v [x] # trees (1)|v [ ] # unsorted (1)")

	l.SetFilter("bloom")
	step("SetFilter", "[-] all (2)|v [-] # herbs (3)")
	l.SetFilter("thy")
	step("narrower filter", "[x] all (1)|v [x] # herbs (3)")

	items := append(samplePlants(), ListItem{ID: "sage", Cells: []string{"sage", "blooming", "20", "thyme-like"}, Group: "herbs"})
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	step("SetItems", "[-] all (2)|v [-] # herbs (4)")

	l.SetFilter("")
	l.Select("basil")
	keysTo(l, "left")
	step("fold", "[-] all (6)|> [-] # herbs (4)|v [x] # trees (1)|v [ ] # unsorted (1)")
	keysTo(l, "space")
	step("toggle of the folded group", "[-] all (6)|> [x] # herbs (4)|v [x] # trees (1)|v [ ] # unsorted (1)")
	l.Reset()
	step("Reset", "[-] all (6)|> [x] # herbs (4)|v [x] # trees (1)|v [ ] # unsorted (1)")

	l.SetGrouped(false)
	l.SetFilter("resting")
	step("flat with a filter", "[-] all (3)")
}

// The columns widen with a wider value of SetItems and a longer group title of SetGroups.
func TestListColumnsWidenWithNewEntriesAndGroups(t *testing.T) {
	l := groupedList(t, nil)
	before := titleAt(t, l, "STATE")
	items := append(samplePlants(), ListItem{ID: "rosemary", Cells: []string{"rosemary-by-the-gate", "resting", "5", ""}, Group: "herbs"})
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	wider := titleAt(t, l, "STATE")
	if wider <= before {
		t.Errorf("SetItems: STATE at %d, before at %d", wider, before)
	}
	groups := sampleGroups()
	groups[0].Title = "Kitchen herbs along the long south wall"
	if err := l.SetGroups(groups); err != nil {
		t.Fatal(err)
	}
	if got := titleAt(t, l, "STATE"); got <= wider {
		t.Errorf("SetGroups: STATE at %d, before at %d", got, wider)
	}
}

// The modal asks for a row more per new entry, new group and kept group.
func TestListModalHeightFollowsEntriesAndKeptGroups(t *testing.T) {
	l := groupedList(t, nil)
	before := l.wanted(80, Styles{})
	items := append(samplePlants(), ListItem{ID: "tulip", Cells: []string{"tulip", "resting", "3", ""}, Group: "bulbs"})
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if got := l.wanted(80, Styles{}); got != before+2 {
		t.Errorf("SetItems: %d, want %d", got, before+2)
	}
	groups := append(sampleGroups(), ListGroup{Key: "seeds", Title: "Seed trays", Keep: true}, ListGroup{Key: "bulbs", Keep: true})
	if err := l.SetGroups(groups); err != nil {
		t.Fatal(err)
	}
	if got := l.wanted(80, Styles{}); got != before+3 {
		t.Errorf("SetGroups with Keep: %d, want %d", got, before+3)
	}
}
