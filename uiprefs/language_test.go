package uiprefs

import (
	"runtime"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func TestLanguage(t *testing.T) {
	cases := map[string]i18n.Lang{
		LanguageEnglish: i18n.English,
		LanguageGerman:  i18n.German,
		"":              i18n.English,
		"Deutsch":       i18n.English,
		"de":            i18n.English,
		"french":        i18n.English,
	}
	for pref, want := range cases {
		if got := Language(pref); got != want {
			t.Errorf("Language(%q) = %q, want %q", pref, got, want)
		}
	}
}

func TestSystemLanguageFollowsTheDevice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reads the preferred UI languages, not the locale variables")
	}
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	if got := Language(LanguageSystem); got != i18n.German {
		t.Errorf("system with LC_ALL=de_DE.UTF-8 = %q, want German", got)
	}
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	if got := Language(LanguageSystem); got != i18n.English {
		t.Errorf("system with an unsupported locale = %q, want English", got)
	}
}
