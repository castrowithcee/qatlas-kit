// Command tuidemo shows the terminal building blocks of the kit at work, with invented sample data: a frame
// with product name, tabs, sub-tabs, a header and a footer with key hints, a key table, mouse support,
// dialogs and questions, help, explanations, a command line, and live switches for theme, language, icons,
// mouse and the form of the navigation. It is a workbench to try and compare variants of the blocks, not a
// program to install; start it from a checkout of the module:
//
//	go run ./cmd/tuidemo
//
// Optional flags set the start values of the switches: -theme, -language (english, deutsch or system), -icons
// (unicode, nerdfont or none), -navigation (tabs, pathline or header) and -mouse=false; the kit checks the
// values and an invalid one ends the program with an error that names it. The switches keep their values in
// memory only; the program reads and writes no configuration file.
//
// Operation: the digit of a tab opens it, and q or ctrl+c quits. The focus stands on the tab line, the
// sub-tab line or the work area of the open tab: its buttons, shown as "> label <", or its live block. Tab and
// shift+tab move it through these in turn; down and up step one line at a time, and enter on a tab line leads
// into the work area. Left and right switch tabs only while the focus stands on a tab line. On the buttons,
// down and up move between them and enter triggers the focused one as a click would. A live block reads the
// keys first while it has the focus, so a letter typed into its filter is no switch; up from its top leads
// back to the sub-tab line, except in the text, the log and the chat of the Views tab, where up scrolls or
// selects. Esc leads back from the buttons and from every block, once the block has nothing of its own left
// to close. The footer shows the keys of the focused block, or else the moves that act there, and each
// sub-tab adds the keys of its own actions only; the key list of the help is that footer. The switches act at
// once: t for the theme, T (shift+t) for the theme one step back, l for the language, I for the icons and M
// for the mouse; each also runs from the command line as theme, theme-back, language, icons and mouse.
//
// The Frame tab has a mouse probe whose button counts clicks (+). The Settings tab is a form of choice fields
// for five switches (Look: theme, icons and navigation, Input: language and mouse): left and right step through the
// values, which take effect at once, and enter or a click opens the list of values. In the theme field, left
// is the step back. The form shows the value a switch sets elsewhere; a switch of the language builds the form
// again and keeps the field in focus. The Dialogs tab opens a question with a destroying answer (a), an input
// (r) and a notice (o); the Help tab shows the help (?), the explanation of the sample data (i) and the command
// line (:), which runs commands by name, such as the tabs and the switches. The command line stands over the
// dimmed screen, at the top and centred, with a key line of its own; where the terminal is too small for that
// it fills the workspace and its keys stand in the footer. Esc closes whatever stands over the screen, and
// nothing behind it takes a key or a click meanwhile. The workbench turns on the CloseOutside option of the
// kit, so a click beside the input and the notice of the Dialogs tab, the reveal question of the Views tab
// (it answers no), the help, the list of beds, the sample paths and the path overlays closes them as Esc
// does, without running what stands behind. The question with the destroying answer ignores it and stays
// open; the explanation keeps its own way of closing.
//
// The Lists tab shows a list with a filter bar, a grouped list that folds and a list with tick boxes. The Forms
// tab shows a form that adds a bed (text, choice, tick boxes and a masked field) and one with date fields and
// their calendar, opened by enter or a click on its symbol. F2 or the save button checks the values and reports
// what is missing; the reset button starts the form again.
// The Pickers tab shows a list of beds as a modal over the screen (p or its button; typing filters, enter or a
// click on the marked row takes it, esc closes), a file browser over a sample folder and an input that offers
// sample paths while it is typed (s or its button; up and down mark one, tab completes it). The sample folder
// is a temporary directory the program creates at its start and removes when it ends, on every way out but a
// kill; the browser reads and writes nothing outside it.
// The Views tab shows a read-only text with syntax colours, line numbers, search and a masked secret that the
// workbench uncovers only after a question (V, answered by y or n); a log of invented lines over time with
// following, search, wrapping, a time column and a period dialog (P) with a calendar for a range; and a chat
// whose field takes several lines (enter breaks the line, tab leads to the send button, where enter sends) and
// whose quick replies answer the selected message.
//
// The form of the navigation switches at run time: the key N, the command navigation and the field in the
// Settings tab step through the tab lines, the path line (one line below the header, "Area ▾ › Tab ▾") and
// the path in the header, and -navigation sets the start form. On both path forms the path takes the place
// of the tab lines in the focus: left and right move the focus between its area and its tab section, enter
// opens the overview of all areas for the area section and the choice among the tabs of the area for the tab
// section, down and tab lead into the work area and up and esc lead back to the path as to the sub-tab line,
// and m opens the overview from anywhere nothing is typed; on the tab lines m does nothing. A click on a
// section opens the same, a click on a place of an overlay chooses it as a click on that tab would, a click
// on an area with tabs folds it, and esc closes the overlay. Nothing behind an overlay takes a key or a
// click. The digits still open the tabs.
//
// Every function can be clicked while the mouse is on: tabs, the key hints of the header and the footer, the
// fields of the Settings tab, the buttons of a dialog and of the tabs, the topics and the key list of the help,
// and the commands of the command line and the keys of its key line. The mouse switch is one of them: a click
// on its field or on its footer hint switches the mouse off, and only the keyboard (the key M or the command
// mouse) switches it on again, since a switched-off mouse takes no click.
//
// The program uses the public API of the kit packages only, as a tool built on them would. Each tab belongs
// to one group of blocks and is one entry of the list returned by groups, so a further group adds a tab
// without changing the frame; its actions join the key table in keyTable and run in model.run, which keys,
// clicks and commands share.
package main
