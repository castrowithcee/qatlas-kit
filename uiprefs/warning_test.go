package uiprefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/configfile"
	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// Every warning of Load has a text in both languages, keeps its English text and translates through the
// translator of a tool.
func TestWarningsAreTranslatable(t *testing.T) {
	noKeys := testSchema(t)
	noKeys.Keys = nil
	badSchema := testSchema(t)
	badSchema.Themes = nil
	cases := []struct {
		name   string
		schema Schema
		data   string
		dir    bool // the path is a directory, so reading fails
		id     tui.TextID
		want   string // the English text with the path left as %s
		german string
	}{
		{name: "schema", schema: badSchema, id: tui.TextPrefsSchemaInvalid,
			want: `invalid preferences schema: the themes lack "terminal"`},
		{name: "unreadable", schema: testSchema(t), dir: true, id: tui.TextPrefsUnreadable},
		{name: "unparsable", schema: testSchema(t), data: "- a list\n", id: tui.TextPrefsUnparsable},
		{name: "notoneof", schema: testSchema(t), data: "theme: \"x\\ty\"\n", id: tui.TextPrefsNotOneOf,
			want:   `%s: theme "x\ty" is not one of ["terminal" "sample dark" "sample light"], using terminal`,
			german: `%s: theme "x\ty" ist keiner von ["terminal" "sample dark" "sample light"], verwendet wird terminal`},
		{name: "navigation", schema: testSchema(t), data: "navigation: tabs\n", id: tui.TextPrefsNavigationNotOffered,
			want: `%s: navigation: this application does not offer the setting, using its own form`},
		{name: "editor", schema: testSchema(t), data: "editor: \"vi\\x07\"\n", id: tui.TextPrefsEditorInvalid,
			want: `%s: editor "vi\a" holds a control character or invalid UTF-8, using none`},
		{name: "keysnone", schema: noKeys, data: "keys:\n  save: f4\n", id: tui.TextPrefsKeysNone,
			want: `%s: keys: this application has no keys to rebind, using the defaults`},
		{name: "keyinvalid", schema: testSchema(t), data: "keys:\n  save: \"?\"\n", id: tui.TextPrefsKeyInvalid},
		{name: "keyinvalidnoextra", schema: testSchema(t), data: "keys:\n  info: \"ctrl+l\"\n", id: tui.TextPrefsKeyInvalidNoExtra},
	}
	translator, err := tui.NewTranslator(i18n.German, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[tui.TextID]bool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			if c.dir {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if c.data != "" {
				write(t, path, c.data)
			}
			_, texts := Load(path, c.schema)
			_, warnings := LoadWarnings(path, c.schema)
			if len(warnings) != 1 || len(texts) != 1 || warnings[0].ID != c.id {
				t.Fatalf("warnings = %+v, texts = %q; want one with ID %q", warnings, texts, c.id)
			}
			seen[c.id] = true
			if texts[0] != warnings[0].String() {
				t.Errorf("Load text %q differs from String %q", texts[0], warnings[0].String())
			}
			if c.want != "" && texts[0] != strings.Replace(c.want, "%s", path, 1) {
				t.Errorf("English text = %q, want %q", texts[0], strings.Replace(c.want, "%s", path, 1))
			}
			de := warnings[0].Format(translator)
			if de == texts[0] || strings.Contains(de, "%!") || de == string(c.id) {
				t.Errorf("German text = %q, English %q", de, texts[0])
			}
			if c.german != "" && de != strings.Replace(c.german, "%s", path, 1) {
				t.Errorf("German text = %q, want %q", de, strings.Replace(c.german, "%s", path, 1))
			}
		})
	}
	for _, id := range []tui.TextID{tui.TextPrefsSchemaInvalid, tui.TextPrefsUnreadable, tui.TextPrefsUnparsable,
		tui.TextPrefsNotOneOf, tui.TextPrefsNavigationNotOffered, tui.TextPrefsEditorInvalid, tui.TextPrefsKeysNone,
		tui.TextPrefsKeyInvalid, tui.TextPrefsKeyInvalidNoExtra} {
		if !seen[id] {
			t.Errorf("no case yields %q", id)
		}
	}
	catalog := tui.ModuleCatalog()
	if err := tui.CheckCatalogs(catalog); err != nil {
		t.Error(err)
	}
	for id := range catalog[i18n.English] {
		if strings.HasPrefix(string(id), "tui.prefs.") && !seen[id] {
			t.Errorf("catalog text %q is yielded by no warning", id)
		}
	}
}

// configfile already names the file around a decode error, so the error of the codec holds the path once
// and the text of the first warning without a prefix. Neither Load nor Save decodes, so the test reads the
// store the way a tool that holds the codec would.
func TestDecodeErrorNamesThePathOnce(t *testing.T) {
	s := testSchema(t)
	path := Path(t.TempDir())
	write(t, path, "theme: nowhere\n")
	store, err := configfile.NewStore(path, codec(s))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.LoadVersioned()
	if err == nil {
		t.Fatal("the codec accepted a file with an invalid value")
	}
	if n := strings.Count(err.Error(), path); n != 1 || !strings.Contains(err.Error(), `theme "nowhere" is not one of`) {
		t.Errorf("err = %v; want the path once and the warning", err)
	}
}
