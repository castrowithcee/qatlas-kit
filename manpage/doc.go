// Package manpage adds application help topics to a manpage generated from its command tree, so the page
// shows the same text as the help command.
//
// # Always enforced
//
// Topic names and text are escaped so they never become roff requests. Generate accepts only a program name
// of lower-case letters, digits and hyphens that starts with a letter, and WithHelpTopics only topic names of
// that form. An introduction must be one line of printable, valid UTF-8 text that does not start with "." or
// "'"; anything else is an error.
//
// # Options
//
//   - intro: the sentence that opens the topics section. Default: an empty intro opens the section without
//     one.
package manpage
