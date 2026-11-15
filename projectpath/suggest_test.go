package projectpath

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func sep(s string) string { return strings.ReplaceAll(s, "/", string(filepath.Separator)) }

func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func touch(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(root, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func isolatedHome(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestSuggestEmptyAndTilde(t *testing.T) {
	for _, mode := range []Mode{Directories, Files} {
		s := NewSuggester(mode, sep("/start/dir"), "")
		if got, want := s.Suggest(""), []string{sep("/start/dir/")}; !reflect.DeepEqual(got, want) {
			t.Errorf("empty = %q, want %q", got, want)
		}
		if got, want := s.Suggest("~"), []string{"~" + string(filepath.Separator)}; !reflect.DeepEqual(got, want) {
			t.Errorf("~ = %q, want %q", got, want)
		}
		if got := NewSuggester(mode, "", "").Suggest(""); got != nil {
			t.Errorf("empty without start = %q", got)
		}
	}
	if got := NewSuggester(Mode(0), "x", "").Suggest("~"); got != nil {
		t.Errorf("invalid mode = %q", got)
	}
	if got := NewSuggester(Directories, sep("/start/dir/"), "").Suggest(""); got[0] != sep("/start/dir/") {
		t.Errorf("start with separator = %q", got)
	}
}

func TestSuggestModesAndOrder(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "beta", "alpha")
	touch(t, root, "a.txt", "alpine.txt", "Alpha.txt")
	dir := root + string(filepath.Separator)

	got := NewSuggester(Directories, "", "").Suggest(dir + "al")
	if want := []string{dir + "alpha" + sep("/")}; !reflect.DeepEqual(got, want) {
		t.Errorf("directories = %q, want %q", got, want)
	}
	got = NewSuggester(Files, "", "").Suggest(dir + "al")
	want := []string{dir + "alpha" + sep("/"), dir + "alpine.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files = %q, want %q", got, want)
	}
	// Case is respected, directories come before files.
	got = NewSuggester(Files, "", "").Suggest(dir)
	want = []string{dir + "alpha" + sep("/"), dir + "beta" + sep("/"), dir + "Alpha.txt", dir + "a.txt", dir + "alpine.txt"}
	if len(got) != len(want) {
		t.Fatalf("all = %q", got)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Errorf("%q missing from %q", w, got)
		}
	}
	if !strings.HasSuffix(got[1], string(filepath.Separator)) || strings.HasSuffix(got[2], string(filepath.Separator)) {
		t.Errorf("directories not first: %q", got)
	}
}

func TestSuggestRelativePerMode(t *testing.T) {
	work := t.TempDir()
	mkdirs(t, work, "sub/inner")
	touch(t, work, "sub/file.txt", "top.txt")

	if got := NewSuggester(Directories, "", work).Suggest("sub" + sep("/")); got != nil {
		t.Errorf("directories mode relative = %q", got)
	}
	if got := NewSuggester(Directories, "", work).Suggest("to"); got != nil {
		t.Errorf("directories mode bare = %q", got)
	}
	s := NewSuggester(Files, "", work)
	if got, want := s.Suggest("sub"+sep("/")), []string{sep("sub/inner/"), sep("sub/file.txt")}; !reflect.DeepEqual(got, want) {
		t.Errorf("relative dir = %q, want %q", got, want)
	}
	if got, want := s.Suggest("to"), []string{"top.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("bare = %q, want %q", got, want)
	}
	if got, want := s.Suggest("su"), []string{"sub" + sep("/")}; !reflect.DeepEqual(got, want) {
		t.Errorf("bare dir = %q, want %q", got, want)
	}
}

func TestSuggestHomeRelative(t *testing.T) {
	home := isolatedHome(t)
	mkdirs(t, home, "docs", ".config")
	touch(t, home, "notes.txt")
	for _, mode := range []Mode{Directories, Files} {
		s := NewSuggester(mode, "", "")
		got := s.Suggest("~" + sep("/") + "do")
		if want := []string{"~" + sep("/docs/")}; !reflect.DeepEqual(got, want) {
			t.Errorf("mode %d: %q, want %q", mode, got, want)
		}
	}
	got := NewSuggester(Files, "", "").Suggest("~" + sep("/"))
	if want := []string{"~" + sep("/docs/"), "~" + sep("/notes.txt")}; !reflect.DeepEqual(got, want) {
		t.Errorf("home = %q, want %q", got, want)
	}
}

func TestSuggestHiddenEntries(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, ".git", "src")
	touch(t, root, ".env", "x")
	dir := root + string(filepath.Separator)
	s := NewSuggester(Files, "", "")
	if got, want := s.Suggest(dir), []string{dir + "src" + sep("/"), dir + "x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("without dot = %q, want %q", got, want)
	}
	if got, want := s.Suggest(dir+"."), []string{dir + ".git" + sep("/"), dir + ".env"}; !reflect.DeepEqual(got, want) {
		t.Errorf("with dot = %q, want %q", got, want)
	}
	if got, want := s.Suggest(dir+".g"), []string{dir + ".git" + sep("/")}; !reflect.DeepEqual(got, want) {
		t.Errorf("with .g = %q, want %q", got, want)
	}
}

func TestSuggestSymlinks(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "real")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "linkdir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "linkbroken")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	dir := root + string(filepath.Separator)
	got := NewSuggester(Directories, "", "").Suggest(dir + "link")
	if want := []string{dir + "linkdir" + sep("/")}; !reflect.DeepEqual(got, want) {
		t.Errorf("directories = %q, want %q", got, want)
	}
	got = NewSuggester(Files, "", "").Suggest(dir + "link")
	if want := []string{dir + "linkdir" + sep("/"), dir + "linkbroken"}; !reflect.DeepEqual(got, want) {
		t.Errorf("files = %q, want %q", got, want)
	}
}

func TestSuggestUnreadableAndMissingDirectory(t *testing.T) {
	root := t.TempDir()
	dir := root + string(filepath.Separator)
	s := NewSuggester(Files, "", "")
	if got := s.Suggest(dir + "missing" + sep("/")); len(got) != 0 {
		t.Errorf("missing = %q", got)
	}
	if got := s.Suggest(dir + "plain.txt" + sep("/") + "x"); len(got) != 0 {
		t.Errorf("not a directory = %q", got)
	}
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not enforced like this on Windows")
	}
	locked := filepath.Join(root, "locked")
	mkdirs(t, root, "locked/inside")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("directory is readable despite mode 0, for instance as root")
	}
	if got := s.Suggest(locked + string(filepath.Separator)); len(got) != 0 {
		t.Errorf("unreadable = %q", got)
	}
}

func TestSuggestLimit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < MaxSuggestions+20; i++ {
		touch(t, root, "f"+string(rune('a'+i%26))+strings.Repeat("x", i/26)+".txt")
	}
	mkdirs(t, root, "zdir")
	dir := root + string(filepath.Separator)
	got := NewSuggester(Files, "", "").Suggest(dir)
	if len(got) != MaxSuggestions {
		t.Fatalf("got %d suggestions, want %d", len(got), MaxSuggestions)
	}
	if got[0] != dir+"zdir"+sep("/") {
		t.Errorf("directory not first: %q", got[0])
	}
}

func TestSuggestSkipsUnsafeNames(t *testing.T) {
	names := []string{"ok", "esc\x1bseq", "line\nbreak", "bidi\u202eexe", "zero\u200bwidth", "bad\xffutf8", "tab\there", "del\x7f"}
	s := NewSuggester(Files, "", "")
	var asked []string
	s.list = func(dir string) []entry {
		asked = append(asked, dir)
		var out []entry
		for _, n := range names {
			out = append(out, entry{n, false}, entry{n + "d", true})
		}
		return out
	}
	dir := sep("/some/dir/")
	got := s.Suggest(dir)
	if want := []string{dir + "okd" + sep("/"), dir + "ok"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggestUnsafeNamesOnDisk(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"ok", "bad\x1bname", "bidi\u202ename", "line\nbreak"} {
		if err := os.WriteFile(filepath.Join(root, n), nil, 0o644); err != nil {
			t.Skipf("name %q cannot be created here: %v", n, err)
		}
	}
	dir := root + string(filepath.Separator)
	got := NewSuggester(Files, "", "").Suggest(dir)
	if want := []string{dir + "ok"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggestReadsOnlyTypedDirectoryOnce(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "base")
	if runtime.GOOS == "windows" {
		root = `C:\base`
	}
	s := NewSuggester(Files, "", "")
	var asked []string
	s.list = func(dir string) []entry {
		asked = append(asked, dir)
		return []entry{{"one", true}, {"two", false}}
	}
	typed := filepath.Join(root, "inner") + string(filepath.Separator)
	s.Suggest(typed)
	s.Suggest(typed + "o")
	s.Suggest(typed + "t")
	if want := []string{filepath.Join(root, "inner")}; !reflect.DeepEqual(asked, want) {
		t.Errorf("read %q, want %q", asked, want)
	}
	s.Suggest("~")
	s.Suggest("")
	if len(asked) != 1 {
		t.Errorf("empty text and ~ must not read: %q", asked)
	}
}

func TestSuggestersDoNotShareCache(t *testing.T) {
	root := t.TempDir()
	dir := root + string(filepath.Separator)
	a := NewSuggester(Files, "", "")
	if got := a.Suggest(dir); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
	touch(t, root, "late")
	if got := a.Suggest(dir); len(got) != 0 {
		t.Errorf("same instance reads once: %q", got)
	}
	if got := NewSuggester(Files, "", "").Suggest(dir); len(got) != 1 {
		t.Errorf("new instance = %q", got)
	}
}
