// Package tui provides the neutral mechanics of a terminal user interface. Applications supply their own
// areas, actions, data and texts; this package holds no application content and no package state.
//
// Every text of a TUI lives in a catalog of stable, untranslated TextID keys per language, built on the
// i18n package. ModuleCatalog holds the general texts of this package under the reserved "tui." prefix;
// an application adds its own catalog under other IDs. NewTranslator merges both for one language and
// rejects a catalog that would draw wrong text: a reserved or empty ID, a text in a language other than
// English without an English text, format verbs that differ from the English text, or a key placeholder
// that names no known action. A missing translation falls back to English at run time; CheckCatalogs is the
// stricter test-time contract that every text exists in English and German and none is empty.
//
// A text names the key of an action by the placeholder "{action}", where the action ID starts with a
// lowercase ASCII letter followed by lowercase ASCII letters, digits, '.', '_' or '-'. Lookup replaces each
// placeholder with the keys currently bound before it formats the arguments, so a rebinding shows in every
// text at once and an argument that holds braces stays as it is. A text that must show such a brace
// sequence literally passes it as an argument. To change the language, build a new Translator.
package tui
