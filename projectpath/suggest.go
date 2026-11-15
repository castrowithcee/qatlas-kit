package projectpath

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Mode says which entries a Suggester offers.
type Mode int

const (
	// Directories offers directories only, and completes only absolute and home-relative paths.
	Directories Mode = iota + 1
	// Files offers directories and files, and also completes paths relative to the working directory. The
	// file itself may not exist yet, so only its directory part has to name a directory.
	Files
)

// MaxSuggestions bounds the suggestions returned for one typed text.
const MaxSuggestions = 100

// entry is one name of a listed directory.
type entry struct {
	name string
	dir  bool
}

// Suggester completes a typed file or directory path from the file system. Its cache belongs to the
// instance, so a new Suggester sees the file system anew. It is not safe for concurrent use.
type Suggester struct {
	mode    Mode
	start   string
	workDir string
	listed  map[string][]entry
	list    func(dir string) []entry // replaced by tests
}

// NewSuggester returns a Suggester for mode. start is the single suggestion for an empty text, usually the
// directory a dialog opens in; workDir is what a relative path is resolved against in Files mode, and the
// process working directory when empty.
func NewSuggester(mode Mode, start, workDir string) *Suggester {
	return &Suggester{mode: mode, start: start, workDir: workDir, list: readEntries}
}

// Suggest returns at most MaxSuggestions completions of typed by the rules of the package documentation.
func (s *Suggester) Suggest(typed string) []string {
	if s.mode != Directories && s.mode != Files {
		return nil
	}
	if typed == "" {
		if s.start == "" {
			return nil
		}
		return []string{withSeparator(s.start)}
	}
	if typed == "~" {
		return []string{"~" + string(filepath.Separator)}
	}
	cut := strings.LastIndexFunc(typed, isSeparator) + 1
	dir, rest := typed[:cut], typed[cut:]
	lookup := dir
	relative := !IsHomeRelative(dir) && !filepath.IsAbs(dir)
	if relative {
		if s.mode != Files {
			return nil
		}
		lookup = filepath.Join(s.base(), dir)
	}
	expanded, err := Expand(lookup)
	if err != nil {
		return nil
	}
	var dirs, files []string
	for _, e := range s.entries(filepath.Clean(expanded)) {
		if !strings.HasPrefix(e.name, rest) || (e.name[0] == '.' && rest == "") || !safeName(e.name) {
			continue
		}
		switch {
		case e.dir:
			dirs = append(dirs, dir+e.name+string(filepath.Separator))
		case s.mode == Files:
			files = append(files, dir+e.name)
		}
	}
	all := append(dirs, files...)
	return all[:min(len(all), MaxSuggestions)]
}

func (s *Suggester) base() string {
	if s.workDir == "" {
		return "."
	}
	return s.workDir
}

func (s *Suggester) entries(dir string) []entry {
	if e, ok := s.listed[dir]; ok {
		return e
	}
	e := s.list(dir)
	if s.listed == nil {
		s.listed = map[string][]entry{}
	}
	s.listed[dir] = e
	return e
}

// readEntries lists dir; a directory that cannot be read has no entries. A link to a directory counts as a
// directory, a broken link as a file.
func readEntries(dir string) []entry {
	read, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	entries := make([]entry, 0, len(read))
	for _, e := range read {
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(dir, e.Name()))
			isDir = err == nil && info.IsDir()
		}
		entries = append(entries, entry{e.Name(), isDir})
	}
	return entries
}

// safeName reports whether a file name is fit to show: valid UTF-8 without control or format characters.
func safeName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func isSeparator(r rune) bool {
	return r < 0x80 && os.IsPathSeparator(uint8(r))
}

func withSeparator(p string) string {
	if p != "" && os.IsPathSeparator(p[len(p)-1]) {
		return p
	}
	return p + string(filepath.Separator)
}
