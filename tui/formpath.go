package tui

import (
	"fmt"
	"strings"
	"unicode"
)

// PathKind says what a path field takes.
type PathKind uint8

const (
	// PathDirs is a folder: every entry is a folder, and taking one ends the text with a separator. It is the
	// zero value.
	PathDirs PathKind = iota
	// PathFiles is a file or a folder: an entry that Continues is a folder to go on in and ends with a
	// separator when taken, any other entry is a file and the target.
	PathFiles
)

// PathSpec is what a path field adds to its FormField. Priority are the entries the application always
// offers first, such as known folders, with Text as it should stand in the field; the field shows those whose
// text starts with the typed text and marks them.
type PathSpec struct {
	Kind     PathKind
	Priority []Suggestion
}

func (p PathSpec) set() bool { return p.Kind != PathDirs || len(p.Priority) > 0 }

// PathLoad asks the application for the entries of the text typed into a path field. Search says that Typed
// holds "*": the entries are the matches of a search over several levels, which the application splits and
// runs and may refuse by answering without entries; otherwise they are the entries of the level Typed ends
// in. Token identifies the request: the answer must carry the token of the PathLoad it answers. A new
// request, with a new token, is the application's cue to cancel the one still running.
type PathLoad struct {
	Field  FieldID
	Typed  string
	Search bool
	Kind   PathKind
	Token  uint64
}

// PathEnd says why a search ended; the field shows a line for every reason but PathComplete.
type PathEnd uint8

const (
	// PathComplete is a search that ran to its end, or a plain level.
	PathComplete PathEnd = iota
	// PathEndEntries is a search that read as many entries as it may; PathResult.Limit counts them.
	PathEndEntries
	// PathEndTime is a search that ran as long as it may; PathResult.Limit is the time in seconds.
	PathEndTime
	// PathEndMatches is a search that found as many matches as it may; PathResult.Limit counts them.
	PathEndMatches
	// PathEndCanceled is a search that was canceled.
	PathEndCanceled
)

// PathResult is the answer to a PathLoad. Entries are the entries in the order shown: their Text is data of the
// application, which the field escapes to draw it and cleans as pasted text when it becomes the value. End and
// Limit say why a search ended, in the line under the entries.
type PathResult struct {
	Token   uint64
	Entries []Suggestion
	End     PathEnd
	Limit   int
}

// formPathRef leads the target item of an entry of a path field, followed by its index.
const formPathRef = "\x00P"

// asker is the ask function of the path line of field i: it numbers the request and, while the field has the
// focus, queues it for the application.
func (f *Form) asker(i int) func(string) uint64 {
	return func(typed string) uint64 {
		f.tokens++
		if i == f.focus {
			f.load = &PathLoad{Field: f.fields[i].ID, Typed: typed, Search: strings.Contains(typed, "*"),
				Kind: f.fields[i].Path.Kind, Token: f.tokens}
		}
		return f.tokens
	}
}

// wirePaths gives the path fields their source, their priority entries and the rules of taking an entry.
func (f *Form) wirePaths() {
	for i := range f.fields {
		field := &f.fields[i]
		if field.Kind != FieldPath {
			continue
		}
		line := &field.line
		line.ask, line.refPrefix = f.asker(i), formPathRef
		for _, p := range field.Path.Priority {
			p.priority = true
			line.priority = append(line.priority, p)
		}
		dirs := field.Path.Kind == PathDirs
		line.folder = func(c Suggestion) bool { return dirs || c.Continues }
	}
}

// pathErrors are the reasons the path settings of f are wrong.
func pathErrors(f FormField) []string {
	var out []string
	switch {
	case f.Kind != FieldPath && f.Path.set():
		out = append(out, "only a path field has path settings")
	case f.Path.Kind > PathFiles:
		out = append(out, fmt.Sprintf("unknown path kind %d", f.Path.Kind))
	}
	for _, p := range f.Path.Priority {
		if p.Text == "" || strings.IndexFunc(p.Text, unicode.IsControl) >= 0 {
			out = append(out, fmt.Sprintf("priority entry %q is not empty and holds no control character", p.Text))
		}
	}
	return out
}

// TakePathLoad returns the request the last change asks the application to answer, once: the application calls
// it after every key, paste, click, SetValue and move of the focus, and answers a request it gets with SetPath.
func (f *Form) TakePathLoad() (PathLoad, bool) {
	if f.load == nil {
		return PathLoad{}, false
	}
	load := *f.load
	f.load = nil
	return load, true
}

// SetPath shows the answer to a PathLoad under its field. It reports false and changes nothing for a stale
// answer, one whose token is not the token of the latest request of any path field. The marked entry stays.
func (f *Form) SetPath(r PathResult) bool {
	for i := range f.fields {
		if f.fields[i].Kind != FieldPath {
			continue
		}
		if f.fields[i].line.receive(r.Token, r.Entries, f.pathNotice(r)) {
			return true
		}
	}
	return false
}

// pathNotice is the line a search that ended early gets, empty for a complete one.
func (f *Form) pathNotice(r PathResult) string {
	switch r.End {
	case PathEndEntries:
		return f.text.Lookup(TextFormPathEndEntries, r.Limit)
	case PathEndTime:
		return f.text.Lookup(TextFormPathEndTime, r.Limit)
	case PathEndMatches:
		return f.text.Lookup(TextFormPathEndMatches, r.Limit)
	case PathEndCanceled:
		return f.text.Lookup(TextFormPathEndCancel)
	}
	return ""
}

// pathLine is the line of the focused path field, nil while another field or a button has the focus.
func (f *Form) pathLine() *suggestLine {
	if f.btn >= 0 || f.fields[f.focus].Kind != FieldPath {
		return nil
	}
	return &f.fields[f.focus].line
}

// pathMove moves the mark in the entries of the focused path field by one, down from the first without a mark
// and stopping at the last; up from the first entry takes the mark away. It reports false where the key is
// left to the form: no path field or no entry, and up without a mark.
func (f *Form) pathMove(by int) bool {
	l := f.pathLine()
	if l == nil || !l.shown() {
		return false
	}
	switch {
	case by > 0 && !l.marked:
		l.markAt(0)
	case by > 0:
		l.markAt(min(l.mark+1, len(l.items)-1))
	case !l.marked:
		return false
	case l.mark == 0:
		l.marked = false
	default:
		l.markAt(l.mark - 1)
	}
	return true
}

// canComplete reports whether tab would take the marked entry: there is one and it differs from the text.
func (f *Form) canComplete() bool {
	l := f.pathLine()
	return l != nil && l.marked && l.takenText(l.items[l.mark]) != l.String()
}

// complete takes the marked entry into the focused path field where canComplete says so.
func (f *Form) complete() bool {
	if !f.canComplete() {
		return false
	}
	f.pathLine().take(f.pathLine().mark)
	f.changed(f.focus)
	return true
}

// clickPath takes the entry a click on its row names.
func (f *Form) clickPath(item string) bool {
	l := f.pathLine()
	if l == nil {
		return false
	}
	i, ok := l.suggestionOf(item)
	if !ok {
		return false
	}
	l.take(i)
	f.changed(f.focus)
	return true
}
