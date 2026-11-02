// Package i18n holds the language choice of an application and the lookup of its texts. It is a leaf: it
// imports nothing of this module, so any surface can use it.
//
// A catalog maps typed keys to texts per language. English is the complete catalog; a key that another
// language lacks falls back to English, so a screen that is not translated yet keeps working.
package i18n
