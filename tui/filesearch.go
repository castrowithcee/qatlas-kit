package tui

import (
	"slices"
	"strconv"
	"strings"
)

// syncSearch looks at the filter after a key or a click changed it: a filter of "*" and a text asks for
// matches, any other leaves the list of matches. It asks once per change.
func (b *FileBrowser) syncSearch() FileResult {
	q := b.list.Query()
	text, ok := strings.CutPrefix(q, "*")
	text = strings.TrimSpace(text)
	if ok && text != "" {
		q = "*" + text // white space around the text is no new search
	}
	if q == b.seen {
		return FileResult{}
	}
	b.seen = q
	if !ok || text == "" {
		b.leaveHits()
		return FileResult{}
	}
	b.stoken++
	b.hits, b.waiting, b.matches, b.note = true, true, nil, ""
	b.draw(nil, b.text.Lookup(TextFileSearching))
	return FileResult{Search: &FileSearch{Dir: slices.Clone(b.dir), Query: text, Token: b.stoken}}
}

// leaveHits shows the entries of the folder again; the filter text stays.
func (b *FileBrowser) leaveHits() {
	if !b.hits {
		return
	}
	b.hits, b.waiting, b.matches, b.note = false, false, nil, ""
	b.draw(b.items, b.empty)
}

// hitKey reads back and enter in the list of matches: back leaves it, enter jumps to the marked match even
// while the filter is typed.
func (b *FileBrowser) hitKey(id ActionID) (FileResult, bool) {
	k := b.spec.Keys
	switch {
	case !b.hits:
	case id == k.Back:
		b.leaveHits()
		return FileResult{}, true
	case id == k.Enter:
		if sel, ok := b.list.Selected(); ok {
			return b.activate(sel), true
		}
	}
	return FileResult{}, false
}

// SetMatches shows the matches answering search. current is false, and nothing changes, when search is not the
// last request or the list of matches was left. A match that leads outside the root or the searched folder, or
// holds a part that is not a plain name, is dropped; dropped counts them. The reason found.End names stands
// in a line under the list.
func (b *FileBrowser) SetMatches(search FileSearch, found FileFound) (current bool, dropped int) {
	if search.Token != b.stoken || !b.hits || !b.waiting {
		return false, 0
	}
	b.waiting = false
	query := b.list.Query()
	var items []ListItem
	b.matches = map[ItemID]FileMatch{}
	for _, m := range found.Matches {
		if len(m.Path) <= len(search.Dir) || !slices.Equal(m.Path[:len(search.Dir)], search.Dir) || m.Kind > FileLink ||
			slices.ContainsFunc(m.Path, func(n string) bool { return !validFileName(n) }) {
			dropped++
			continue
		}
		id := ItemID("m" + strconv.Itoa(len(items)))
		b.matches[id] = m
		name := strings.Join(m.Path[len(search.Dir):], "/")
		if m.Kind == FileDir {
			name += "/"
		}
		// The filter text is part of what the list matches, so the typed "*" does not hide a match.
		items = append(items, ListItem{ID: id, Cells: []string{name, "", "", "", ""}, SearchText: query})
	}
	b.draw(items, b.text.Lookup(TextFileSearchNone))
	b.note = b.endNote(found)
	return true, dropped
}

// SetSearchError shows that the search of search failed, with the reason cause gives; like SetMatches it
// ignores a search that is not the last request. cause is untrusted text and shown escaped.
func (b *FileBrowser) SetSearchError(search FileSearch, cause error) (current bool) {
	if search.Token != b.stoken || !b.hits || !b.waiting {
		return false
	}
	b.waiting = false
	reason := ""
	if cause != nil {
		reason = cause.Error()
	}
	b.draw(nil, b.text.Lookup(TextFileSearchFailed, reason))
	return true
}

// endNote is the line that says why the search stopped early, empty for a complete one.
func (b *FileBrowser) endNote(f FileFound) string {
	var id TextID
	switch {
	case f.End == FileSearchComplete:
		return ""
	case f.End == FileSearchCanceled:
		return b.text.Lookup(TextFileSearchCanceled)
	case f.Limit <= 0:
		return b.text.Lookup(TextFileSearchStopped)
	case f.End == FileSearchEntryLimit:
		id = TextFileSearchEntries
	case f.End == FileSearchTimeLimit:
		id = TextFileSearchTime
	case f.End == FileSearchMatchLimit:
		id = TextFileSearchMatches
	default:
		return b.text.Lookup(TextFileSearchStopped)
	}
	return b.text.Lookup(id, f.Limit)
}
