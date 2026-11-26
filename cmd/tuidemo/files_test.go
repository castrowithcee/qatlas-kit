package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/projectpath"
	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// searchBlock is the files block over a folder of the test with a nested match, wired to a channel.
func searchBlock(t *testing.T) (*model, *filesBlock, chan tea.Msg) {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{"stacks/n8n/compose.yml", "n8n-notes.txt", "other/readme.txt"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(f)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "8")
	m.tabs["pickers"] = "files"
	b := m.live().(*filesBlock)
	b.root = root
	b.run(m, b.browser.Load(nil))
	ch := make(chan tea.Msg, 8)
	m.send = func(msg tea.Msg) { ch <- msg }
	m.zone = tui.ZoneRows
	return m, b, ch
}

func typeInto(m *model, b *filesBlock, text string) {
	b.key(m, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, c := range text {
		b.key(m, tea.KeyPressMsg{Code: c, Text: string(c)})
	}
}

func awaitSearch(t *testing.T, ch chan tea.Msg) searchDone {
	t.Helper()
	for {
		select {
		case msg := <-ch:
			if d, ok := msg.(searchDone); ok {
				return d
			}
		case <-time.After(10 * time.Second):
			t.Fatal("no answer to the search")
		}
	}
}

// lastAnswer collects the answers to the letters typed and returns the one to the whole text; the others
// are stale, or were stopped by the next letter.
func lastAnswer(t *testing.T, ch chan tea.Msg, text string) searchDone {
	t.Helper()
	for {
		if d := awaitSearch(t, ch); d.search.Query == text {
			return d
		}
	}
}

func TestFilesSearchFindsMatchesBelowTheFolderAndJumps(t *testing.T) {
	m, b, ch := searchBlock(t)
	typeInto(m, b, "*compose")
	d := lastAnswer(t, ch, "compose")
	if d.res.End != projectpath.Complete {
		t.Fatalf("end = %v", d.res.End)
	}
	m.handle(d)
	if !b.browser.Searching() {
		t.Fatal("the matches are not shown")
	}
	b.key(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := b.browser.Dir(); !slices.Equal(got, []string{"stacks", "n8n"}) {
		t.Fatalf("jumped to %q", got)
	}
	if e, ok := b.browser.Selected(); !ok || e.Name != "compose.yml" || b.browser.Searching() || b.cancel != nil {
		t.Errorf("selected %+v, %v after the jump", e, ok)
	}
}

func TestFilesSearchLeavesOutMatchesOutsideTheRoot(t *testing.T) {
	m, b, ch := searchBlock(t)
	typeInto(m, b, "*compose")
	d := lastAnswer(t, ch, "compose")
	d.res.Matches = append(d.res.Matches, projectpath.Match{Path: filepath.Join(filepath.Dir(b.root), "elsewhere")},
		projectpath.Match{Path: filepath.Join(b.root, "..", "..", "compose.yml")})
	m.handle(d)
	view, _ := b.browser.View(m.styles, 100, 12)
	if !strings.Contains(view, "stacks/n8n/compose.yml") || strings.Contains(view, "elsewhere") || strings.Contains(view, "..") {
		t.Errorf("view:\n%s", view)
	}
}

func TestFilesSearchStopsWhenLeft(t *testing.T) {
	m, b, ch := searchBlock(t)
	typeInto(m, b, "*n")
	if b.cancel == nil {
		t.Fatal("no search runs")
	}
	b.key(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // back leaves the matches
	if b.browser.Searching() || b.cancel != nil {
		t.Errorf("searching %v, running %v after leaving", b.browser.Searching(), b.cancel != nil)
	}
	awaitSearch(t, ch) // the stopped search still answers; the browser drops it
}
