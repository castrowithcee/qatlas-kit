package helppages

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func page(front, body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("---\n" + front + "\n---\n" + body + "\n")}
}

func valid() fstest.MapFS {
	return fstest.MapFS{
		"start/index.md":    page("title: Start\norder: 1\nscreens: [home]", "Welcome, press {help}."),
		"start/index.de.md": page("title: Anfang", "Willkommen."),
		"start/keys.md":     page("title: Keys\norder: 2\nkeywords: [shortcut]\nlink: https://example.org/keys\nscreens: [keys]", "Text"),
		"start/keys.de.md":  page("title: Tasten\nkeywords: [Kürzel]", "Text de"),
		"more/index.md":     page("title: More\norder: 0", "More."),
	}
}

func mutate(f func(fstest.MapFS)) fstest.MapFS {
	m := valid()
	f(m)
	return m
}

func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
		want string
		opts Options
	}{
		{"empty set", fstest.MapFS{}, "no pages", Options{}},
		{"file in root", mutate(func(m fstest.MapFS) { m["stray.md"] = page("title: X", "") }), "stray.md: a file in the root", Options{}},
		{"deeper folder", mutate(func(m fstest.MapFS) { m["start/sub/a.md"] = page("title: X", "") }), "start/sub: a folder below a topic", Options{}},
		{"not markdown", mutate(func(m fstest.MapFS) { m["start/a.txt"] = page("title: X", "") }), "start/a.txt: not a Markdown file", Options{}},
		{"translation without English", mutate(func(m fstest.MapFS) { m["start/solo.de.md"] = page("title: X", "") }), "start/solo.de.md: translation without start/solo.md", Options{}},
		{"unknown language", mutate(func(m fstest.MapFS) { m["start/a.xx.md"] = page("title: X", "") }), `unknown language "xx"`, Options{}},
		{"explicit English", mutate(func(m fstest.MapFS) { m["start/a.en.md"] = page("title: X", "") }), "English pages carry no language suffix", Options{}},
		{"topic without index", mutate(func(m fstest.MapFS) { delete(m, "more/index.md"); m["more/a.md"] = page("title: A", "") }), "more: topic without index.md", Options{}},
		{"missing title", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("order: 3", "") }), "start/a.md: title is missing", Options{}},
		{"multiline title", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("title: \"a\\nb\"", "") }), "title is not one line", Options{}},
		{"unknown key", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("title: A\ncolour: red", "") }), "colour", Options{}},
		{"no front matter", mutate(func(m fstest.MapFS) { m["start/a.md"] = &fstest.MapFile{Data: []byte("# Hi")} }), "front matter is missing", Options{}},
		{"unclosed front matter", mutate(func(m fstest.MapFS) { m["start/a.md"] = &fstest.MapFile{Data: []byte("---\ntitle: A\n")} }), "not closed", Options{}},
		{"duplicate screen across pages", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("title: A\nscreens: [keys]", "") }), `screen "keys" is also carried`, Options{}},
		{"duplicate screen in page", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("title: A\nscreens: [x, x]", "") }), `screen ID "x" is listed twice`, Options{}},
		{"blank in screen", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("title: A\nscreens: [\"a b\"]", "") }), "blank or control", Options{}},
		{"http link", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("title: A\nlink: http://example.org", "") }), "is no https address", Options{}},
		{"link with user", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("title: A\nlink: https://u@example.org", "") }), "user information", Options{}},
		{"link in translation", mutate(func(m fstest.MapFS) { m["start/keys.de.md"] = page("title: T\nlink: ftp://example.org", "") }), "start/keys.de.md", Options{}},
		{"bad link in text", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("title: A", "See [x](http://example.org).") }), "in the text", Options{}},
		{"non-ASCII link in text", mutate(func(m fstest.MapFS) { m["start/a.md"] = page("title: A", "See [x](https://example.org/ä).") }), "non-ASCII", Options{}},
		{"order in translation", mutate(func(m fstest.MapFS) { m["start/keys.de.md"] = page("title: T\norder: 1", "") }), "a translation carries title, keywords and link only", Options{}},
		{"screens in translation", mutate(func(m fstest.MapFS) { m["start/keys.de.md"] = page("title: T\nscreens: [z]", "") }), "a translation carries title, keywords and link only", Options{}},
		{"unknown placeholder", valid(), "unknown placeholder {help}", Options{Placeholder: func(string) bool { return false }}},
		{"unknown placeholder in translation", mutate(func(m fstest.MapFS) { m["start/index.de.md"] = page("title: A", "Drücke {nope}.") }), "start/index.de.md: unknown placeholder {nope}", Options{Placeholder: func(n string) bool { return n == "help" }}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tt.fsys, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one holding %q", err, tt.want)
			}
		})
	}
}

func TestLoadReportsAllBreaches(t *testing.T) {
	m := mutate(func(m fstest.MapFS) {
		m["start/a.md"] = page("order: 1", "")
		m["start/b.md"] = page("title: B\nlink: http://example.org", "")
	})
	_, err := Load(m, Options{})
	if err == nil || !strings.Contains(err.Error(), "start/a.md") || !strings.Contains(err.Error(), "start/b.md") {
		t.Fatalf("error = %v, want both files named", err)
	}
}

func TestLoadAcceptsPlaceholdersWithoutOption(t *testing.T) {
	if _, err := Load(valid(), Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(valid(), Options{Placeholder: func(n string) bool { return n == "help" }}); err != nil {
		t.Fatal(err)
	}
}

func TestLanguageFallback(t *testing.T) {
	s, err := Load(valid(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, keys, ok := s.Screen("keys")
	if !ok {
		t.Fatal("screen keys not found")
	}
	de := keys.In(i18n.German)
	if de.Title != "Tasten" || de.Text != "Text de" || de.Keywords[0] != "Kürzel" {
		t.Errorf("German = %+v", de)
	}
	if de.Link != "https://example.org/keys" {
		t.Errorf("link = %q, want the English one", de.Link)
	}
	if en := keys.In(i18n.English); en.Title != "Keys" {
		t.Errorf("English = %+v", en)
	}
	// a page without translation shows English
	_, home, _ := s.Screen("home")
	if got := home.In(i18n.German).Title; got != "Anfang" {
		t.Errorf("title = %q", got)
	}
	s.Topics[0].Index.tr = nil
	if got := s.Topics[0].Index.In(i18n.German).Title; got != s.Topics[0].Index.en.Title {
		t.Errorf("fallback title = %q", got)
	}
}

func TestScreenUnknown(t *testing.T) {
	s, _ := Load(valid(), Options{})
	if _, _, ok := s.Screen("nope"); ok {
		t.Fatal("unknown screen found")
	}
}

func TestSorting(t *testing.T) {
	m := fstest.MapFS{
		"b/index.md": page("title: B\norder: 5", ""),
		"a/index.md": page("title: A", ""),
		"c/index.md": page("title: C\norder: 1", ""),
		"z/index.md": page("title: Z", ""),
		"c/p1.md":    page("title: Zed\norder: 2", ""),
		"c/p2.md":    page("title: Alpha", ""),
		"c/p3.md":    page("title: Beta\norder: 1", ""),
		"c/p4.md":    page("title: Alpha", ""),
		"c/p0.md":    page("title: Alpha", ""),
	}
	s, err := Load(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var topics, pages []string
	for _, tp := range s.Topics {
		topics = append(topics, tp.ID)
	}
	for _, p := range s.Topics[0].Pages {
		pages = append(pages, p.Name)
	}
	if want := []string{"c", "b", "a", "z"}; !slices.Equal(topics, want) {
		t.Errorf("topics = %v, want %v", topics, want)
	}
	if want := []string{"p3", "p1", "p0", "p2", "p4"}; !slices.Equal(pages, want) {
		t.Errorf("pages = %v, want %v", pages, want)
	}
	for _, p := range s.Topics[0].Pages {
		if p.Name == "index" {
			t.Fatal("index in pages")
		}
	}
}

func TestFrontMatterVariants(t *testing.T) {
	m := fstest.MapFS{"t/index.md": &fstest.MapFile{Data: []byte("---\r\ntitle: CRLF\r\n---\r\nBody\r\n")}}
	s, err := Load(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c := s.Topics[0].Index.In(i18n.English); c.Title != "CRLF" || c.Text != "Body" {
		t.Errorf("content = %+v", c)
	}
	m = fstest.MapFS{"t/index.md": &fstest.MapFile{Data: []byte("---\ntitle: End\n---")}}
	if _, err := Load(m, Options{}); err != nil {
		t.Fatal(err)
	}
}
