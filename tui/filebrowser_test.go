package tui

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func fileKeys() FileBrowserKeys {
	return FileBrowserKeys{Up: "up", Down: "down", PageUp: "pageup", PageDown: "pagedown", First: "first", Last: "last",
		Left: "left", Right: "right", Enter: "enter", Search: "search", Back: "back", Reload: "reload"}
}

func fileCatalog() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {"app.new": "new", "app.delete": "delete"},
		i18n.German:  {"app.new": "Neu", "app.delete": "Löschen"},
	}
}

func fileSpec() FileBrowserSpec {
	return FileBrowserSpec{Keys: fileKeys(), RootLabel: "stacks",
		Actions: []FormButton{{Action: "new", Label: "app.new", Primary: true}, {Action: "delete", Label: "app.delete"}}}
}

func newFileBrowser(t *testing.T, lang i18n.Lang) *FileBrowser {
	t.Helper()
	keys := sampleKeymap(t, CharsetUnicode)
	b, err := NewFileBrowser(fileSpec(), keys, newTranslator(t, lang, fileCatalog(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var fileTime = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

func mixedEntries() []FileEntry {
	return []FileEntry{
		{Name: "compost", Kind: FileDir, Rights: "rwxr-xr-x", Owner: "gardener", Modified: fileTime},
		{Name: "current", Kind: FileLink, LinkTarget: "compost/2026", Rights: "rwxrwxrwx", Owner: "gardener", Modified: fileTime},
		{Name: "private", Kind: FileDir, Rights: "rwx------", Owner: "root", Modified: fileTime, Unreadable: true},
		{Name: "notes.txt", Size: 1536, Rights: "rw-r--r--", Owner: "gardener", Modified: fileTime.Add(-48 * time.Hour)},
		{Name: "plan.md", Size: 220, Rights: "rw-r--r--", Owner: "gardener", Modified: fileTime.Add(-24 * time.Hour)},
	}
}

// loaded returns a browser that has asked for dir and been answered with entries.
func loaded(t *testing.T, lang i18n.Lang, dir []string, entries []FileEntry) *FileBrowser {
	t.Helper()
	b := newFileBrowser(t, lang)
	res := b.Load(dir)
	if res.Load == nil {
		t.Fatalf("Load(%q) asks for nothing", dir)
	}
	if ok, err := b.SetEntries(*res.Load, entries); !ok || err != nil {
		t.Fatalf("SetEntries = %v, %v", ok, err)
	}
	return b
}

func plainView(t *testing.T, b *FileBrowser, w, h int) []string {
	t.Helper()
	out, _ := b.View(looks(t)["noneascii"], w, h)
	return strings.Split(out, "\n")
}

func TestNewFileBrowserRejectsWhatCannotWork(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, fileCatalog(), keys)
	cases := []struct {
		name string
		edit func(*FileBrowserSpec)
		want string
	}{
		{"missing key", func(s *FileBrowserSpec) { s.Keys.Back = "" }, "back key is missing"},
		{"unknown key", func(s *FileBrowserSpec) { s.Keys.Enter = "open" }, "no action of the key table"},
		{"shared key", func(s *FileBrowserSpec) { s.Keys.Last = "first" }, "also the first key"},
		{"unknown action", func(s *FileBrowserSpec) { s.Actions = []FormButton{{Action: "nope", Label: "x"}} }, "no action of the key table"},
		{"action is a key", func(s *FileBrowserSpec) { s.Actions = []FormButton{{Action: "enter", Label: "x"}} }, "a key of the browser"},
		{"action twice", func(s *FileBrowserSpec) {
			s.Actions = []FormButton{{Action: "new", Label: "x"}, {Action: "new", Label: "y"}}
		}, "on another button"},
		{"action without label", func(s *FileBrowserSpec) { s.Actions = []FormButton{{Action: "new"}} }, "has no label"},
	}
	for _, tt := range cases {
		spec := fileSpec()
		tt.edit(&spec)
		if _, err := NewFileBrowser(spec, keys, text); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := NewFileBrowser(fileSpec(), nil, text); err == nil {
		t.Error("a missing key table is accepted")
	}
}

// Names that are no plain part of a path never become a step, a request or an entry of a folder.
func TestFileBrowserRefusesNamesThatLeaveTheFolder(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "a\x00b", "/etc", "../x", "x/.."} {
		b := newFileBrowser(t, i18n.English)
		if res := b.Load([]string{"ok", name}); res.Load != nil {
			t.Errorf("Load accepts the part %q", name)
		}
		res := b.Load(nil)
		ok, err := b.SetEntries(*res.Load, []FileEntry{{Name: "fine", Kind: FileDir}, {Name: name, Kind: FileDir}})
		if !ok || err == nil {
			t.Errorf("name %q: SetEntries = %v, %v, want a refusal of the folder", name, ok, err)
		}
		if _, any := b.Selected(); any {
			t.Errorf("name %q: the folder still lists an entry", name)
		}
		if r, _ := b.Update(press("enter")); r.Load != nil || r.Action != nil {
			t.Errorf("name %q: enter leads somewhere: %+v", name, r)
		}
		if got := strings.Join(plainView(t, b, 100, 10), "\n"); !strings.Contains(got, "invalid name") {
			t.Errorf("name %q: the refusal is not shown:\n%s", name, got)
		}
		if len(b.Dir()) != 0 {
			t.Errorf("name %q: dir = %q", name, b.Dir())
		}
	}
	b := newFileBrowser(t, i18n.English)
	res := b.Load(nil)
	if _, err := b.SetEntries(*res.Load, []FileEntry{{Name: "a"}, {Name: "a"}}); err == nil {
		t.Error("a name listed twice is accepted")
	}
}

func TestFileBrowserCannotLeaveTheRoot(t *testing.T) {
	b := loaded(t, i18n.English, nil, mixedEntries())
	// Back at the root is left to the application.
	if r, handled := b.Update(press("esc")); handled || r.Load != nil || r.Action != nil {
		t.Errorf("back at the root = %+v, %v", r, handled)
	}
	if r, handled := b.Act("back"); handled || r.Load != nil {
		t.Errorf("Act(back) at the root = %+v, %v", r, handled)
	}
	// A part of the path line that does not exist, or lies below the shown folder, leads nowhere.
	for _, item := range []string{fileCrumbRef + "-1", fileCrumbRef + "2", fileCrumbRef + "x"} {
		if r, _ := b.Click(MouseResult{Item: item, Handled: true}); r.Load != nil {
			t.Errorf("click on %q asks to load %v", item, r.Load.Dir)
		}
	}
	// The path line shows the root only; clicking it keeps the folder.
	_, targets := b.View(looks(t)["noneascii"], 60, 12)
	if r, _ := b.Click(clickOn(t, targets, 3, 0)); r.Load != nil {
		t.Errorf("click on the root at the root loads %v", r.Load.Dir)
	}

	// One folder down, back and the path line end at the root and never beyond.
	res, _ := b.Update(press("enter"))
	if res.Load == nil || !slices.Equal(res.Load.Dir, []string{"compost"}) {
		t.Fatalf("enter on a folder = %+v", res)
	}
	b.SetEntries(*res.Load, nil)
	up, handled := b.Update(press("esc"))
	if !handled || up.Load == nil || len(up.Load.Dir) != 0 {
		t.Fatalf("back in a folder = %+v, %v", up, handled)
	}
	b.SetEntries(*up.Load, mixedEntries())
	if r, handled := b.Update(press("esc")); handled || r.Load != nil {
		t.Errorf("a second back leaves the root: %+v, %v", r, handled)
	}
}

func clickOn(t *testing.T, targets []Target, x, y int) MouseResult {
	t.Helper()
	res := NewMouseMap(targets, true).Handle(click(x, y))
	if !res.Handled {
		t.Fatalf("no target at %d,%d", x, y)
	}
	return res
}

func TestFileBrowserPathLineLeadsToEveryUpperFolder(t *testing.T) {
	b := loaded(t, i18n.English, []string{"beds", "spring"}, mixedEntries())
	view, targets := b.View(looks(t)["noneascii"], 60, 12)
	if first := ansi.Strip(strings.Split(view, "\n")[0]); strings.TrimSpace(first) != "stacks / beds / spring" {
		t.Fatalf("path line = %q", first)
	}
	cases := []struct {
		x    int
		want []string
	}{{3, nil}, {11, []string{"beds"}}}
	for _, tt := range cases {
		r, handled := b.Click(clickOn(t, targets, tt.x, 0))
		if !handled || r.Load == nil || !slices.Equal(r.Load.Dir, tt.want) {
			t.Errorf("click at %d = %+v, want folder %q", tt.x, r, tt.want)
		}
		b = loaded(t, i18n.English, []string{"beds", "spring"}, mixedEntries())
		_, targets = b.View(looks(t)["noneascii"], 60, 12)
	}
	// The current folder is the last part; clicking it changes nothing.
	if r, handled := b.Click(clickOn(t, targets, 20, 0)); !handled || r.Load != nil {
		t.Errorf("click on the current folder = %+v, %v", r, handled)
	}
	// A narrow line keeps the deepest folder and a mark that leads to the nearest hidden one.
	b = loaded(t, i18n.English, []string{"alpha", "beta", "gamma"}, nil)
	view, targets = b.View(looks(t)["noneascii"], 20, 8)
	line := ansi.Strip(strings.Split(view, "\n")[0])
	if !strings.Contains(line, "gamma") || !strings.Contains(line, "~ /") {
		t.Fatalf("narrow path line = %q", line)
	}
	r, _ := b.Click(clickOn(t, targets, 2, 0))
	if r.Load == nil || len(r.Load.Dir) == 0 || r.Load.Dir[len(r.Load.Dir)-1] == "gamma" {
		t.Errorf("the mark of hidden folders = %+v", r)
	}
}

func TestFileBrowserOpensFoldersAndReportsOtherEntries(t *testing.T) {
	b := loaded(t, i18n.English, []string{"beds"}, mixedEntries())
	res, handled := b.Update(press("enter"))
	if !handled || res.Load == nil || !slices.Equal(res.Load.Dir, []string{"beds", "compost"}) || !b.Loading() {
		t.Fatalf("enter on a folder = %+v, %v", res, handled)
	}
	if !slices.Equal(b.Dir(), []string{"beds", "compost"}) {
		t.Errorf("dir = %q", b.Dir())
	}
	// A link is not followed: enter reports it like a file.
	b = loaded(t, i18n.English, nil, mixedEntries())
	b.Update(press("down"))
	res, _ = b.Update(press("enter"))
	if res.Load != nil || res.Action == nil || res.Action.Action != "enter" || res.Action.Entry.Name != "current" || !res.Action.HasEntry {
		t.Errorf("enter on a link = %+v", res)
	}
	b.Update(press("down"))
	b.Update(press("down"))
	res, _ = b.Update(press("enter"))
	if res.Load != nil || res.Action == nil || res.Action.Entry.Name != "notes.txt" {
		t.Errorf("enter on a file = %+v", res)
	}
}

// The answer of a folder left in the meantime never replaces the shown one.
func TestFileBrowserIgnoresLateAnswers(t *testing.T) {
	b := newFileBrowser(t, i18n.English)
	root := b.Load(nil).Load
	b.SetEntries(*root, []FileEntry{{Name: "beds", Kind: FileDir}, {Name: "pots", Kind: FileDir}})
	beds, _ := b.Update(press("enter"))
	pots := b.Load([]string{"pots"}).Load // the user moved on before the answer for beds came

	if ok, err := b.SetEntries(*beds.Load, []FileEntry{{Name: "stale.txt"}}); ok || err != nil {
		t.Errorf("late entries = %v, %v, want them ignored", ok, err)
	}
	if ok := b.SetError(*beds.Load, errors.New("late failure")); ok {
		t.Error("a late failure is taken")
	}
	if ok, _ := b.SetEntries(*root, []FileEntry{{Name: "stale.txt"}}); ok {
		t.Error("the answer of an older request is taken")
	}
	if !b.Loading() || !slices.Equal(b.Dir(), []string{"pots"}) {
		t.Errorf("loading %v, dir %q after late answers", b.Loading(), b.Dir())
	}
	if got := strings.Join(plainView(t, b, 60, 8), "\n"); strings.Contains(got, "stale") || strings.Contains(got, "late") || !strings.Contains(got, "Loading") {
		t.Errorf("a late answer changed the view:\n%s", got)
	}
	if ok, err := b.SetEntries(*pots, []FileEntry{{Name: "clay.txt"}}); !ok || err != nil {
		t.Fatalf("the current answer = %v, %v", ok, err)
	}
	if e, ok := b.Selected(); !ok || e.Name != "clay.txt" {
		t.Errorf("selected = %+v, %v", e, ok)
	}
	// Once answered, the same answer again is late too.
	if ok, _ := b.SetEntries(*pots, []FileEntry{{Name: "other"}}); ok {
		t.Error("an answered request answers twice")
	}
	if e, _ := b.Selected(); e.Name != "clay.txt" {
		t.Errorf("selected after a repeated answer = %q", e.Name)
	}
}

func TestFileBrowserReloadKeepsTheMarkedEntry(t *testing.T) {
	b := loaded(t, i18n.English, nil, mixedEntries())
	b.Update(press("down"))
	b.Update(press("down"))
	res, handled := b.Update(press("ctrl+r"))
	if !handled || res.Load == nil || len(res.Load.Dir) != 0 {
		t.Fatalf("reload = %+v, %v", res, handled)
	}
	b.SetEntries(*res.Load, mixedEntries())
	if e, _ := b.Selected(); e.Name != "private" {
		t.Errorf("selected after the reload = %q, want private", e.Name)
	}
}

// fileFrame is a frame and a browser that read one key table, with the texts of both.
func fileFrame(t *testing.T, st Styles, lang i18n.Lang) (styledFrame, *FileBrowser) {
	t.Helper()
	keys := sampleKeymap(t, st.Charset())
	catalogs := frameCatalogs()
	for l, texts := range fileCatalog() {
		maps.Copy(catalogs[l], texts)
	}
	text := newTranslator(t, lang, catalogs, keys)
	b, err := NewFileBrowser(fileSpec(), keys, text)
	if err != nil {
		t.Fatal(err)
	}
	return styledFrame{NewFrame(keys, text), st}, b
}

// fileScreen is the browser in the frame, with its actions in the bar and its hints in the footer.
func fileScreen(f styledFrame, b *FileBrowser, width, height int) (FrameSpec, string, []Target) {
	spec := sampleSpec(width, height)
	spec.Bar, spec.Footer = b.BarActions(), b.Hints()
	ws := f.Workspace(spec)
	spec.Workspace, _ = b.View(f.styles, ws.Width, ws.Height)
	out, targets := f.Render(spec)
	return spec, out, targets
}

// The browser draws no button below the list: every row below the path line is a row of the table.
func TestFileBrowserDrawsNoButtonsBelowTheList(t *testing.T) {
	for _, dir := range [][]string{nil, {"beds"}} {
		b := loaded(t, i18n.English, dir, mixedEntries())
		view, targets := b.View(looks(t)["noneascii"], 80, 14)
		for _, word := range []string{"[", "new", "delete", "up"} {
			if strings.Contains(view, word) {
				t.Errorf("dir %q: %q in\n%s", dir, word, view)
			}
		}
		for _, tg := range targets {
			if slices.Contains([]ActionID{"new", "delete", "back"}, tg.Action) || tg.Rect.Y >= 8 {
				t.Errorf("dir %q: target %+v is no part of the path or a row", dir, tg)
			}
		}
	}
}

// BarActions gives the actions of the spec in its order and the primary one as a primary button.
func TestFileBrowserBarActionsFollowTheSpec(t *testing.T) {
	b := newFileBrowser(t, i18n.English)
	want := []BarAction{{Action: "new", Label: "app.new", Kind: ButtonPrimary}, {Action: "delete", Label: "app.delete"}}
	if got := b.BarActions(); !slices.Equal(got, want) {
		t.Errorf("BarActions = %+v, want %+v", got, want)
	}
	b.BarActions()[0].Action = "changed"
	if got := b.BarActions(); got[0].Action != "new" {
		t.Error("BarActions hands out the state of the browser")
	}
}

// In the frame the actions stand in the bar and leave the footer, in the order of the spec.
func TestFileBrowserActionsStandInTheBarNotInTheFooter(t *testing.T) {
	f, b := fileFrame(t, looks(t)["noneascii"], i18n.English)
	b.Load([]string{"beds"})
	spec, out, _ := fileScreen(f, b, 100, 14)
	rows := plainRows(out)
	ws := f.Workspace(spec)
	if got := strings.TrimRight(rows[ws.Y-2], " "); got != "[ n New ] [ d Remove ]" && got != "[ n new ] [ d delete ]" {
		t.Errorf("bar = %q", got)
	}
	if foot := strings.Join(rows[ws.Y+ws.Height:], "\n"); strings.Contains(foot, "new") || strings.Contains(foot, "delete") {
		t.Errorf("an action of the bar stands in the footer:\n%s", foot)
	}
	for _, h := range b.Hints() {
		if h.Action == "" {
			t.Errorf("hint %+v carries no action", h)
		}
	}
}

// A key and a click on its button of the bar report the same action on the same entry and folder.
func TestFileBrowserActionsByKeyAndClickAreEqual(t *testing.T) {
	for _, tt := range []struct {
		key    string
		action ActionID
	}{{"n", "new"}, {"d", "delete"}} {
		f, byKey := fileFrame(t, looks(t)["noneascii"], i18n.English)
		_, byClickBrowser := fileFrame(t, looks(t)["noneascii"], i18n.English)
		for _, b := range []*FileBrowser{byKey, byClickBrowser} {
			res := b.Load([]string{"beds"})
			b.SetEntries(*res.Load, mixedEntries())
			for range 3 {
				b.Update(press("down"))
			}
		}
		keyRes, handled := byKey.Update(press(tt.key))

		spec, out, targets := fileScreen(f, byClickBrowser, 100, 14)
		y := f.Workspace(spec).Y - 2
		hit := MouseResult{}
		for _, tg := range targets {
			if tg.Action == tt.action && tg.Rect.Y == y {
				hit = clickOn(t, targets, tg.Rect.X+1, y)
			}
		}
		if hit.Action != tt.action {
			t.Fatalf("%s: no button in the bar of\n%s", tt.action, out)
		}
		// The click comes back as MouseResult.Action, which the application runs as the key.
		clickRes, clicked := byClickBrowser.Act(hit.Action)
		if !handled || !clicked || keyRes.Action == nil || clickRes.Action == nil {
			t.Fatalf("%s: key %+v %v, click %+v %v", tt.action, keyRes, handled, clickRes, clicked)
		}
		if keyRes.Action.Action != tt.action || keyRes.Action.Entry.Name != "notes.txt" ||
			!slices.Equal(keyRes.Action.Dir, []string{"beds"}) {
			t.Errorf("%s: key reported %+v", tt.action, *keyRes.Action)
		}
		if !slices.Equal(keyRes.Action.Dir, clickRes.Action.Dir) || keyRes.Action.Action != clickRes.Action.Action ||
			keyRes.Action.Entry != clickRes.Action.Entry || keyRes.Action.HasEntry != clickRes.Action.HasEntry {
			t.Errorf("%s: key %+v differs from click %+v", tt.action, *keyRes.Action, *clickRes.Action)
		}
	}
	// A folder-wide action without an entry reports none.
	b := loaded(t, i18n.English, nil, nil)
	if res, _ := b.Update(press("n")); res.Action == nil || res.Action.HasEntry {
		t.Errorf("new in an empty folder = %+v", res)
	}
	// Back goes up one folder as before, by key and as an action.
	b = loaded(t, i18n.English, []string{"beds"}, mixedEntries())
	if res, ok := b.Act("back"); !ok || res.Load == nil || len(res.Load.Dir) != 0 {
		t.Errorf("Act(back) = %+v, %v", res, ok)
	}
}

// Every control the browser draws is a click target: the parts of the path and the rows.
func TestFileBrowserEveryControlIsAClickTarget(t *testing.T) {
	b := loaded(t, i18n.English, []string{"beds"}, mixedEntries())
	_, targets := b.View(looks(t)["noneascii"], 80, 14)
	for row := 3; row < 8; row++ {
		if res := NewMouseMap(targets, true).Handle(click(4, row)); res.Item == "" {
			t.Errorf("row %d has no target", row)
		}
	}
	if res := NewMouseMap(targets, true).Handle(click(3, 0)); res.Item == "" {
		t.Error("the path line has no target")
	}
}

func TestFileBrowserEscapesForeignText(t *testing.T) {
	evil := "x\x1b[31mred\x1b]0;title\x07\nline\r\u202e"
	entries := []FileEntry{
		{Name: evil, Kind: FileDir, Owner: evil, Rights: evil},
		{Name: "link" + evil, Kind: FileLink, LinkTarget: evil, Owner: "o\x1b[2J"},
		{Name: "plain", Unreadable: true, Owner: evil, Rights: "\x1b[1m"},
	}
	for _, w := range []int{20, 40, 120} {
		b := loaded(t, i18n.English, []string{"a\x1b[2Jb", "c\nd"}, entries)
		for look, st := range looks(t) {
			out, _ := b.View(st, w, 12)
			plain := ansi.Strip(out)
			if strings.ContainsAny(strings.ReplaceAll(plain, "\n", ""), "\x1b\x07\r\x00") || strings.Contains(plain, "\u202e") {
				t.Errorf("%s width %d: raw control character in %q", look, w, plain)
			}
			if st.Profile().Colorless() {
				noColorSequence(t, look, out)
			}
		}
	}
	// The failure text and the root label are untrusted too.
	b := newFileBrowser(t, i18n.English)
	b.spec.RootLabel = "ro\x1b[2Jot"
	load := b.Load(nil).Load
	b.SetError(*load, errors.New("denied\x1b[31m\nby \x07policy"))
	got := strings.Join(plainView(t, b, 100, 8), "\n")
	if strings.ContainsAny(got, "\x1b\x07\r\x00") || !strings.Contains(got, `denied\x1b[31m\nby \apolicy`) || !strings.Contains(got, `ro\x1b[2Jot`) {
		t.Errorf("failure view:\n%q", got)
	}
}

func TestFileBrowserViewKeepsItsSize(t *testing.T) {
	for _, state := range []string{"mixed", "empty", "loading", "error"} {
		for _, dir := range [][]string{nil, {"alpha", "beta", "gamma", "delta"}} {
			for look, st := range looks(t) {
				for w := 0; w <= 60; w += 3 {
					for h := 0; h <= 14; h++ {
						b := fileBrowserState(t, state, dir)
						out, targets := b.View(st, w, h)
						lines := strings.Split(out, "\n")
						if h > 0 && len(lines) != h {
							t.Fatalf("%s %s %dx%d: %d lines", state, look, w, h, len(lines))
						}
						for _, l := range lines {
							if got := ansi.StringWidth(l); h > 0 && got != w {
								t.Fatalf("%s %s %dx%d: line is %d cells: %q", state, look, w, h, got, l)
							}
						}
						for _, tg := range targets {
							if tg.Rect.X < 0 || tg.Rect.Y < 0 || tg.Rect.X+tg.Rect.Width > w || tg.Rect.Y >= h {
								t.Fatalf("%s %s %dx%d: target %+v outside", state, look, w, h, tg)
							}
						}
					}
				}
			}
		}
	}
}

func fileBrowserState(t *testing.T, state string, dir []string) *FileBrowser {
	t.Helper()
	switch state {
	case "mixed":
		return loaded(t, i18n.English, dir, mixedEntries())
	case "empty":
		return loaded(t, i18n.English, dir, nil)
	case "loading":
		b := newFileBrowser(t, i18n.English)
		b.Load(dir)
		return b
	}
	b := newFileBrowser(t, i18n.English)
	b.SetError(*b.Load(dir).Load, errors.New("permission denied"))
	return b
}

func TestGoldenFileBrowser(t *testing.T) {
	for _, state := range []string{"empty", "mixed", "loading", "error"} {
		for _, tt := range []struct {
			variant, look string
			lang          i18n.Lang
			w, h          int
		}{
			{"narrow", "noneascii", i18n.English, 40, 14},
			{"wide", "noneascii", i18n.English, 100, 14},
			{"wide.dark", "dark", i18n.English, 100, 14},
			{"wide.de", "noneascii", i18n.German, 100, 14},
		} {
			name := "filebrowser." + state + "." + tt.variant + ".golden"
			t.Run(name, func(t *testing.T) {
				b := fileBrowserState(t, state, []string{"beds", "spring"})
				if tt.lang != i18n.English {
					keys := sampleKeymap(t, CharsetUnicode)
					var err error
					if b, err = NewFileBrowser(fileSpec(), keys, newTranslator(t, tt.lang, fileCatalog(), keys)); err != nil {
						t.Fatal(err)
					}
					load := b.Load([]string{"beds", "spring"}).Load
					switch state {
					case "mixed":
						b.SetEntries(*load, mixedEntries())
					case "empty":
						b.SetEntries(*load, nil)
					case "error":
						b.SetError(*load, errors.New("permission denied"))
					}
				}
				out, _ := b.View(looks(t)[tt.look], tt.w, tt.h)
				checkGolden(t, name, out+"\n")
			})
		}
	}
}

func TestGoldenFileBrowserBar(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		lang       i18n.Lang
		width      int
	}{
		{"filebrowser.bar.wide.golden", "light256", i18n.English, 100},
		{"filebrowser.bar.wide.dark.golden", "dark", i18n.English, 100},
		{"filebrowser.bar.wide.de.golden", "light256", i18n.German, 100},
		{"filebrowser.bar.wide.de.dark.golden", "dark", i18n.German, 100},
		{"filebrowser.bar.narrow.golden", "light256", i18n.English, 40},
		{"filebrowser.bar.narrow.dark.golden", "dark", i18n.English, 40},
		{"filebrowser.bar.narrow.de.golden", "light256", i18n.German, 40},
		{"filebrowser.bar.narrow.de.dark.golden", "dark", i18n.German, 40},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, b := fileFrame(t, looks(t)[tt.look], tt.lang)
			res := b.Load([]string{"beds", "spring"})
			b.SetEntries(*res.Load, mixedEntries())
			_, out, _ := fileScreen(f, b, tt.width, 16)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}
