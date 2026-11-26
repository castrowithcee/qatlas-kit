package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/projectpath"
	"github.com/castrowithcee/qatlas-kit/tui"
)

const (
	fieldFolder tui.FieldID = "folder"
	fieldFile   tui.FieldID = "file"
)

// pathAnswer is the answer to a PathLoad, tagged with the form that asked so that the answer to a form
// the reset button or a switch of the language replaced is dropped.
type pathAnswer struct {
	form *tui.Form
	res  tui.PathResult
}

// pathPriority are the folders the folder field offers first, found when the form is built: the home folder
// and the working directory of the demo. A folder that cannot be found is left out.
func pathPriority() []tui.Suggestion {
	var out []tui.Suggestion
	for _, find := range []func() (string, error){os.UserHomeDir, os.Getwd} {
		if dir, err := find(); err == nil && dir != "" {
			out = append(out, tui.Suggestion{Text: filepath.Clean(dir) + string(filepath.Separator), Continues: true})
		}
	}
	return out
}

// takeLoad starts the answer to the request the last change left in the form, if any. A new request cancels
// the search the last one started; its answer is stale by then and the form drops it.
func (b *formBlock) takeLoad() tea.Cmd {
	load, ok := b.form.TakePathLoad()
	if !ok {
		return nil
	}
	if b.cancel != nil {
		b.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	form := b.form
	return func() tea.Msg { return pathAnswer{form: form, res: answerPath(ctx, load)} }
}

// answerPath answers a request: the entries of a level, or the matches of a search. A start that cannot be
// read gives no entries. A Suggester is not safe for concurrent use, so every request gets its own.
func answerPath(ctx context.Context, load tui.PathLoad) tui.PathResult {
	res := tui.PathResult{Token: load.Token}
	if load.Search {
		return searchPath(ctx, load, res)
	}
	mode := projectpath.Directories
	if load.Kind == tui.PathFiles {
		mode = projectpath.Files
	}
	wd, _ := os.Getwd()
	for _, s := range projectpath.NewSuggester(mode, "", wd).Suggest(load.Typed) {
		res.Entries = append(res.Entries, tui.Suggestion{Text: s, Continues: len(s) > 0 && os.IsPathSeparator(s[len(s)-1])})
	}
	return res
}

func searchPath(ctx context.Context, load tui.PathLoad, res tui.PathResult) tui.PathResult {
	start, text, ok := projectpath.SplitSearch(load.Typed)
	if !ok {
		return res
	}
	opt := projectpath.SearchOptions{Kind: projectpath.OnlyDirectories}
	if load.Kind == tui.PathFiles {
		opt.Kind = projectpath.Both
	}
	found, err := projectpath.Search(ctx, start, text, opt)
	if err != nil {
		return res
	}
	for _, m := range found.Matches {
		res.Entries = append(res.Entries, tui.Suggestion{Text: m.Path, Continues: m.Dir})
	}
	switch found.End {
	case projectpath.EntryLimit:
		res.End, res.Limit = tui.PathEndEntries, projectpath.DefaultMaxEntries
	case projectpath.TimeLimit:
		res.End, res.Limit = tui.PathEndTime, int(projectpath.DefaultTimeout/time.Second)
	case projectpath.MatchLimit:
		res.End, res.Limit = tui.PathEndMatches, projectpath.DefaultMaxMatches
	case projectpath.Canceled:
		res.End = tui.PathEndCanceled
	}
	return res
}

// takePath gives the form that asked its answer; one for a form that is gone changes nothing.
func (m *model) takePath(a pathAnswer) {
	for _, b := range m.blocks {
		if fb, ok := b.(*formBlock); ok && fb.form == a.form {
			fb.form.SetPath(a.res)
		}
	}
}

// pathsLoad starts the answer to the request of the open form, if the open tab is a form.
func (m *model) pathsLoad() tea.Cmd {
	if fb, ok := m.live().(*formBlock); ok {
		return fb.takeLoad()
	}
	return nil
}

// checkPaths takes what is typed; the demo does not check that the paths exist, as the form leaves it to
// the application.
func checkPaths(m *model, v map[tui.FieldID]tui.FormValue) ([]tui.FormProblem, string) {
	return nil, m.text.Lookup(textFormPathsSaved, tui.Escape(v[fieldFolder].Text), tui.Escape(v[fieldFile].Text))
}
