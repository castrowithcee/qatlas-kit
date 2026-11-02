package uiprefs

import "github.com/castrowithcee/qatlas-kit/i18n"

// The values of the language preference. They are stored and shown untranslated.
const (
	LanguageEnglish = "english"
	LanguageGerman  = "deutsch"
	LanguageSystem  = "system"
)

// Language resolves a language preference to the language of the texts. LanguageSystem follows the device;
// any value that is not a language preference, including the empty string, yields English.
func Language(pref string) i18n.Lang {
	switch pref {
	case LanguageGerman:
		return i18n.German
	case LanguageSystem:
		return i18n.Detect()
	}
	return i18n.English
}
