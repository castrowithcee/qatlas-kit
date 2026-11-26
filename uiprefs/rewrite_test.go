package uiprefs

import (
	"reflect"
	"strings"
	"testing"
)

// formerSchema invents an application that renamed a theme and changed the meaning of icons: a file without
// the format marker holds the former values.
func formerSchema(t *testing.T) Schema {
	s := testSchema(t)
	s.Rewrite = func(raw map[string]any) {
		if raw["theme"] == "old dark" {
			raw["theme"] = "sample dark"
		}
		if _, ok := raw["format"]; !ok && raw["icons"] == "off" {
			raw["icons"] = IconsNone
		}
	}
	s.WriteKeys = map[string]any{"format": 2}
	return s
}

func TestRewriteMakesFormerValuesLoadWithoutWarning(t *testing.T) {
	s := formerSchema(t)
	path := Path(t.TempDir())
	write(t, path, "theme: old dark\nicons: off\n")
	got, warnings := Load(path, s)
	if got.Theme != "sample dark" || got.Icons != IconsNone || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want the rewritten values and no warning", got, warnings)
	}
	write(t, path, "format: 2\nicons: off\n")
	got, warnings = Load(path, s)
	if got.Icons != IconsUnicode || len(warnings) != 1 || !strings.Contains(warnings[0], `icons "off" is not one of`) {
		t.Fatalf("Load() = %+v, %v; want the usual warning for a value invalid after the rewrite", got, warnings)
	}
}

func TestRewriteSeesKeysSectionAndEmptyFile(t *testing.T) {
	s := testSchema(t)
	var seen map[string]any
	s.Rewrite = func(raw map[string]any) { seen = raw }
	path := Path(t.TempDir())
	write(t, path, "extra: 1\nkeys:\n  save: f4\n")
	got, warnings := Load(path, s)
	if len(warnings) != 0 || got.Keys["save"] != "f4" || seen["extra"] != 1 ||
		!reflect.DeepEqual(seen["keys"], map[string]any{"save": "f4"}) {
		t.Fatalf("Load() = %+v, %v; seen %v", got, warnings, seen)
	}
	write(t, path, "")
	seen = nil
	if _, warnings := Load(path, s); len(warnings) != 0 || seen == nil {
		t.Fatalf("empty file: warnings %v, map %v", warnings, seen)
	}
}

func TestWriteKeysAreSavedAndDoNotDisturbLoading(t *testing.T) {
	s := formerSchema(t)
	path := Path(t.TempDir())
	want := Default(s)
	want.Icons = IconsNone
	if err := Save(path, want, s); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); !strings.HasSuffix(got, "mouse: true\nformat: 2\n") {
		t.Fatalf("file = %q", got)
	}
	got, warnings := Load(path, s)
	if !reflect.DeepEqual(got, want) || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want %+v", got, warnings, want)
	}
	// A schema without the options reads the same file as before.
	if got, warnings := Load(path, testSchema(t)); !reflect.DeepEqual(got, want) || len(warnings) != 0 {
		t.Fatalf("plain Load() = %+v, %v", got, warnings)
	}
}

func TestWriteKeysCollidingWithOwnKeysAreRefused(t *testing.T) {
	for _, name := range append([]string{""}, reservedKeys...) {
		s := testSchema(t)
		s.WriteKeys = map[string]any{name: "x"}
		path := Path(t.TempDir())
		if _, warnings := Load(path, s); len(warnings) != 1 || !strings.Contains(warnings[0], "schema") {
			t.Errorf("%q: Load warnings = %v", name, warnings)
		}
		if err := Save(path, Default(s), s); err == nil {
			t.Errorf("%q: Save accepted the schema", name)
		}
		if err := Validate(Default(s), s); err == nil {
			t.Errorf("%q: Validate accepted the schema", name)
		}
	}
}
