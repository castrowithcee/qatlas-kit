package tui

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-kit/helppages"
)

// searchRef starts the target item of a hit; the rest is its index into helpSearch.hits.
const searchRef = "s"

// The ranks of a hit, best first.
const (
	hitTitle = iota
	hitKeyword
	hitText
)

// helpSearch is the open search: the typed text, its hits and the one marked.
type helpSearch struct {
	query  textLine
	needle []rune
	hits   []helpHit
	cursor int
	// top is the first hit drawn and shown the number of hits that fit, as the last drawing found them.
	top, shown int
}

// helpHit is a page that matches: its index in Help.pages, its rank and the line the snippet is cut from, with
// the match in it as rune offsets (from == to: none to mark). inText tells that the text of the page holds the
// match, so the page opens at it.
type helpHit struct {
	page, rank int
	inText     bool
	line       []rune
	from, to   int
}

// foldIndex is the offset of the first place in text where needle, already folded, stands without regard to
// case; -1 where it does not.
func foldIndex(text, needle []rune) int {
	for i := 0; i+len(needle) <= len(text); i++ {
		j := 0
		for j < len(needle) && unicode.ToLower(text[i+j]) == needle[j] {
			j++
		}
		if j == len(needle) {
			return i
		}
	}
	return -1
}

// Input is the input focus the help reads keys in: text while the search takes typed characters, navigation
// otherwise.
func (h *Help) Input() InputFocus {
	if h.search != nil {
		return InputText
	}
	return InputNavigation
}

// Paste puts pasted text into the search at its cursor, cleaned as a text line cleans it. Nothing is taken
// unless the search is open.
func (h *Help) Paste(text string) bool {
	if h.search == nil || !h.search.query.insert(text) {
		return false
	}
	h.research()
	return true
}

func (h *Help) openSearch() { h.search = &helpSearch{} }

// searchKeys are the keys that act while the search takes text: the others type or move the cursor.
func (h *Help) searchKeys() []ActionID {
	k := h.spec.Keys
	var ids []ActionID
	for _, id := range []ActionID{k.Back, k.Help, k.Enter, k.Up, k.Down, k.PageUp, k.PageDown} {
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// research finds the pages of the query again, best rank first and in the order of the pages within a rank.
// It looks in the title, the keywords and the text of the page in the language of the help, the text without
// marks and with the keys as they are drawn.
func (h *Help) research() {
	s := h.search
	s.hits, s.cursor, s.top = nil, 0, 0
	s.needle = foldRunes(strings.Join(strings.Fields(s.query.String()), " "))
	if len(s.needle) == 0 {
		return
	}
	for i, p := range h.pages {
		c := p.page.In(h.spec.Lang)
		hit := helpHit{page: i, rank: -1}
		if foldIndex([]rune(c.Title), s.needle) >= 0 {
			hit.rank = hitTitle
		}
		keywords := []rune(strings.Join(c.Keywords, ", "))
		inKeywords := foldIndex(keywords, s.needle) >= 0
		if inKeywords && hit.rank < 0 {
			hit.rank = hitKeyword
		}
		var first []rune
		for _, line := range strings.Split(helppages.PlainText(c.Text), "\n") {
			line, _ = resolvePlaceholders(line, h.keys)
			r := []rune(line)
			if first == nil {
				first = r
			}
			if at := foldIndex(r, s.needle); at >= 0 {
				hit.inText, hit.line, hit.from, hit.to = true, r, at, at+len(s.needle)
				if hit.rank < 0 {
					hit.rank = hitText
				}
				break
			}
		}
		switch {
		case hit.rank < 0:
			continue
		case hit.inText:
		case inKeywords:
			at := foldIndex(keywords, s.needle)
			hit.line, hit.from, hit.to = keywords, at, at+len(s.needle)
		default:
			hit.line = first
		}
		s.hits = append(s.hits, hit)
	}
	slices.SortStableFunc(s.hits, func(a, b helpHit) int { return a.rank - b.rank })
}

// move marks the hit by steps from the one marked and stops at both ends.
func (s *helpSearch) move(by int) { s.cursor = WheelStep(s.cursor, len(s.hits), by) }

// searchAct runs a help key while the search is open: back closes the search, the help key the whole help,
// enter opens the hit marked, and up, down and the page keys move among the hits.
func (h *Help) searchAct(id ActionID) (ActionID, bool) {
	k, s := h.spec.Keys, h.search
	switch id {
	case k.Help:
		return id, true
	case k.Back:
		h.search = nil
	case k.Up:
		s.move(-1)
	case k.Down:
		s.move(1)
	case k.PageUp:
		s.move(-max(s.shown, 1))
	case k.PageDown:
		s.move(max(s.shown, 1))
	case k.Enter:
		h.openHit(s.cursor)
	default:
		return "", false
	}
	return "", true
}

// searchClick handles a click while the search is open: a hit opens its page, a key of the key line acts as
// that key.
func (h *Help) searchClick(r MouseResult) (ActionID, bool) {
	if ref, ok := strings.CutPrefix(r.Item, searchRef); ok {
		i, err := strconv.Atoi(ref)
		return "", err == nil && h.openHit(i)
	}
	if r.Action != "" && slices.Contains(h.navKeys(), r.Action) {
		return h.Act(r.Action)
	}
	return "", false
}

// openHit shows the page of a hit and closes the search. A hit in the text scrolls to the first place of the
// match when the page is drawn; any other opens from the top.
func (h *Help) openHit(i int) bool {
	s := h.search
	if i < 0 || i >= len(s.hits) {
		return false
	}
	hit, needle := s.hits[i], s.needle
	h.showPage(hit.page)
	if hit.inText {
		h.find, h.findRow = needle, -1
	}
	return true
}

// searchHints are the keys of the open search as hints of a key line.
func (h *Help) searchHints() []Hint {
	k := h.spec.Keys
	return []Hint{{Action: k.Enter, Label: TextKeyChoose}, {Action: k.Back, Label: TextKeyBack},
		{Action: k.Up, Label: TextKeyUp, Kind: HintNavigation}, {Action: k.Down, Label: TextKeyDown, Kind: HintNavigation},
		{Action: k.Help, Label: TextHelpClose, Kind: HintAlways}}
}

// mark draws a match: in the style of the active, and in brackets where the terminal draws no style.
func (s Styles) mark(text string) string {
	if s.profile == ProfileNone || s.profile > ProfileTrueColor {
		return "[" + text + "]"
	}
	return s.Render(StyleActive, text)
}

// placeSearch draws the search in width by at most room cells below lines: the field, then the hits two lines
// each with the one marked in view, or the sentence of no hit. Only the lines it needs are drawn.
func (h *Help) placeSearch(lines []string, targets []Target, width, room int, styles Styles) ([]string, []Target) {
	s := h.search
	field := s.query.view(width, styles, true, "")
	if len(s.query.runes) == 0 {
		hint := styles.Render(StyleHint, styles.Truncate(Escape(h.text.Lookup(TextSearchPrompt)), max(width-1, 0)))
		field = Pad(s.query.view(1, styles, true, "")+hint, width)
	}
	lines = append(lines, styles.Field(Pad(Clip(field, width), width)))
	room--
	switch {
	case len(s.needle) == 0 || room < 1:
	case len(s.hits) == 0:
		lines = append(lines, "  "+styles.Render(StyleHint, styles.Truncate(Escape(h.text.Lookup(TextHelpSearchNone, s.query.String())), max(width-2, 0))))
	default:
		s.shown = max(room/2, 1)
		s.top = max(min(s.top, s.cursor), s.cursor-s.shown+1)
		s.top = max(min(s.top, len(s.hits)-s.shown), 0)
		for i := s.top; i < min(len(s.hits), s.top+s.shown); i++ {
			first, second := h.hitLines(s.hits[i], i == s.cursor, width, styles)
			targets = append(targets, Target{Rect: Rect{Y: len(lines), Width: width, Height: 2}, Item: searchRef + strconv.Itoa(i)})
			lines = append(lines, first, second)
		}
	}
	return lines, targets
}

// hitLines are the two lines of a hit: the topic and the page, with the pointer on the one marked and the match
// marked in a title, and a snippet of the line the page matched in.
func (h *Help) hitLines(hit helpHit, marked bool, width int, styles Styles) (string, string) {
	p := h.pages[hit.page]
	pointer := "  "
	if marked {
		pointer = styles.Render(StyleActive, styles.Glyph(GlyphCollapsed)) + " "
	}
	title := []rune(p.page.In(h.spec.Lang).Title)
	body := styles.Render(StyleTitle, Escape(string(title)))
	if at := foldIndex(title, h.search.needle); hit.rank == hitTitle && at >= 0 {
		end := at + len(h.search.needle)
		body = styles.Render(StyleTitle, Escape(string(title[:at]))) + styles.mark(Escape(string(title[at:end]))) +
			styles.Render(StyleTitle, Escape(string(title[end:])))
	}
	topic := styles.Render(StyleHint, Escape(p.topic.Index.In(h.spec.Lang).Title)+" "+styles.Glyph(GlyphPathSeparator)+" ")
	first := styles.Truncate(pointer+topic+body, width)
	return Pad(first, width), "  " + h.snippet(hit, max(width-2, 0), styles)
}

// snippet is the line of a hit cut to width cells around its match: about a third of the room before it, with
// the cuts marked, and the match marked.
func (h *Help) snippet(hit helpHit, width int, styles Styles) string {
	r := hit.line
	if hit.from == hit.to {
		return styles.Render(StyleHint, styles.Truncate(Escape(string(r)), width))
	}
	match := styles.mark(styles.Truncate(Escape(string(r[hit.from:hit.to])), max(width-2, 1)))
	before, after := Escape(string(r[:hit.from])), Escape(string(r[hit.to:]))
	room := width - lipgloss.Width(match)
	if room <= 0 {
		return Clip(match, width)
	}
	keep := min(lipgloss.Width(before), max(room/3, room-lipgloss.Width(after)))
	before = styles.TruncateLeft(before, keep)
	after = styles.Truncate(after, room-lipgloss.Width(before))
	dim := func(s string) string {
		if s == "" {
			return ""
		}
		return styles.Render(StyleHint, s)
	}
	return dim(before) + match + dim(after)
}
