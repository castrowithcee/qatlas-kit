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
//
// Keys come from one key table per TUI. NewKeymap takes the application's actions, each with a level,
// default keys, a help text and whether a user may replace its key or add an extra one, and rejects a table
// that would give one key two meanings: within the main levels, a key belongs to one action on every
// screen, and only the overlay levels of a question's answers and of the help may repeat one. It also
// rejects a key that terminals, multiplexers or text lines take for themselves, unless the action is the
// second way of another. Screens ask the Keymap for an action, never for a key name; Match lets the overlay
// levels a screen names win over the main levels. The input focus decides what reaches the table: where
// text is typed, every key that types a character is text, and that is the zero value; while a key is
// captured as a new binding only ctrl+c and esc act; in an embedded terminal no key does. Bind applies the
// user's keys as a whole or not at all, and CheckBinding and Resolve apply the same rules of a binding and
// name the reason of every refusal. A Keymap satisfies KeyLabels, so labels and the key placeholders of
// every text follow a binding at once.
//
// A Frame draws the frame every screen stands in from the FrameSpec the application fills for each
// render: the header with the product name, status marks, back button and header keys; the tab line with
// one tab per Area as its digit and title; the sub-tabs of the active area where it has more than one; a
// rule; the workspace, cut or padded to its rows, with the message under it; and the footer as the last
// line. Workspace gives the room the content has, and Render returns the click targets of the drawn keys
// and tabs for that frame only. The header stays one line: the status marks take their short words and go
// before the header keys, and the header keys go before the back button. A tab line that runs short closes
// its gaps and then shrinks the inactive tabs to their digit; the active tab stays whole. CheckAreas holds
// areas to what the tab line can draw. The focus zones have a fixed order that CycleZone walks with
// wrap-around, as StepArea and StepTab do for the tabs.
//
// Header and footer show hints. Keymap.Hints resolves each hint to the keys of its action that act in the
// input focus of the screen, so a line never names a key that does nothing, and folds hints whose labels
// read the same into one group; a language with longer words folds two hints by giving them the same text,
// never by an empty one. When the footer runs short, navigation and position at its right end go first,
// then the normal hints by priority; a hint that always stays, such as help, is never dropped, and the
// answers of an open question are shortened instead.
//
// The mouse is a second way to the same actions and never stronger than a key. Render and the other drawing
// parts return the targets they drew; a MouseMap holds the targets of one drawn frame, so a target no longer
// on screen cannot act. Handle acts on a press of the left button and on the wheel only. A click reports the
// action, area or tab of the target it hit, which the application runs exactly as it runs the key or the tab
// click, with every question and check of that way; the free area of a zone reports the zone to focus, and an
// element inside a zone wins over it. A wheel notch reports the zone under the pointer, so a side pane scrolls
// without taking the focus; WheelStep stops it at the ends, and WheelTextLines is its step in a text area. The
// application disables the map where the mouse is switched off, while a key is captured and while the
// terminal is too small; under a dialog it passes the targets of the dialog alone, so the background takes no
// click, and it passes the footer targets only where the footer holds the answers of a question.
//
// Mouse reports never become text. An InputFilter, installed with tea.WithFilter and attached to the program,
// drops the pieces of a report that a terminal behind SSH or tmux delivered in more than one read, so a text
// or passphrase field never receives them, and merges a wheel notch the terminal reports twice. Characters
// typed by hand, pasted or arriving outside the short window of a report pass unchanged; keys it held back
// arrive later as one InputBatch.
//
// A CommandLine runs commands by name: lower-case words without arguments, with aliases that keep older names
// valid. Typing filters, an exact name first, then names that begin with the text, then a word that does,
// then the text inside a word; the empty line lists the commands that are not hidden. A command names the
// action, and where it opens one, the area and tab that its key or tab click already triggers, so Update and
// Click yield what the dispatcher of the application runs for that key. It acts only where Available says its
// key acts; anywhere else the line stays open with the typed text and the reason. The line reads keys where
// text is typed, so a letter is always text, and moves with the actions of CommandKeys. View draws a panel
// with the key in force beside each command, at most twelve rows that scroll with the choice and one target
// per row; the application places it over the workspace, or on it where the terminal is too small.
//
// Colours come from themes. Screen code names roles, never colour values: a Palette assigns one colour per
// role, Styles build text styles and widgets on it. A ThemeSet holds the terminal theme, the application's
// own themes and the built-in schemes; each theme is data, either hand-picked palettes per colour profile
// (DarkTheme and LightTheme are such palettes under a name the application chooses) or a scheme of
// foreground, background and 16 ANSI colours. The terminal theme is the default: it paints no ground,
// names the ANSI slots for its accents and derives its surfaces from the foreground and background the
// terminal reports, plain until it has; AsksTerminalColors says when to ask. NewThemeSet rejects a theme
// whose text reads below 4.5:1 or whose dimmed text reads below 3:1 on its ground and fills; a derived
// palette that would fall below falls back to the plain palette, which has no ground and no fills and shows
// the selection in reverse video. Any other theme with a ground colour paints every cell through Paint. An
// unknown theme name resolves to the terminal theme and is reported, so the application can show
// TextThemeUnknown.
//
// DetectColorProfile and DetectCharsetMode read the terminal once at start. A profile without colour, set by
// NO_COLOR with any non-empty value or by a missing terminal, wins over every theme; ProfileNone also drops
// bold, faint and reverse. Meaning never depends on colour alone: status marks keep their word, the selected
// row its mark, the active tab its brackets and a focused frame its double line without colour, and every
// glyph has an ASCII form for a terminal without UTF-8. Widgets return lines of exactly the requested width;
// messages are wrapped and never cut, table cells are shortened with the ellipsis glyph, and a cut never
// splits an escape sequence and closes any style it leaves open. Widgets escape every control character in
// the plain text they take, and Escape does the same for data an application puts into styled content.
// CheckIcon holds an application's icons to a width every terminal agrees on.
//
// The built-in schemes come from the iTerm2-Color-Schemes collection at a fixed commit, generated by
// scripts/gen_tui_themes.go; their licences are in themes_builtin_licenses.txt.
package tui
