package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func dialogCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {
			"d.delete": "Delete plant", "d.leave": "Unsaved changes", "d.unlock": "Unlock the shed",
			"a.delete": "Delete", "a.keep": "Keep", "a.save": "Save", "a.discard": "Discard", "a.edit": "Keep editing",
			"a.yes": "Water", "a.no": "Not now", "a.unlock": "Unlock", "a.cancel": "Cancel",
		},
		i18n.German: {
			"d.delete": "Pflanze löschen", "d.leave": "Ungespeicherte Änderungen", "d.unlock": "Schuppen öffnen",
			"a.delete": "Löschen", "a.keep": "Behalten", "a.save": "Speichern", "a.discard": "Verwerfen",
			"a.edit": "Weiter bearbeiten", "a.yes": "Gießen", "a.no": "Nicht jetzt", "a.unlock": "Öffnen", "a.cancel": "Abbrechen",
		},
	}
}

// The standard questions with invented data: delete, unsaved changes, a confirmation with a default, and an
// input of a passphrase.
func deleteSpec() DialogSpec {
	return DialogSpec{Title: "d.delete", Question: `Delete "basil"?`, Consequence: "Its notes go with it.",
		Answers: []Answer{{Action: "yes", Label: "a.delete"}, {Action: "no", Label: "a.keep"}, {Action: "back", Label: "a.keep"}},
		Danger:  "yes"}
}

func leaveSpec() DialogSpec {
	return DialogSpec{Title: "d.leave", Question: "Save the changes to basil?", Consequence: "Leaving drops them.",
		Answers: []Answer{{Action: "save", Label: "a.save"}, {Action: "discard", Label: "a.discard"}, {Action: "back", Label: "a.edit"}},
		Danger:  "discard"}
}

func confirmSpec() DialogSpec {
	return DialogSpec{Title: "d.delete", Question: "Water every plant now?", Consequence: "The pump runs for a minute.",
		Details: []string{"5 plants", "2 of them are resting"},
		Answers: []Answer{{Action: "yes", Label: "a.yes"}, {Action: "no", Label: "a.no"}}, Default: "yes", Keys: DialogKeys{Enter: "enter"}}
}

func unlockSpec() DialogSpec {
	return DialogSpec{Title: "d.unlock", Question: "Type the passphrase of the shed.", Input: &DialogInput{Masked: true},
		Answers: []Answer{{Action: "enter", Label: "a.unlock"}, {Action: "back", Label: "a.cancel"}}, Default: "enter", Keys: DialogKeys{Enter: "enter"}}
}

func newSampleDialog(t *testing.T, spec DialogSpec, lang i18n.Lang) *Dialog {
	t.Helper()
	keys := sampleKeymap(t, CharsetUnicode)
	d, err := NewDialog(spec, keys, newTranslator(t, lang, dialogCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// sampleScreen is a full background screen of width by height cells with a header and tab line.
func sampleScreen(width, height int) string {
	lines := []string{Pad("Garden  ok", width), Pad("  1 Plants  2 Beds", width)}
	for len(lines) < height {
		lines = append(lines, Pad("  \x1b[31mbasil\x1b[0m  resting", width))
	}
	return strings.Join(lines, "\n")
}

func TestNewDialogRejectsWhatCannotWork(t *testing.T) {
	cases := []struct {
		name string
		spec func() DialogSpec
		want string
	}{
		{"no question", func() DialogSpec { s := deleteSpec(); s.Question = " "; return s }, "no question"},
		{"no consequence", func() DialogSpec { s := deleteSpec(); s.Consequence = ""; return s }, "what follows"},
		{"no answer", func() DialogSpec { s := deleteSpec(); s.Answers, s.Danger = nil, ""; return s }, "no answer"},
		{"unknown answer", func() DialogSpec { s := deleteSpec(); s.Answers[0].Action = "burn"; return s }, `"burn" is no action`},
		{"answer twice", func() DialogSpec { s := deleteSpec(); s.Answers[1].Action = "yes"; return s }, "listed twice"},
		{"no label", func() DialogSpec { s := deleteSpec(); s.Answers[1].Label = ""; return s }, "has no label"},
		{"default no answer", func() DialogSpec { s := confirmSpec(); s.Default = "back"; return s }, "default \"back\" is no answer"},
		{"default without enter", func() DialogSpec { s := confirmSpec(); s.Keys.Enter = ""; return s }, "needs the enter action"},
		{"enter is another answer", func() DialogSpec {
			s := confirmSpec()
			s.Answers = append(s.Answers, Answer{Action: "enter", Label: "a.keep"})
			return s
		}, "besides the default"},
		{"danger no answer", func() DialogSpec { s := deleteSpec(); s.Danger = "discard"; return s }, "danger \"discard\" is no answer"},
		{"danger on enter", func() DialogSpec { s := deleteSpec(); s.Default, s.Keys.Enter = "yes", "enter"; return s }, "enter never destroys"},
		{"letter answer while typing", func() DialogSpec {
			s := unlockSpec()
			s.Answers = append(s.Answers, Answer{Action: "yes", Label: "a.yes"})
			return s
		}, "no key that acts while text is typed"},
		{"masked with value", func() DialogSpec { s := unlockSpec(); s.Input.Value = "kept-secret"; return s }, "masked input starts empty"},
	}
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, dialogCatalogs(), keys)
	for _, c := range cases {
		if _, err := NewDialog(c.spec(), keys, text); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	if _, err := NewDialog(deleteSpec(), keys, nil); err == nil {
		t.Error("a dialog without translator was accepted")
	}
}

// Each standard question answers only to its keys: a stray key neither saves nor destroys.
func TestStandardQuestionsAnswerOnlyToTheirKeys(t *testing.T) {
	cases := []struct {
		name    string
		spec    DialogSpec
		answers map[string]ActionID
		ignored []string
	}{
		{"delete", deleteSpec(), map[string]ActionID{"y": "yes", "n": "no", "esc": "back"}, []string{"enter", "d", "f2", "q"}},
		{"leave", leaveSpec(), map[string]ActionID{"f2": "save", "d": "discard", "esc": "back"}, []string{"y", "n", "enter"}},
		{"confirm", confirmSpec(), map[string]ActionID{"y": "yes", "enter": "yes", "n": "no"}, []string{"d", "f2"}},
	}
	for _, c := range cases {
		d := newSampleDialog(t, c.spec, i18n.English)
		for key, want := range c.answers {
			if got, handled := d.Update(press(key)); got != want || !handled {
				t.Errorf("%s: %s answers %q, %v, want %q", c.name, key, got, handled, want)
			}
		}
		for _, key := range c.ignored {
			if got, handled := d.Update(press(key)); got != "" || handled {
				t.Errorf("%s: %s gave %q, %v; it is no answer", c.name, key, got, handled)
			}
		}
	}
}

func TestDialogStandsOverTheDimmedScreen(t *testing.T) {
	st := looks(t)["dark"]
	d := newSampleDialog(t, deleteSpec(), i18n.English)
	room := Rect{Y: 3, Width: 100, Height: 26}
	view, targets, ok := d.Overlay(st, sampleScreen(100, 30), 100, 30, room)
	if !ok {
		t.Fatal("the dialog does not fit 100x30")
	}
	plain := strings.Split(ansi.Strip(view), "\n")
	if len(plain) != 30 {
		t.Fatalf("%d lines", len(plain))
	}
	top, bottom, left, right := -1, -1, -1, -1
	for i, line := range plain {
		if lipgloss.Width(line) != 100 {
			t.Fatalf("line %d has %d cells", i, lipgloss.Width(line))
		}
		if j := strings.Index(line, "╭"); j >= 0 {
			top, left = i, lipgloss.Width(line[:j])
			right = 100 - left - lipgloss.Width(line[j:strings.Index(line, "╮")+len("╮")])
		}
		if strings.Contains(line, "╰") {
			bottom = i
		}
	}
	if top < room.Y || abs(left-right) > 1 || abs((top-room.Y)-(room.Y+room.Height-1-bottom)) > 1 {
		t.Errorf("the dialog spans rows %d-%d, columns from %d, not centred in its room:\n%s", top, bottom, left, ansi.Strip(view))
	}
	if !strings.HasPrefix(plain[0], "Garden") || !strings.Contains(plain[1], "1 Plants") {
		t.Error("the header or the tab line is covered")
	}
	if strings.Contains(view, "\x1b[31m") {
		t.Error("the screen behind keeps its colours")
	}
	text := strings.Join(plain, "\n")
	order := []string{"⚠ Delete \"basil\"?", "Its notes go with it.", "[ Delete ]", "y Delete"}
	at := 0
	for _, part := range order {
		i := strings.Index(text[at:], part)
		if i < 0 {
			t.Fatalf("%q missing or out of order:\n%s", part, text)
		}
		at += i
	}
	if strings.Count(text, "[ Keep ]") != 1 || !strings.Contains(text, "n/esc Keep") {
		t.Errorf("answers that read the same share one button and one key group:\n%s", text)
	}
	if strings.Contains(text, "> Delete <") {
		t.Error("a question that enter does not answer shows a default")
	}
	for _, tg := range targets {
		if tg.Rect.Y < top || tg.Rect.Y > bottom || tg.Rect.X < left {
			t.Errorf("target %+v lies outside the dialog", tg)
		}
	}
	danger := st.Button(ButtonDanger, "Delete", false)
	if !strings.Contains(view, danger) {
		t.Error("the destroying answer is not the danger button")
	}
	keep := st.Button(ButtonNormal, "Keep", false)
	if !strings.Contains(view, keep) {
		t.Error("the keeping answer is not a normal button")
	}
	{
		c := newSampleDialog(t, confirmSpec(), i18n.English)
		view, _, _ := c.Overlay(st, sampleScreen(100, 30), 100, 30, room)
		if strings.Contains(view, st.Button(ButtonDanger, "Water", false)) || !strings.Contains(ansi.Strip(view), "> Water <") {
			t.Errorf("a question without danger draws one, or its default is not focused:\n%s", ansi.Strip(view))
		}
		if !strings.Contains(ansi.Strip(view), "y/enter Water") {
			t.Errorf("the key line does not name enter beside the default:\n%s", ansi.Strip(view))
		}
	}
}

func abs(n int) int { return max(n, -n) }

// Where the dialog does not fit, the question fills the workspace and its answers stand in the footer, whose
// targets answer it.
func TestDialogTooSmallFillsTheWorkspace(t *testing.T) {
	st := looks(t)["dark"]
	for _, room := range []Rect{{Y: 3, Width: 43, Height: 26}, {Y: 3, Width: 100, Height: 8}} {
		d := newSampleDialog(t, confirmSpec(), i18n.English)
		if _, _, ok := d.Overlay(st, sampleScreen(100, 30), 100, 30, room); ok {
			t.Errorf("the dialog claims to fit %+v", room)
		}
	}
	d := newSampleDialog(t, confirmSpec(), i18n.English)
	page, _ := d.View(st, 40, 8)
	plain := ansi.Strip(page)
	if !strings.Contains(plain, "Water every plant") || !strings.Contains(plain, "pump") || strings.Contains(plain, "[ Water ]") {
		t.Errorf("the page lacks the question or names its answers twice:\n%s", plain)
	}
	if lines := strings.Split(plain, "\n"); len(lines) != 8 || lipgloss.Width(lines[0]) != 40 {
		t.Errorf("the page is not 40x8:\n%s", plain)
	}
	keys := d.keys
	frame := NewFrame(keys, d.text)
	out, targets := frame.Render(st, FrameSpec{Width: 40, Height: 10, Product: "Garden", Workspace: page, Footer: d.Hints(),
		Input: d.Input()})
	if !strings.Contains(ansi.Strip(out), "y/enter Water") {
		t.Fatalf("the footer lacks the answers:\n%s", ansi.Strip(out))
	}
	m := NewMouseMap(targets, true)
	answered := map[ActionID]bool{}
	for _, tg := range targets {
		if answer, ok := d.Click(m.Handle(click(tg.Rect.X, tg.Rect.Y))); ok {
			answered[answer] = true
		}
	}
	if !answered["yes"] || !answered["no"] || len(answered) != 2 {
		t.Errorf("the footer answers %v", answered)
	}
}

// Every button and key of a dialog is a target, and a click on it answers as its key does.
func TestEveryDialogControlHasATargetAndClicksAsItsKey(t *testing.T) {
	st := looks(t)["noneascii"]
	for _, spec := range []DialogSpec{deleteSpec(), leaveSpec(), confirmSpec(), unlockSpec()} {
		d := newSampleDialog(t, spec, i18n.English)
		view, targets, ok := d.Overlay(st, sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27})
		if !ok {
			t.Fatal("the dialog does not fit")
		}
		lines := strings.Split(ansi.Strip(view), "\n")
		covered := func(x, y int) bool {
			for _, tg := range targets {
				r := tg.Rect
				if x >= r.X && x < r.X+r.Width && y == r.Y {
					return true
				}
			}
			return false
		}
		for y, line := range lines {
			for off := 0; strings.Contains(line[off:], "[ "); {
				i := off + strings.Index(line[off:], "[ ")
				if !covered(ansi.StringWidth(line[:i]), y) {
					t.Errorf("%s: button on line %d has no target: %q", spec.Title, y, line)
				}
				off = i + 2
			}
		}
		if spec.Input != nil {
			found := false
			for _, tg := range targets {
				found = found || tg.Item == dialogInputRef
			}
			if !found {
				t.Errorf("%s: the input line has no target", spec.Title)
			}
		}
		m := NewMouseMap(targets, true)
		for _, tg := range targets {
			got := m.Handle(click(tg.Rect.X, tg.Rect.Y))
			answer, handled := d.Click(got)
			if tg.Action == "" {
				continue
			}
			want, _ := d.Update(press(d.keys.Actions()[d.keys.index[tg.Action]].DefaultKeys[0]))
			if !handled || answer != want {
				t.Errorf("%s: a click on %q answers %q, its key %q", spec.Title, tg.Action, answer, want)
			}
		}
	}
}

func TestInputDialogNeverShowsTheSecret(t *testing.T) {
	const canary = "canary-shed-51c2"
	for name, st := range looks(t) {
		d := newSampleDialog(t, unlockSpec(), i18n.English)
		for _, r := range canary {
			if _, handled := d.Update(tea.KeyPressMsg{Code: r, Text: string(r)}); !handled {
				t.Fatalf("typing %q is not taken", r)
			}
		}
		if answer, _ := d.Update(press("enter")); answer != "enter" || d.Value() != canary {
			t.Fatalf("enter gives %q with %q", answer, d.Value())
		}
		d.SetMessage(&Message{Kind: MessageError, Text: "error: " + canary + " is no passphrase of the shed"})
		view, targets, ok := d.Overlay(st, sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27})
		page, pageTargets := d.View(st, 50, 10)
		if !ok {
			t.Fatal("the dialog does not fit")
		}
		for _, out := range []string{view, page} {
			if plain := ansi.Strip(out); strings.Contains(plain, canary) || strings.Contains(plain, "shed-51c2") {
				t.Fatalf("%s shows the secret:\n%s", name, plain)
			}
		}
		if !strings.Contains(ansi.Strip(view), "is no passphrase") {
			t.Errorf("%s: the error does not stand in the dialog:\n%s", name, ansi.Strip(view))
		}
		for _, tg := range append(targets, pageTargets...) {
			if strings.Contains(tg.Item, "canary") {
				t.Fatalf("a target names the secret: %+v", tg)
			}
		}
		if answer, _ := d.Update(press("esc")); answer != "back" {
			t.Errorf("esc gives %q", answer)
		}
		d.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		if view, _, _ := d.Overlay(st, sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27}); strings.Contains(view, "is no passphrase") {
			t.Error("typing does not remove the error")
		}
	}
	d := newSampleDialog(t, unlockSpec(), i18n.English)
	d.Paste("pass\nword\x1b")
	if d.Value() != "password" {
		t.Errorf("a paste gives %q", d.Value())
	}
	d.SetMessage(&Message{Kind: MessageBusy, Text: "checking"})
	d.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if view, _, _ := d.Overlay(looks(t)["dark"], sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27}); !strings.Contains(view, "checking") {
		t.Error("typing removes what is still running")
	}
	d.ClearInput()
	if d.Value() != "" || d.Input() != InputText {
		t.Errorf("ClearInput leaves %q", d.Value())
	}
	if _, handled := d.Update(press("a")); !handled || d.Value() != "a" {
		t.Error("a letter in an input dialog is no text")
	}
	if view, _, _ := d.Overlay(looks(t)["dark"], sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27}); !strings.Contains(view, "passphrase") {
		t.Error("a secret too short to hide in other texts garbles them and shows where its letters stand")
	}
}

func TestDialogReadsWithoutColourAndInGerman(t *testing.T) {
	for _, look := range []string{"noneascii", "mono"} {
		st := looks(t)[look]
		for _, spec := range []DialogSpec{deleteSpec(), unlockSpec()} {
			d := newSampleDialog(t, spec, i18n.German)
			view, _, ok := d.Overlay(st, sampleScreen(80, 24), 80, 24, Rect{Y: 2, Width: 80, Height: 21})
			if !ok {
				t.Fatalf("%s: does not fit 80x24", look)
			}
			noColorSequence(t, look, view)
			plain := ansi.Strip(view)
			if spec.Input == nil && !strings.Contains(plain, "! Delete") && !strings.Contains(plain, "⚠ Delete") {
				t.Errorf("%s: the question lacks its mark:\n%s", look, plain)
			}
			if spec.Input != nil && strings.Contains(plain, "⚠") {
				t.Errorf("%s: an input dialog carries the warning mark", look)
			}
			if spec.Input == nil && (!strings.Contains(plain, "[ Löschen ]") || !strings.Contains(plain, "Pflanze löschen")) {
				t.Errorf("%s: German question:\n%s", look, plain)
			}
			for _, line := range strings.Split(plain, "\n") {
				if lipgloss.Width(line) != 80 {
					t.Fatalf("%s: line of %d cells", look, lipgloss.Width(line))
				}
			}
		}
	}
}

func TestDialogEscapesControlCharacters(t *testing.T) {
	spec := deleteSpec()
	spec.Question = "Delete \x1b[2Jbasil?"
	spec.Details = []string{"note\x07"}
	d := newSampleDialog(t, spec, i18n.English)
	view, _, _ := d.Overlay(looks(t)["noneascii"], sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27})
	if strings.Contains(view, "\x1b[2J") || strings.Contains(view, "\x07") || !strings.Contains(view, `\x1b[2Jbasil`) {
		t.Errorf("control characters reach the terminal:\n%q", view)
	}
}
