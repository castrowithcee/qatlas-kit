package uiprefs

import (
	"reflect"
	"strings"
	"testing"
)

func navSchema(t *testing.T) Schema {
	s := testSchema(t)
	s.Navigations = []string{NavigationTabs, NavigationPathLine, NavigationPathHeader}
	s.DefaultNavigation = NavigationTabs
	return s
}

func TestNavigationRoundTripAndDefault(t *testing.T) {
	s := navSchema(t)
	if got := Default(s).Navigation; got != NavigationTabs {
		t.Fatalf("default navigation %q", got)
	}
	path := Path(t.TempDir())
	want := Default(s)
	want.Navigation = NavigationPathLine
	if err := Save(path, want, s); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); !strings.Contains(got, "navigation: pathline\n") {
		t.Fatalf("file = %q", got)
	}
	if got, warnings := Load(path, s); !reflect.DeepEqual(got, want) || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want %+v", got, warnings, want)
	}
}

// A file without the key loads with the default of the schema; an unknown value keeps it with a warning.
func TestNavigationMissingOrInvalidInTheFile(t *testing.T) {
	s := navSchema(t)
	s.DefaultNavigation = NavigationPathLine
	path := Path(t.TempDir())
	write(t, path, "theme: sample dark\nstart: items\n")
	if got, warnings := Load(path, s); got.Navigation != NavigationPathLine || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v", got, warnings)
	}
	write(t, path, "navigation: burger\n")
	got, warnings := Load(path, s)
	if got.Navigation != NavigationPathLine || len(warnings) != 1 || !strings.Contains(warnings[0], "navigation") {
		t.Fatalf("Load() = %+v, %v", got, warnings)
	}
}

// Without offered forms the setting does not exist: empty by default, refused when set, ignored with a
// warning in the file, and never written.
func TestNavigationNotOffered(t *testing.T) {
	s := testSchema(t)
	prefs := Default(s)
	if prefs.Navigation != "" {
		t.Fatalf("navigation %q", prefs.Navigation)
	}
	path := Path(t.TempDir())
	if err := Save(path, prefs, s); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); strings.Contains(got, "navigation") {
		t.Fatalf("file = %q", got)
	}
	prefs.Navigation = NavigationTabs
	if err := Validate(prefs, s); err == nil || !strings.Contains(err.Error(), "navigation") {
		t.Fatalf("Validate() = %v", err)
	}
	write(t, path, "navigation: pathline\n")
	got, warnings := Load(path, s)
	if got.Navigation != "" || len(warnings) != 1 {
		t.Fatalf("Load() = %+v, %v", got, warnings)
	}
}

func TestNavigationValidateAndSchemaCheck(t *testing.T) {
	s := navSchema(t)
	prefs := Default(s)
	prefs.Navigation = "burger"
	if err := Validate(prefs, s); err == nil || !strings.Contains(err.Error(), "navigation") {
		t.Fatalf("Validate() = %v", err)
	}
	prefs.Navigation = ""
	if Validate(prefs, s) == nil {
		t.Fatal("an empty navigation passes where forms are offered")
	}
	for name, bad := range map[string]func(*Schema){
		"default outside":   func(s *Schema) { s.DefaultNavigation = NavigationPathLine; s.Navigations = []string{NavigationTabs} },
		"default empty":     func(s *Schema) { s.DefaultNavigation = "" },
		"unknown form":      func(s *Schema) { s.Navigations = []string{NavigationTabs, "burger"} },
		"twice":             func(s *Schema) { s.Navigations = []string{NavigationTabs, NavigationTabs} },
		"default no offers": func(s *Schema) { s.Navigations = nil },
	} {
		s := navSchema(t)
		bad(&s)
		if err := Validate(Default(s), s); err == nil || !strings.Contains(err.Error(), "invalid preferences schema") {
			t.Errorf("%s: Validate() = %v", name, err)
		}
	}
}

// The path in the header is a form of its own that round-trips under its name.
func TestNavigationPathHeaderRoundTrip(t *testing.T) {
	s := navSchema(t)
	path := Path(t.TempDir())
	want := Default(s)
	want.Navigation = NavigationPathHeader
	if err := Save(path, want, s); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); !strings.Contains(got, "navigation: header\n") {
		t.Fatalf("file = %q", got)
	}
	if got, warnings := Load(path, s); !reflect.DeepEqual(got, want) || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want %+v", got, warnings, want)
	}
}
