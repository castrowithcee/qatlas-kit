// Command tuidemo shows the terminal building blocks of the kit at work, with invented sample data: a
// frame with product name, tabs, sub-tabs, a header and a footer with key hints, a key table, mouse
// support, dialogs and questions, help, explanations, a command line, and live switches for theme,
// language, icons and mouse. It is a workbench to try and compare variants of the blocks, not a program to
// install; start it from a checkout of the module:
//
//	go run ./cmd/tuidemo
//
// Optional flags set the start values of the switches: -theme, -language (english or deutsch), -icons
// (unicode, nerdfont or none) and -mouse=false. The switches keep their values in memory only; the
// program reads and writes no configuration file.
//
// Operation: the digit of a tab opens it, left and right step through the tabs of the focused line, tab and
// shift+tab move the focus between the tab line and the sub-tab line, and q or ctrl+c quits. The switches
// act at once: t for the theme, l for the language, I for the icons and M for the mouse. The Dialogs tab opens
// a question with a destroying answer (a), an input (r) and a notice (o); the Help tab shows the help (?),
// the explanation of the sample data (i) and the command line (:), which runs commands by name, such as
// the tabs and the switches. Esc closes whatever stands over the screen, and nothing behind it takes a key
// or a click meanwhile.
//
// Every function can be clicked while the mouse is on: tabs, the key hints of the header and the footer, the
// rows of the Settings tab, the buttons of a dialog and of the Dialogs and Help tabs, the topics and the
// key list of the help, and the commands of the command line. The mouse switch is one of them: a click on
// its row or on its footer hint switches the mouse off, and only its key M switches it on again, since a
// switched-off mouse takes no click.
//
// The program uses the public API of the kit packages only, as a tool built on them would. Each tab belongs
// to one group of blocks and is one entry of the list returned by groups, so a further group adds a tab
// without changing the frame; its actions join the key table in keyTable and run in model.run, which keys,
// clicks and commands share.
package main
