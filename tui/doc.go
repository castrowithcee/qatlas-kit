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
// The parts with state share one shape. NewX checks an XSpec, which holds the content and, in Keys, the
// actions of the key table the part reads, against the Keymap and the Translator and refuses what cannot
// work. Update reads a key press and Click the MouseResult of a target the part drew; both return what the
// application runs. View draws with the Styles of that drawing in width by height cells and returns the lines
// and their targets; a part that also stands over the screen draws there with Overlay, which reports false
// where the room is too small. The styles come with every drawing, so a change of the theme keeps the state.
//
// A Frame draws the frame every screen stands in from the FrameSpec the application fills for each
// render: the header with the product name, status marks, back button and header keys; the tab line with
// one tab per Area as its digit and title; the sub-tabs of the active area where it has more than one; a
// rule; the workspace, cut or padded to its rows, with the message under it; and the footer in the last
// row, or the last two. Workspace gives the room the content has, less the rows the footer takes, and
// Render returns the click targets of the drawn keys and tabs for that frame only. The header stays one
// line: the status marks take their short words and go before the header keys, and the header keys go
// before the back button. A tab line that runs short closes its gaps and then shrinks the inactive tabs to
// their digit; the active tab stays whole. CheckAreas holds areas to what the tab line can draw. FrameSpec.Icon
// stands before the product name and Area.Icon before the title of its tab, each as "icon name"; CheckFrame
// and CheckAreas reject an icon that fails CheckIcon, and the frame draws no icon that fails it. The tab line gives
// up its icons before it closes any gap, and the digit-only level holds none; no icon is drawn in an ASCII
// terminal. The application chooses the icon set; the kit knows none. Nerd Font glyphs measure one cell, but
// a font that draws them wider is the application's risk. The focus
// zones have a fixed order that CycleZone walks with wrap-around, as StepArea and StepTab do for the tabs.
//
// Header and footer show hints. Keymap.Hints resolves each hint to the keys of its action that act in the
// input focus of the screen, so a line never names a key that does nothing, and folds hints whose labels
// read the same into one group; a language with longer words folds two hints by giving them the same text,
// never by an empty one. When the footer does not fit one line it takes a second, never more, and groups
// are never broken between them; navigation and position stand at the right end of the last line. Only when
// two lines run short, navigation and position go first, then the normal hints by priority; a hint that
// always stays, such as help, is never dropped, and the answers of an open question are shortened instead.
//
// The mouse is a second way to the same actions and never stronger than a key. Render and the other drawing
// parts return the targets they drew; a MouseMap holds the targets of one drawn frame, so a target no longer
// on screen cannot act. Handle acts on a press of the left button and on the wheel only. A click reports the
// action, area, tab or item of the target it hit, which the application runs exactly as it runs the key or the
// tab click, with every question and check of that way; the free area of a zone reports the zone to focus, and an
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
// key acts, in the Context the application sets when it opens the line; anywhere else the line stays open
// with the typed text and the reason. The line reads keys where text is typed, so a letter is always text,
// and moves with the actions of CommandLineKeys. View draws a panel with the key in force beside each
// command, at most twelve rows that scroll with the choice and one target per row; the application places it
// over the workspace, or on it where the terminal is too small.
//
// A List shows the entries an application hands it, each with a stable ItemID, its cells and a group key, as
// a table under dimmed upper-case column titles. The selection follows the ID, never the position, when the
// entries, the filter or the grouping change; it is always on screen, and the scroll position stays while the
// selection is on the page. The filter reads the cells and the search text of an entry, ignores case, and
// only decides what shows: it removes nothing and keeps the order. A list whose letters are actions starts
// the filter with its search key; a pure choice list filters with every typed character. Enter keeps a typed
// filter, the first back clears it, and the next one is left to the application. Groups are presentation
// only: they stand in alphabetical order of their shown title with the group without key last, left, right
// and enter fold them, a filter opens the groups with hits without changing their fold state, and the
// position counts entries only, "pos/total" or, with a filter, "pos/shown (N total)". SetGroups gives a group,
// by the key in ListItem.Group, a title and cells; the row then shows the fold glyph, the box, the category
// mark, the title and the count as the group head in the first column, and the cells from the second column on
// by the rules of their column; the first cell is not shown. The fold state, the partial selection and the
// marked row follow the key, so new titles or cells lose none of them, and the filter never reads them.
// SelectedGroup names the marked group row; Selected and Current stay empty there. When a row does not fit,
// the free text column gives way first, then the short labels of a column, then the optional columns from the
// right, and then the widest values, never below their title, for group rows as for entries; the first column
// and the group head are never cut, and the widths come from every entry and described group, so they stay
// while a filter is typed. An empty list shows the application's sentence with the next step, and a filter
// without match names the key that clears it. Cells, group keys, titles and the typed filter are escaped.
//
// A List with Ticks picks any number of entries: a tick box before the first column, "[ ]", "[x]" and for a
// group with some of its entries ticked "[-]", so the three states differ without colour, and Ticked and
// SetTicked read and set the IDs. The toggle key ticks the marked entry; on a group row it steps the group
// through the partial selection it had, all and none and back to the partial selection, or between all and
// none where there was none. A single toggle in the group, SetTicked and SetItems forget the partial
// selection. The row of all, with AllRow, above all groups does the same for every entry. While a filter is
// set, the boxes and the toggle count and act on the entries it shows only. Current gives the marked entry
// with its cells for a panel beside the list.
//
// Overlay draws a list as a modal in the way of Dialog.Overlay: under ListSpec.Title the search line, which a
// list that filters as typed always has, the table, one button per ListSpec.Buttons entry behind the key in
// force, and the key line. The frame keeps its height while a filter is typed, the table scrolls with the
// marked row in view, and the targets are the modal's alone. Where the room is too small for the search line,
// the titles, one row, the buttons and the key line, ok is false; the application then draws View in the
// workspace and Hints in the footer. Hints lists the buttons first and the list keys after them, no action
// twice. A click on a button acts as its key does through Act: an action that is no list key comes back with
// handled false, and the application runs the action of the MouseResult as it runs the key. What enter and
// back mean, and the state from before the modal opened, are the application's.
//
// A click on a row marks it, and only a click on the marked row while the rows have the focus, as SetFocused
// says, opens it, as enter does; a click on a group row folds it and never changes the ticks, a click on a
// tick box does what the toggle key does, and one on the search line starts the search. Each such target
// carries the action of its key and an Item the list reads back in Click; a target with a zone alone is the
// free area of the zone. A FilterBar holds choice fields such as the grouping, a state or the sort order:
// left and right change the value, enter opens the list of values, which back closes without change, and tab
// and shift+tab step through the fields and leave the bar at its ends to the application. Its values change
// only what the application hands to the list, never its data. A click on a field opens its values and one on
// a value takes it; the search field of a list may stand at the right end of the bar. On a narrow terminal
// the values are cut first; where the fields still do not fit, a single button with the number of fields off
// their first value takes their place, always, with the search field beside it or, with little room, only the
// search key. The button is one stop of the focus. Enter or a click opens a modal with all fields one under
// the other: up and down choose a field, left and right change its value, enter takes all changes at once and
// back drops them all. The modal takes every key; it is drawn over the screen where it fits and otherwise in
// the workspace, with its keys in the footer.
//
// A Form shows the fields an application hands it, each with a stable ID, a kind, a label and a value, and
// knows no schema: the application checks the values when the save key is pressed, from any field, and hands
// the problems back with SetProblems. Enter never saves: it goes to the next field, opens the values of a
// choice, starts taking a key, or yields the enter key on a list field, whose editor the application opens. A
// choice holds known values only, a multiple choice ticks them, and the values a form returns are stable IDs.
// FormSpec.Buttons add buttons below the fields, each an action of the key table and a label drawn behind the
// key in force. Tab and shift+tab cycle through the fields and then the buttons, from the last button to the
// first field and back; the focused button reads "> label <" without colour. Enter on a button, its click and
// its key give its action and nothing more, as Update and Click report it; back is reported as the back key
// with handled false, so the application still asks before it leaves a changed form. While a button has the focus,
// typing, paste, left, right, up, down and the wheel change nothing; a click on a field takes the focus.
// A button with the focus is always in view: the fields get the height that is left, or none, and only where
// not even the buttons fit does the form ask for a larger terminal. With a field focused, the form is drawn
// without the buttons where they do not fit beside it. Overlay draws the form as a modal in the way of
// Dialog.Overlay, under FormSpec.Title in its frame, with its buttons and the keys of Hints, and ok is false
// where room does not hold it, also with a list of values or the calendar open; the application then draws
// View in the workspace and Hints in the footer. The modal shows no more of a masked field than View does.
// FormSpec.Lead is one or two dimmed sentences under the title, wrapped and never cut. It stays in view at
// every focus, in View as in the modal, counts for ok, and SetLead changes it and keeps values, focus and
// open list. A masked field never shows what is typed, not in its row, a target or a problem, and is never
// handed a stored secret: Stored says one exists, which an empty field keeps. FieldMaskedBlock is the same
// for a secret of several lines, such as a private key: its value is text with "\n" line breaks of at most 64
// KiB, and it shows a placeholder with the number of lines, never the content or its length. Paste replaces
// the whole value, typed characters are appended, enter adds a line break instead of going on, and backspace
// or delete clears the field; tab, the save key and a click work as in any field. A paste or key with a
// character other than line break and tab, or that would pass the limit, is refused whole with a message
// under the field. Each problem stands under its field until that field changes, a problem without a field
// above the form, and a section with a problem carries a mark and opens. An advanced section stays closed and
// dimmed while it holds nothing. On a wide terminal the sections stand as a numbered list beside the fields
// of the focused one. The fields lose their frames only where the width does not hold them, and then only the
// focused field stands on a surface. Where the height is short, the form scrolls with the focus, which is
// always in view: whole fields stand around it, with "…" above and below where fields are left out. Each
// section has its own label column, in which a longer label wraps. Only where even the focused field does not
// fit does the form ask for a larger terminal. A click on a field focuses it and types nothing; on a choice
// or list field it is enter, on a tick box the toggle key, on a section a jump to its first field.
//
// A date field (FieldDate) is typed as YYYY-MM-DD, or as YYYY-MM-DD HH:MM where DateField.Time is set, and
// its value is a time.Time in the local time zone of the process (time.Local), cut to the day or the minute;
// an empty field has the zero time. DateField.Min and Max bound it, both allowed, and a date without time
// compares by its day. A text that is not a value, whether in the wrong format, a day that does not exist or
// outside the bounds, never becomes the value: Values holds the last valid value meanwhile, and a problem
// stands under the field once it is left, or at once where the save key was refused over it; the save key
// stays in the form on that field. DateField.Now is the clock of the field, time.Now where nil, and gives
// the calendar its today.
//
// Enter, or a click on the calendar symbol at the right end of the value, opens a month view below the field,
// above it where it does not fit. Left and right move the day, up and down the week, page up and page down
// the month, first goes to today, enter takes the day and back closes it unchanged; the keys are the form
// keys and no others. The month view has a target per selectable day and one per button for the previous and
// the next month and for today, each drawn with the key in force and acting as that key; the wheel moves by
// a week. A day outside Min and Max is drawn as dashes, has no target, and no key lands on it. A day taken
// keeps the hour and minute of the field, 00:00 where it is empty, moved into the bounds where the day has no
// such time. The week starts on the day of tui.calendar.weekstart and the names of months and weekdays come
// from the catalog, so a language sets them. While the calendar is open it takes every key, and a click on
// anything but the calendar and the symbol of its field does nothing. A click on the row of a date field
// only focuses it.
//
// A Dialog asks a question over the screen: the warning glyph and the question in bold, one sentence on what
// follows, the details, one button per answer and the key line of the answers, from the same answers, so no
// dialog names one twice. A detail is a DialogLine of plain text in one of the text, hint, warn and fail styles,
// optionally behind a prefix such as a prompt sign; fail is for a Notice only, since a question keeps red for
// the button of a destructive answer; a wrapped line continues under the text, not the prefix. The dialog takes
// no styled text, so it escapes and wraps before it styles, and the tool chooses prefix, style and wording.
// Irreversible draws the sentence on what follows in the warn colour. A Notice is a calm message without the
// warning glyph that needs at least one detail: a dialog interrupts only where there is something to read or to
// decide, so a plain success without details belongs in the message line. Only the answers act; every other key
// goes back to the application, so a stray key neither saves nor destroys. The answer to enter is the default
// and shows focused, and the destroying answer of a destructive question is drawn in the fail colour and can
// never be the default. A dialog with an input reads typed text, shows what runs and the last error inside
// itself and never shows a masked input. Overlay dims the screen behind it and returns the targets of the dialog
// alone; where it does not fit, View fills the workspace and Hints go into the footer, whose targets answer it.
//
// Help explains in three stages: the one-line hint under the focused field, the explanation of the focused
// element, and the help with its topics. The Help shows the application's helptopics.Topic values, the same
// topics its command line and manpage show, wrapped as the command line wraps them, with a short unindented
// line that ends in a colon as a heading. The first topic ends with the keys of the screen the help was
// opened from and the keys that act everywhere, in the binding in force. Left and right, and the topic keys
// of the help level, change the topic with wrap-around; the scroll keys and the wheel stop at the ends; back
// and the help key close it. An Explanation shows a title, the long text, a helptopics.Term and the address
// of the documentation, which is shown in full and, where the terminal takes escapes, also linked; it accepts
// only an absolute http or https address of printable ASCII, so the address cannot end the link sequence.
// Back and the info key close it, and the copy key hands Text to the application, which holds nothing that is
// not shown and no control character. Both stand over the screen like a dialog, or fill the workspace with
// View where room is too small; an explanation also stands in a Pane beside a form or a list. Every topic
// tab, key, line of the key list and address is a target that does what its key does, and a click anywhere
// else on an explanation over the screen closes it. Only their own keys act; every other key goes back to the
// application.
//
// A LogView shows lines that arrive over time. The application supplies them with Append, one text or
// several, each with an optional time and source; where they come from, saving them and any structure in them
// stay with the application. The lines live in a ring buffer of LogViewSpec.Lines, LogViewDefaultLines when
// zero and at most LogViewMaxLines; when it is full the oldest lines go, and the position and the matches
// follow what remains. Every line, source and search text is escaped before it is stored, so no control
// sequence reaches the terminal, and a line longer than LogViewMaxLineRunes is cut and carries a visible
// mark; neither can be switched off. A line break in a text starts a new line. The view follows the newest
// line until a key or the wheel scrolls up; the follow key and the end key resume, and the status line names
// the state with the number of lines that arrived meanwhile. The search key starts a case-insensitive search
// of the escaped text, enter keeps it and back cancels or clears it; every match is highlighted and the lines
// that hold one carry a mark, so nothing depends on colour, and the next and previous match key jump with
// wrap-around and stop the following. Wrapping and the time column are switches; without wrapping a long line
// is shortened with the ellipsis glyph. LogViewActions gives the actions only this part needs; scrolling,
// search, enter and back are the actions the table already holds.
//
// A LogFilter is the period a LogView shows: all, the last count of minutes, hours, days or months, or a range
// from one minute to another; LogViewSpec.Filter starts it, LogViewSpec.Units limits the units on offer and
// LogViewSpec.Now is the clock. Bounds turns it into a begin and an end for a given now, a zero time being no
// limit, and Validate names what is wrong with one. The view loads nothing: a line outside the period at its
// arrival is not kept, a line without time is, and a change of the period drops the held lines outside it, so
// search, matches and position count only what is shown. The application takes the user's choice with
// TakeFilterChange, calls Clear and supplies the lines of the new period with Append; SetFilter sets a period
// without reporting it. Clear empties the buffer and the matches and keeps the period, the search and the
// switches. Following resumes with a change to all or to a count and ends with a range whose end has passed on
// the clock of the view.
//
// The filter action, by key or by its button, opens a modal over the view, a Form built from
// LogViewKeys.FilterForm: a choice for all, last or range; for last a count from 1 to LogFilterMaxCount and a
// unit choice limited to the units on offer; for a range two dates with the time of day and their calendar.
// Only the fields of the chosen kind stand in it, filled in from the period, and what was typed for another
// kind returns with it. While it is open the view takes every key, click and notch of the wheel, and
// ModalOverlay draws it over the screen, whose targets then take no click; ModalView, with the buttons of the
// form where they fit, and Hints serve a terminal too small for that, as for a Dialog. Back discards; the save
// key and enter apply, enter from every field but a date, where it opens the calendar as in every form. A choice
// steps with left and right, and its list opens by a click on the field; a list or the calendar takes every
// key, enter chooses, and back closes only that. The form's buttons apply and discard show the save and back keys
// in force and act as they do, enter on one of them included.
// A period that is not valid, no count or one below 1, an empty date or an end before the start, stays open
// with its problem under the field. An applied period that differs from the current one is reported once.
//
// The status line holds a button for following, search, the period, wrap and the time column, and for the
// previous and next match while a search has matches, with the position at its right. The period button, which
// LogViewKeys.Filter enables, names the period in short form. Each button names the key in force
// and a switch says on or off in words; a click does what its key does and no more. Where the width runs
// short the labels go first, one by one by rank, and the keys stay; then buttons go by rank, following and
// search last and the period before them; the position gives way last, to a shorter form. While the search text is typed the row holds that field and
// the position only.
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
// CheckIcon holds an application's icons to one code point of a width every terminal agrees on; Escape stays
// as it is, so only an icon that passed CheckIcon appears unmasked and any other private-use character
// is still written as its escape.
//
// The built-in schemes come from the iTerm2-Color-Schemes collection at a fixed commit, generated by
// scripts/gen_tui_themes.go; their licences are in themes_builtin_licenses.txt.
package tui
