package helptopics

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Term is the short explanation of one technical word, supplied by the application. ID is the stable key a
// screen or a manpage refers to; it never changes once shipped. Text is one to three sentences.
type Term struct {
	ID    string
	Title string
	Text  string
}

// LookupTerm returns the term with the given ID without changing the caller's order.
func LookupTerm(terms []Term, id string) (Term, bool) {
	for _, term := range terms {
		if term.ID == id {
			return term, true
		}
	}
	return Term{}, false
}

// maxTermSentences is the most sentences a term explains itself in.
const maxTermSentences = 3

// CheckTerms reports every term that breaks the contract of a term: an ID that is empty, holds a blank or a
// control character, or is listed twice, a title that is empty or more than one line, and a text that is
// empty, holds a control character or has more than three sentences. Stability of the IDs across releases is
// the application's contract: its own test fixes the list.
func CheckTerms(terms []Term) error {
	var errs []error
	seen := map[string]bool{}
	for i, term := range terms {
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("term %d (%q): "+format, append([]any{i, term.ID}, args...)...))
		}
		switch {
		case term.ID == "" || strings.IndexFunc(term.ID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
			fail("an ID is not empty and holds no blank or control character")
		case seen[term.ID]:
			fail("the ID is listed twice")
		}
		seen[term.ID] = true
		if strings.TrimSpace(term.Title) == "" || hasControl(term.Title) {
			fail("a title is one line of text")
		}
		switch {
		case strings.TrimSpace(term.Text) == "" || hasControl(term.Text):
			fail("a text is one paragraph without control characters")
		case sentences(term.Text) > maxTermSentences:
			fail("the text has %d sentences, at most %d explain a term", sentences(term.Text), maxTermSentences)
		}
	}
	return errors.Join(errs...)
}

func hasControl(text string) bool { return strings.IndexFunc(text, unicode.IsControl) >= 0 }

// sentences counts the sentences of text by the marks that end them before a blank or at the end.
func sentences(text string) int {
	text = strings.TrimSpace(text)
	n := 0
	for i, r := range text {
		if (r == '.' || r == '?' || r == '!') && (i == len(text)-1 || text[i+1] == ' ') {
			n++
		}
	}
	return max(n, 1)
}
