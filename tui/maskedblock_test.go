package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// blockLines is an invented key; none of it is a real secret.
var blockLines = []string{
	"-----BEGIN EXAMPLE BLOCK-----",
	"AAAAexampleexampleexampleexampleexampleexampleexample",
	"BBBBinventedinventedinventedinventedinventedinvented==",
	"-----END EXAMPLE BLOCK-----",
}

func blockKey() string { return strings.Join(blockLines, "\n") + "\n" }

func blockCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {"f.name": "name", "f.key": "private key", "b.save": "save"},
		i18n.German:  {"f.name": "Name", "f.key": "Privater Schlüssel", "b.save": "Speichern"},
	}
}

func newBlockForm(t *testing.T, lang i18n.Lang, edit func(*FormSpec)) *Form {
	t.Helper()
	spec := FormSpec{
		Fields: []FormField{
			{ID: "name", Kind: FieldText, Label: "f.name", Value: FormValue{Text: "server"}},
			{ID: "key", Kind: FieldMaskedBlock, Label: "f.key"},
			{ID: "after", Kind: FieldText, Label: "f.name"},
		},
		Keys: sampleFormKeys,
	}
	if edit != nil {
		edit(&spec)
	}
	keys := sampleKeymap(t, CharsetUnicode)
	f, err := NewForm(spec, keys, newTranslator(t, lang, blockCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	f.Focus("key")
	return f
}

func typeBlock(f *Form, text string) {
	for _, r := range text {
		if r == '\n' {
			f.Update(keyOf("enter"))
			continue
		}
		f.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func blockValue(f *Form) string { return f.Values()["key"].Text }

func problemText(f *Form) string {
	var b strings.Builder
	for _, p := range f.problems["key"] {
		b.WriteString(p.Cause + " " + p.NextStep + "\n")
	}
	return b.String()
}

func TestMaskedBlockPasteArrivesByteForByte(t *testing.T) {
	f := newBlockForm(t, i18n.English, nil)
	if !f.Paste(blockKey()) || blockValue(f) != blockKey() {
		t.Fatalf("value = %q", blockValue(f))
	}
	f = newBlockForm(t, i18n.English, nil)
	f.Paste(strings.ReplaceAll(blockKey(), "\n", "\r\n"))
	if blockValue(f) != blockKey() {
		t.Errorf("CRLF paste = %q", blockValue(f))
	}
	f = newBlockForm(t, i18n.English, nil)
	f.Paste("a\rb\tc\r\n")
	if blockValue(f) != "a\nb\tc\n" {
		t.Errorf("CR paste = %q", blockValue(f))
	}
	f.Paste("replaced")
	if blockValue(f) != "replaced" {
		t.Errorf("a paste does not replace the whole value: %q", blockValue(f))
	}
	if !f.Changed() {
		t.Error("a pasted value is no change")
	}
}

func TestMaskedBlockTypedKeysGiveTheSameValue(t *testing.T) {
	f := newBlockForm(t, i18n.English, nil)
	typeBlock(f, blockKey())
	if blockValue(f) != blockKey() {
		t.Errorf("typed value = %q", blockValue(f))
	}
	if f.Focused() != "key" {
		t.Errorf("enter left the field: %s", f.Focused())
	}
}

func TestMaskedBlockNeverShowsAnyPartOfTheValue(t *testing.T) {
	secrets := append([]string{blockKey()}, blockLines...)
	// The fragments find a line even where a narrow frame wraps it.
	shown := append([]string{"exampleexample", "inventedinvented"}, secrets...)
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		for _, focused := range []bool{true, false} {
			f := newBlockForm(t, lang, func(s *FormSpec) { withButtons(s) })
			f.Paste(blockKey())
			if !focused {
				f.Focus("name")
			}
			// A problem that quotes the value and each of its lines.
			var problems []FormProblem
			for _, s := range secrets {
				problems = append(problems, FormProblem{Field: "key", Cause: "bad " + s, NextStep: "fix " + s})
			}
			f.SetProblems(problems)
			if !focused {
				f.Focus("name")
			}
			for _, look := range []string{"noneascii", "dark"} {
				for _, size := range [][2]int{{120, 40}, {40, 30}} {
					w, h := size[0], size[1]
					view, targets := f.View(looks(t)[look], w, h)
					over, otargets, ok := f.Overlay(looks(t)[look], sampleScreen(w, h), w, h, Rect{Width: w, Height: h})
					frames := []string{view}
					if ok {
						frames = append(frames, over)
					}
					targets = append(targets, otargets...)
					for _, frame := range frames {
						for _, s := range shown {
							if strings.Contains(frame, strings.TrimSpace(s)) {
								t.Errorf("%s focused=%v %s %dx%d shows %q:\n%s", lang, focused, look, w, h, s, frame)
							}
						}
					}
					for _, tg := range targets {
						for _, s := range secrets {
							if strings.Contains(tg.Item, strings.TrimSpace(s)) {
								t.Errorf("target item %q holds the secret", tg.Item)
							}
						}
					}
				}
			}
		}
	}
}

func TestGoldenMaskedBlock(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		w, h       int
		lang       i18n.Lang
	}{
		{"maskedblock.narrow.golden", "noneascii", 40, 12, i18n.English},
		{"maskedblock.wide.golden", "noneascii", 120, 10, i18n.English},
		{"maskedblock.wide.de.golden", "noneascii", 120, 10, i18n.German},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newBlockForm(t, tt.lang, nil)
			f.Paste(blockKey())
			out, _ := f.View(looks(t)[tt.look], tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

func TestMaskedBlockEmptyAndStoredDisplay(t *testing.T) {
	f := newBlockForm(t, i18n.English, nil)
	view, _ := f.View(looks(t)["noneascii"], 100, 12)
	if !strings.Contains(view, "paste the value") {
		t.Errorf("a focused empty field gives no paste hint:\n%s", view)
	}
	f = newBlockForm(t, i18n.English, func(s *FormSpec) { s.Fields[1].Stored = true })
	view, _ = f.View(looks(t)["noneascii"], 100, 12)
	if !strings.Contains(view, "(stored, unchanged)") {
		t.Errorf("no stored note:\n%s", view)
	}
	f.Paste("one\ntwo\nthree")
	view, _ = f.View(looks(t)["noneascii"], 100, 12)
	if !strings.Contains(view, "lines: 3") {
		t.Errorf("no line count:\n%s", view)
	}
}

func TestMaskedBlockRefusesWhatItCannotTake(t *testing.T) {
	big := strings.Repeat("a", maxBlockBytes+1)
	for _, tt := range []struct {
		name string
		do   func(f *Form)
		want string
	}{
		{"paste over limit", func(f *Form) { f.Paste(big) }, "64 KiB"},
		{"paste with escape", func(f *Form) { f.Paste("abc\x1b[2Jdef") }, "cannot take"},
		{"paste with NUL", func(f *Form) { f.Paste("abc\x00") }, "cannot take"},
		{"paste invalid UTF-8", func(f *Form) { f.Paste("abc\xff") }, "cannot take"},
		{"typed control", func(f *Form) { f.Update(tea.KeyPressMsg{Code: 0x1b, Text: "\x1b"}) }, "cannot take"},
		{"typed over limit", func(f *Form) {
			f.Paste(strings.Repeat("a", maxBlockBytes))
			f.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
		}, "64 KiB"},
		{"enter over limit", func(f *Form) {
			f.Paste(strings.Repeat("a", maxBlockBytes))
			f.Update(keyOf("enter"))
		}, "64 KiB"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newBlockForm(t, i18n.English, nil)
			f.Paste("kept")
			before := blockValue(f)
			tt.do(f)
			after := blockValue(f)
			if strings.HasPrefix(tt.name, "paste") && after != before || len(after) > maxBlockBytes {
				t.Errorf("value changed to %d bytes", len(after))
			}
			if tt.name == "typed control" && after != before {
				t.Errorf("value = %q", after)
			}
			if !strings.Contains(problemText(f), tt.want) {
				t.Errorf("problem = %q, want %q", problemText(f), tt.want)
			}
			view, _ := f.View(looks(t)["noneascii"], 100, 14)
			if !strings.Contains(view, tt.want) {
				t.Errorf("the message is not under the field:\n%s", view)
			}
			// The message stays until the field changes.
			f.Update(keyOf("backspace"))
			if problemText(f) != "" {
				t.Error("the message outlives a change of the field")
			}
		})
	}
	f := newBlockForm(t, i18n.English, nil)
	f.Paste(strings.Repeat("a", maxBlockBytes))
	if len(blockValue(f)) != maxBlockBytes {
		t.Error("exactly the limit is refused")
	}
	if got := newBlockForm(t, i18n.German, nil); got != nil {
		got.Paste(big)
		if !strings.Contains(problemText(got), "64 KiB") || !strings.Contains(problemText(got), "Nichts wurde geändert") {
			t.Errorf("German message: %q", problemText(got))
		}
	}
}

func TestMaskedBlockRefusesAHandedValue(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	_, err := NewForm(FormSpec{Fields: []FormField{{ID: "key", Kind: FieldMaskedBlock, Label: "f.key", Value: FormValue{Text: "x"}}},
		Keys: sampleFormKeys}, keys, newTranslator(t, i18n.English, blockCatalogs(), keys))
	if err == nil || !strings.Contains(err.Error(), "handed no value") {
		t.Errorf("NewForm err = %v", err)
	}
	f := newBlockForm(t, i18n.English, nil)
	f.Paste("kept")
	if err := f.SetValue("key", FormValue{Text: "x"}); err == nil || blockValue(f) != "kept" {
		t.Errorf("SetValue err = %v, value %q", err, blockValue(f))
	}
	if err := f.SetValue("key", FormValue{}); err != nil || blockValue(f) != "" {
		t.Errorf("SetValue empty err = %v, value %q", err, blockValue(f))
	}
}

func TestMaskedBlockBackspaceClearsAndStoredKeeps(t *testing.T) {
	f := newBlockForm(t, i18n.English, func(s *FormSpec) { s.Fields[1].Stored = true })
	f.Paste(blockKey())
	f.Update(keyOf("backspace"))
	if blockValue(f) != "" {
		t.Errorf("backspace left %q", blockValue(f))
	}
	view, _ := f.View(looks(t)["noneascii"], 100, 12)
	if !strings.Contains(view, "(stored, unchanged)") {
		t.Errorf("no stored note after clearing:\n%s", view)
	}
	f.Paste("abc")
	f.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if blockValue(f) != "" {
		t.Errorf("delete left %q", blockValue(f))
	}
}

func TestMaskedBlockTabLeavesAndSaveSavesFromTheField(t *testing.T) {
	f := newBlockForm(t, i18n.English, nil)
	typeBlock(f, "ab\ncd")
	if act, _ := f.Update(keyOf("f2")); act != "save" {
		t.Errorf("save key gave %q", act)
	}
	if blockValue(f) != "ab\ncd" {
		t.Errorf("value = %q", blockValue(f))
	}
	f.Update(keyOf("tab"))
	if f.Focused() != "after" {
		t.Errorf("tab left to %s", f.Focused())
	}
	f.Update(press("shift+tab"))
	if f.Focused() != "key" {
		t.Errorf("shift+tab left to %s", f.Focused())
	}
}

func TestMaskedBlockPasteIsIgnoredOnAButton(t *testing.T) {
	f := newBlockForm(t, i18n.English, withButtons)
	f.Update(keyOf("tab"))
	f.Update(keyOf("tab"))
	if _, ok := f.FocusedButton(); !ok {
		t.Fatal("no button focused")
	}
	if f.Paste("abc") || blockValue(f) != "" {
		t.Errorf("paste on a button changed %q", blockValue(f))
	}
}

func TestMaskedBlockPasteIsIgnoredWhileKeyIsTaken(t *testing.T) {
	f := newBlockForm(t, i18n.English, func(s *FormSpec) {
		s.Fields = append(s.Fields, FormField{ID: "shortcut", Kind: FieldKey, Label: "f.name"})
	})
	f.Focus("shortcut")
	f.Update(keyOf("enter"))
	if !f.Capturing() {
		t.Fatal("not capturing")
	}
	if f.Paste("abc") || f.Values()["shortcut"].Text != "" || blockValue(f) != "" {
		t.Error("paste changed a field during key capture")
	}
}

func TestMaskedBlockHintsDoNotPromiseNext(t *testing.T) {
	f := newBlockForm(t, i18n.English, nil)
	var enter []Hint
	for _, h := range f.Hints() {
		if h.Action == "enter" {
			enter = append(enter, h)
		}
	}
	if len(enter) != 1 || enter[0].Label != TextKeyNewLine {
		t.Errorf("enter hints = %v", enter)
	}
}

func TestMaskedBlockInputIsText(t *testing.T) {
	if got := newBlockForm(t, i18n.English, nil).Input(); got != InputText {
		t.Errorf("Input = %v", got)
	}
}

func TestMaskedBlockClickFocuses(t *testing.T) {
	f := newBlockForm(t, i18n.English, nil)
	f.Focus("name")
	_, targets := f.View(looks(t)["noneascii"], 100, 14)
	for _, tg := range targets {
		if tg.Item == formFieldRef+"key" {
			if _, ok := f.Click(MouseResult{Handled: true, Item: tg.Item}); !ok || f.Focused() != "key" {
				t.Errorf("click did not focus: %s", f.Focused())
			}
			return
		}
	}
	t.Error("no click target for the field")
}
