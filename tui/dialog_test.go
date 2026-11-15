package tui

import (
	"fmt"
	"slices"
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
			"d.delete": "Delete plant", "d.leave": "Unsaved changes", "d.unlock": "Unlock the shed", "d.reset": "Reset service", "d.done": "Service reset", "a.reset": "Reset", "a.close": "Close",
			"a.delete": "Delete", "a.keep": "Keep", "a.save": "Save", "a.discard": "Discard", "a.edit": "Keep editing",
			"a.yes": "Water", "a.no": "Not now", "a.unlock": "Unlock", "a.cancel": "Cancel",
		},
		i18n.German: {
			"d.delete": "Pflanze löschen", "d.leave": "Ungespeicherte Änderungen", "d.unlock": "Schuppen öffnen", "d.reset": "Dienst zurücksetzen", "d.done": "Dienst zurückgesetzt", "a.reset": "Zurücksetzen", "a.close": "Schließen",
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
		Details: []DialogLine{{Text: "5 plants"}, {Text: "2 of them are resting"}},
		Answers: []Answer{{Action: "yes", Label: "a.yes"}, {Action: "no", Label: "a.no"}}, Default: "yes", Keys: DialogKeys{Enter: "enter"}}
}

// previewSpec asks with the commands it would run behind a prompt sign.
func previewSpec() DialogSpec {
	return DialogSpec{Title: "d.reset", Question: "Reset the demo service?", Consequence: "Its state is erased and cannot be restored.",
		Irreversible: true,
		Details: []DialogLine{
			{Prefix: "$ ", Text: "example-tool stop demo", Style: StyleHint},
			{Prefix: "$ ", Text: "example-tool reset demo --config /srv/example/demo/very-long-directory-name/settings.toml --verbose", Style: StyleHint},
		},
		Answers: []Answer{{Action: "yes", Label: "a.reset"}, {Action: "no", Label: "a.cancel"}}, Danger: "yes"}
}

// noticeSpec is a calm message that carries the commands which ran.
func noticeSpec() DialogSpec {
	return DialogSpec{Title: "d.done", Question: "The demo service was reset.", Notice: true,
		Details: []DialogLine{
			{Prefix: "$ ", Text: "example-tool stop demo", Style: StyleHint},
			{Prefix: "$ ", Text: "example-tool reset demo --config /srv/example/demo/very-long-directory-name/settings.toml", Style: StyleHint},
		},
		Answers: []Answer{{Action: "no", Label: "a.close"}}, Default: "no", Keys: DialogKeys{Enter: "enter"}}
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
		{"detail style", func() DialogSpec { s := confirmSpec(); s.Details[0].Style = StyleOK; return s }, "style other than"},
		{"fail detail in question", func() DialogSpec { s := confirmSpec(); s.Details[0].Style = StyleFail; return s }, "is red"},
		{"fail detail in input dialog", func() DialogSpec {
			s := unlockSpec()
			s.Details = []DialogLine{{Text: "x", Style: StyleFail}}
			return s
		}, "is red"},
		{"notice without detail", func() DialogSpec { s := noticeSpec(); s.Details = nil; return s }, "needs a detail line"},
		{"notice with danger", func() DialogSpec { s := noticeSpec(); s.Danger = "yes"; return s }, "no danger"},
		{"notice with input", func() DialogSpec { s := noticeSpec(); s.Input = &DialogInput{}; return s }, "no input"},
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
	if d.Value() != "pass word" {
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
	spec.Details = []DialogLine{{Prefix: "\x1b[2J$ ", Text: "note\x07"}}
	d := newSampleDialog(t, spec, i18n.English)
	view, _, _ := d.Overlay(looks(t)["noneascii"], sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27})
	if strings.Contains(view, "\x1b[2J") || strings.Contains(view, "\x07") || !strings.Contains(view, `\x1b[2Jbasil`) {
		t.Errorf("control characters reach the terminal:\n%q", view)
	}
}

func TestDialogDetailsWrapUnderTheText(t *testing.T) {
	spec := previewSpec()
	spec.Details = []DialogLine{{Prefix: "$ ", Text: "run " + strings.Repeat("x", 80) + " end", Style: StyleHint}}
	d := newSampleDialog(t, spec, i18n.English)
	lines, _ := d.body(40, looks(t)["noneascii"])
	var got []string
	for _, l := range lines {
		got = append(got, ansi.Strip(l))
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "$ run") || strings.Count(joined, "x") != 80 || !strings.Contains(joined, "end") {
		t.Fatalf("text lost or not wrapped:\n%s", joined)
	}
	for _, l := range got {
		if lipgloss.Width(l) > 40 {
			t.Errorf("row of %d cells: %q", lipgloss.Width(l), l)
		}
	}
	if !strings.HasPrefix(got[len(got)-1], "  ") {
		t.Errorf("continuation not indented under the text: %q", got[len(got)-1])
	}
}

func TestDialogMasksSecretInDetails(t *testing.T) {
	spec := unlockSpec()
	spec.Details = []DialogLine{{Prefix: "canary-shed-51c2> ", Text: "uses canary-shed-51c2"}}
	d := newSampleDialog(t, spec, i18n.English)
	for _, r := range "canary-shed-51c2" {
		d.Update(press(string(r)))
	}
	view, _, _ := d.Overlay(looks(t)["noneascii"], sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27})
	if strings.Contains(view, "canary-shed") {
		t.Errorf("secret reaches the detail lines:\n%s", view)
	}
}

func TestGoldenDialogDetails(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		spec       func() DialogSpec
		w          int
	}{
		{"dialog.preview.narrow.golden", "noneascii", previewSpec, 46},
		{"dialog.preview.wide.golden", "noneascii", previewSpec, 100},
		{"dialog.preview.wide.dark.golden", "dark", previewSpec, 100},
		{"dialog.notice.narrow.golden", "noneascii", noticeSpec, 46},
		{"dialog.notice.wide.golden", "noneascii", noticeSpec, 100},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := newSampleDialog(t, tt.spec(), i18n.English)
			view, _, ok := d.Overlay(looks(t)[tt.look], sampleScreen(tt.w, 18), tt.w, 18, Rect{Y: 2, Width: tt.w, Height: 16})
			if !ok {
				t.Fatal("dialog does not fit")
			}
			checkGolden(t, tt.name, view+"\n")
		})
	}
}

func TestWrapWordsBreaksOnlyAtSpaces(t *testing.T) {
	// Tokens that fit a row stay whole, hyphens included.
	got := wrapWords("start --verbose /a/very-long-path --flag", 20)
	want := []string{"start --verbose", "/a/very-long-path", "--flag"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	// A word wider than the row is split by cell width and appears completely.
	long := strings.Repeat("宽x", 30)
	rows := wrapWords("go "+long, 10)
	if len(rows) < 3 || rows[0] != "go" || strings.Join(rows[1:], "") != long {
		t.Errorf("long word: %q", rows)
	}
	for _, l := range rows {
		if lipgloss.Width(l) > 10 {
			t.Errorf("row of %d cells: %q", lipgloss.Width(l), l)
		}
	}
	hyphens := wrapWords("a "+strings.Repeat("x-", 15), 10)
	if strings.Join(hyphens[1:], "") != strings.Repeat("x-", 15) {
		t.Errorf("hyphenated long word: %q", hyphens)
	}
}

func TestNoticeAcceptsFailDetail(t *testing.T) {
	spec := noticeSpec()
	spec.Details[0].Style = StyleFail
	keys := sampleKeymap(t, CharsetUnicode)
	if _, err := NewDialog(spec, keys, newTranslator(t, i18n.English, dialogCatalogs(), keys)); err != nil {
		t.Fatal(err)
	}
}

func plainInputSpec() DialogSpec {
	spec := unlockSpec()
	spec.Input = &DialogInput{Value: "one two"}
	return spec
}

func TestInputDialogEditsWithTheLineKeys(t *testing.T) {
	d := newSampleDialog(t, plainInputSpec(), i18n.English)
	keys := func(names ...string) {
		t.Helper()
		for _, n := range names {
			if answer, handled := d.Update(lineKey(n)); answer != "" || !handled {
				t.Fatalf("%s gives answer %q handled %v", n, answer, handled)
			}
		}
	}
	keys("ctrl+a", "delete", "alt+d")
	if d.Value() != " two" {
		t.Fatalf("the start edits give %q", d.Value())
	}
	keys("end", "ctrl+w", "ctrl+left", "ctrl+right", "left", "left", "backspace", "ctrl+f", "ctrl+b", "ctrl+d")
	if d.Value() != "" {
		t.Fatalf("the end edits give %q", d.Value())
	}
	d.Paste("one two")
	keys("home", "alt+right", "alt+f", "ctrl+k")
	if d.Value() != "one two" {
		t.Fatalf("a jump to the end and ctrl+k give %q", d.Value())
	}
	keys("alt+left", "ctrl+u")
	if d.Value() != "two" {
		t.Fatalf("ctrl+u gives %q", d.Value())
	}
	if _, handled := d.Update(press("ctrl+z")); handled {
		t.Error("a key that edits nothing is taken")
	}
	if !d.Paste("\x1b[0m x\r\n") || d.Value() != "[0m xtwo" {
		t.Errorf("a paste at the start gives %q", d.Value())
	}
}

func TestInputDialogErrorGoesOnEditingOnly(t *testing.T) {
	d := newSampleDialog(t, plainInputSpec(), i18n.English)
	st := looks(t)["dark"]
	shows := func() bool {
		view, _, _ := d.Overlay(st, sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27})
		return strings.Contains(view, "wrong name")
	}
	fail := func() { d.SetMessage(&Message{Kind: MessageError, Text: "wrong name"}) }
	fail()
	for _, n := range []string{"left", "home", "ctrl+e", "alt+b", "right", "ctrl+left"} {
		d.Update(lineKey(n))
		if !shows() {
			t.Fatalf("%s removes the error", n)
		}
	}
	for _, n := range []string{"backspace", "ctrl+w", "ctrl+u", "ctrl+k"} {
		d = newSampleDialog(t, plainInputSpec(), i18n.English)
		fail()
		d.Update(lineKey("home"))
		if n != "ctrl+k" {
			d.Update(lineKey("end"))
		}
		d.Update(lineKey(n))
		if shows() {
			t.Errorf("%s keeps the error", n)
		}
	}
	d = newSampleDialog(t, plainInputSpec(), i18n.English)
	fail()
	d.Paste("x")
	if shows() {
		t.Error("a paste keeps the error")
	}
}

func TestMaskedInputDialogEditsWithoutShowingText(t *testing.T) {
	const secret = "ab cd canary"
	for name, st := range looks(t) {
		d := newSampleDialog(t, unlockSpec(), i18n.English)
		d.Paste(secret + "\n")
		if d.Value() != secret {
			t.Fatalf("a paste with a line break at the end gives %q", d.Value())
		}
		check := func(step string) {
			t.Helper()
			view, targets, _ := d.Overlay(st, sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27})
			page, pageTargets := d.View(st, 50, 10)
			for _, out := range []string{view, page} {
				plain := ansi.Strip(out)
				if strings.Contains(plain, "canary") || strings.Contains(plain, "ab cd") {
					t.Fatalf("%s: %s shows the secret:\n%s", name, step, plain)
				}
			}
			for _, tg := range append(targets, pageTargets...) {
				if strings.Contains(tg.Item, "canary") || strings.Contains(tg.Item, "ab") {
					t.Fatalf("%s: %s: a target names the secret: %+v", name, step, tg)
				}
			}
		}
		check("paste")
		d.Update(lineKey("ctrl+left"))
		d.Update(lineKey("alt+left"))
		check("jump")
		d.Update(lineKey("alt+b"))
		if d.input.cursor != 0 {
			t.Errorf("a word jump left stops at %d", d.input.cursor)
		}
		d.Update(lineKey("end"))
		d.Update(lineKey("ctrl+w"))
		if d.Value() != "" {
			t.Errorf("ctrl+w leaves %q", d.Value())
		}
		d.Paste("one two")
		d.Update(lineKey("home"))
		d.Update(lineKey("right"))
		d.Update(lineKey("alt+d"))
		if d.Value() != "o" {
			t.Errorf("alt+d leaves %q", d.Value())
		}
		check("delete")
	}
}

func TestSecondWayOfAnAnswerWinsBeforeEditing(t *testing.T) {
	second := func(a Action) Action { a.SecondWayOf = "back"; return a }
	acts := append(sampleActions(), second(Action{ID: "wipe", Level: LevelCtrl, DefaultKeys: []string{"alt+backspace"}, Help: "sample.wipe"}))
	keys, err := NewKeymap(acts, CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	spec := plainInputSpec()
	spec.Answers = []Answer{{Action: "wipe", Label: "a.cancel"}, {Action: "enter", Label: "a.unlock"}}
	d, err := NewDialog(spec, keys, newTranslator(t, i18n.English, dialogCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	if answer, handled := d.Update(lineKey("alt+backspace")); answer != "wipe" || !handled || d.Value() != "one two" {
		t.Errorf("the second way gives %q, handled %v, with %q", answer, handled, d.Value())
	}
	if _, err := NewKeymap(append(sampleActions(), Action{ID: "bad", Level: LevelCtrl, DefaultKeys: []string{"alt+delete"}, Help: "sample.bad"}), CharsetUnicode); err == nil {
		t.Error("alt+delete is accepted as the key of an action")
	}
}

// longDetailsSpec is a question whose n details do not fit a small room, with every scroll key.
func longDetailsSpec(n int) DialogSpec {
	s := confirmSpec()
	s.Details = nil
	for i := 1; i <= n; i++ {
		s.Details = append(s.Details, DialogLine{Prefix: "- ", Text: fmt.Sprintf("plant %02d needs water", i)})
	}
	s.Keys = DialogKeys{Enter: "enter", Up: "up", Down: "down", PageUp: "pageup", PageDown: "pagedown", First: "first", Last: "last"}
	return s
}

func dialogKey(d *Dialog, names ...string) {
	for _, n := range names {
		var msg tea.KeyPressMsg
		switch n {
		case "pgup":
			msg = tea.KeyPressMsg{Code: tea.KeyPgUp}
		case "pgdown":
			msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
		case "home":
			msg = tea.KeyPressMsg{Code: tea.KeyHome}
		case "end":
			msg = tea.KeyPressMsg{Code: tea.KeyEnd}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		default:
			msg = press(n)
		}
		d.Update(msg)
	}
}

func overlayText(t *testing.T, d *Dialog, height int) (string, []Target, bool) {
	t.Helper()
	view, targets, ok := d.Overlay(looks(t)["noneascii"], sampleScreen(100, 30), 100, 30, Rect{Y: 2, Width: 100, Height: height})
	return ansi.Strip(view), targets, ok
}

// Details that do not fit scroll between the consequence and the answers; every detail is reachable, the
// position is right and the keys stop at both ends.
func TestDialogScrollsLongDetails(t *testing.T) {
	d := newSampleDialog(t, longDetailsSpec(12), i18n.English)
	seen := map[int]bool{}
	for step := 0; step < 20; step++ {
		text, _, ok := overlayText(t, d, 14)
		if !ok {
			t.Fatal("the dialog gives up although its details can scroll")
		}
		for _, part := range []string{"Water every plant now?", "The pump runs for a minute.", "> Water <", "[ Not now ]", "y/enter Water", "n Not now"} {
			if !strings.Contains(text, part) {
				t.Fatalf("step %d: %q is missing:\n%s", step, part, text)
			}
		}
		shown := 0
		for i := 1; i <= 12; i++ {
			if strings.Contains(text, fmt.Sprintf("plant %02d ", i)) {
				seen[i] = true
				shown++
			}
		}
		first := d.scroll.offset + 1
		if want := fmt.Sprintf("lines %d-%d of 12", first, first+shown-1); !strings.Contains(text, want) {
			t.Fatalf("step %d: position %q is not right:\n%s", step, want, text)
		}
		dialogKey(d, "down")
	}
	if len(seen) != 12 {
		t.Errorf("only %d of 12 details were reachable", len(seen))
	}
	if d.scroll.offset != d.scroll.last {
		t.Errorf("down went past the end: %d of %d", d.scroll.offset, d.scroll.last)
	}
	dialogKey(d, "up")
	if d.scroll.offset != d.scroll.last-1 {
		t.Errorf("up from the end is at %d", d.scroll.offset)
	}
	dialogKey(d, "home")
	if d.scroll.offset != 0 {
		t.Errorf("first is at %d", d.scroll.offset)
	}
	dialogKey(d, "up")
	if d.scroll.offset != 0 {
		t.Errorf("up went above the top: %d", d.scroll.offset)
	}
	dialogKey(d, "pgdown")
	if want := d.scroll.page - 1; d.scroll.offset != want {
		t.Errorf("a page moved to %d, want %d", d.scroll.offset, want)
	}
	dialogKey(d, "end")
	if d.scroll.offset != d.scroll.last {
		t.Errorf("last is at %d of %d", d.scroll.offset, d.scroll.last)
	}
	dialogKey(d, "pgup")
	if want := d.scroll.last - (d.scroll.page - 1); d.scroll.offset != want {
		t.Errorf("a page up moved to %d, want %d", d.scroll.offset, want)
	}
	dialogKey(d, "pgdown", "pgdown", "pgdown")
	if d.scroll.offset != d.scroll.last {
		t.Errorf("page down went past the end: %d", d.scroll.offset)
	}
}

// A room the details no longer fill clamps the position, and a dialog whose details fit draws no position, no
// scroll hint and no wheel target.
func TestDialogClampsPositionWhenRoomGrows(t *testing.T) {
	d := newSampleDialog(t, longDetailsSpec(12), i18n.English)
	overlayText(t, d, 14)
	dialogKey(d, "end")
	last := d.scroll.last
	text, targets, ok := overlayText(t, d, 18)
	if !ok || d.scroll.offset >= last || d.scroll.offset != d.scroll.last {
		t.Errorf("the position %d was not clamped to %d (was %d)", d.scroll.offset, d.scroll.last, last)
	}
	if !strings.Contains(text, "plant 12 ") {
		t.Errorf("the end is not in view:\n%s", text)
	}
	text, targets, ok = overlayText(t, d, 28)
	if !ok || strings.Contains(text, "lines ") || d.scroll.offset != 0 || !strings.Contains(text, "plant 01 ") || !strings.Contains(text, "plant 12 ") {
		t.Errorf("details that fit still scroll:\n%s", text)
	}
	for _, tg := range targets {
		if tg.Zone == ZoneDetail {
			t.Error("a dialog whose details fit has a wheel target")
		}
	}
	if slices.ContainsFunc(d.Hints(), func(h Hint) bool { return h.Action == "up" || h.Action == "last" }) {
		t.Error("a dialog whose details fit names the scroll keys")
	}
	plain := newSampleDialog(t, func() DialogSpec { s := longDetailsSpec(12); s.Keys = DialogKeys{Enter: "enter"}; return s }(), i18n.English)
	a, _, _ := plain.Overlay(looks(t)["dark"], sampleScreen(100, 30), 100, 30, Rect{Y: 2, Width: 100, Height: 28})
	b, _, _ := d.Overlay(looks(t)["dark"], sampleScreen(100, 30), 100, 30, Rect{Y: 2, Width: 100, Height: 28})
	if a != b {
		t.Error("scroll keys change a dialog that fits")
	}
}

// The overlay gives up only where not even three detail lines and their position fit.
func TestDialogOverlayNeedsThreeDetailLines(t *testing.T) {
	d := newSampleDialog(t, longDetailsSpec(12), i18n.English)
	if _, _, ok := overlayText(t, d, 11); ok {
		t.Error("the dialog claims to fit with two detail lines")
	}
	text, _, ok := overlayText(t, d, 12)
	if !ok {
		t.Fatal("the dialog gives up with three detail lines")
	}
	if n := strings.Count(text, " needs water"); n != 3 || !strings.Contains(text, "lines 1-3 of 12") {
		t.Errorf("%d detail lines shown:\n%s", n, text)
	}
}

// View scrolls the details in the workspace and names the scroll keys for the footer.
func TestDialogViewScrollsDetails(t *testing.T) {
	st := looks(t)["noneascii"]
	d := newSampleDialog(t, longDetailsSpec(12), i18n.English)
	page, targets := d.View(st, 40, 10)
	plain := ansi.Strip(page)
	if lines := strings.Split(plain, "\n"); len(lines) != 10 || lipgloss.Width(lines[0]) != 40 {
		t.Fatalf("the page is not 40x10:\n%s", plain)
	}
	for _, part := range []string{"Water every plant now?", "The pump runs for a minute.", "plant 01 ", "lines 1-6 of 12"} {
		if !strings.Contains(plain, part) {
			t.Errorf("%q is missing:\n%s", part, plain)
		}
	}
	if !slices.ContainsFunc(d.Hints(), func(h Hint) bool { return h.Action == "down" }) || !slices.ContainsFunc(d.Hints(), func(h Hint) bool { return h.Action == "yes" }) {
		t.Errorf("the hints of a scrolling dialog are %+v", d.Hints())
	}
	if !slices.ContainsFunc(targets, func(tg Target) bool { return tg.Zone == ZoneDetail }) {
		t.Error("no wheel target")
	}
	dialogKey(d, "end")
	plain = ansi.Strip(func() string { p, _ := d.View(st, 40, 10); return p }())
	if !strings.Contains(plain, "plant 12 ") || !strings.Contains(plain, "lines 7-12 of 12") {
		t.Errorf("the end is not in view:\n%s", plain)
	}
}

// The key line names the scroll keys while the details scroll, behind the answers, and the footer keeps every
// answer where it is short.
func TestDialogHintsNameScrollKeysAfterTheAnswers(t *testing.T) {
	d := newSampleDialog(t, longDetailsSpec(12), i18n.English)
	if slices.ContainsFunc(d.Hints(), func(h Hint) bool { return h.Action == "down" }) {
		t.Error("hints name scroll keys before anything scrolled")
	}
	overlayText(t, d, 14)
	hints := d.Hints()
	last := -1
	for i, h := range hints {
		if h.Kind == HintAnswer {
			last = i
		}
	}
	if len(hints) == 0 || hints[last].Action == "" || !slices.ContainsFunc(hints[last+1:], func(h Hint) bool { return h.Action == "last" }) {
		t.Errorf("hints %+v", hints)
	}
	st := looks(t)["noneascii"]
	for _, w := range []int{80, 40, 30} {
		line, _ := st.footer(d.keys.Hints(d.Hints(), d.focus(), d.text), "", w)
		plain := ansi.Strip(line)
		for _, answer := range []string{"y", "n"} {
			if !strings.Contains(plain, answer+" ") && !strings.Contains(plain, answer+"/") {
				t.Errorf("width %d lost the answer %q: %q", w, answer, plain)
			}
		}
	}
}

// The wheel over the dialog scrolls three lines a notch; beside it, it does not.
func TestDialogWheelScrollsOverTheDialogOnly(t *testing.T) {
	d := newSampleDialog(t, longDetailsSpec(12), i18n.English)
	_, targets, ok := overlayText(t, d, 14)
	if !ok {
		t.Fatal("no overlay")
	}
	m := NewMouseMap(targets, true)
	wheel := func(x, y int, b tea.MouseButton) {
		if r := m.Handle(tea.MouseWheelMsg{X: x, Y: y, Button: b}); r.Zone == ZoneDetail {
			d.Wheel(r.WheelSteps)
		}
	}
	box := Rect{}
	for _, tg := range targets {
		if tg.Zone == ZoneDetail {
			box = tg.Rect
		}
	}
	if box.Width == 0 {
		t.Fatal("no wheel target")
	}
	wheel(box.X+2, box.Y+3, tea.MouseWheelDown)
	if d.scroll.offset != WheelTextLines {
		t.Errorf("a notch scrolled to %d", d.scroll.offset)
	}
	wheel(0, 0, tea.MouseWheelDown)
	wheel(box.X-1, box.Y+3, tea.MouseWheelDown)
	wheel(box.X+box.Width, box.Y+3, tea.MouseWheelDown)
	wheel(box.X+2, box.Y+box.Height, tea.MouseWheelDown)
	if d.scroll.offset != WheelTextLines {
		t.Errorf("the wheel beside the dialog scrolled to %d", d.scroll.offset)
	}
	wheel(box.X+2, box.Y, tea.MouseWheelUp)
	wheel(box.X+2, box.Y, tea.MouseWheelUp)
	if d.scroll.offset != 0 {
		t.Errorf("the wheel went above the top: %d", d.scroll.offset)
	}
	for range 10 {
		wheel(box.X+2, box.Y, tea.MouseWheelDown)
	}
	if d.scroll.offset != d.scroll.last {
		t.Errorf("the wheel went past the end: %d of %d", d.scroll.offset, d.scroll.last)
	}
	// A click on a button still answers, and a click on the free area of the dialog does nothing.
	for _, tg := range targets {
		if tg.Action == "yes" {
			if a, ok := d.Click(m.Handle(click(tg.Rect.X, tg.Rect.Y))); !ok || a != "yes" {
				t.Errorf("a click on the button gave %q, %v", a, ok)
			}
		}
	}
	if a, ok := d.Click(m.Handle(click(box.X+1, box.Y+1))); ok || a != "" {
		t.Errorf("a click on the details gave %q, %v", a, ok)
	}
}

// With an input, the scroll keys scroll the details and never edit the text; first and last stay with the text
// line, and the typed text is never a scroll key.
func TestDialogWithInputKeepsTypingAndScrolling(t *testing.T) {
	s := unlockSpec()
	s.Details = longDetailsSpec(12).Details
	s.Keys.Up, s.Keys.Down, s.Keys.PageUp, s.Keys.PageDown = "up", "down", "pageup", "pagedown"
	s.Keys.First, s.Keys.Last = "first", "last"
	d := newSampleDialog(t, s, i18n.English)
	if _, _, ok := overlayText(t, d, 16); !ok {
		t.Fatal("no overlay")
	}
	for _, r := range "hunter" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	dialogKey(d, "down", "down")
	if d.scroll.offset != 2 || d.Value() != "hunter" {
		t.Errorf("offset %d, value %q", d.scroll.offset, d.Value())
	}
	for _, name := range []string{"home", "end"} {
		if _, handled := d.Update(map[string]tea.KeyPressMsg{"home": {Code: tea.KeyHome}, "end": {Code: tea.KeyEnd}}[name]); !handled {
			t.Errorf("%s does not reach the text line", name)
		}
	}
	if d.scroll.offset != 2 {
		t.Errorf("home or end scrolled to %d", d.scroll.offset)
	}
	dialogKey(d, "pgdown")
	if d.scroll.offset <= 2 {
		t.Errorf("a page did not scroll: %d", d.scroll.offset)
	}
	if slices.ContainsFunc(d.Hints(), func(h Hint) bool { return h.Action == "first" || h.Action == "last" }) {
		t.Error("an input dialog names first and last as scroll keys")
	}
	d.Update(press("j"))
	if d.Value() != "hunterj" {
		t.Errorf("a letter did not type: %q", d.Value())
	}
}

// Control characters and masked values stay masked in every scroll position.
func TestDialogScrollKeepsDetailsMasked(t *testing.T) {
	s := unlockSpec()
	s.Details = nil
	for i := 0; i < 8; i++ {
		s.Details = append(s.Details, DialogLine{Text: fmt.Sprintf("row %d \x1b[31mred\x07 with hunter22 inside", i)})
	}
	s.Keys.Down, s.Keys.Up = "down", "up"
	d := newSampleDialog(t, s, i18n.English)
	for _, r := range "hunter22" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	for step := 0; step < 10; step++ {
		view, _, ok := d.Overlay(looks(t)["noneascii"], sampleScreen(100, 30), 100, 30, Rect{Y: 2, Width: 100, Height: 14})
		if !ok {
			t.Fatal("no overlay")
		}
		if strings.ContainsAny(view, "\x1b\x07") || strings.Contains(view, "hunter22") {
			t.Fatalf("step %d leaks a control character or the secret:\n%q", step, view)
		}
		dialogKey(d, "down")
	}
	if d.scroll.last == 0 || !strings.Contains(fmt.Sprint(d.parts(50, looks(t)["noneascii"]).details), "***") {
		t.Error("the details did not scroll or lost the mask")
	}
}

func TestNewDialogRejectsBadScrollKeys(t *testing.T) {
	cases := []struct {
		name string
		edit func(*DialogSpec)
		want string
	}{
		{"unknown", func(s *DialogSpec) { s.Keys.Up = "fly" }, `up key "fly" is no action`},
		{"twice", func(s *DialogSpec) { s.Keys.Down = "up" }, `also the up key`},
		{"answer", func(s *DialogSpec) { s.Keys.PageUp = "yes" }, "also an answer or the enter key"},
		{"enter", func(s *DialogSpec) { s.Keys.Last = "enter" }, "also an answer or the enter key"},
		{"typing", func(s *DialogSpec) {
			*s = unlockSpec()
			s.Keys.Up = "vim.up"
		}, "no key that acts while text is typed"},
	}
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, dialogCatalogs(), keys)
	for _, c := range cases {
		s := longDetailsSpec(3)
		c.edit(&s)
		if _, err := NewDialog(s, keys, text); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	if _, err := NewDialog(longDetailsSpec(3), keys, text); err != nil {
		t.Errorf("valid scroll keys refused: %v", err)
	}
}
