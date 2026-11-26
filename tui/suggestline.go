package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// suggestionsShown is how many suggestions stand under a text line at once.
const suggestionsShown = 5

// maxAnswered is how many suggestions of one answer a line takes; the rest is dropped.
const maxAnswered = 1000

// suggestLine is a text line with the suggestions its source gives for the text. Every change of the text asks
// the source anew and drops the mark. Without a source it is a plain text line.
//
// A line with ask gets its suggestions asynchronously: every change of the text calls ask, which returns the
// token of the new request, and the suggestions of the application arrive later through receive; an answer of
// another token is stale and dropped. Until then the line shows the priority entries that start with the text.
type suggestLine struct {
	textLine
	source func(typed string) []Suggestion
	ask    func(typed string) (token uint64)
	// priority are the entries the application always offers first, shown while their text starts with the
	// typed text, drawn with a mark of their own.
	priority []Suggestion
	// folder says which suggestion is a folder, which take ends with a separator; nil means none.
	folder func(Suggestion) bool
	// refPrefix leads the target items of the rows; suggestionRefPrefix where empty.
	refPrefix string
	token     uint64
	// notice is a line under the suggestions that stands for no suggestion, such as why a search ended.
	notice string
	got    []Suggestion
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
	s.got, s.notice = nil, ""
	switch {
	case s.ask != nil:
		s.token = s.ask(s.String())
		s.items = s.lead()
	case s.source != nil:
		s.items = slices.Clone(s.source(s.String()))
	}
}

// lead are the priority entries the typed text is the start of.
func (s *suggestLine) lead() []Suggestion {
	var out []Suggestion
	typed := s.String()
	for _, p := range s.priority {
		if strings.HasPrefix(p.Text, typed) {
			out = append(out, p)
		}
	}
	return out
}

// reset replaces the text, which asks the source anew.
func (s *suggestLine) reset(text string) {
	s.textLine = newTextLine(text)
	s.refresh()
}

// receive takes the answer to the request of token: the entries behind the priority entries, without one of
// them twice, and the notice. The mark stays, as the priority entries keep their place. It reports false and
// changes nothing for an answer of another token.
func (s *suggestLine) receive(token uint64, entries []Suggestion, notice string) bool {
	if s.ask == nil || token != s.token {
		return false
	}
	lead := s.lead()
	s.got = nil
	for _, e := range entries[:min(len(entries), maxAnswered)] {
		if !slices.ContainsFunc(lead, func(p Suggestion) bool { return p.Text == e.Text }) {
			s.got = append(s.got, e)
		}
	}
	s.items = append(slices.Clone(lead), s.got...)
	s.notice = notice
	return true
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

// step marks the suggestion by places on, with wrap-around; without a mark down starts at the first and up at
// the last. The window follows the mark.
func (s *suggestLine) step(step int) {
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
	s.textLine.insert(s.takenText(chosen))
	s.refresh()
	return chosen
}

// takenText is what taking c puts into the line: its text, and a separator behind a folder.
func (s *suggestLine) takenText(c Suggestion) string {
	if s.folder != nil && s.folder(c) && !strings.HasSuffix(c.Text, "/") && !strings.HasSuffix(c.Text, `\`) {
		return c.Text + "/"
	}
	return c.Text
}

// complete takes the marked suggestion, the first without a mark.
func (s *suggestLine) complete() {
	s.take(s.mark)
}

// suggestionRefPrefix leads the target item of a suggestion row, followed by its index.
const suggestionRefPrefix = "\x00S"

func (s *suggestLine) prefix() string {
	if s.refPrefix != "" {
		return s.refPrefix
	}
	return suggestionRefPrefix
}

// suggestionOf is the index of the suggestion a target item names, if it names one shown now.
func (s *suggestLine) suggestionOf(item string) (int, bool) {
	rest, ok := strings.CutPrefix(item, s.prefix())
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
		targets = append(targets, Target{Rect: Rect{Y: len(lines), Width: width, Height: 1}, Zone: ZoneForm, Item: s.prefix() + strconv.Itoa(i)})
		text := Escape(s.items[i].Text)
		if s.items[i].priority {
			text = styles.Glyph(GlyphStatus) + " " + text
		}
		lines = append(lines, styles.Row(text, width, s.marked && i == s.mark))
	}
	if s.notice != "" {
		lines = append(lines, styles.Render(StyleHint, "  "+styles.Truncate(Escape(s.notice), max(width-2, 0))))
	}
	return lines, targets
}
