// Package tui provides the neutral mechanics of a terminal user interface. Applications supply their own
// areas, actions, data and texts; this package holds no application content and no package state.
//
// Every text of a TUI lives in a catalog of stable, untranslated TextID keys per language, built on the
// i18n package. ModuleCatalog holds the general texts of this package under the reserved "tui." prefix;
// an application adds its own catalog under other IDs. NewTranslator merges both for one language. A
// missing translation falls back to English at run time.
//
// A text names the key of an action by the placeholder "{action}". Lookup replaces each
// placeholder with the keys currently bound before it formats the arguments, so a rebinding shows in every
// text at once and an argument that holds braces stays as it is. A text that must show such a brace
// sequence literally passes it as an argument. Find returns the raw text and whether the language or English
// holds it. To change the language, build a new Translator.
//
// Keys come from one key table per TUI. NewKeymap takes the application's actions, each with a level,
// default keys, a help text and whether a user may replace its key or add an extra one. Keymap.Page returns
// the view of one page, which Match, Is, Label, Hints and every part that takes a Keymap read, so a footer
// and the help show the actions of the active page and those without a page; the application decides which
// page is active. Screens ask the Keymap for an action, never for a key name; Match lets the overlay
// levels a screen names win over the main levels. The input focus decides what reaches the table. Bind
// applies the user's keys, and CheckBinding and Resolve name the reason of every refusal. A Keymap satisfies
// KeyLabels, so labels and the key placeholders of every text follow a binding at once; Keys and
// KeyNameLabel give a tool the keys in force and their labels.
//
// StandardActions returns the actions every tool shares, each with a level, one default key and a help text
// in the catalog; an application adds them to its table with one append. The overlay levels may repeat their
// keys, and ctrl+s stays free as the second way of the save action.
//
// A one-line text field, the input of a Dialog, the text, masked and date fields of a Form, the search of a
// LogView, the filter of a List and the input of a CommandLine edit with a fixed set of keys. A screen asks
// its own actions first, so a Form, a LogView and a List keep theirs for left, right, home and end, and a
// second way of an action wins over an edit. The Paste of each of these parts and a suggestion a Dialog
// takes insert whole at the cursor, never as keys.
//
// The parts with state share one shape, and Dialog, Help, Explanation and Form, which answer with an ActionID,
// satisfy the interface Modal. NewX takes an XSpec, which holds the content and, in Keys, the actions of the
// key table the part reads, with the Keymap and the Translator. Update reads a key press and Click the
// MouseResult of a target the part drew; both return what the application runs. View draws with the Styles of
// that drawing in width by height cells and returns the lines and their targets; a part that also stands over
// the screen draws there with Overlay, which reports false where the room is too small. The styles come with
// every drawing, so a change of the theme keeps the state.
//
// A Frame draws the frame every screen stands in from the FrameSpec the application fills for each render: the
// header with the product name, status marks, back button and header keys; the tab line with one tab per Area
// as its digit and title; the sub-tabs of the active area where it has more than one; a rule; the workspace,
// cut or padded to its rows, with the message under it; and the footer in the last row, or the last two.
// Workspace gives the room the content has, less the rows the footer takes, and Render returns the click
// targets of the drawn keys and tabs for that frame only. The header stays one line within the width: each
// status mark gives way in the order of its MarkRank, and the header keys go before the back button. A tab line
// that runs short closes its gaps and then shrinks the inactive tabs to their digit; the active tab stays
// whole. A Tab with GroupStart has a separator before it on the sub-tab line, which is no tab and no target and
// goes where only the active sub-tab fits. FrameSpec.ModeTab shows the application outside every area, such as
// a guided setup: no area is active, the text stands after the areas as the active tab, whole at every level
// and no target, and the sub-tab line stays empty so the workspace keeps its height. FrameSpec.Icon stands
// before the product name and Area.Icon before the title of its tab, each as "icon name". The tab line gives up
// its icons before it closes any gap, and the digit-only level holds none; no icon is drawn in an ASCII
// terminal. The application chooses the icon set; the kit knows none. Nerd Font glyphs measure one cell, but a
// font that draws them wider is the application's risk. FrameSpec.Nav chooses the form of the navigation:
// NavTabs, the zero value, is the tab line with sub-tabs; NavPathLine replaces both by one line that names the
// current place as "Area ▾ › Tab ▾", the tab only where the area has more than one, and gives the freed row to
// the workspace. Each section is a click target of ZonePath that carries the area, and the tab for the last, so
// the application handles it like a tab click; the focus mark stands at the line and the section named by
// FrameSpec.PathFocusArea, else the last, is drawn as the focused tab. A line that runs short takes the mark
// from all sections but the last, then keeps the last section alone and cuts its name; ModeTab stands as the
// only section, without mark and target. NavPathHeader draws the same path in the header, after the product
// name and before the status marks, and no navigation line at all: the workspace takes the rows of the tab
// line, the sub-tab line and the path line. Its targets, focus and mode tab are those of the path line, the
// focus drawn as the focused tab without the focus mark. The header fills its room in this order: the name and
// the marks that always stand, the marks before keys, the widest block at the right, and the normal marks, as
// before, with the path as the current section alone standing at every width. The rest of the room widens the
// path to the widest form of the path line that fits with the normal marks in their shortest forms, and the
// marks take the longest forms that fit beside it. Where the product name and the current section do not fit,
// the product name gives way down to nothing and the section is cut, keeping its mark. NavHeaderChoice puts
// only the current area in the header, after the product name, as a choice field "Icon Area ▾" with the icon of
// the area as on the tab line, and draws no tab line; the sub-tab line of the area stays below the header as
// with NavTabs, and an area with one tab has none. The field is a click target of ZoneTabs that carries the
// area, it is drawn as the focused tab while ZoneTabs has the focus, it shortens as the section of
// NavPathHeader does, and the mode tab stands in it as text without mark and target. A screen with this form
// reports ZoneTabs for the field and ZoneSubtabs for the sub-tab line, so the application runs left and right
// at ZoneTabs as a step through the areas and StepZoneLevel walks field, sub-tab line and work area; it opens
// PathNav.OpenAreas for enter and for a click on the field. A screen with a path form reports ZonePath instead
// of ZoneTabs and ZoneSubtabs. The focus zones have a fixed order that CycleZone walks with wrap-around, as
// StepArea and StepTab do for the tabs. StepZoneLevel steps vertically between the tab line, the sub-tab line
// or the path line and the work area, skips a missing line and does not wrap.
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
// parts return the targets they drew; a MouseMap holds the targets of one drawn frame. A click reports the
// action, area, tab or item of the target it hit, which the application runs exactly as it runs the key or the
// tab click, with every question and check of that way; the free area of a zone reports the zone to focus, and an
// element inside a zone wins over it. A wheel notch reports the zone under the pointer, so a side pane scrolls
// without taking the focus; WheelStep stops it at the ends, and WheelTextLines is its step in a text area. The
// application disables the map where the mouse is switched off, while a key is captured and while the
// terminal is too small; under a dialog it passes the targets of the dialog alone, so the background takes no
// click, and it passes the footer targets only where the footer holds the answers of a question. A notch
// over a dialog whose details scroll reports ZoneDetail, which the application hands to Dialog.Wheel; beside
// the dialog it reports no zone, so nothing scrolls.
//
// With CloseOutside, a click outside the frame of an open modal acts as Esc, the back key of the overlay, does
// (for a dialog the answer Esc gives): the overlay then adds targets of ZoneOutside, which cover the screen
// around the frame and none of the frame itself, to the targets its Overlay returns, and Click reads a
// MouseResult of that zone. A click inside the frame that hits no target never closes, since it lies beside
// those targets. A form reports Keys.Back with handled false, as Esc does, so the application asks before it
// discards a changed form, and an open list of values or calendar closes first. A list acts as Back does
// through Act: it clears a filter and otherwise comes back unhandled, which the application closes the modal on
// as on the key. A click on the footer or the header behind the modal is outside as well and runs nothing.
// A command line closes as Keys.Back does, and an explanation reports Keys.Back. An explanation is the one
// exception to the rule on the frame: a click inside its frame beside its keys and address closes it with or
// without the option. The View drawn in a terminal too small for an overlay has no frame and no such targets.
//
// A TextSelection selects text with the mouse, one for the screen. The application hands it every mouse event with
// the MouseMap of the frame on screen and calls Clear on every key. Dragging with the left button held from the
// text of a part that supports it selects characters of that part, over several lines too, and the part highlights
// them in the selection fill, reverse video without one; a terminal without colour shows no highlight. The release
// returns the text, which the application hands to the clipboard and reports. A press starts a selection only
// where that text is the topmost target, so a button, an overlay or a part drawn later keeps its click; the
// selection stays within the rows shown and does not scroll, and it ends with the next click or key.
//
// A click and a drag stay apart. A press on selectable text is held back: after Mouse read a click, Holds says so,
// and the application does not hand that press to Handle. At the release without motion, Replay returns the held
// press, and the application handles it as the click it was, so a click acts as before, only at the release; a
// drag never acts as a click and so never marks, opens, folds or presses anything. A press anywhere else, on a
// button, a tick box, a field being typed into or a target without text, is no held press and acts at once. The
// application therefore reads the event with Mouse, returns while Holds is true for a click, replaces the event by
// what Replay returns, and hands the rest to Handle:
//
//	if text, copied := sel.Mouse(m, msg); copied { /* hand text to the clipboard */ }
//	if _, ok := msg.(tea.MouseClickMsg); ok && sel.Holds() { return }
//	if click, ok := sel.Replay(); ok { msg = click }
//
// TextView and LogView support the selection, and so do List, with the file browser and the path overlays that
// draw one, the values a Form shows, Dialog, Help and Explanation. The text holds content only. In TextView and
// LogView, line numbers, the mark, time and source columns, the mark of a cut line, the ellipsis and the fill
// right of a line never come with it; a line break ends each line of the content, never a row where the view
// wraps, and a drag right of the end of a line or onto its ellipsis takes the line to its end. A List gives the
// cell values as drawn, escaped and cut as shown, with a tab between cells and a line break between rows; the
// header, the rule, tick boxes, fold glyphs, the indent and the counts never come with it, a group row gives
// its title and its cells, and a press on a tick box starts nothing. A Form offers each shown value as a part
// of its own, one field per selection: the text of a field that is not being typed into, the label of a choice,
// a key, a date, the value of a list and of a read-only field; never the label of the field, its frame, marker,
// hint, problems or buttons, and not the choices of a multiple choice. Dialog, Help and Explanation give the
// question, the consequence and the details, the text of the topic, or the text, the term and the address, one
// line break between paragraphs and none where a paragraph wraps; the prompt sign of a detail, the tabs of
// Help, keys, buttons, the position line, the input and the messages never come with it. It is the text as
// drawn: a control character as its escape, a tab of a TextView as its spaces and a wide character whole.
//
// Mouse reports never become text. An InputFilter, installed with tea.WithFilter and attached to the program,
// drops the pieces of a report that a terminal behind SSH or tmux delivered in more than one read, so a text
// or passphrase field never receives them, and merges a wheel notch the terminal reports twice. Characters
// typed by hand, pasted or arriving outside the short window of a report pass unchanged; keys it held back
// arrive later as one InputBatch.
//
// A CommandLine runs commands by name: lower-case words without arguments, with aliases that keep older names
// valid. Typing filters, an exact name first, then names that begin with the text, then a word that does, then
// the text inside a word; the empty line lists the commands that are not hidden. A command names an action the
// application already has, which a key, a tab click or a row of its screen triggers, and where it opens one,
// the area and tab, so Update and Click yield what the dispatcher of the application runs for that trigger:
// the line is a register of existing actions, never a second way to operate, and a key is optional. It acts
// only where Available says its trigger acts, in the Context the application sets when it opens the line;
// anywhere else the line stays open with the typed text and the reason. The line reads keys where text is
// typed, so a letter is always text; it edits its text as a text line does and moves the choice with the
// actions of CommandLineKeys. View draws a panel with an optional icon before each name, in a Unicode terminal
// only, and the key in force beside it, rows that scroll with the choice and one target per row. Overlay draws
// it at the top of the room it may cover, centred across it, over the dimmed screen, with a key line of its
// own from Hints whose keys a click answers like the keys do; where the room is too small it returns ok false,
// and the application draws View in the workspace and Hints in the footer.
//
// A List shows the entries an application hands it, each with a stable ItemID, its cells and a group key, as a
// table under bold upper-case column titles with a rule below them, "─" or "-" in an ASCII terminal. The
// selection follows the ID, never the position, when the entries, the filter or the grouping change; it is
// always on screen, and the scroll position stays while the selection is on the page. The filter reads the
// cells and the search text of an entry, ignores case, and only decides what shows: it removes nothing and
// keeps the order. A list whose letters are actions starts the filter with its search key; a pure choice list
// filters with every typed character. The filter is a text line: the actions of the list move the rows, and
// left and right its cursor where no group is left to fold. Enter keeps a typed filter, the first back clears
// it, and the next one is left to the application. Groups are presentation only: the entries of a group stand
// four cells in, plain space with no tree characters; they stand in alphabetical order of their shown title
// with the group without key last (an outline keeps the order of SetGroups), left, right and enter fold them, a
// filter opens the groups with hits without changing their fold state, and the position counts entries only,
// "pos/total" or, with a filter, "pos/shown (N total)". SetGroups gives a group, by the key in ListItem.Group,
// a title and cells; the row then shows the fold glyph, the box, the category mark, the title and the count as
// the group head in the first column, and the cells from the second column on by the rules of their column; the
// first cell is not shown. The fold state, the partial selection and the marked row follow the key, so new
// titles or cells lose none of them, and the filter never reads them. A described group with Keep shows also
// without entries, as a row that marks but neither folds nor ticks. SelectedGroup names the marked group row;
// Selected and Current stay empty there. When a row does not fit, the free text column gives way first, then
// the short labels of a column, then the optional columns from the right, and then the widest values, never
// below their title, for group rows as for entries; the first column and the group head are never cut, and the
// widths come from every entry and described group, so they stay while a filter is typed. An empty list shows
// the application's sentence with the next step, which SetEmpty sets for each state, and a filter without match
// names the key that clears it. Shown names the entries the filter lets through in the order they show, also
// those of folded groups.
//
// The search field of a List, also the one in a filter bar, is a tinted field over its whole width that shows
// its placeholder while empty; where there is no colour but attributes it is underlined, and without both it
// is known by its placeholder alone, never by brackets.
//
// A List with Ticks picks any number of entries: a tick box before the first column, "[ ]", "[x]" and for a
// group with some of its entries ticked "[-]", so the three states differ without colour, and Ticked and
// SetTicked read and set the IDs. The toggle key ticks the marked entry; on a group row it steps the group
// through the partial selection it had, all and none and back to the partial selection, or between all and
// none where there was none. A single toggle in the group, SetTicked and SetItems forget the partial
// selection. The row of all, with AllRow, above all groups does the same for every entry. While a filter is
// set, the boxes and the toggle count and act on the entries it shows only. A list that filters as typed
// reads a text key such as space as filter text, so the toggle key does not tick there; TickWhileTyping lets
// every key of Toggle tick before the filter reads it, at the price that it types no character, and Hints
// then names it in the text focus too through Hint.WhileTyping. Current gives the marked entry
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
// ListSpec.Search can also be SearchNone, a list without filter and without search key, for a short choice
// such as a menu, and ListSpec.Outline draws grouped entries as an outline: the groups stand in the order of
// SetGroups and a group row shows its fold glyph and title only.
//
// A PathNav holds the two overlays the path line opens, both a List in a modal with SearchNone and Outline,
// centred over the screen like any Overlay, the current place marked. The overview lists every
// area of FrameSpec.Areas in order, each with its tabs below it, as a tree: enter or a click on an area with
// tabs folds it, and on an area without tabs chooses it. The choice lists the tabs of the active area. The
// application opens one from the state it already holds: OpenClick for the MouseResult of a ZonePath target
// (the area section opens the overview, a tab section the choice), OpenFocused for enter while the focus is
// at ZonePath, which follows FrameSpec.PathFocusArea, and OpenOverview for the standard action ActionMenu,
// which acts only where nothing is typed. While Open is true the application gives PathNav
// every key and click and the screen behind takes none, as under a Dialog, passing only the targets that
// Overlay returns. Up, down, page up and down, first and last move, enter or a click on a tab chooses, back
// closes. Update and Click return a PathChoice, the area and tab, which the application runs exactly as a
// click on that tab; a PathChoice that is the zero value is no choice. PathNav answers with a PathChoice, so
// it has the shape of Modal without satisfying the interface.
//
// Area.Key and Tab.Key give an entry of the overview and of the choice a key of its own: one ASCII letter or
// digit, shown dimmed in a column of its own at the entry and chosen at once as enter on the entry does, so it
// folds an area with tabs and chooses a tab or an area without tabs. The key acts only while the overview or
// the choice is open and never on a tab of a folded area, whose key is not shown. Any letter or digit is
// allowed, also one that is a standard action elsewhere, since no action runs while an overlay is open.
// Applications run PathNav.CheckAreas with CheckAreas, in their tests or at start.
//
// The marked row is drawn as selected only while the rows have the focus, as SetFocused says (a new list has it;
// the modal always draws the mark), so no second mark shows while the focus lies elsewhere. A click on a row
// marks it, and only a click on the marked row while the rows have the focus opens it, as enter does; with
// ListSpec.ClickOpens a click on any entry row opens it at once instead, while the rows have the focus. In both
// forms a click on a list without the focus only focuses and marks it; a click on a group row folds it and never
// changes the ticks, a click on a tick box does what the toggle key does, and one on the search line starts the
// search. Each such target carries the action of its key and an Item the list reads back in Click; a target with
// a zone alone is the free area of the zone. A FilterBar holds choice fields of one value or more: left and
// right change the value, and tab and shift+tab step through the fields and leave the bar at its ends. Enter
// opens the list of values: a search line as its first row filters them by part of their text in any case, up,
// down and the optional page keys move the cursor in a few rows that scroll, enter takes the value under it and
// back closes it without change. The panel shows the position at its top edge, as a List does, a check mark at
// the value in force and, for a search without match, the way out; ValueList is given the room it may fill. An
// action field shows a text, set with SetActionText, in the same order; enter, left, right or a click report its
// action once to TakeAction, which the application runs, and it never counts as a changed filter. Its values
// change only what the application hands to the list, never its data. SetOptions replaces a field's values; the
// value, the open list with its search and the modal's draft stay on their IDs, and a call that drops the value
// is rejected unchanged. A click opens a field's values, one on a value takes it; the search field of a list may
// stand at its right end. On a narrow terminal the values are cut first; where the fields still do not fit, a
// single button with the number of fields off their first value takes their place, always, with the search field
// beside it or, with little room, only the search key. The button is one stop of the focus. Enter or a click
// opens a modal with all fields one under the other: up and down choose a field, left and right change its
// value, enter takes all changes at once and back drops them all; pressing an action field there takes them
// first, then reports the action. The modal takes every key and is drawn over the screen where it fits, else in
// the workspace with its keys in the footer.
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
// with handled false, so the application still asks before it leaves a changed form. While a button has the
// focus, typing, paste, left, right, up, down and the wheel change nothing; a click on a field takes the
// focus. A button with the focus is always in view: the fields get the height that is left, or none, and only
// where not even the buttons fit does the form ask for a larger terminal. With a field focused, the form is
// drawn without the buttons where they do not fit beside it. While the focus of the screen lies elsewhere,
// SetFocused(false) draws the form without any sign of its focus: no marker, accent, frame, cursor or hint at
// the focused field, no mark at its section and no focused button, so only one focus shows. It only draws: the
// focused field or button, open lists and what is typed stay, and keys, clicks and paste act as before, as the
// application hands them on. A new form has the focus, and Overlay always draws it. Overlay draws the form as
// a modal in the way of Dialog.Overlay, under FormSpec.Title in its frame, with its buttons and the keys of
// Hints, and ok is false where room does not hold it, also with a list of values or the calendar open; the
// application then draws View in the workspace and Hints in the footer. FormSpec.Lead is one or two dimmed
// sentences under the title, wrapped and never cut. It stays in view at every focus, in View as in the modal,
// counts for ok, and SetLead changes it and keeps values, focus and open list. Stored says that a masked field
// has a secret, which an empty field keeps. FieldMaskedBlock is the same for a secret of several lines, such
// as a private key: its value is text with "\n" line breaks and it shows a placeholder with the number of
// lines. Paste replaces the whole value, typed characters are appended, enter adds a line break instead of
// going on, and backspace or delete clears the field; tab, the save key and a click work as in any field. Each
// problem stands under its field until that field changes, a problem without a field above the form, and a
// section with a problem carries a mark and opens. An advanced section stays closed and dimmed while it holds
// nothing. On a wide terminal the sections stand as a numbered list beside the fields of the focused one. The
// fields lose their frames only where the width does not hold them, and then only the focused field stands on
// a surface. Where the height is short, the form scrolls with the focus, which is always in view: whole fields
// stand around it, with "…" above and below where fields are left out. Each section has its own label column,
// in which a longer label wraps. Only where even the focused field does not fit does the form ask for a larger
// terminal. A click on a field focuses it and types nothing; on a choice or list field it is enter, on a tick
// box the toggle key, on a section a jump to its first field.
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
// follows, the details, one button per answer and the key line of the same answers, so no dialog names one
// twice. A detail is a DialogLine of plain text, optionally behind a prefix such as a prompt sign; the dialog
// wraps it before it styles, and Irreversible draws the sentence on what follows in the warn colour. A Notice
// is a calm message headed by its Question, without the warning glyph. Enter gives the default, whose button
// carries the enter key, "[ enter OK ]"; "> label <" marks a focus only, which a dialog has none of. The
// destroying answer is drawn in the fail colour. A dialog with an input is a text field, shows what runs and
// the last error inside itself and drops the error when the text changes. Overlay dims the screen behind it
// and returns the targets of the dialog alone; where it does not fit, View fills the workspace and Hints go
// into the footer, whose targets answer it.
//
// Details that do not fit never push out the question: everything else stays, and the details scroll in the
// rest with their position under them; Overlay gives way to View only where not even three detail lines fit.
// Hints names the scroll keys behind the answers while the details scroll, the first to go where the line is
// short. DialogSpec.Extra adds hints of the application, such as help, behind the answers: click targets that
// give way before the answers and that Update and Click leave to the application.
//
// DialogInput.Suggest hands an input suggestions, such as from a projectpath.Suggester; tui reads no file
// system. One row and one click target stand under the input for each. The complete action
// and up and down mark and take them, and enter takes the typed text unless one was marked; whether a taken
// one keeps the dialog open is Continues, never a path separator. Without suggestions nothing changes.
//
// Help explains in three stages: the one-line hint under the focused field, the explanation of the focused
// element, and the help with its topics. The Help shows the application's helptopics.Topic values, the same
// topics its command line and manpage show, wrapped as the command line wraps them, with a short unindented
// line that ends in a colon as a heading. An optional dimmed Note stands fixed between the tabs and the text
// of every topic where three text rows remain below it, and is otherwise left out whole. The first topic ends
// with the keys of the screen the help was opened from and the keys that act everywhere, in the binding in
// force. In place of topics the Help can show the application's Markdown pages from an fs.FS, as package
// helppages loads and splits them, in HelpSpec.Lang and in English where a page has no translation. A page
// shows its title and its text escaped, with every key placeholder spelt in the binding in force when it is
// drawn, the address of a link after its text and linked where the terminal takes escapes, and the link of
// the page under TextHelpMore. Pages have no tabs: the topic keys turn the pages topic by topic, the page of
// each topic first, with wrap-around. OpenScreen shows the page of the first screen ID that a page carries,
// else the first page, and that page instead of the first one ends with the key lists. Left and right, and
// the topic keys of the help level, change the topic with wrap-around; the scroll
// keys and the wheel stop at the ends; back and the help key close it. A click on a line of the key list
// yields its action, and the application closes the help and then runs it. An Explanation shows a title, the
// long text, a helptopics.Term and the address of the documentation, which is shown in full and, where the
// terminal takes escapes, also linked; Back and the info key close it, its scroll keys may page and jump to
// the ends, and the copy key hands Text to the application. Both stand over the screen like a dialog, or fill
// the workspace with View where room is too small; an explanation also stands in a Pane beside a form or a
// list. Every topic tab, key, line of the key list and address is a target that does what its key does, and a
// click anywhere else on an explanation over the screen closes it. Only their own keys act; every other key
// goes back to the application.
//
// A LogView shows lines that arrive over time. The application supplies them with Append, one text or several,
// each with an optional time and source; where they come from, saving them and any structure in them stay with
// the application. The lines live in a ring buffer of LogViewSpec.Lines; when it is full the oldest lines go,
// and the position and the matches follow what remains. A line break in a text starts a new line. The view
// follows the newest line until a key or the wheel scrolls up; the follow key and the end key resume, and the
// status line names the state with the number of lines that arrived meanwhile. The search key starts a
// case-insensitive search of the escaped text, enter keeps it and back cancels or clears it; every match is
// highlighted and the lines that hold one carry a mark, so nothing depends on colour, and the next and
// previous match key jump with wrap-around and stop the following. Wrapping and the time column are switches;
// without wrapping a long line is shortened with the ellipsis glyph. LogViewActions gives the actions only
// this part needs; scrolling, search, enter and back are the actions the table already holds.
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
// key and enter apply, enter from every field but a date, where it opens the calendar as in every form. A
// choice steps with left and right, and its list opens by a click on the field; a list or the calendar takes
// every key, enter chooses, and back closes only that. The form's buttons apply and discard show the save and
// back keys in force and act as they do, enter on one of them included. A period that is not valid, no count
// or one below 1, an empty date or an end before the start, stays open with its problem under the field. An
// applied period that differs from the current one is reported once.
//
// The status line holds a button for following, search, the period, wrap and the time column, and for the
// previous and next match while a search has matches, with the position at its right. The period button, which
// LogViewKeys.Filter enables, names the period in short form. Each button names the key in force and a switch
// says on or off in words; a click does what its key does and no more. Where the width runs short the labels
// go first, one by one by rank, and the keys stay; then buttons go by rank, following and search last and the
// period before them; the position gives way last, to a shorter form. While the search text is typed the row
// holds that field and the position only.
//
// A TextView shows a text read-only: line numbers, scrolling, wrapping, search and a status row with the
// position, from the text and optional TextSpan sections the application gives SetText. It is made for foreign
// content, and the status row names a cut. A masked section is uncovered by the application with SetRevealed;
// the reveal key only asks, through TakeRevealRequest. The roles of the sections are neutral and drawn in the
// syntax roles of the Palette; a package that tells them apart builds the spans, TextView knows no language.
//
// A Chat shows the messages an application hands it, oldest first, each with a stable ID, sender, time, status
// and details, above a field to write the next one. The selection follows the ID, and the newest message while
// it is selected; a status names its state by a glyph and a word. Enter unfolds the details of the selected
// message, and the quick replies of ChatSpec.Actions stand as buttons under it and answer it by their key or a
// click. In the field every key that types a character is text, enter adds a line break and back leaves it;
// ctrl+enter sends where the terminal tells it from enter. The send button, the stop after the field for the
// next key, sends with enter in every terminal. Update and Click return the sent text or the quick reply;
// delivering, storing and the state of a message stay with the application.
//
// A FileBrowser shows the folders and files below a root from entries the application hands it; it reads no
// file system. A folder is a list of name parts below the root, never a joined string. It asks
// for a folder with a FileLoad and shows only the answer to the last one; actions come back as a FileAction.
// The application still joins the parts below its own root and checks the result, and runs every action.
//
// Colours come from themes. Screen code names roles, never colour values: a Palette assigns one colour per
// role, Styles build text styles and widgets on it. A ThemeSet holds the terminal theme, the application's own
// themes and the built-in schemes; each theme is data, either hand-picked palettes per colour profile
// (DarkTheme and LightTheme are such palettes under a name the application chooses) or a scheme of foreground,
// background and 16 ANSI colours. The terminal theme is the default: it paints no ground, names the ANSI slots
// for its accents and derives its surfaces from the foreground and background the terminal reports, plain
// until it has; AsksTerminalColors says when to ask. Step names the theme a number of places before or after
// another in the order of Names, wrapping at both ends. A derived palette that would fall below the contrast
// of a theme falls back to the plain palette, which has no ground and no fills and shows the selection in
// reverse video. The syntax roles of a Palette may stay empty and then take the text colour. Any other theme
// with a ground colour paints every cell through Paint. An unknown theme name resolves to the terminal theme
// and is reported, so the application can show TextThemeUnknown.
//
// DetectColorProfile and DetectCharsetMode read the terminal once at start. Widgets return lines of exactly the
// requested width; messages are wrapped and never cut, table cells are shortened with the ellipsis glyph, and
// a cut never splits an escape sequence and closes any style it leaves open. Escape is for data an
// application puts into styled content.
//
// The built-in schemes come from the iTerm2-Color-Schemes collection at a fixed commit, generated by
// scripts/gen_tui_themes.go; their licences are in themes_builtin_licenses.txt.
//
// # Always enforced
//
// Catalogs. NewTranslator rejects a catalog that would draw wrong text: an empty ID, an ID under the reserved
// prefix "tui.", a text in a language other than English without an English text, format verbs that differ
// from the English text, and a key placeholder that names no known action. CheckCatalogs is the stricter
// test-time contract that every text exists in English and German, none is empty and the verbs agree. An
// action ID starts with a lowercase ASCII letter followed by lowercase ASCII letters, digits, '.', '_' or '-'.
//
// Key table. NewKeymap rejects an empty table, an action without a valid placeholder ID, level, key or help
// text, an ID or a key listed twice, and a key that two actions of the main levels or of one overlay level
// share. Within the main levels a key belongs to one action everywhere, except that an action tied to a page
// (Action.Page) may share it with an action of another page, never with one of its own page or one without a
// page; only the overlay levels LevelConfirm and LevelHelp may repeat a key of the main levels. It also
// rejects a key that terminals, multiplexers or text lines take for themselves unless the action is the
// second way of another (Action.SecondWayOf), the key of a standard action held by any other action of the
// main levels whether or not the standard action is in the table, and a rebindable action whose default key
// breaks the rules of a binding. Bind applies the user's keys as a whole or not at all, and CheckBinding and
// Resolve apply the same rules to a binding, refuse a key that belongs to another standard action and name
// the reason of every refusal. The input focus decides what reaches the table: where text is typed, every key
// that types a character is text, and that is the zero value, InputText; while a key is captured as a new
// binding only ctrl+c and esc act; in an embedded terminal no key does. No action of a key table may hold
// ctrl+enter, which the chat uses to send.
//
// Text editing. A one-line text field, the input of a Dialog, the text, masked and date fields of a Form, the
// search of a LogView, the filter of a List and the input of a CommandLine edit with keys that no action may
// hold: the arrows, home, end, ctrl+a, ctrl+e, ctrl+b and ctrl+f move by a character and to the ends; ctrl or
// alt with an arrow, alt+b and alt+f by a word; backspace, delete, ctrl+h, ctrl+d, ctrl+w, alt+d,
// alt+backspace and alt+delete delete a character or a word; ctrl+u and ctrl+k delete to the start or end. A
// word ends at white space; in a masked field a word jump goes to an end and a word deletion takes everything
// on that side, so no boundary shows. The Paste of each of these parts and a suggestion a Dialog takes insert
// whole at the cursor, never as keys: line breaks at the end go, those and tabs inside become one space, and
// other control characters, invalid UTF-8 and direction marks such as U+202E are dropped, in a masked field
// too.
//
// Specs. Every NewX checks its XSpec against the Keymap and the Translator and refuses what cannot work: a
// missing key or one that names no action of the key table, two keys that name one action, a missing text or
// label, and the further rules the doc comment of the constructor lists. Among them a Dialog needs a question,
// answers and, unless it is a Notice, a consequence; a Notice needs a detail line and has no input or danger;
// red detail lines belong to a Notice only; the destroying answer is never the default, so enter never
// destroys; an answer or scroll key of a dialog with an input needs a key that acts while text is typed; a
// masked input starts empty and has no suggestions. A List has a first column that is never free text or
// optional, at most one free text column, a text for its empty state, AllRow only with Ticks and
// TickWhileTyping only with Ticks and SearchAsTyped. A Form refuses an unknown field kind, a missing or
// repeated ID and an ID with a control character. A CommandLine refuses names that are not lower-case words,
// two commands that do the same and an icon that fails CheckIcon. An Explanation accepts only an absolute http
// or https address of printable ASCII, so the address cannot end the link sequence. CheckAreas holds areas to
// what the tab line can draw and refuses an entry key (Area.Key, Tab.Key) used twice in the overview, which
// lists every area and tab, or one that is no ASCII letter or digit; PathNav.CheckAreas also refuses a key of
// PathNavKeys and the key of ActionMenu, and an overlay opened with areas that fail it shows and acts on no
// entry key. CheckFrame refuses an unknown navigation form and an icon that fails CheckIcon, and the frame
// draws no icon that fails it. CheckIcon holds an icon to one code point of a width every terminal agrees on.
// Setters such as List.SetItems and FileBrowser.SetEntries refuse data that breaks the contract of the part and
// change nothing then; an entry of a FileBrowser whose name is empty, ".", "..", or holds "/", "\" or NUL
// refuses the whole folder, so no step leaves the root.
//
// Escaping and secrets. Control characters and escape sequences never reach the terminal as such: Escape writes
// every control or otherwise unprintable character as its Go escape, and widgets and parts escape every plain
// text they take, among them cells, group keys and titles of a List, the empty sentence, the typed filter, the
// lines, sources and search text of a LogView, every character of a TextView, every text of a Chat message, a
// FileBrowser entry, the suggestions and details of a Dialog and the names of a PathNav. A part that takes
// styled content leaves the escaping of its data to the application. Only an icon that passed CheckIcon appears
// unmasked. A masked field or dialog input never shows what is typed, not in its row, a target or a problem,
// and is never handed a stored secret. FieldMaskedBlock shows a placeholder with the number of lines, never the
// content or its length, and refuses whole a paste or key with a character other than line break and tab. A
// masked section of a TextView stays hidden in every drawing and out of every match until the application calls
// SetRevealed, and hiding again needs no permission. A TextSelection takes a masked section, hidden or
// revealed, and a masked field of a Form as the mask of fixed length, so neither its content nor its length
// leaves the view. A Dialog acts only on its answers, the scroll keys and the suggestion keys; every other key
// goes back to the application, so a stray key neither saves nor destroys.
//
// Limits. A FieldMaskedBlock holds at most 64 KiB, counted after line breaks are normalized. The TextView
// keeps at most TextViewMaxLines lines of at most TextViewMaxLineRunes characters, a longer line is cut with
// a visible mark and a negative limit is refused. A LogView buffer holds at most LogViewMaxLines lines, each
// at most LogViewMaxLineRunes characters, and a longer line is cut with a visible mark. A LogFilter count
// goes from 1 to LogFilterMaxCount. A Chat draft holds at most ChatMaxDraftBytes. A Dialog shows at most five
// suggestions, a CommandLine at most twelve rows at once.
//
// Drawing. A profile without colour, set by NO_COLOR with any non-empty value or by a missing terminal, wins
// over every theme; ProfileNone also drops bold, faint and reverse. Meaning never depends on colour alone:
// status marks keep their word, the selected row its mark, the default of a dialog its key, the active tab its
// brackets and a focused frame its double line without colour, a message status names its state by a glyph and
// a word, and every glyph has an ASCII form for a terminal without UTF-8. NewThemeSet rejects an invalid or
// repeated theme name, a theme with both or neither of Profiles and Scheme, a colour that is not of its
// profile's form, a hand-picked palette whose text does not reach 4.5:1 on its ground and fills or whose dimmed
// text does not reach 3:1, and a scheme that cannot be derived. A filled syntax role of a Palette must read
// 4.5:1 on the base and the panel, a comment 3:1. The mouse acts on a press of the left button and on the wheel
// only, and a MouseMap holds the targets of one drawn frame, so a target no longer on screen cannot act; a
// click is never stronger than the key it stands for. A dialog with a Danger ignores CloseOutside and adds no
// target outside its frame, so a click beside a destructive question never answers it.
//
// Boundary. The package holds neither application content nor package state and reads no file system. It
// protects against foreign text driving the terminal, against secrets showing on screen and against a key
// or click doing more than the action the application named. It does not check the application's data: what
// a name, a value or a path means, whether a revealed secret may be shown and what an action does stay with
// the application.
//
// # Options
//
// Key table:
//
//   - Action.Rebind: lets a user replace the single default key (RebindReplace) or add one key that also works
//     where text is typed (RebindExtra). Default: RebindNone, the keys stay fixed.
//   - Action.SecondWayOf: names the action this one repeats with keys no terminal takes, which may then hold a
//     key terminals take for themselves. Default: empty, such a key is refused.
//   - Action.Page: ties the action to one page, so it may share a key with an action of another page.
//     Default: empty, no page; the action is visible on every page and shares a key with none.
//   - Keymap.Page: the view of one page. Default: the table from NewKeymap sees the actions without a page
//     only.
//   - NewKeymap charset: how Label spells the arrow keys. Default: the zero value CharsetASCII.
//   - Keymap.Match overlays: the overlay levels LevelConfirm and LevelHelp a screen names so that they win
//     over the main levels. Default: none, only the main levels match.
//   - StandardActions: the actions every tool shares. Default: not in the table until the application
//     appends them.
//   - NewTranslator keys: resolves the key placeholders of the texts. Default: nil, a text with a placeholder
//     is rejected as naming an unknown action.
//
// Frame and hints:
//
//   - FrameSpec.ProductShort, Icon, ModeTab, Back and Message: the short product name, the logo glyph, the
//     text of the mode tab, the back button and the message line. Default: empty or nil, each is left out.
//   - FrameSpec.Nav: the form of the navigation, NavTabs, NavPathLine, NavPathHeader or NavHeaderChoice.
//     Default: NavTabs, the tab line with sub-tabs.
//   - FrameSpec.PathFocusArea: puts the focus of ZonePath on the area section. Default: false, the section of
//     the current place.
//   - FrameSpec.Input: the input focus of the screen, which decides what the hints show. Default: InputText.
//   - FrameSpec.HeaderKeys, Footer and Position: the header keys, the footer hints and the position text.
//     Default: none.
//   - Area.Icon, Tab.GroupStart: an icon before the tab title and a separator before a sub-tab. Default: none.
//   - StatusMark.Short, Rank and NoGlyph: shorter forms of the text, the place in the order the header fills
//     its room, and the text without glyph. Default: no shorter form, MarkNormal, with the glyph.
//   - Hint.Priority, Kind and WhileTyping: the order in which a short line drops hints, the kind of the hint
//     and whether it names keys that type a character as well. Default: 0, HintNormal, false.
//
// Dialog:
//
//   - DialogSpec.Irreversible, Notice: draws the consequence in the warn colour, makes the dialog a calm
//     message. Default: false, false.
//   - DialogSpec.Default, Danger: the answer enter gives and the answer that destroys. Default: empty, enter
//     answers nothing and no answer is drawn as destroying; Default needs DialogKeys.Enter.
//   - DialogSpec.Input and DialogInput.Masked, Value, Suggest: an input line, whether it is masked, its start
//     value and its suggestions. Default: nil, no input; not masked, empty, no suggestions.
//   - DialogSpec.Extra, DialogLine.Prefix and Style: hints of the application behind the answers, a prefix
//     before a detail and its style. Default: none, none, StyleText.
//   - DialogSpec.CloseOutside, and the same option of HelpSpec, FormSpec, ListSpec, CommandLineSpec,
//     ExplanationSpec and PathNavKeys: a click outside the frame of the open modal gives the named answer of
//     a dialog, one of its answers, and acts as the back key of the others. Default: empty or false, such a
//     click does nothing.
//   - DialogKeys.Up, Down, PageUp, PageDown, First, Last: scroll keys for details that do not fit. Default:
//     empty, only the wheel scrolls.
//   - DialogKeys.Complete, SuggestUp, SuggestDown, Suggestion.Continues: take and move through suggestions,
//     and keep the dialog open when a suggestion is taken. Default: empty, false; an input with suggestions
//     needs Complete, Up and Down.
//
// List, filter bar and path navigation:
//
//   - ListSpec.Search: how the list takes its filter, SearchByKey, SearchAsTyped or SearchNone. Default:
//     SearchByKey, which needs ListKeys.Search.
//   - ListSpec.Wrap, SearchInBar, Outline: wrap at the ends, draw the search field in the FilterBar, draw
//     groups as an outline. Default: false each.
//   - ListSpec.Ticks, AllRow, TickWhileTyping and ListKeys.Toggle: tick boxes, the row of all and ticking in
//     the text focus; Toggle is needed with Ticks. Default: false each, no tick boxes.
//   - ListSpec.Title, NoGroup, Buttons: the title of the modal, the title of the group without key and the
//     buttons of the modal. Default: no title, TextListNoGroup, no buttons.
//   - Column.Optional, Flex, Right, Dimmed, Short, Tone: leave out the column on a narrow terminal, mark the
//     free text column, align right, dim, shorter labels, style of a cell by value. Default: false, false,
//     false, false, nil, nil.
//   - ListSpec.ClickOpens: a click on any entry row opens it at once while the rows have the focus. Default:
//     false, a click marks the row and only a click on the marked row opens it.
//   - ListGroup.Keep: shows a described group without entries. Default: false, the row is not drawn.
//   - Area.Key, Tab.Key: a key of its own for an entry of the path overview and the choice. Default: none, no
//     key column shows.
//   - FilterBarKeys.PageUp, PageDown and FilterBarSpec.List: page keys of the value list and the list the bar
//     stands above. Default: empty, nil.
//
// Form, calendar, command line, help:
//
//   - FormSpec.Title, Lead, Sections, Buttons: the title of the modal, the lead sentences, the sections and
//     the buttons below the fields. Default: none each; FormButton.Primary and FormSection.Advanced default
//     to false.
//   - DateField.Time, Min, Max, Now: adds the time of day, bounds the value and sets the clock. Default:
//     false, no bound, time.Now; FormField.Date applies to a FieldDate only.
//   - Form.SetFocused, List.SetFocused, FileBrowser.SetFocused: draw without any sign of focus while the focus
//     lies elsewhere. Default: focused.
//   - Command.Aliases, Icon, DescriptionArgs, Key, Hidden: further names, a glyph, arguments of the
//     description, the position of the one key to show and whether the empty line hides the command. Default:
//     none, none, none, 0 (all keys of the action), false.
//   - HelpSpec.Pages, Lang: Markdown pages in place of Topics, exactly one of the two, and the language of
//     the pages. Default: no pages, English.
//   - HelpSpec.Screen, Global, Note and HelpKeys.Next, Prev, First, Last: the key lists, the fixed note and
//     further keys. Default: no key list, no note, no further key.
//   - ExplanationItem.Term, URL, LinkText and ExplanationKeys.PageUp, PageDown, First, Last, Link: the term,
//     the documentation address, the text above it and further keys. Default: none, none, TextInfoLink, no
//     key.
//
// Log, text and chat views:
//
//   - LogViewSpec.Lines: the size of the ring buffer. Default: LogViewDefaultLines.
//   - LogViewSpec.Wrap, Timestamps: the start state of the two switches. Default: false each.
//   - LogViewSpec.Filter, Units, Now: the start period, the units on offer and the clock. Default: every line,
//     all four units, time.Now.
//   - LogViewKeys.First and Filter: the jump to the oldest line and the choice of the period. Default: empty,
//     no key and no filter button; Filter needs every key of FilterForm.
//   - TextViewSpec.MaxLines, MaxLineRunes: the lines kept and the characters of a line. Default:
//     TextViewDefaultLines and TextViewDefaultLineRunes.
//   - TextViewSpec.Wrap, Numbers: the start state of the two switches. Default: false each.
//   - TextViewKeys.First, Last, Reveal: jump to the ends and ask to uncover masked sections. Default: empty,
//     no key; the application calls SetRevealed.
//   - ChatSpec.Actions, Now: quick replies and the clock. Default: none, time.Now.
//   - FileBrowserSpec.Actions, RootLabel, FileBrowserKeys.Reload: the buttons on the marked entry, the label of
//     the root and the reload key. Default: none, TextFileRoot, none.
//
// Input and colour:
//
//   - InputFilter: drops split mouse reports from text input once installed with tea.WithFilter. Default: not
//     installed, the pieces of a report can reach a text field. NewInputFilter takes a clock, nil is the system
//     clock, and SetAfterFunc replaces the timer, both for tests.
//   - NewThemeSet extra: the application's own themes. Default: none, the terminal theme and the built-in
//     schemes.
//   - Theme.Profiles or Scheme: a hand-picked palette per colour profile or a scheme the palette is derived
//     from. Default: the terminal theme, which paints no ground.
package tui
