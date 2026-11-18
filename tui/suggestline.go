package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// suggestionsShown is how many suggestions stand under a text line at once.
const suggestionsShown = 5

// suggestLine is a text line with the suggestions its source gives for the text. Every change of the text asks
// the source anew and drops the mark. Without a source it is a plain text line.
type suggestLine struct {
	textLine
	source func(typed string) []Suggestion
	items  []Suggestion
	// mark is the marked suggestion where marked is set, top the first one shown.
	mark, top int
	marked    bool
}

func newSuggestLine(text string, source func(typed string) []Suggestion) suggestLine {
	s := suggestLine{textLine: newTextLine(text), source: source}
	s.refresh()
	return s
}

func (s *suggestLine) refresh() {
	s.items, s.mark, s.top, s.marked = nil, 0, 0, false
	if s.source != nil {
		s.items = slices.Clone(s.source(s.String()))
	}
}

// shown reports whether suggestions stand under the line.
func (s *suggestLine) shown() bool { return len(s.items) > 0 }

func (s *suggestLine) edit(msg tea.KeyPressMsg, masked bool) (handled, changed bool) {
	handled, changed = s.textLine.edit(msg, masked)
	if changed {
		s.refresh()
	}
	return handled, changed
}

func (s *suggestLine) insert(text string) bool {
	if !s.textLine.insert(text) {
		return false
	}
	s.refresh()
	return true
}

func (s *suggestLine) clear() {
	s.textLine.clear()
	s.refresh()
}

// move marks the suggestion step places on, with wrap-around; without a mark down starts at the first and up at
// the last. The window follows the mark.
func (s *suggestLine) move(step int) {
	n := len(s.items)
	if n == 0 {
		return
	}
	switch {
	case !s.marked && step > 0:
		s.markAt(0)
	case !s.marked:
		s.markAt(n - 1)
	default:
		s.markAt(((s.mark+step)%n + n) % n)
	}
}

// markAt marks suggestion i and moves the window as little as it takes to show it.
func (s *suggestLine) markAt(i int) {
	s.mark, s.marked = i, true
	if i < s.top {
		s.top = i
	}
	if i >= s.top+suggestionsShown {
		s.top = i - suggestionsShown + 1
	}
}

// window is the range of the suggestions shown.
func (s *suggestLine) window() (start, end int) {
	return s.top, min(len(s.items), s.top+suggestionsShown)
}

// take puts suggestion i into the line the way pasted text arrives, so its text never brings a control
// character, a direction mark or invalid UTF-8 into the value, and returns it.
func (s *suggestLine) take(i int) Suggestion {
	chosen := s.items[i]
	s.textLine.clear()
	s.textLine.insert(chosen.Text)
	s.refresh()
	return chosen
}

// complete takes the marked suggestion, the first without a mark.
func (s *suggestLine) complete() {
	s.take(s.mark)
}

// suggestionRefPrefix leads the target item of a suggestion row, followed by its index.
const suggestionRefPrefix = "\x00S"

func suggestionRef(i int) string { return suggestionRefPrefix + strconv.Itoa(i) }

// suggestionOf is the index of the suggestion a target item names, if it names one shown now.
func (s *suggestLine) suggestionOf(item string) (int, bool) {
	rest, ok := strings.CutPrefix(item, suggestionRefPrefix)
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(rest)
	start, end := s.window()
	return i, err == nil && i >= start && i < end
}

// rows draws the suggestions shown, one row of width cells each, escaped and shortened, the marked one
// selected, and returns a target per row, counted from the first row.
func (s *suggestLine) rows(width int, styles Styles) ([]string, []Target) {
	var lines []string
	var targets []Target
	start, end := s.window()
	for i := start; i < end; i++ {
		targets = append(targets, Target{Rect: Rect{Y: len(lines), Width: width, Height: 1}, Zone: ZoneForm, Item: suggestionRef(i)})
		lines = append(lines, styles.Row(Escape(s.items[i].Text), width, s.marked && i == s.mark))
	}
	return lines, targets
}
