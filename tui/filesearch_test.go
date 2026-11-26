package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// typeFilter presses the search key and then the characters of text.
func typeFilter(t *testing.T, b *FileBrowser, text string) (last FileResult) {
	t.Helper()
	if r, ok := b.Update(press("/")); !ok || r.Search != nil {
		t.Fatalf("search key = %+v, %v", r, ok)
	}
	for _, c := range text {
		r, ok := b.Update(press(string(c)))
		if !ok {
			t.Fatalf("key %q not handled", c)
		}
		if r.Search != nil {
			last = r
		}
	}
	return last
}

func hitPaths() []FileMatch {
	return []FileMatch{
		{Path: []string{"beds", "spring", "n8n", "compose.yml"}},
		{Path: []string{"beds", "n8n-data"}, Kind: FileDir},
		{Path: []string{"beds", "n8n-link"}, Kind: FileLink},
	}
}

// searched returns a browser in dir with the filter "*n8n" typed and the request it made.
func searched(t *testing.T, lang i18n.Lang, dir []string) (*FileBrowser, FileSearch) {
	t.Helper()
	b := loaded(t, lang, dir, mixedEntries())
	r := typeFilter(t, b, "*n8n")
	if r.Search == nil {
		t.Fatal("no request")
	}
	return b, *r.Search
}

func TestFileBrowserStarSearchAsksTheApplication(t *testing.T) {
	b := loaded(t, i18n.English, []string{"beds"}, mixedEntries())
	b.Update(press("/"))
	if r, _ := b.Update(press("*")); r.Search != nil || b.Searching() {
		t.Fatalf("a bare * asks: %+v", r)
	}
	var tokens []uint64
	for _, c := range "n8n" {
		r, _ := b.Update(press(string(c)))
		if r.Search == nil || r.Search.Query != "n8n"[:len(tokens)+1] || !slices.Equal(r.Search.Dir, []string{"beds"}) {
			t.Fatalf("request after %q = %+v", c, r.Search)
		}
		tokens = append(tokens, r.Search.Token)
	}
	if r, _ := b.Update(press(" ")); r.Search != nil {
		t.Errorf("a trailing blank asks again: %+v", r.Search)
	}
	if !slices.IsSorted(tokens) || tokens[0] == tokens[2] {
		t.Errorf("tokens do not grow: %v", tokens)
	}
	if !b.Searching() {
		t.Error("Searching is false while the answer is due")
	}
}

func TestFileBrowserWithoutStarFiltersAsBefore(t *testing.T) {
	b := loaded(t, i18n.English, []string{"beds"}, mixedEntries())
	if r := typeFilter(t, b, "plan"); r.Search != nil || b.Searching() {
		t.Fatalf("a plain filter asks for a search: %+v", r)
	}
	if e, ok := b.Selected(); !ok || e.Name != "plan.md" {
		t.Errorf("selected = %+v, %v", e, ok)
	}
}

func TestFileBrowserDropsAStaleSearchAnswer(t *testing.T) {
	b, first := searched(t, i18n.English, nil)
	r, _ := b.Update(press("x"))
	second := *r.Search
	if cur, _ := b.SetMatches(first, FileFound{Matches: hitPaths()}); cur {
		t.Fatal("the answer to the first request is taken")
	}
	if !b.waiting {
		t.Error("a stale answer ended the wait")
	}
	if cur, _ := b.SetMatches(second, FileFound{Matches: hitPaths()}); !cur {
		t.Fatal("the answer to the last request is dropped")
	}
	if cur, _ := b.SetMatches(second, FileFound{}); cur {
		t.Error("a second answer to one request is taken")
	}
	// An answer after the list was left is stale too.
	r, _ = b.Update(press("esc"))
	if cur, _ := b.SetMatches(second, FileFound{}); cur || b.Searching() {
		t.Error("an answer after leaving is taken")
	}
}

func TestFileBrowserDropsMatchesOutsideTheRoot(t *testing.T) {
	b, s := searched(t, i18n.English, []string{"beds"})
	bad := []FileMatch{
		{Path: []string{"beds", "..", "x"}},
		{Path: []string{"..", "x"}},
		{Path: []string{"beds", ""}},
		{Path: []string{"beds", "."}},
		{Path: []string{"beds", "a/b"}},
		{Path: []string{"beds", `a\b`}},
		{Path: []string{"beds", "a\x00b"}},
		{Path: []string{"beds"}},
		{Path: nil},
		{Path: []string{"other", "n8n"}},
		{Path: []string{"beds", "n8n"}, Kind: 9},
	}
	good := FileMatch{Path: []string{"beds", "sub", "n8n"}}
	cur, dropped := b.SetMatches(s, FileFound{Matches: append(bad, good)})
	if !cur || dropped != len(bad) {
		t.Fatalf("SetMatches = %v, %d, want %d dropped", cur, dropped, len(bad))
	}
	if len(b.matches) != 1 {
		t.Errorf("%d matches shown, want 1", len(b.matches))
	}
	if got := strings.Join(plainView(t, b, 100, 12), "\n"); !strings.Contains(got, "sub/n8n") {
		t.Errorf("the match is not shown relative to the folder:\n%s", got)
	}
	// Nothing valid left: the empty sentence.
	b, s = searched(t, i18n.English, []string{"beds"})
	b.SetMatches(s, FileFound{Matches: bad})
	if got := strings.Join(plainView(t, b, 100, 12), "\n"); !strings.Contains(got, "No match below this folder.") {
		t.Errorf("empty answer:\n%s", got)
	}
}

func TestFileBrowserShowsHostileMatchNamesEscaped(t *testing.T) {
	b, s := searched(t, i18n.English, nil)
	b.SetMatches(s, FileFound{Matches: []FileMatch{{Path: []string{"n8n\x1b[31m‮"}}}})
	if out, _ := b.View(looks(t)["noneascii"], 60, 12); strings.ContainsAny(out, "\x1b‮") {
		t.Errorf("a match name reaches the screen raw: %q", out)
	}
}

func TestFileBrowserJumpsToTheMatch(t *testing.T) {
	b, s := searched(t, i18n.English, []string{"beds"})
	b.SetMatches(s, FileFound{Matches: hitPaths()})
	b.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // n8n-data, a folder
	r, handled := b.Update(press("enter"))
	if !handled || r.Load == nil || r.Search != nil || !slices.Equal(r.Load.Dir, []string{"beds"}) {
		t.Fatalf("enter on a match = %+v, %v", r, handled)
	}
	if b.Searching() || b.list.Query() != "" {
		t.Error("the search stays after the jump")
	}
	b.SetEntries(*r.Load, []FileEntry{{Name: "a", Kind: FileDir}, {Name: "n8n-data", Kind: FileDir}, {Name: "z"}})
	if e, ok := b.Selected(); !ok || e.Name != "n8n-data" {
		t.Errorf("selected = %+v, %v, want n8n-data", e, ok)
	}

	// A match deep below: the folder of the match, not the match itself.
	b, s = searched(t, i18n.English, []string{"beds"})
	b.SetMatches(s, FileFound{Matches: hitPaths()})
	r, _ = b.Update(press("enter"))
	if r.Load == nil || !slices.Equal(r.Load.Dir, []string{"beds", "spring", "n8n"}) {
		t.Fatalf("enter = %+v", r)
	}
	b.SetEntries(*r.Load, []FileEntry{{Name: "compose.yml"}, {Name: "other"}})
	if e, _ := b.Selected(); e.Name != "compose.yml" {
		t.Errorf("selected = %q", e.Name)
	}
}

func TestFileBrowserBackLeavesMatchesThenFilterThenFolder(t *testing.T) {
	b, s := searched(t, i18n.English, []string{"beds"})
	b.SetMatches(s, FileFound{Matches: hitPaths()})

	r, handled := b.Update(press("esc"))
	if !handled || r.Load != nil || b.Searching() || b.list.Query() != "*n8n" {
		t.Fatalf("first back = %+v, %v, searching %v, filter %q", r, handled, b.Searching(), b.list.Query())
	}
	r, handled = b.Update(press("esc"))
	if !handled || r.Load != nil || b.list.Query() != "" {
		t.Fatalf("second back = %+v, %v, filter %q", r, handled, b.list.Query())
	}
	if e, ok := b.Selected(); !ok || e.Name != "compost" {
		t.Errorf("the folder is not shown again: %+v, %v", e, ok)
	}
	r, handled = b.Update(press("esc"))
	if !handled || r.Load == nil || len(r.Load.Dir) != 0 {
		t.Fatalf("third back = %+v, %v", r, handled)
	}
	// The same through Act.
	b, s = searched(t, i18n.English, []string{"beds"})
	b.SetMatches(s, FileFound{Matches: hitPaths()})
	if r, _ := b.Act("back"); r.Load != nil || b.Searching() || b.list.Query() == "" {
		t.Errorf("Act(back) in the matches = %+v", r)
	}
}

func TestFileBrowserNamesWhyTheSearchEnded(t *testing.T) {
	for _, tt := range []struct {
		found FileFound
		want  string
	}{
		{FileFound{End: FileSearchTimeLimit, Limit: 3}, "Search stopped after 3 seconds"},
		{FileFound{End: FileSearchEntryLimit, Limit: 200000}, "Search stopped after 200000 entries"},
		{FileFound{End: FileSearchMatchLimit, Limit: 50}, "Only the first 50 matches"},
		{FileFound{End: FileSearchCanceled}, "The search was canceled"},
		{FileFound{End: FileSearchTimeLimit}, "The search stopped early"},
		{FileFound{End: 99, Limit: 5}, "The search stopped early"},
		{FileFound{}, ""},
	} {
		b, s := searched(t, i18n.English, nil)
		tt.found.Matches = hitPaths()
		b.SetMatches(s, tt.found)
		view := strings.Join(plainView(t, b, 100, 12), "\n")
		if tt.want == "" && strings.Contains(view, "stopped") || !strings.Contains(view, tt.want) || strings.Contains(view, "%!") {
			t.Errorf("%+v: want %q in\n%s", tt.found, tt.want, view)
		}
	}
	// The note also stands where the list is cut short, and goes away with the list.
	b, s := searched(t, i18n.English, nil)
	b.SetMatches(s, FileFound{Matches: hitPaths(), End: FileSearchTimeLimit, Limit: 3})
	b.Update(press("esc"))
	if view := strings.Join(plainView(t, b, 100, 12), "\n"); strings.Contains(view, "stopped") {
		t.Errorf("the note stays after leaving:\n%s", view)
	}
}

func TestFileBrowserShowsASearchRunning(t *testing.T) {
	b, s := searched(t, i18n.English, nil)
	if view := strings.Join(plainView(t, b, 100, 12), "\n"); !strings.Contains(view, "Searching…") {
		t.Errorf("no sign of the running search:\n%s", view)
	}
	if !b.SetSearchError(s, errors.New("denied\x1b[2J")) {
		t.Fatal("the error is dropped")
	}
	out, _ := b.View(looks(t)["noneascii"], 100, 12)
	if !strings.Contains(out, "The search failed: denied") || strings.Contains(out, "\x1b") {
		t.Errorf("search error:\n%q", out)
	}
	if b.SetSearchError(s, nil) {
		t.Error("a second answer is taken")
	}
}

func TestFileBrowserKeepsTheFolderThatLoadsDuringASearch(t *testing.T) {
	b := newFileBrowser(t, i18n.English)
	load := *b.Load(nil).Load
	r := typeFilter(t, b, "*n8n")
	if cur, _ := b.SetEntries(load, mixedEntries()); !cur {
		t.Fatal("the folder answer is dropped by the search")
	}
	b.SetMatches(*r.Search, FileFound{Matches: []FileMatch{{Path: []string{"n8n"}}}})
	if got := strings.Join(plainView(t, b, 100, 12), "\n"); !strings.Contains(got, "n8n") || strings.Contains(got, "compost") {
		t.Errorf("matches replaced by the folder:\n%s", got)
	}
	b.Update(press("esc"))
	b.Update(press("esc"))
	if _, ok := b.Selected(); !ok {
		t.Error("the loaded folder is not shown after leaving")
	}
}

func fileSearchState(t *testing.T, state string, lang i18n.Lang) *FileBrowser {
	t.Helper()
	b, s := searched(t, lang, []string{"beds"})
	if state == "hits" {
		matches := append(hitPaths(), FileMatch{Path: []string{"beds", "spring", "notes-n8n.txt"}})
		b.SetMatches(s, FileFound{Matches: matches, End: FileSearchTimeLimit, Limit: 3})
	}
	return b
}

func TestGoldenFileSearch(t *testing.T) {
	for _, state := range []string{"hits", "searching"} {
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
				out, _ := fileSearchState(t, state, tt.lang).View(looks(t)[tt.look], tt.w, tt.h)
				checkGolden(t, name, out+"\n")
			})
		}
	}
}
