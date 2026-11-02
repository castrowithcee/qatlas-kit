package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

type keyTable map[ActionID]string

func (k keyTable) KeyLabel(id ActionID) (string, bool) {
	label, ok := k[id]
	return label, ok
}

// declaredTextIDs are the IDs declared as constants of catalog.go.
func declaredTextIDs(t *testing.T) []TextID {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "catalog.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []TextID
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			for _, value := range spec.(*ast.ValueSpec).Values {
				if lit, ok := value.(*ast.BasicLit); ok {
					id, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					ids = append(ids, TextID(id))
				}
			}
		}
	}
	return ids
}

func TestModuleCatalogIsComplete(t *testing.T) {
	catalog := ModuleCatalog()
	if err := CheckCatalogs(catalog); err != nil {
		t.Fatal(err)
	}
	ids := declaredTextIDs(t)
	if len(ids) < 50 {
		t.Fatalf("found %d declared IDs, the scan is broken", len(ids))
	}
	for _, id := range ids {
		if !strings.HasPrefix(string(id), modulePrefix) {
			t.Errorf("%q lacks the module prefix", id)
		}
		if catalog[i18n.English][id] == "" {
			t.Errorf("%q has no English text", id)
		}
	}
	if len(catalog[i18n.English]) != len(ids) {
		t.Errorf("the English catalog holds %d texts, catalog.go declares %d", len(catalog[i18n.English]), len(ids))
	}
	if _, err := NewTranslator(i18n.German, nil, nil); err != nil {
		t.Fatalf("the module catalog alone: %v", err)
	}
}

func TestModuleCatalogReturnsACopy(t *testing.T) {
	ModuleCatalog()[i18n.English][TextCancelled] = "changed"
	if got := ModuleCatalog()[i18n.English][TextCancelled]; got != "Cancelled" {
		t.Errorf("a changed copy leaked into the next call: %q", got)
	}
}

func TestNewTranslatorRejectsCatalogsThatDrawWrongText(t *testing.T) {
	keys := keyTable{"save": "F2"}
	cases := []struct {
		name string
		app  i18n.Catalogs[TextID]
		keys KeyLabels
		want string
	}{
		{"empty ID", i18n.Catalogs[TextID]{i18n.English: {"": "x"}}, nil, "empty text ID"},
		{"module ID", i18n.Catalogs[TextID]{i18n.English: {TextCancelled: "Stopped"}}, nil, "reserved prefix"},
		{"reserved prefix", i18n.Catalogs[TextID]{i18n.German: {"tui.later": "x"}}, nil, "reserved prefix"},
		{"translation only", i18n.Catalogs[TextID]{i18n.German: {"app.only": "nur deutsch"}}, nil, "no English text"},
		{"other verb", i18n.Catalogs[TextID]{i18n.English: {"app.n": "%d items"}, i18n.German: {"app.n": "%s Einträge"}}, nil, "format verbs"},
		{"missing verb", i18n.Catalogs[TextID]{i18n.English: {"app.n": "%d of %d"}, i18n.German: {"app.n": "%d"}}, nil, "format verbs"},
		{"extra verb", i18n.Catalogs[TextID]{i18n.English: {"app.n": "done"}, i18n.German: {"app.n": "fertig %s"}}, nil, "format verbs"},
		{"reordered verbs", i18n.Catalogs[TextID]{i18n.English: {"app.n": "%s in %s"}, i18n.German: {"app.n": "%[2]s: %[1]s"}}, nil, "format verbs"},
		{"verb flags", i18n.Catalogs[TextID]{i18n.English: {"app.n": "%-8s"}, i18n.German: {"app.n": "%s"}}, nil, "format verbs"},
		{"placeholder without keys", i18n.Catalogs[TextID]{i18n.English: {"app.k": "{save} saves"}}, nil, "{save}"},
		{"unknown placeholder", i18n.Catalogs[TextID]{i18n.English: {"app.k": "{lock} locks"}}, keys, "{lock}"},
		{"unknown placeholder in translation", i18n.Catalogs[TextID]{
			i18n.English: {"app.k": "{save} saves"}, i18n.German: {"app.k": "{sav} speichert"},
		}, keys, "{sav}"},
	}
	for _, c := range cases {
		tr, err := NewTranslator(i18n.English, c.app, c.keys)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one naming %q", c.name, err, c.want)
		}
		if tr != nil {
			t.Errorf("%s: a translator was returned with the error", c.name)
		}
	}
}

func TestNewTranslatorReportsEveryProblem(t *testing.T) {
	_, err := NewTranslator(i18n.English, i18n.Catalogs[TextID]{
		i18n.English: {"app.a": "%d", "app.b": "{nope}"},
		i18n.German:  {"app.a": "%s", "app.c": "x"},
	}, nil)
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{`"app.a"`, `"app.b"`, `"app.c"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error lacks %s: %v", want, err)
		}
	}
}

func TestCheckCatalogsRejectsIncompleteCatalogs(t *testing.T) {
	cases := []struct {
		name     string
		catalogs i18n.Catalogs[TextID]
		want     string
	}{
		{"no German catalog", i18n.Catalogs[TextID]{i18n.English: {"app.a": "a"}}, "no de text"},
		{"German lacks a text", i18n.Catalogs[TextID]{i18n.English: {"app.a": "a", "app.b": "b"}, i18n.German: {"app.a": "a"}}, `"app.b" has no de text`},
		{"further language lacks a text", i18n.Catalogs[TextID]{
			i18n.English: {"app.a": "a"}, i18n.German: {"app.a": "a"}, "fr": {},
		}, "no fr text"},
		{"empty English", i18n.Catalogs[TextID]{i18n.English: {"app.a": ""}, i18n.German: {"app.a": "a"}}, "en text \"app.a\" is empty"},
		{"blank German", i18n.Catalogs[TextID]{i18n.English: {"app.a": "a"}, i18n.German: {"app.a": "  "}}, "de text \"app.a\" is empty"},
		{"German only", i18n.Catalogs[TextID]{i18n.English: {}, i18n.German: {"app.a": "a"}}, "no English text"},
		{"verbs differ", i18n.Catalogs[TextID]{i18n.English: {"app.a": "%d"}, i18n.German: {"app.a": "%v"}}, "format verbs"},
	}
	for _, c := range cases {
		if err := CheckCatalogs(c.catalogs); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one naming %q", c.name, err, c.want)
		}
	}
	complete := i18n.Catalogs[TextID]{
		i18n.English: {"app.a": "%d of %d", "app.b": "100%% {save}"},
		i18n.German:  {"app.a": "%d von %d", "app.b": "100%% {save}"},
	}
	if err := CheckCatalogs(complete); err != nil {
		t.Errorf("a complete catalog: %v", err)
	}
}

func sampleCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {"app.greeting": "Hello", "app.items": "%d items", "app.english": "only English", "app.save": "{save} saves %s"},
		i18n.German:  {"app.greeting": "Hallo", "app.items": "%d Einträge", "app.save": "{save} speichert %s"},
	}
}

func newTranslator(t *testing.T, lang i18n.Lang, app i18n.Catalogs[TextID], keys KeyLabels) Translator {
	t.Helper()
	tr, err := NewTranslator(lang, app, keys)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestLookupFallsBackToEnglish(t *testing.T) {
	keys := keyTable{"save": "F2"}
	cases := []struct {
		lang i18n.Lang
		id   TextID
		args []any
		want string
	}{
		{i18n.German, "app.greeting", nil, "Hallo"},
		{i18n.German, "app.items", []any{3}, "3 Einträge"},
		{i18n.German, "app.english", nil, "only English"},
		{i18n.German, TextCancelled, nil, "Abgebrochen"},
		{i18n.German, "app.unknown", nil, "app.unknown"},
		{i18n.English, "app.greeting", nil, "Hello"},
		{i18n.English, TextResizeNeed, []any{80, 24}, "Need 80x24"},
		{i18n.Lang("fr"), "app.greeting", nil, "Hello"},
	}
	for _, c := range cases {
		if got := newTranslator(t, c.lang, sampleCatalogs(), keys).Lookup(c.id, c.args...); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.lang, c.id, got, c.want)
		}
	}
}

func TestLookupResolvesKeysBeforeFormatting(t *testing.T) {
	keys := keyTable{"save": "F2"}
	tr := newTranslator(t, i18n.English, sampleCatalogs(), keys)
	if got := tr.Lookup("app.save", "{save}"); got != "F2 saves {save}" {
		t.Errorf("an argument with braces = %q, want it kept as it is", got)
	}
	keys["save"] = "ctrl+s"
	if got := tr.Lookup("app.save", "the file"); got != "ctrl+s saves the file" {
		t.Errorf("after rebinding = %q, want the new key", got)
	}
	keys["save"] = "%d"
	if got := tr.Lookup("app.save", "x"); got != "%d saves x" {
		t.Errorf("a label with a percent sign = %q, want it literal", got)
	}
	if got := newTranslator(t, i18n.German, sampleCatalogs(), keys).Lookup("app.save", "x"); got != "%d speichert x" {
		t.Errorf("German = %q", got)
	}
}

func TestPlaceholderSyntax(t *testing.T) {
	keys := keyTable{"a.b-c_1": "K", "save": "F2"}
	app := i18n.Catalogs[TextID]{i18n.English: {
		"app.x": "{Save} {1a} { } {} {save x} {a.b-c_1} {{save}} {save",
	}}
	got := newTranslator(t, i18n.English, app, keys).Lookup("app.x")
	if want := "{Save} {1a} { } {} {save x} K {F2} {save"; got != want {
		t.Errorf("Lookup = %q, want %q", got, want)
	}
}

func TestTranslatorsAreIndependentInstances(t *testing.T) {
	app := sampleCatalogs()
	keys := keyTable{"save": "F2"}
	en := newTranslator(t, i18n.English, app, keys)
	de := newTranslator(t, i18n.German, app, keys)
	app[i18n.English]["app.greeting"] = "changed"
	delete(app[i18n.German], "app.greeting")
	if got := en.Lookup("app.greeting"); got != "Hello" {
		t.Errorf("English after changing the source = %q", got)
	}
	if got := de.Lookup("app.greeting"); got != "Hallo" {
		t.Errorf("German after changing the source = %q", got)
	}
}

func TestFormatVerbs(t *testing.T) {
	cases := map[string][]string{
		"plain":            nil,
		"%d of %d":         {"%d", "%d"},
		"100%% done":       {"%%"},
		"%-8s|%5.2f|%#x":   {"%-8s", "%5.2f", "%#x"},
		"%[2]s and %[1]s":  {"%[2]s", "%[1]s"},
		"%*d trailing %":   {"%*d", "%"},
		"%q name %v value": {"%q", "%v"},
	}
	for text, want := range cases {
		if got := formatVerbs(text); !slices.Equal(got, want) {
			t.Errorf("formatVerbs(%q) = %q, want %q", text, got, want)
		}
	}
}
