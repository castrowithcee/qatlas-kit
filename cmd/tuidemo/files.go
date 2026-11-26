package main

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// filesBlock is a FileBrowser over the sample folder in the temporary directory of the program. The browser
// reads no file system: this block lists a folder when it asks, below root and nowhere else.
type filesBlock struct {
	browser *tui.FileBrowser
	root    string
	chosen  string // the last file taken, as a "/" path below the root
	note    tui.TextID
	cancel  context.CancelFunc // the search that runs, nil for none
}

func pickBlocks(m *model) (map[tui.TabID]block, error) {
	k := tui.FileBrowserKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst, Last: actLast,
		Left: actLeft, Right: actRight, Enter: actEnter, Search: actSearch, Back: actBack}
	br, err := tui.NewFileBrowser(tui.FileBrowserSpec{Keys: k, RootLabel: m.text.Lookup(textFileRoot),
		Actions: []tui.FormButton{{Action: actTake, Label: textFileTake, Primary: true},
			{Action: actClear, Label: textFileClear}}}, m.keys, m.text)
	if err != nil {
		return nil, err
	}
	b := &filesBlock{browser: br, root: m.root, note: textNotePickFiles}
	if b.root != "" {
		b.run(m, br.Load(nil))
	}
	return map[tui.TabID]block{"files": b}, nil
}

// below joins the name parts of a folder below the root and refuses a result that leaves it.
func below(root string, parts []string) (string, error) {
	if len(parts) == 0 {
		return root, nil
	}
	rel := filepath.Join(parts...)
	if !filepath.IsLocal(rel) {
		return "", errors.New("the folder is outside the sample folder")
	}
	return filepath.Join(root, rel), nil
}

// list answers a request of the browser with the entries of the folder, or the reason it cannot be read.
func (b *filesBlock) list(load tui.FileLoad) {
	dir, err := below(b.root, load.Dir)
	var des []fs.DirEntry
	if err == nil {
		des, err = os.ReadDir(dir)
	}
	if err != nil {
		b.browser.SetError(load, err)
		return
	}
	var entries []tui.FileEntry
	for _, de := range des {
		info, ierr := de.Info()
		e := tui.FileEntry{Name: de.Name(), Size: -1, Unreadable: ierr != nil}
		switch {
		case de.IsDir():
			e.Kind = tui.FileDir
		case de.Type()&fs.ModeSymlink != 0:
			e.Kind = tui.FileLink
		case ierr == nil:
			e.Size = info.Size()
		}
		if ierr == nil {
			e.Modified = info.ModTime()
		}
		entries = append(entries, e)
	}
	// Folders first, as a file manager shows them; ReadDir already sorted by name.
	slices.SortStableFunc(entries, func(a, c tui.FileEntry) int {
		return cmp.Compare(btoi(a.Kind != tui.FileDir), btoi(c.Kind != tui.FileDir))
	})
	_, _ = b.browser.SetEntries(load, entries) // an entry it refuses shows as the failed folder
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

// run does what the browser asked: list a folder, search below it, take the marked file, or forget the chosen
// one.
func (b *filesBlock) run(m *model, res tui.FileResult) {
	if res.Load != nil {
		b.list(*res.Load)
	}
	if res.Search != nil {
		b.search(m, *res.Search)
	}
	if !b.browser.Searching() {
		b.stopSearch()
	}
	a := res.Action
	if a != nil && a.Action == actClear {
		b.chosen = ""
		return
	}
	if a == nil || (a.Action != actEnter && a.Action != actTake) {
		return
	}
	if !a.HasEntry || a.Entry.Kind == tui.FileDir {
		m.message = &tui.Message{Kind: tui.MessageWarning, Text: m.text.Lookup(textFileNeedFile)}
		return
	}
	if _, err := below(b.root, append(slices.Clone(a.Dir), a.Entry.Name)); err != nil {
		m.message = &tui.Message{Kind: tui.MessageError, Text: err.Error()}
		return
	}
	b.chosen = path.Join(append(slices.Clone(a.Dir), a.Entry.Name)...)
	m.message = &tui.Message{Kind: tui.MessageResult, Text: m.text.Lookup(textFileChosenMsg, tui.Escape(b.chosen))}
}

func (b *filesBlock) zones() []tui.FocusZone             { return []tui.FocusZone{tui.ZoneRows} }
func (b *filesBlock) focus(zone tui.FocusZone)           { b.browser.SetFocused(zone == tui.ZoneRows) }
func (b *filesBlock) input(tui.FocusZone) tui.InputFocus { return b.browser.Input() }
func (b *filesBlock) wheel(res tui.MouseResult) {
	if res.Zone == tui.ZoneRows {
		b.browser.Wheel(res.WheelSteps)
	}
}
func (b *filesBlock) bar() []tui.BarAction           { return b.browser.BarActions() }
func (b *filesBlock) paste(string)                   {}
func (b *filesBlock) hints(tui.FocusZone) []tui.Hint { return b.browser.Hints() }

func (b *filesBlock) key(m *model, msg tea.KeyPressMsg) bool {
	res, handled := b.browser.Update(msg)
	b.run(m, res)
	return handled
}

func (b *filesBlock) click(m *model, res tui.MouseResult) {
	r, _ := b.browser.Click(res)
	b.run(m, r)
}

// act takes the file from anywhere, as the button or its key names it; the moves act only in the rows.
func (b *filesBlock) act(m *model, action tui.ActionID) bool {
	if action != actTake && m.zone != tui.ZoneRows {
		return false
	}
	res, handled := b.browser.Act(action)
	b.run(m, res)
	return handled
}

func (b *filesBlock) draw(m *model, room tui.Rect) ([]string, []tui.Target) {
	var lines []string
	if room.Height >= 16 {
		lines = append(m.paragraph(b.note, room.Width), "")
	}
	chosen := m.text.Lookup(textNone)
	if b.chosen != "" {
		chosen = tui.Escape(b.chosen)
	}
	lines = append(lines, m.text.Lookup(textFileChosen, chosen))
	view, ts := b.browser.View(m.styles, room.Width, max(room.Height-len(lines), 0))
	return append(lines, strings.Split(view, "\n")...), tui.OffsetTargets(ts, 0, len(lines))
}
