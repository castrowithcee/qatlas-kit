package exteditor

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

var (
	vimArgs   = []string{"-n", "-i", "NONE", "-c", "set nobackup nowritebackup noundofile"}
	microArgs = []string{"-backup", "false", "-saveundo", "false"}
	nanoArgs  = []string{"--ignorercfiles"}
)

// fakeBin makes a directory of invented executables and puts only it on PATH.
func fakeBin(t *testing.T, names ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake executables are shell scripts")
	}
	dir := t.TempDir()
	for _, name := range names {
		target, link, isLink := strings.Cut(name, "->")
		if isLink {
			if err := os.Symlink(link, filepath.Join(dir, target)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestResolveOrder(t *testing.T) {
	bin := fakeBin(t, "micro", "nano", "vim", "ed")
	all := map[string]string{"VISUAL": "nano", "EDITOR": "vim"}
	for _, c := range []struct {
		name, pref string
		env        map[string]string
		path       string
		args       []string
	}{
		{"preference first", "micro -x", all, "micro", append(append([]string{}, microArgs...), "-x")},
		{"then VISUAL", "", all, "nano", nanoArgs},
		{"then EDITOR", " ", map[string]string{"EDITOR": "vim"}, "vim", vimArgs},
		{"then the fallbacks", "", nil, "micro", microArgs},
	} {
		got, err := Resolve(c.pref, env(c.env))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		want := Editor{Path: filepath.Join(bin, c.path), Args: c.args, NoTraces: true}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Resolve = %+v, want %+v", c.name, got, want)
		}
	}
}

func TestResolveFallbacksInOrder(t *testing.T) {
	bin := fakeBin(t, "nano", "vim", "vi->vim")
	if got, err := Resolve("", env(nil)); err != nil || got.Path != filepath.Join(bin, "nano") {
		t.Fatalf("Resolve = %+v, %v; want nano before vi", got, err)
	}
	bin = fakeBin(t, "vim", "vi->vim")
	got, err := Resolve("", env(nil))
	want := Editor{Path: filepath.Join(bin, "vi"), Args: vimArgs, NoTraces: true}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve = %+v, %v; want %+v", got, err, want)
	}
}

func TestResolveReadsTheProcessEnvironmentWithoutGetenv(t *testing.T) {
	bin := fakeBin(t, "ed")
	t.Setenv("VISUAL", "ed")
	t.Setenv("EDITOR", "")
	if got, err := Resolve("", nil); err != nil || got.Path != filepath.Join(bin, "ed") {
		t.Fatalf("Resolve = %+v, %v", got, err)
	}
}

func TestResolveRefusesWhatItCannotFind(t *testing.T) {
	t.Chdir(fakeBin(t, "vim"))
	for name, c := range map[string]struct {
		pref string
		env  map[string]string
	}{
		"missing preference does not fall through": {"sample-editor", map[string]string{"VISUAL": "vim"}},
		"missing VISUAL":       {"", map[string]string{"VISUAL": "sample-editor", "EDITOR": "vim"}},
		"split only at spaces": {"vim\t-R", nil},
		"relative path":        {"./vim", nil},
	} {
		if got, err := Resolve(c.pref, env(c.env)); !errors.Is(err, ErrNoEditor) {
			t.Errorf("%s: Resolve = %+v, %v; want ErrNoEditor", name, got, err)
		}
	}
	fakeBin(t, "ed")
	if got, err := Resolve("", env(nil)); !errors.Is(err, ErrNoEditor) {
		t.Errorf("no fallback: Resolve = %+v, %v; want ErrNoEditor", got, err)
	}
}

func TestResolveSwitchesOffTracesPerKnownEditor(t *testing.T) {
	bin := fakeBin(t, "vim", "vim.basic", "nvim", "nano", "micro", "ed", "busybox", "vi->busybox", "view->vim.basic")
	for name, want := range map[string]Editor{
		"vim":       {Args: vimArgs, NoTraces: true},
		"vim.basic": {Args: vimArgs, NoTraces: true},
		"view":      {Args: vimArgs, NoTraces: true},
		"nvim":      {Args: vimArgs, NoTraces: true},
		"nano":      {Args: nanoArgs, NoTraces: true},
		"micro":     {Args: microArgs, NoTraces: true},
		"ed":        {},
		"vi":        {},
	} {
		got, err := Resolve(name+"  -R", env(nil))
		want.Path = filepath.Join(bin, name)
		want.Args = append(append([]string{}, want.Args...), "-R")
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Resolve = %+v, %v; want %+v", name, got, err, want)
		}
	}
}

func TestExtensionRefusesPathsAndControlCharacters(t *testing.T) {
	for name, want := range map[string]string{"notes.md": ".md", "": "", "Makefile": "", "a..b.txt": ".txt", "x.": "."} {
		if got, err := extension(name); err != nil || got != want {
			t.Errorf("extension(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"../x.txt", "a/b", `a\b`, "..", ".", "x.t\x1bt", "x.t\nt", "x.\xff",
		"x." + strings.Repeat("e", maxExtension)} {
		if got, err := extension(name); err == nil {
			t.Errorf("extension(%q) = %q; want an error", name, got)
		}
	}
}

func TestCheckRefusesInvalidRequests(t *testing.T) {
	good := Request{Editor: Editor{Path: filepath.Join(string(filepath.Separator), "bin", "sample")},
		Dir: filepath.Join(string(filepath.Separator), "run", "sample"), MaxSize: 10, Text: []byte("text")}
	if runtime.GOOS == "windows" {
		good.Editor.Path, good.Dir = `C:\bin\sample.exe`, `C:\run\sample`
	}
	if err := good.check(); err != nil {
		t.Fatalf("check(%+v) = %v", good, err)
	}
	for name, c := range map[string]struct {
		change func(*Request)
		want   error
	}{
		"no editor":         {func(r *Request) { r.Editor = Editor{} }, ErrInvalidRequest},
		"relative editor":   {func(r *Request) { r.Editor.Path = "sample" }, ErrInvalidRequest},
		"relative dir":      {func(r *Request) { r.Dir = "run" }, ErrInvalidRequest},
		"no size limit":     {func(r *Request) { r.MaxSize = 0 }, ErrInvalidRequest},
		"path in name":      {func(r *Request) { r.Name = "../notes.txt" }, ErrInvalidRequest},
		"root target":       {func(r *Request) { r.Target = &Target{UID: 0, GID: 1000, Home: good.Dir} }, ErrInvalidRequest},
		"root group":        {func(r *Request) { r.Target = &Target{UID: 1000, GID: 0, Home: good.Dir} }, ErrInvalidRequest},
		"relative home":     {func(r *Request) { r.Target = &Target{UID: 1000, GID: 1000, Home: "home"} }, ErrInvalidRequest},
		"text too large":    {func(r *Request) { r.Text = []byte("eleven byte") }, ErrTooLarge},
		"binary text":       {func(r *Request) { r.Text = []byte("a\x00b") }, ErrBinary},
		"text not in UTF-8": {func(r *Request) { r.Text = []byte("caf\xe9") }, ErrNotUTF8},
	} {
		r := good
		c.change(&r)
		if err := r.check(); !errors.Is(err, c.want) {
			t.Errorf("%s: check = %v, want %v", name, err, c.want)
		}
	}
}
