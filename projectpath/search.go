package projectpath

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charlievieth/fastwalk"
)

// Default limits of [Search], used for every limit of [SearchOptions] that is zero.
const (
	DefaultMaxEntries = 200_000
	DefaultTimeout    = 3 * time.Second
	DefaultMaxMatches = 50
)

// searchWorkers is the number of directories read at the same time.
const searchWorkers = 4

// Kind says which entries [Search] reports.
type Kind int

const (
	// Both reports directories and files. It is the zero value.
	Both Kind = iota
	// OnlyDirectories reports directories only.
	OnlyDirectories
	// OnlyFiles reports everything that is not a directory, links included.
	OnlyFiles
)

// End says why a search ended.
type End int

const (
	// Complete means the whole tree below the start was read.
	Complete End = iota
	// EntryLimit means MaxEntries entries were read.
	EntryLimit
	// TimeLimit means Timeout elapsed.
	TimeLimit
	// MatchLimit means MaxMatches matches were found.
	MatchLimit
	// Canceled means the context ended.
	Canceled
)

// String returns the name of e.
func (e End) String() string {
	switch e {
	case Complete:
		return "complete"
	case EntryLimit:
		return "entries"
	case TimeLimit:
		return "time"
	case MatchLimit:
		return "matches"
	case Canceled:
		return "canceled"
	}
	return fmt.Sprintf("End(%d)", int(e))
}

// SearchOptions holds the options of [Search]. The zero value searches for both kinds with the default
// limits.
type SearchOptions struct {
	Kind       Kind
	MaxEntries int           // entries read at most; default [DefaultMaxEntries]
	Timeout    time.Duration // time at most; default [DefaultTimeout]
	MaxMatches int           // matches at most; default [DefaultMaxMatches]
}

// Match is one entry whose name holds the search text.
type Match struct {
	Path string
	Dir  bool
	Link bool // a symbolic link, which is never followed; Dir is false for it
}

// SearchResult is the outcome of [Search]. Matches are ordered by [Search]. When End is not [Complete],
// the matches are those found before the end, not necessarily the best ones.
type SearchResult struct {
	Matches []Match
	End     End
	Entries int // entries read
	Skipped int // directories not entered: unreadable, on another device or excluded
}

var errStop = errors.New("projectpath: search stopped")

// SplitSearch splits typed of the form "<start>*<text>" at its first "*". ok is false, and the typed text
// stays a plain completion, when there is no "*" or one side is empty.
func SplitSearch(typed string) (start, text string, ok bool) {
	start, text, found := strings.Cut(typed, "*")
	return start, text, found && start != "" && text != ""
}

// Search finds the entries below start whose name holds text, by the rules of the package documentation.
// A start that is not a readable directory is an error; a search that ends early, by a limit or by ctx, is
// not: SearchResult.End says why it ended.
func Search(ctx context.Context, start, text string, opt SearchOptions) (SearchResult, error) {
	return search(ctx, start, text, opt, searchEnv{now: time.Now, device: deviceLookup(), excluded: excludedDirs()})
}

// searchEnv holds what tests replace: the clock, the device lookup and the excluded directories. A nil
// device switches the check off.
type searchEnv struct {
	now      func() time.Time
	device   func(path string) (id uint64, ok bool)
	excluded []string
}

type hit struct {
	Match
	depth, rank int
}

func search(ctx context.Context, start, text string, opt SearchOptions, env searchEnv) (SearchResult, error) {
	if text == "" {
		return SearchResult{}, errors.New("projectpath: empty search text")
	}
	if opt.Kind < Both || opt.Kind > OnlyFiles {
		return SearchResult{}, fmt.Errorf("projectpath: unknown kind %d", opt.Kind)
	}
	expanded, err := Expand(start)
	if err != nil {
		return SearchResult{}, err
	}
	root := Canonical(expanded)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return SearchResult{}, fmt.Errorf("projectpath: %q is not a readable directory", root)
	}
	var rootDev uint64
	if env.device != nil {
		var ok bool
		if rootDev, ok = env.device(root); !ok {
			return SearchResult{}, fmt.Errorf("projectpath: cannot read %q", root)
		}
	}
	maxEntries, timeout, maxMatches := opt.MaxEntries, opt.Timeout, opt.MaxMatches
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if maxMatches <= 0 {
		maxMatches = DefaultMaxMatches
	}
	needle := strings.ToLower(text)
	deadline := env.now().Add(timeout)

	var (
		entries, skipped atomic.Int64
		end              atomic.Int32 // the first reason to stop wins
		mu               sync.Mutex
		hits             []hit
	)
	stop := func(reason End) error {
		end.CompareAndSwap(int32(Complete), int32(reason))
		return errStop
	}
	walk := func(path string, d fs.DirEntry, err error) error {
		if end.Load() != int32(Complete) {
			return errStop
		}
		if err != nil { // the second call for a directory that could not be read
			skipped.Add(1)
			return nil
		}
		depth := fastwalk.DirEntryDepth(d)
		if depth == 0 {
			return nil
		}
		if ctx.Err() != nil {
			return stop(Canceled)
		}
		if !env.now().Before(deadline) {
			return stop(TimeLimit)
		}
		if entries.Add(1) > int64(maxEntries) {
			entries.Add(-1)
			return stop(EntryLimit)
		}
		name := d.Name()
		isDir := d.IsDir()
		if safeName(name) && (opt.Kind == Both || isDir == (opt.Kind == OnlyDirectories)) {
			if rank, ok := rankName(strings.ToLower(name), needle); ok {
				mu.Lock()
				if len(hits) >= maxMatches {
					mu.Unlock()
					return stop(MatchLimit)
				}
				hits = append(hits, hit{Match{path, isDir, d.Type()&fs.ModeSymlink != 0}, depth, rank})
				full := len(hits) >= maxMatches
				mu.Unlock()
				if full {
					return stop(MatchLimit)
				}
			}
		}
		if isDir && (slices.Contains(env.excluded, path) || !sameDevice(env, path, rootDev)) {
			skipped.Add(1)
			return fs.SkipDir
		}
		return nil
	}
	conf := fastwalk.Config{NumWorkers: searchWorkers}
	if err := fastwalk.Walk(&conf, root, walk); err != nil && !errors.Is(err, errStop) {
		return SearchResult{}, err
	}
	slices.SortFunc(hits, func(a, b hit) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank), cmp.Compare(a.depth, b.depth), strings.Compare(a.Path, b.Path))
	})
	res := SearchResult{End: End(end.Load()), Entries: int(entries.Load()), Skipped: int(skipped.Load())}
	for _, h := range hits {
		res.Matches = append(res.Matches, h.Match)
	}
	return res, nil
}

// rankName orders matches: 0 for the same name, 1 for a name that starts with the text, 2 for a name that
// holds it. Both arguments are lower case.
func rankName(name, needle string) (int, bool) {
	switch {
	case name == needle:
		return 0, true
	case strings.HasPrefix(name, needle):
		return 1, true
	case strings.Contains(name, needle):
		return 2, true
	}
	return 0, false
}

func sameDevice(env searchEnv, path string, rootDev uint64) bool {
	if env.device == nil {
		return true
	}
	dev, ok := env.device(path)
	return ok && dev == rootDev
}
