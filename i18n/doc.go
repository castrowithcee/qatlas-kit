// Package i18n holds the language choice of an application and the lookup of its texts. It is a leaf: it
// imports nothing of this module, so any surface can use it.
//
// A catalog maps typed keys to texts per language.
//
// # Always enforced
//
// English is the complete catalog; a key that another language lacks falls back to English, so a screen
// that is not translated yet keeps working. Lookup returns the key itself when no text exists, Find reports
// it instead. Parse knows only the languages the package defines; Detect returns English when the system names no
// supported language; on Unix it reads LC_ALL, LC_MESSAGES and LANG in that order and takes the first one
// that is set and not empty.
//
// # Options
//
// The package has no options.
package i18n
