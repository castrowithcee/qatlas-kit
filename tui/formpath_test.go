package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func pathCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {"f.dir": "folder", "f.file": "file", "f.name": "name", "h.dir": "Where the plants are kept."},
		i18n.German:  {"f.dir": "Ordner", "f.file": "Datei", "f.name": "Name", "h.dir": "Wo die Pflanzen liegen."},
	}
}

func dirEntries(texts ...string) []Suggestion {
	var out []Suggestion
	for _, t := range texts {
		out = append(out, Suggestion{Text: t, Continues: strings.HasSuffix(t, "/")})
	}
	return out
}

func newPathForm(t *testing.T, lang i18n.Lang, edit func(*FormSpec)) *Form {
	t.Helper()
	keys := sampleKeymap(t, CharsetUnicode)
	spec := FormSpec{Keys: sampleFormKeys, Fields: []FormField{
		{ID: "dir", Kind: FieldPath, Label: "f.dir", Hint: "h.dir",
			Path: PathSpec{Priority: []Suggestion{{Text: "/srv/plants/"}, {Text: "/home/ann/garden/"}}}},
		{ID: "file", Kind: FieldPath, Label: "f.file", Path: PathSpec{Kind: PathFiles}},
		{ID: "name", Kind: FieldText, Label: "f.name"},
	}}
	if edit != nil {
		edit(&spec)
	}
	f, err := NewForm(spec, keys, newTranslator(t, lang, pathCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// shownTexts are the texts of the entries a path field shows now.
func shownTexts(f *Form) []string {
	var out []string
	for _, e := range f.fields[f.focus].line.items {
		out = append(out, e.Text)
	}
	return out
}

func TestPathFieldAsksForTheFocusedFieldAndDropsStaleAnswers(t *testing.T) {
	f := newPathForm(t, i18n.English, nil)
	first, ok := f.TakePathLoad()
	if !ok || first.Field != "dir" || first.Typed != "" || first.Search || first.Kind != PathDirs {
		t.Fatalf("no request when the focus enters the field: %+v %v", first, ok)
	}
	if _, again := f.TakePathLoad(); again {
		t.Error("a request is handed over once")
	}
	typeForm(f, "/ho")
	load, ok := f.TakePathLoad()
	if !ok || load.Typed != "/ho" || load.Token == first.Token {
		t.Fatalf("typing asks anew with a new token: %+v", load)
	}
	if f.SetPath(PathResult{Token: first.Token, Entries: dirEntries("/old/")}) || len(f.fields[0].line.items) != 1 {
		t.Error("an answer to an earlier request must be dropped and change nothing")
	}
	if !f.SetPath(PathResult{Token: load.Token, Entries: dirEntries("/home/", "/hostname/")}) {
		t.Fatal("the answer to the latest request is taken")
	}
	if got := strings.Join(shownTexts(f), " "); got != "/home/ann/garden/ /home/ /hostname/" {
		t.Errorf("shown entries %q", got)
	}
	// An answer is also stale once the text moved on, and a field other than the focused one asks nothing.
	typeForm(f, "m")
	if f.SetPath(PathResult{Token: load.Token, Entries: dirEntries("/stale/")}) {
		t.Error("an answer to the text before the last key must be dropped")
	}
	formKeys(f, "down")
	if l, ok := f.TakePathLoad(); !ok || l.Field != "dir" {
		t.Fatalf("the load of the last key stays until taken: %+v", l)
	}
	f.SetValue("file", FormValue{Text: "x"})
	if _, ok := f.TakePathLoad(); ok {
		t.Error("a field without the focus asks nothing")
	}
	f.Focus("file")
	if l, ok := f.TakePathLoad(); !ok || l.Field != "file" || l.Typed != "x" || l.Kind != PathFiles {
		t.Errorf("the focus entering a path field asks: %+v", l)
	}
}

func TestPathFieldNeverBlocksInput(t *testing.T) {
	f := newPathForm(t, i18n.English, nil)
	typeForm(f, "/a")
	typeForm(f, "b")
	if got := f.Values()["dir"].Text; got != "/ab" {
		t.Errorf("typing without any answer gives %q", got)
	}
}

func answer(t *testing.T, f *Form, entries ...Suggestion) {
	t.Helper()
	load, ok := f.TakePathLoad()
	if !ok {
		t.Fatal("no request to answer")
	}
	f.SetPath(PathResult{Token: load.Token, Entries: entries})
}

func TestPathFieldTabTakesTheMarkedEntryOrMoves(t *testing.T) {
	f := newPathForm(t, i18n.English, nil)
	typeForm(f, "/h")
	answer(t, f, dirEntries("/home/", "/hold/")...)
	// No mark: tab goes on as before.
	formKeys(f, "tab")
	if f.Focused() != "file" {
		t.Fatalf("tab without a mark moves on, focus %q", f.Focused())
	}
	f.Focus("dir")
	answer(t, f, dirEntries("/home/", "/hold/")...)
	f.fields[0].line.markAt(2)
	formKeys(f, "tab")
	if f.Focused() != "dir" || f.Values()["dir"].Text != "/hold/" {
		t.Fatalf("tab takes the marked entry: focus %q value %q", f.Focused(), f.Values()["dir"].Text)
	}
	// The text now equals the entry, so tab moves on.
	answer(t, f, dirEntries("/hold/a/")...)
	f.fields[0].line.markAt(0)
	f.fields[0].line.items = dirEntries("/hold/")
	f.fields[0].line.markAt(0)
	formKeys(f, "tab")
	if f.Focused() != "file" {
		t.Errorf("tab with a marked entry equal to the text moves on, focus %q", f.Focused())
	}
	// shift+tab never takes.
	f.Focus("dir")
	answer(t, f, dirEntries("/hold/b/")...)
	f.fields[0].line.markAt(0)
	formKeys(f, "shift+tab")
	if f.Values()["dir"].Text != "/hold/" {
		t.Errorf("shift+tab changed the text to %q", f.Values()["dir"].Text)
	}
}

func TestPathFieldArrowKeysMarkAndLeave(t *testing.T) {
	f := newPathForm(t, i18n.English, nil)
	answer(t, f, dirEntries("/a/", "/b/")...)
	// Priority entries are first: three entries.
	line := &f.fields[0].line
	if len(line.items) != 4 {
		t.Fatalf("items %v", shownTexts(f))
	}
	formKeys(f, "down")
	if !line.marked || line.mark != 0 {
		t.Fatal("down marks the first entry")
	}
	formKeys(f, "down", "down", "down", "down", "down")
	if line.mark != 3 || f.Focused() != "dir" {
		t.Errorf("down stops at the last entry and stays in the field: mark %d focus %q", line.mark, f.Focused())
	}
	formKeys(f, "up", "up", "up")
	if line.mark != 0 {
		t.Fatalf("up marks the one before, mark %d", line.mark)
	}
	formKeys(f, "up")
	if line.marked {
		t.Error("up from the first entry takes the mark away")
	}
	// Without a mark up leaves the field as before: it is the first, so it is not handled.
	if _, handled := formKeys(f, "up"); handled {
		t.Error("up above the first field is left to the application")
	}
	// Without entries down moves on as before.
	f.fields[0].line.items = nil
	formKeys(f, "down")
	if f.Focused() != "file" {
		t.Errorf("down without entries moves on, focus %q", f.Focused())
	}
}

func TestPathFieldClickTakesAnEntry(t *testing.T) {
	f := newPathForm(t, i18n.English, nil)
	answer(t, f, dirEntries("/a/", "/b/")...)
	st := looks(t)["noneascii"]
	_, targets := f.View(st, 80, 30)
	var row Target
	for _, tg := range targets {
		if strings.HasPrefix(tg.Item, formPathRef) && tg.Item == formPathRef+"3" {
			row = tg
		}
	}
	if row.Item == "" {
		t.Fatalf("no target for the fourth entry in %v", targets)
	}
	res := NewMouseMap(targets, true).Handle(click(row.Rect.X+1, row.Rect.Y))
	if act, handled := f.Click(res); act != "" || !handled {
		t.Fatalf("click: %q %v", act, handled)
	}
	if got := f.Values()["dir"].Text; got != "/b/" {
		t.Errorf("a click takes the entry, value %q", got)
	}
	if _, ok := f.TakePathLoad(); !ok {
		t.Error("taking an entry asks for the next level")
	}
}

func TestPathFieldPriorityEntriesStandOnTopFilteredAndMarked(t *testing.T) {
	f := newPathForm(t, i18n.English, nil)
	answer(t, f, dirEntries("/srv/plants/", "/srv/other/")...)
	if got := strings.Join(shownTexts(f), " "); got != "/srv/plants/ /home/ann/garden/ /srv/other/" {
		t.Fatalf("priority entries on top, once: %q", got)
	}
	typeForm(f, "/srv")
	answer(t, f, dirEntries("/srv/plants/", "/srv/other/")...)
	if got := strings.Join(shownTexts(f), " "); got != "/srv/plants/ /srv/other/" {
		t.Errorf("priority entries are filtered by the text: %q", got)
	}
	view, _ := f.View(looks(t)["noneascii"], 80, 30)
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "* /srv/plants/") || strings.Contains(plain, "* /srv/other/") {
		t.Errorf("only the priority entry carries the mark:\n%s", plain)
	}
}

func TestPathFieldKindsDirsAndFiles(t *testing.T) {
	f := newPathForm(t, i18n.English, nil)
	// A folder field ends every taken entry with a separator, even one that does not say it continues.
	answer(t, f, Suggestion{Text: "/srv"})
	f.fields[0].line.markAt(2)
	formKeys(f, "tab")
	if got := f.Values()["dir"].Text; got != "/srv/" {
		t.Errorf("dirs: %q", got)
	}
	f.Focus("file")
	answer(t, f, Suggestion{Text: "/srv/notes.txt"}, Suggestion{Text: "/srv/plants", Continues: true})
	formKeys(f, "down", "tab")
	if got := f.Values()["file"].Text; got != "/srv/notes.txt" {
		t.Errorf("files take a file as the target: %q", got)
	}
	f.SetValue("file", FormValue{})
	answer(t, f, Suggestion{Text: "/srv/plants", Continues: true}, Suggestion{Text: `C:\plants\`, Continues: true})
	formKeys(f, "down", "tab")
	if got := f.Values()["file"].Text; got != "/srv/plants/" {
		t.Errorf("files end a folder with a separator: %q", got)
	}
	f.SetValue("file", FormValue{})
	answer(t, f, Suggestion{Text: `C:\plants\`, Continues: true})
	formKeys(f, "down", "tab")
	if got := f.Values()["file"].Text; got != `C:\plants\` {
		t.Errorf("a separator that is there stays single: %q", got)
	}
}

func TestPathFieldSearchModeAndEndLine(t *testing.T) {
	for _, tt := range []struct {
		lang i18n.Lang
		end  PathEnd
		n    int
		want string
	}{
		{i18n.English, PathEndTime, 3, "Search ended after 3 s, give the start path more precisely"},
		{i18n.English, PathEndEntries, 200000, "Search ended after 200000 entries, give the start path more precisely"},
		{i18n.English, PathEndMatches, 50, "Only the first 50 matches shown, give more of the name"},
		{i18n.English, PathEndCanceled, 0, "Search canceled"},
		{i18n.German, PathEndTime, 3, "Suche nach 3 s beendet, Startpfad genauer angeben"},
		{i18n.German, PathEndEntries, 200000, "Suche nach 200000 Einträgen beendet, Startpfad genauer angeben"},
		{i18n.German, PathEndMatches, 50, "Nur die ersten 50 Treffer gezeigt, mehr vom Namen angeben"},
		{i18n.German, PathEndCanceled, 0, "Suche abgebrochen"},
	} {
		f := newPathForm(t, tt.lang, nil)
		f.TakePathLoad()
		typeForm(f, "/srv*fern")
		load, _ := f.TakePathLoad()
		if !load.Search || load.Typed != "/srv*fern" {
			t.Fatalf("a text with * asks for a search: %+v", load)
		}
		f.SetPath(PathResult{Token: load.Token, Entries: dirEntries("/srv/plants/fern/", "/srv/old/fernery/"), End: tt.end, Limit: tt.n})
		view, _ := f.View(looks(t)["noneascii"], 120, 30)
		plain := ansi.Strip(view)
		if !strings.Contains(plain, "/srv/old/fernery/") || !strings.Contains(plain, tt.want) {
			t.Errorf("%s %v: matches and end line:\n%s", tt.lang, tt.end, plain)
		}
	}
	f := newPathForm(t, i18n.English, nil)
	typeForm(f, "/srv*fern")
	load, _ := f.TakePathLoad()
	f.SetPath(PathResult{Token: load.Token, Entries: dirEntries("/srv/fern/")})
	view, _ := f.View(looks(t)["noneascii"], 120, 30)
	if strings.Contains(ansi.Strip(view), "Search") {
		t.Error("a complete search shows no end line")
	}
	// The end line stands for no entry: it is never marked or taken, and the next key drops it.
	typeForm(f, "n")
	f.SetPath(PathResult{Token: load.Token, End: PathEndTime, Limit: 3})
	if f.fields[0].line.notice != "" {
		t.Error("a stale end line is dropped")
	}
}

func TestPathFieldEntriesAreMasked(t *testing.T) {
	f := newPathForm(t, i18n.English, nil)
	answer(t, f, Suggestion{Text: "/srv/\x1b[31mred\nline\u202e/"})
	view, _ := f.View(looks(t)["mono"], 80, 30)
	if strings.Contains(view, "\x1b[31m") || strings.Contains(view, "\u202e") {
		t.Errorf("an entry reaches the screen unmasked: %q", view)
	}
	if !strings.Contains(ansi.Strip(view), Escape("/srv/\x1b[31mred\nline\u202e/")) {
		t.Errorf("the entry is shown in its escaped form:\n%s", ansi.Strip(view))
	}
	// Taken, the entry is cleaned as pasted text.
	formKeys(f, "down", "tab")
	got := f.Values()["dir"].Text
	if strings.ContainsAny(got, "\x1b\n\u202e") {
		t.Errorf("a taken entry keeps a control or direction character: %q", got)
	}
}

func TestPathFieldHintsNameTheKeys(t *testing.T) {
	f := newPathForm(t, i18n.English, func(s *FormSpec) { s.Fields[0].Path.Priority = nil })
	has := func(label TextID) bool {
		for _, h := range f.Hints() {
			if h.Label == label {
				return true
			}
		}
		return false
	}
	if has(TextKeyUp) || has(TextKeyComplete) {
		t.Error("no entries, no hint for them")
	}
	answer(t, f, dirEntries("/a/")...)
	if !has(TextKeyUp) || !has(TextKeyDown) || has(TextKeyComplete) {
		t.Error("with entries up and down are named, tab still goes on")
	}
	formKeys(f, "down")
	if !has(TextKeyComplete) || has(TextKeyNext) {
		t.Error("with a marked entry tab is named as the key that takes it")
	}
}

func TestNewFormRejectsBrokenPathFields(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, pathCatalogs(), keys)
	for name, tt := range map[string]struct {
		edit func(*FormField)
		want string
	}{
		"path settings on text": {func(f *FormField) { f.Kind = FieldText }, "only a path field has path settings"},
		"unknown kind":          {func(f *FormField) { f.Path.Kind = 7 }, "unknown path kind"},
		"empty priority":        {func(f *FormField) { f.Path.Priority = []Suggestion{{}} }, "priority entry"},
		"control priority":      {func(f *FormField) { f.Path.Priority = []Suggestion{{Text: "/a\n"}} }, "priority entry"},
		"control value":         {func(f *FormField) { f.Value.Text = "/a\x00" }, "control character"},
		"choices":               {func(f *FormField) { f.Choices = []Choice{{ID: "x", Label: "f.dir"}} }, "only a choice field"},
	} {
		spec := FormSpec{Keys: sampleFormKeys, Fields: []FormField{{ID: "dir", Kind: FieldPath, Label: "f.dir",
			Path: PathSpec{Priority: []Suggestion{{Text: "/srv/"}}}}}}
		tt.edit(&spec.Fields[0])
		if _, err := NewForm(spec, keys, text); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error %v, want %q", name, err, tt.want)
		}
	}
}

func TestGoldenFormPath(t *testing.T) {
	for _, tt := range []struct {
		lang   i18n.Lang
		tag    string
		search bool
		w, h   int
		look   string
	}{
		{i18n.English, "level.en.narrow", false, 40, 16, "noneascii"},
		{i18n.English, "level.en.wide", false, 100, 16, "noneascii"},
		{i18n.English, "level.en.wide.dark", false, 100, 16, "dark"},
		{i18n.German, "level.de.narrow", false, 40, 16, "noneascii"},
		{i18n.German, "level.de.wide", false, 100, 16, "noneascii"},
		{i18n.English, "search.en.narrow", true, 40, 16, "noneascii"},
		{i18n.English, "search.en.wide", true, 100, 16, "noneascii"},
		{i18n.German, "search.de.narrow", true, 40, 16, "noneascii"},
		{i18n.German, "search.de.wide", true, 100, 16, "noneascii"},
	} {
		name := fmt.Sprintf("formpath.%s.golden", tt.tag)
		t.Run(name, func(t *testing.T) {
			f := newPathForm(t, tt.lang, nil)
			res := PathResult{Entries: dirEntries("/srv/plants/", "/srv/old/", "/srv/cellar/fern pots/")}
			if tt.search {
				typeForm(f, "/srv*fern")
				res.End, res.Limit = PathEndTime, 3
				res.Entries = dirEntries("/srv/plants/fern/", "/srv/plants/ferns-and-moss/", "/srv/old/fernery/", "/srv/cellar/fern pots/")
			} else {
				typeForm(f, "/s")
				res.Entries = append(res.Entries, dirEntries("/sys/")...)
			}
			load, _ := f.TakePathLoad()
			res.Token = load.Token
			f.SetPath(res)
			f.fields[0].line.markAt(1)
			out, _ := f.View(looks(t)[tt.look], tt.w, tt.h)
			checkGolden(t, name, out+"\n")
		})
	}
}
