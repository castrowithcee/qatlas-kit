package helppages

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// Options changes how Load checks the pages. The zero value is the safe choice.
type Options struct {
	// Placeholder reports whether the application knows the placeholder name. When set, a body that holds
	// a placeholder it rejects does not load.
	Placeholder func(name string) bool
}

// Content is what a page says in one language.
type Content struct {
	Title    string
	Keywords []string
	// Link is an https address or empty.
	Link string
	// Text is the Markdown body.
	Text string
}

// Page is one help page.
type Page struct {
	// Name is the file name without .md and language.
	Name string
	// Order sorts the page when Ordered is set.
	Order   int
	Ordered bool
	// Screens are the screen IDs the page answers for.
	Screens []string

	path string
	en   Content
	tr   map[i18n.Lang]Content
}

// In returns the content of the page in lang: the translation when there is one, else the English text.
// The link of the English page stands in for a translation without a link.
func (p *Page) In(lang i18n.Lang) Content {
	c, ok := p.tr[lang]
	if !ok {
		return p.en
	}
	if c.Link == "" {
		c.Link = p.en.Link
	}
	return c
}

// Topic is a folder of pages.
type Topic struct {
	// ID is the folder name.
	ID string
	// Index is the page of the topic itself, from index.md.
	Index *Page
	// Pages are the other pages in display order.
	Pages []*Page
}

// Set is a loaded and checked page set.
type Set struct {
	// Topics are in display order.
	Topics  []*Topic
	screens map[string]screenRef
}

type screenRef struct {
	topic *Topic
	page  *Page
}

// Screen returns the topic and page that carry the screen ID.
func (s *Set) Screen(id string) (*Topic, *Page, bool) {
	r, ok := s.screens[id]
	return r.topic, r.page, ok
}

// Load reads the pages below the root of fsys and checks them. It reports every breach at once.
func Load(fsys fs.FS, opts Options) (*Set, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var errs []error
	if len(entries) == 0 {
		errs = append(errs, errors.New("no pages"))
	}
	var topics []*Topic
	for _, e := range entries {
		if !e.IsDir() {
			errs = append(errs, fmt.Errorf("%s: a file in the root, expected a topic folder", e.Name()))
			continue
		}
		t, es := loadTopic(fsys, e.Name(), opts)
		errs = append(errs, es...)
		if t != nil {
			topics = append(topics, t)
		}
	}
	s := &Set{screens: map[string]screenRef{}}
	for _, t := range topics {
		for _, p := range append([]*Page{t.Index}, t.Pages...) {
			for _, id := range p.Screens {
				if prev, dup := s.screens[id]; dup {
					errs = append(errs, fmt.Errorf("%s: screen %q is also carried by %s", p.path, id, prev.page.path))
					continue
				}
				s.screens[id] = screenRef{t, p}
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	slices.SortFunc(topics, func(a, b *Topic) int { return comparePages(a.Index, b.Index, a.ID, b.ID) })
	s.Topics = topics
	return s, nil
}

func comparePages(a, b *Page, nameA, nameB string) int {
	switch {
	case a.Ordered && !b.Ordered:
		return -1
	case !a.Ordered && b.Ordered:
		return 1
	}
	return cmp.Or(cmp.Compare(a.Order, b.Order), cmp.Compare(a.en.Title, b.en.Title), cmp.Compare(nameA, nameB))
}

type pageFiles struct {
	en    *Page
	tr    map[i18n.Lang]Content
	trDir map[i18n.Lang]string
}

func loadTopic(fsys fs.FS, dir string, opts Options) (*Topic, []error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, []error{err}
	}
	var errs []error
	files := map[string]*pageFiles{}
	var names []string
	for _, e := range entries {
		p := path.Join(dir, e.Name())
		if e.IsDir() {
			errs = append(errs, fmt.Errorf("%s: a folder below a topic, pages have two levels only", p))
			continue
		}
		base, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			errs = append(errs, fmt.Errorf("%s: not a Markdown file, expected .md", p))
			continue
		}
		lang := i18n.English
		if i := strings.LastIndexByte(base, '.'); i >= 0 {
			ext := base[i+1:]
			l, known := i18n.Parse(ext)
			switch {
			case !known || string(l) != ext:
				errs = append(errs, fmt.Errorf("%s: unknown language %q", p, ext))
				continue
			case l == i18n.English:
				errs = append(errs, fmt.Errorf("%s: English pages carry no language suffix", p))
				continue
			}
			lang, base = l, base[:i]
		}
		if base == "" {
			errs = append(errs, fmt.Errorf("%s: no page name", p))
			continue
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		page, es := parsePage(data, lang != i18n.English, opts)
		for _, e := range es {
			errs = append(errs, fmt.Errorf("%s: %w", p, e))
		}
		f := files[base]
		if f == nil {
			f = &pageFiles{tr: map[i18n.Lang]Content{}, trDir: map[i18n.Lang]string{}}
			files[base] = f
			names = append(names, base)
		}
		if lang == i18n.English {
			page.Name, page.path = base, p
			f.en = page
		} else {
			f.tr[lang], f.trDir[lang] = page.en, p
		}
	}
	var index *Page
	var pages []*Page
	for _, name := range names {
		f := files[name]
		if f.en == nil {
			for _, p := range f.trDir {
				errs = append(errs, fmt.Errorf("%s: translation without %s", p, path.Join(dir, name+".md")))
			}
			continue
		}
		f.en.tr = f.tr
		if name == "index" {
			index = f.en
		} else {
			pages = append(pages, f.en)
		}
	}
	if index == nil {
		errs = append(errs, fmt.Errorf("%s: topic without index.md", dir))
	}
	if len(errs) > 0 {
		return nil, errs
	}
	slices.SortFunc(pages, func(a, b *Page) int { return comparePages(a, b, a.Name, b.Name) })
	return &Topic{ID: dir, Index: index, Pages: pages}, nil
}

type frontMatter struct {
	Title    string   `yaml:"title"`
	Order    *int     `yaml:"order"`
	Keywords []string `yaml:"keywords"`
	Screens  []string `yaml:"screens"`
	Link     string   `yaml:"link"`
}

// parsePage returns the page with its content in en, whatever its language.
func parsePage(data []byte, translation bool, opts Options) (*Page, []error) {
	fm, body, err := splitFrontMatter(string(data))
	if err != nil {
		return &Page{}, []error{err}
	}
	var meta frontMatter
	dec := yaml.NewDecoder(bytes.NewReader([]byte(fm)))
	dec.KnownFields(true)
	if err := dec.Decode(&meta); err != nil && !errors.Is(err, io.EOF) {
		return &Page{}, []error{fmt.Errorf("front matter: %w", err)}
	}
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	switch {
	case strings.TrimSpace(meta.Title) == "":
		fail("title is missing or empty")
	case strings.ContainsFunc(meta.Title, unicode.IsControl):
		fail("title is not one line of printable text")
	}
	for _, k := range meta.Keywords {
		if strings.TrimSpace(k) == "" || strings.ContainsFunc(k, unicode.IsControl) {
			fail("keyword %q is empty or holds a control character", k)
		}
	}
	if translation && (meta.Order != nil || meta.Screens != nil) {
		fail("a translation carries title, keywords and link only")
	}
	seen := map[string]bool{}
	for _, id := range meta.Screens {
		if id == "" || strings.ContainsFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			fail("screen ID %q is empty or holds a blank or control character", id)
		} else if seen[id] {
			fail("screen ID %q is listed twice", id)
		}
		seen[id] = true
	}
	if meta.Link != "" {
		if err := checkLink(meta.Link); err != nil {
			fail("link %q %v", meta.Link, err)
		}
	}
	for _, b := range Blocks(body) {
		for _, s := range b.Spans {
			if s.Kind != Link {
				continue
			}
			if err := checkLink(s.URL); err != nil {
				fail("link %q in the text %v", s.URL, err)
			}
		}
	}
	if opts.Placeholder != nil {
		for _, name := range Placeholders(body) {
			if !opts.Placeholder(name) {
				fail("unknown placeholder {%s}", name)
			}
		}
	}
	p := &Page{Screens: meta.Screens, en: Content{Title: meta.Title, Keywords: meta.Keywords, Link: meta.Link, Text: body}}
	if meta.Order != nil {
		p.Order, p.Ordered = *meta.Order, true
	}
	return p, errs
}

// splitFrontMatter cuts the YAML between the "---" lines at the start of text from the body.
func splitFrontMatter(text string) (front, body string, err error) {
	text = strings.ReplaceAll(strings.TrimPrefix(text, "\xef\xbb\xbf"), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", "", errors.New("front matter is missing, the file must start with a --- line")
	}
	for pos := 4; pos <= len(text); {
		line, next := text[pos:], len(text)+1
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line, next = line[:nl], pos+nl+1
		}
		if strings.TrimRight(line, " \t") == "---" {
			return text[4:pos], strings.TrimSpace(text[min(next, len(text)):]), nil
		}
		pos = next
	}
	return "", "", errors.New("front matter is not closed by a --- line")
}

// checkLink accepts an absolute https address of printable ASCII with a host and no user information.
func checkLink(address string) error {
	for _, r := range address {
		if r <= ' ' || r > '~' {
			return errors.New("holds a blank, a control or a non-ASCII character")
		}
	}
	u, err := url.Parse(address)
	switch {
	case err != nil:
		return err
	case u.Scheme != "https":
		return errors.New("is no https address")
	case u.Host == "" || u.User != nil:
		return errors.New("names no host or carries user information")
	}
	return nil
}
