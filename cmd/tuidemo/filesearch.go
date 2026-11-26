package main

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/castrowithcee/qatlas-kit/projectpath"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// searchDone is the answer of a search that ran off the screen loop, for the block that asked.
type searchDone struct {
	block  *filesBlock
	search tui.FileSearch
	res    projectpath.SearchResult
	err    error
}

// search runs the request of the browser below the shown folder and answers with a searchDone. It stops the
// search before, which the browser has stopped waiting for.
func (b *filesBlock) search(m *model, s tui.FileSearch) {
	b.stopSearch()
	start, err := below(b.root, s.Dir)
	if err != nil {
		b.browser.SetSearchError(s, err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	send := m.send
	go func() {
		res, err := projectpath.Search(ctx, start, s.Query, projectpath.SearchOptions{})
		if send != nil {
			send(searchDone{b, s, res, err})
		}
	}()
}

// stopSearch ends the running search, if any.
func (b *filesBlock) stopSearch() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

// answer hands the result of a search to the browser: the matches as name parts below the root, those that
// lead outside it left out, and the reason the search ended with the limit that applies.
func (b *filesBlock) answer(d searchDone) {
	if d.err != nil {
		b.browser.SetSearchError(d.search, d.err)
		return
	}
	found := tui.FileFound{}
	for _, mt := range d.res.Matches {
		rel, err := filepath.Rel(b.root, mt.Path)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		kind := tui.FileRegular
		switch {
		case mt.Dir:
			kind = tui.FileDir
		case mt.Link:
			kind = tui.FileLink
		}
		found.Matches = append(found.Matches, tui.FileMatch{Path: strings.Split(rel, string(filepath.Separator)), Kind: kind})
	}
	switch d.res.End {
	case projectpath.EntryLimit:
		found.End, found.Limit = tui.FileSearchEntryLimit, projectpath.DefaultMaxEntries
	case projectpath.TimeLimit:
		found.End, found.Limit = tui.FileSearchTimeLimit, int(projectpath.DefaultTimeout.Seconds())
	case projectpath.MatchLimit:
		found.End, found.Limit = tui.FileSearchMatchLimit, projectpath.DefaultMaxMatches
	case projectpath.Canceled:
		found.End = tui.FileSearchCanceled
	}
	b.browser.SetMatches(d.search, found)
}
