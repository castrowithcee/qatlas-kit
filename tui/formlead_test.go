package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func withLead(id TextID) func(*FormSpec) {
	return func(s *FormSpec) {
		withModal(s)
		s.Lead = id
	}
}

// panelInner is what stands between the first and the last bar of a line.
func panelInner(l string) string {
	return l[strings.Index(l, "|")+1 : strings.LastIndex(l, "|")]
}

func plainLines(s string) []string { return strings.Split(ansi.Strip(s), "\n") }

func TestFormLeadStandsUnderTheTitleInModalAndView(t *testing.T) {
	st := looks(t)["noneascii"]
	f := newSampleForm(t, i18n.English, withLead("lead.form"))
	out, _, ok := f.Overlay(st, sampleScreen(120, 40), 120, 40, Rect{Y: 2, Width: 120, Height: 38})
	if !ok {
		t.Fatal("does not fit")
	}
	lines := plainLines(out)
	title, first, last := -1, -1, -1
	for i, l := range lines {
		switch {
		case strings.Contains(l, "Edit plant"):
			title = i
		case strings.Contains(l, "Saving replaces"):
			first = i
		case strings.Contains(l, "are."):
			last = i
		}
	}
	if title < 0 || first != title+1 || last < first {
		t.Fatalf("title at %d, lead at %d..%d:\n%s", title, first, last, out)
	}
	if strings.TrimSpace(panelInner(lines[last+1])) != "" {
		t.Errorf("no blank line after the lead: %q", lines[last+1])
	}
	if !strings.Contains(panelInner(lines[last+2]), "General") {
		t.Errorf("the fields do not follow the blank line: %q", lines[last+2])
	}
	view, _ := f.View(st, 80, 30)
	vl := plainLines(view)
	if !strings.HasPrefix(vl[0], "Saving replaces") || strings.TrimSpace(vl[1]) != "" {
		t.Errorf("View does not start with the lead and a blank line:\n%s", view)
	}
}

func TestFormLeadStaysInViewOnEveryFocus(t *testing.T) {
	st := looks(t)["noneascii"]
	const lead = "Saving replaces the stored plant."
	var spec FormSpec
	withButtons(&spec)
	f := newSampleForm(t, i18n.English, withLead("lead.form"))
	room := Rect{Y: 2, Width: 120, Height: 38}
	start := focusPlace(f)
	buttons := map[ActionID]bool{}
	for step := 0; step < 40; step++ {
		place := focusPlace(f)
		if a, ok := f.FocusedButton(); ok {
			buttons[a] = true
		}
		view, _ := f.View(st, 100, 40)
		if !strings.Contains(ansi.Strip(view), lead) {
			t.Fatalf("View: lead missing with focus on %s:\n%s", place, view)
		}
		out, _, ok := f.Overlay(st, sampleScreen(120, 40), 120, 40, room)
		if !ok {
			t.Fatalf("Overlay does not fit with focus on %s", place)
		}
		if !strings.Contains(ansi.Strip(out), lead) {
			t.Fatalf("Overlay: lead missing with focus on %s:\n%s", place, out)
		}
		f.Update(press("tab"))
		if focusPlace(f) == start {
			if len(buttons) != len(spec.Buttons) {
				t.Fatalf("%d of %d buttons had the focus", len(buttons), len(spec.Buttons))
			}
			return
		}
	}
	t.Fatal("tab never returned to the start")
}

func TestFormLeadWrapsAndIsNeverCut(t *testing.T) {
	st := looks(t)["noneascii"]
	f := newSampleForm(t, i18n.English, withLead("lead.long"))
	view, _ := f.View(st, 40, 40)
	plain := ansi.Strip(view)
	if strings.Contains(plain, "…") {
		t.Errorf("cut lead:\n%s", plain)
	}
	text := strings.Join(strings.Fields(strings.Join(plainLines(view)[:5], " ")), " ")
	want := "This sentence is deliberately long so that it has to wrap onto several lines of a narrow form without losing a single word of it."
	if !strings.Contains(text, want) {
		t.Errorf("lead text lost or too many rows:\n%s", plain)
	}
	if strings.TrimSpace(plainLines(view)[0]) == want {
		t.Error("lead not wrapped")
	}
}

func TestFormLeadIsEscaped(t *testing.T) {
	f := newSampleForm(t, i18n.English, withLead("lead.ctrl"))
	view, _ := f.View(looks(t)["noneascii"], 60, 30)
	if strings.Contains(view, "\x1b") {
		t.Errorf("a raw escape byte reaches the screen:\n%q", view)
	}
	// The masked form is what Escape makes of the catalog text, on one line.
	masked := Escape("Careful\x1b[31m red\nsecond")
	if !strings.Contains(plainLines(view)[0], masked) {
		t.Errorf("masked lead %q not in the first row:\n%s", masked, view)
	}
}

func TestFormSetLeadKeepsValuesFocusAndOpenList(t *testing.T) {
	f := newSampleForm(t, i18n.English, withLead("lead.form"))
	f.Focus("kind")
	f.Update(press("enter"))
	f.Update(press("down"))
	if !f.open {
		t.Fatal("list not open")
	}
	pick, focus := f.pick, f.focus
	before := f.Values()
	f.SetLead("lead.long")
	if !f.open || f.pick != pick || f.focus != focus || f.Focused() != "kind" {
		t.Error("focus or open list changed")
	}
	if got := f.Values(); len(got) != len(before) || got["kind"].Choice != before["kind"].Choice || got["name"].Text != before["name"].Text {
		t.Errorf("values changed: %v vs %v", got, before)
	}
	view, _ := f.View(looks(t)["noneascii"], 100, 40)
	if !strings.Contains(ansi.Strip(view), "deliberately long") || strings.Contains(ansi.Strip(view), "Saving replaces") {
		t.Errorf("lead not replaced:\n%s", view)
	}
	f.SetLead("")
	view, _ = f.View(looks(t)["noneascii"], 100, 40)
	if strings.Contains(ansi.Strip(view), "deliberately") {
		t.Error("lead not removed")
	}
}

func TestFormLeadCountsForTheFallbackRule(t *testing.T) {
	st := looks(t)["noneascii"]
	room := func(h int) Rect { return Rect{Width: 120, Height: h} }
	plain := newSampleForm(t, i18n.English, withModal)
	led := newSampleForm(t, i18n.English, withLead("lead.form"))
	rows := plain.fieldRows(st, dialogMaxInner)
	if led.fieldRows(st, dialogMaxInner) < rows+3 {
		t.Fatalf("lead adds %d rows, want at least 3", led.fieldRows(st, dialogMaxInner)-rows)
	}
	h := rows + 4
	if _, _, ok := plain.Overlay(st, sampleScreen(120, h), 120, h, room(h)); !ok {
		t.Fatal("the form without lead must fit")
	}
	if _, _, ok := led.Overlay(st, sampleScreen(120, h), 120, h, room(h)); ok {
		t.Error("the lead must push the modal over room")
	}
}

func TestGoldenFormLead(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		w, h       int
	}{
		{"formlead.narrow.golden", "noneascii", 40, 24},
		{"formlead.wide.golden", "noneascii", 120, 20},
		{"formlead.wide.dark.golden", "dark", 120, 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSampleForm(t, i18n.English, withLead("lead.form"))
			if tt.w < 100 {
				f.SetLead("lead.long")
			}
			out, _ := f.View(looks(t)[tt.look], tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}
