// Command tuidemo shows the terminal building blocks of the kit at work, with invented sample data: a frame
// with product name, tabs, sub-tabs, a header and a footer with key hints, a key table, mouse support,
// dialogs and questions, help, explanations, a command line, and live switches for theme, language, icons and
// mouse. It is a workbench to try and compare variants of the blocks, not a program to install; start it from
// a checkout of the module:
//
//	go run ./cmd/tuidemo
//
// Optional flags set the start values of the switches: -theme, -language (english, deutsch or system), -icons
// (unicode, nerdfont or none) and -mouse=false; the kit checks the values and an invalid one ends the program
// with an error that names it. The switches keep their values in memory only; the program reads and writes no
// configuration file.
//
// Operation: the digit of a tab opens it, left and right step through the tabs of the focused line, tab and
// shift+tab move the focus between the tab line and the sub-tab line, and q or ctrl+c quits. The switches act
// at once: t for the theme, T (shift+t) for the theme one step back, l for the language, i for the icons and
// m for the mouse. The Dialogs tab opens a question with a destroying answer (a), an input (r) and a notice
// (o); the Help tab shows the help (?), the explanation of the sample data (e) and the command line (:),
// which runs commands by name, such as the tabs and the switches, theme-back among them. The command line
// stands over the dimmed screen, at the top and centred, with a key line of its own; where the terminal is
// too small for that it fills the workspace and its keys stand in the footer. Esc closes whatever stands over
// the screen, and nothing behind it takes a key or a click meanwhile.
//
// Every function can be clicked while the mouse is on: tabs, the key hints of the header and the footer, the
// rows of the Settings tab (the theme row and, under it, the row that steps the theme back), the buttons of a
// dialog and of the Dialogs and Help tabs, the topics and the key list of the help, and the commands of the
// command line and the keys of its key line. The mouse switch is one of them: a click on its row or on its
// footer hint switches the mouse off, and only its key m switches it on again, since a switched-off mouse
// takes no click.
//
// The program uses the public API of the kit packages only, as a tool built on them would. Each tab belongs
// to one group of blocks and is one entry of the list returned by groups, so a further group adds a tab
// without changing the frame; its actions join the key table in keyTable and run in model.run, which keys,
// clicks and commands share.
package main
