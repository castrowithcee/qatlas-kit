package uiprefs

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// testSchema is an invented application with two themes, three areas and the shared global keys.
func testSchema(t *testing.T) Schema {
	t.Helper()
	act := func(id tui.ActionID, level tui.KeyLevel, r tui.Rebinding, keys ...string) tui.Action {
		return tui.Action{ID: id, Level: level, DefaultKeys: keys, Help: tui.TextID("sample." + id), Rebind: r}
	}
	keys, err := tui.NewKeymap([]tui.Action{
		act("save", tui.LevelGlobal, tui.RebindReplace, "f2"),
		act("help", tui.LevelGlobal, tui.RebindReplace, "?"),
		act("command", tui.LevelGlobal, tui.RebindReplace, ":"),
		act("lock", tui.LevelGlobal, tui.RebindReplace, "ctrl+l"),
		act("back", tui.LevelGlobal, tui.RebindNone, "esc"),
		act("info", tui.LevelLetter, tui.RebindExtra, "i"),
		act("reload", tui.LevelCtrl, tui.RebindNone, "ctrl+r"),
	}, tui.CharsetASCII)
	if err != nil {
		t.Fatal(err)
	}
	return Schema{Themes: []string{tui.ThemeTerminal, "sample dark", "sample light"}, Areas: []string{"home", "items", "settings"},
		DefaultArea: "home", Keys: keys}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoadMissingFileGivesDefaultsWithoutWarning(t *testing.T) {
	s := testSchema(t)
	got, warnings := Load(Path(t.TempDir()), s)
	want := Prefs{Theme: tui.ThemeTerminal, Start: "home", Icons: IconsUnicode, Language: LanguageEnglish, MouseEnabled: true}
	if !reflect.DeepEqual(got, want) || len(warnings) != 0 || !reflect.DeepEqual(Default(s), want) {
		t.Fatalf("Load() = %+v, %v; want %+v and no warning", got, warnings, want)
	}
}

func TestLoadDamagedFileGivesDefaultsAndWarning(t *testing.T) {
	s := testSchema(t)
	for _, data := range []string{"theme: [unclosed\n\t:", "- a list\n", "keys: f3\n"} {
		path := Path(t.TempDir())
		write(t, path, data)
		got, warnings := Load(path, s)
		if !reflect.DeepEqual(got, Default(s)) || len(warnings) != 1 || !strings.Contains(warnings[0], "cannot parse") {
			t.Errorf("Load(%q) = %+v, %v; want defaults and one parse warning", data, got, warnings)
		}
	}
}

func TestLoadUnreadablePathGivesDefaultsAndWarning(t *testing.T) {
	s := testSchema(t)
	path := Path(t.TempDir())
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	got, warnings := Load(path, s)
	if !reflect.DeepEqual(got, Default(s)) || len(warnings) != 1 || !strings.Contains(warnings[0], "cannot read") {
		t.Fatalf("Load() = %+v, %v; want defaults and one warning", got, warnings)
	}
}

func TestLoadInvalidValuesFallBackPerField(t *testing.T) {
	s := testSchema(t)
	path := Path(t.TempDir())
	write(t, path, "theme: neon\nstart: items\nicons: emoji\nlanguage: de\nmouse: sometimes\n")
	got, warnings := Load(path, s)
	want := Default(s)
	want.Start = "items"
	if !reflect.DeepEqual(got, want) || len(warnings) != 4 {
		t.Fatalf("Load() = %+v, %v; want %+v and four warnings", got, warnings, want)
	}
	for i, field := range []string{"theme \"neon\"", "icons \"emoji\"", "language \"de\"", "mouse \"sometimes\""} {
		if !strings.HasPrefix(warnings[i], path+": "+field) {
			t.Errorf("warning %d = %q, want it to name %s", i, warnings[i], field)
		}
	}
	write(t, path, "theme: \"\"\nstart: gone\n")
	if got, warnings := Load(path, s); !reflect.DeepEqual(got, Default(s)) || len(warnings) != 2 {
		t.Errorf("empty theme and unknown start = %+v, %v", got, warnings)
	}
}

func TestLoadIgnoresUnknownKeys(t *testing.T) {
	s := testSchema(t)
	path := Path(t.TempDir())
	write(t, path, "theme: sample dark\nfont: x\nkeys:\n  later: f6\n")
	got, warnings := Load(path, s)
	if got.Theme != "sample dark" || got.Keys != nil || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want the theme and no warning", got, warnings)
	}
}

func TestSaveRoundTripWritesOnlyKnownKeysAndDeviations(t *testing.T) {
	s := testSchema(t)
	path := Path(t.TempDir())
	write(t, path, "theme: sample dark\nextra: x\n")
	want := Prefs{Theme: "sample light", Start: "settings", Icons: IconsNerdFont, Language: LanguageGerman,
		Keys: map[tui.ActionID]string{"info": "f3", "save": "f4"}}
	if err := Save(path, want, s); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "theme: sample light\nstart: settings\nicons: nerdfont\nlanguage: deutsch\nmouse: false\n"+
		"keys:\n  info: f3\n  save: f4\n" {
		t.Fatalf("file = %q", got)
	}
	got, warnings := Load(path, s)
	if !reflect.DeepEqual(got, want) || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want %+v", got, warnings, want)
	}
	standard := Default(s)
	standard.Keys = map[tui.ActionID]string{"save": "F2", "info": ""}
	if err := Save(path, standard, s); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); strings.Contains(got, "keys") || !strings.Contains(got, "mouse: true\n") {
		t.Fatalf("default keys were written:\n%s", got)
	}
}

func TestSaveRefusesInvalidPrefsAndWritesNothing(t *testing.T) {
	s := testSchema(t)
	dir := t.TempDir()
	base := Default(s)
	for name, change := range map[string]func(*Prefs){
		"theme":          func(p *Prefs) { p.Theme = "neon" },
		"empty theme":    func(p *Prefs) { p.Theme = "" },
		"start":          func(p *Prefs) { p.Start = "gone" },
		"icons":          func(p *Prefs) { p.Icons = "emoji" },
		"language":       func(p *Prefs) { p.Language = "fr" },
		"empty language": func(p *Prefs) { p.Language = "" },
		"key rule":       func(p *Prefs) { p.Keys = map[tui.ActionID]string{"help": "ctrl+c"} },
		"key collision":  func(p *Prefs) { p.Keys = map[tui.ActionID]string{"save": "f3", "info": "f3"} },
		"fixed key":      func(p *Prefs) { p.Keys = map[tui.ActionID]string{"back": "f3"} },
		"unknown action": func(p *Prefs) { p.Keys = map[tui.ActionID]string{"later": "f3"} },
	} {
		p := base
		change(&p)
		if err := Save(Path(dir), p, s); err == nil {
			t.Errorf("%s: Save accepted %+v", name, p)
		}
	}
	noKeys := s
	noKeys.Keys = nil
	p := base
	p.Keys = map[tui.ActionID]string{"save": "f4"}
	if err := Save(Path(dir), p, noKeys); err == nil {
		t.Error("Save wrote keys for an application without a key table")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("directory holds %v; want it empty", entries)
	}
}

func TestInvalidSchemaIsReported(t *testing.T) {
	good := testSchema(t)
	for name, change := range map[string]func(*Schema){
		"no terminal theme": func(s *Schema) { s.Themes = []string{"sample dark"} },
		"theme twice":       func(s *Schema) { s.Themes = append(s.Themes, "sample dark") },
		"empty area":        func(s *Schema) { s.Areas = append(s.Areas, "") },
		"default area":      func(s *Schema) { s.DefaultArea = "gone" },
		"no areas":          func(s *Schema) { s.Areas, s.DefaultArea = nil, "" },
	} {
		s := good
		s.Themes, s.Areas = append([]string(nil), good.Themes...), append([]string(nil), good.Areas...)
		change(&s)
		path := Path(t.TempDir())
		if _, warnings := Load(path, s); len(warnings) != 1 || !strings.Contains(warnings[0], "schema") {
			t.Errorf("%s: Load warnings = %v", name, warnings)
		}
		if err := Save(path, Default(good), s); err == nil {
			t.Errorf("%s: Save accepted the schema", name)
		}
	}
}

func TestInvalidKeysFallBackWithWarning(t *testing.T) {
	s := testSchema(t)
	path := Path(t.TempDir())
	write(t, path, "keys:\n  info: f5\n  save: f3\n  help: f3\n  lock: x\n  back: f6\n  command: ctrl+x\n")
	got, warnings := Load(path, s)
	if want := map[tui.ActionID]string{"command": "ctrl+x"}; !reflect.DeepEqual(got.Keys, want) {
		t.Fatalf("keys = %v, want %v", got.Keys, want)
	}
	if len(warnings) != 5 {
		t.Fatalf("warnings = %v, want one per dropped key", warnings)
	}
	for _, w := range []string{"keys.info f5: terminals take", "keys.lock x: lock acts on text lines", "keys.back f6: cannot be rebound",
		"keys.save f3: already the key of help, using F2", "keys.help f3: already the key of save, using ?", "using no extra key"} {
		found := false
		for _, got := range warnings {
			found = found || strings.Contains(got, w)
		}
		if !found {
			t.Errorf("no warning with %q in %v", w, warnings)
		}
	}
	if err := s.Keys.Bind(got.Keys); err != nil {
		t.Errorf("the loaded keys do not bind: %v", err)
	}
}

func TestFileSwapIsValidAsAWhole(t *testing.T) {
	s := testSchema(t)
	path := Path(t.TempDir())
	write(t, path, "keys:\n  save: ctrl+l\n  lock: ctrl+x\n")
	got, warnings := Load(path, s)
	if want := map[tui.ActionID]string{"save": "ctrl+l", "lock": "ctrl+x"}; !reflect.DeepEqual(got.Keys, want) || len(warnings) != 0 {
		t.Fatalf("keys = %v, warnings %v; want the swap kept", got.Keys, warnings)
	}
	if err := Save(path, got, s); err != nil {
		t.Fatalf("the swap does not save: %v", err)
	}
}

func TestKeysWithoutAKeyTableWarn(t *testing.T) {
	s := testSchema(t)
	s.Keys = nil
	path := Path(t.TempDir())
	write(t, path, "keys:\n  save: f4\n")
	got, warnings := Load(path, s)
	if got.Keys != nil || len(warnings) != 1 {
		t.Fatalf("Load() = %+v, %v; want no keys and one warning", got, warnings)
	}
}

func TestMouseRoundTrip(t *testing.T) {
	s := testSchema(t)
	path := Path(t.TempDir())
	off := Default(s)
	off.MouseEnabled = false
	if err := Save(path, off, s); err != nil {
		t.Fatal(err)
	}
	if got, warnings := Load(path, s); got.MouseEnabled || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want the mouse off", got, warnings)
	}
	write(t, path, "mouse: true\n")
	if got, _ := Load(path, s); !got.MouseEnabled {
		t.Fatal("mouse: true reads as off")
	}
}

func TestLanguageValuesLoad(t *testing.T) {
	s := testSchema(t)
	path := Path(t.TempDir())
	for _, v := range []string{LanguageEnglish, LanguageGerman, LanguageSystem} {
		write(t, path, "language: "+v+"\n")
		if got, warnings := Load(path, s); got.Language != v || len(warnings) != 0 {
			t.Errorf("language %s = %+v, %v", v, got, warnings)
		}
	}
}

// The last writer wins: a damaged file or one another writer changed is replaced, and the replaced bytes
// stay in a backup.
func TestSaveReplacesWhatIsThere(t *testing.T) {
	s := testSchema(t)
	dir := t.TempDir()
	path := Path(dir)
	write(t, path, "theme: [unclosed\n")
	if err := Save(path, Default(s), s); err != nil {
		t.Fatalf("Save over a damaged file: %v", err)
	}
	if got, warnings := Load(path, s); !reflect.DeepEqual(got, Default(s)) || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v", got, warnings)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "backups", FileName+"-*.bak"))
	if err != nil || len(backups) != 1 || read(t, backups[0]) != "theme: [unclosed\n" {
		t.Fatalf("backups = %v, %v; want the damaged bytes kept once", backups, err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temporary file is left: %s", e.Name())
		}
	}
}

func TestSaveCreatesAPrivateFileInANewDirectory(t *testing.T) {
	s := testSchema(t)
	path := Path(filepath.Join(t.TempDir(), "new", "dir"))
	if err := Save(path, Default(s), s); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestSaveWritesThroughASymlink(t *testing.T) {
	s := testSchema(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", FileName)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, target, "theme: sample dark\n")
	path := Path(dir)
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	p := Default(s)
	p.Theme = "sample light"
	if err := Save(path, p, s); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v", err)
	}
	if !strings.Contains(read(t, target), "theme: sample light") {
		t.Fatalf("target = %q", read(t, target))
	}
}

func TestPathIsInTheApplicationFolder(t *testing.T) {
	if got := Path(filepath.Join("a", "b")); got != filepath.Join("a", "b", FileName) {
		t.Errorf("Path = %q", got)
	}
}
