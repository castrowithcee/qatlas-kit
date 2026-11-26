// Package helptopics shares plain-text help topics and terms between a CLI, TUI, and manpage, so all of them
// show the same text. The application supplies the topics and terms; Wrap fits a text to the width where it
// is shown. A term explains one technical word under a stable ID in one to three sentences.
//
// # Always enforced
//
// CheckTerms holds a list of terms to the term contract and reports every breach at once: an ID that is
// empty, holds a blank or a control character, or is listed twice, a title that is empty or more than one
// line, and a text that is empty, holds a control character or has more than three sentences. Stability of
// the IDs across releases is the application's contract: its own test fixes the list.
//
// # Options
//
// The package has no options.
package helptopics
