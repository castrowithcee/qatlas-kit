package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The limits of a TextView. The application chooses them in TextViewSpec; the zero value takes the default
// and anything above the maximum is refused.
const (
	TextViewDefaultLines     = 10000
	TextViewMaxLines         = 1000000
	TextViewDefaultLineRunes = 4096
	TextViewMaxLineRunes     = 65536
)

const (
	textTabCells = 4
	// textMaskCells is how many mask glyphs stand for a masked run, whatever its length.
	textMaskCells = 6
)

// TextRole is the role of a section of text. The roles are neutral: a text view draws each in the syntax
// colour of the palette and knows nothing of the language of the text. A value outside the list is drawn as
// TextRolePlain.
type TextRole uint8

const (
	TextRolePlain TextRole = iota
	TextRoleKey
	TextRoleString
	TextRoleNumber
	TextRoleComment
	TextRoleKeyword
)

// TextSpan is a section of one line of text, from byte Start up to, not including, byte End of the line as
// the application gave it, counted from the start of the line and without its line break. Role colours the
// section. Masked hides it: its characters never reach the screen, the search never finds text in it, and a
// run of masked characters shows as a few mask glyphs of fixed length, whatever its length.
//
// A span the view cannot place exactly is handled as follows, so a damaged section never shows more than it
// should. A span is clipped to its line; one that is empty, reversed or outside the line covers nothing. A
// boundary inside a UTF-8 sequence moves outwards to the whole character, so a masked section only ever
// grows. Where spans overlap, a character is masked if any span covering it is masked, and takes the role of
// the span that starts first, or of the one listed first if they start together.
type TextSpan struct {
	Start, End int
	Role       TextRole
	Masked     bool
}

// TextViewKeys are the actions of the key table a text view reads. TextViewActions defines the two that are
// specific to it; wrap and the two match actions are those of LogViewActions or the application's own, and
// scrolling, search, enter and back are the application's existing actions.
type TextViewKeys struct {
	// Up and Down scroll by a line, PageUp and PageDown by a page; First and Last, where given, jump to the
	// first and last page.
	Up, Down, PageUp, PageDown, First, Last ActionID
	// Search starts typing a search, Enter keeps it and Back cancels or clears it; without either, Back is left
	// to the application.
	Search, Enter, Back ActionID
	// NextMatch and PrevMatch jump to the next and previous line with a match, with wrap-around.
	NextMatch, PrevMatch ActionID
	// Wrap and Numbers switch the wrapping of long lines and the column of line numbers.
	Wrap, Numbers ActionID
	// Reveal asks to uncover the masked sections while they are hidden and hides them again while they are
	// shown. It is optional; without it the view has no such button and the application calls SetRevealed.
	Reveal ActionID
}

// The IDs of the actions TextViewActions defines.
const (
	ActionTextNumbers ActionID = "text.numbers"
	ActionTextReveal  ActionID = "text.reveal"
)

// TextViewActions are the actions only a text view needs, with their default keys, for an application to add
// to its table; TextViewKeys then names them.
func TextViewActions() []Action {
	return []Action{
		{ID: ActionTextNumbers, Level: LevelLetter, DefaultKeys: []string{"#"}, Help: TextViewNumbers},
		{ID: ActionTextReveal, Level: LevelLetter, DefaultKeys: []string{"V"}, Help: TextViewReveal},
	}
}

// TextViewSpec is what a text view keeps and the keys it reads.
type TextViewSpec struct {
	Keys TextViewKeys
	// MaxLines is the most lines the view keeps and MaxLineRunes the most characters of a line, counted in the
	// text as given. Zero takes TextViewDefaultLines and TextViewDefaultLineRunes. Lines past the first are
	// dropped and longer lines are cut with a visible mark, and the status row says so.
	MaxLines, MaxLineRunes int
	// Wrap and Numbers are the start state of the two switches.
	Wrap, Numbers bool
}

type textRun struct {
	end    int // byte offset of the end; a run starts where the one before it ends
	role   TextRole
	masked bool
}

type textViewLine struct {
	text string    // as given, cut at a character boundary
	runs []textRun // cover text from the start; nil is plain and unmasked
	cut  bool
}

// textCell is one drawn character: a character of the text, a part of its escaped form, or a mask glyph.
type textCell struct {
	r      rune
	role   TextRole
	hidden bool // a mask glyph
	masked bool // part of a masked section, hidden or revealed
}

// TextView shows a text read-only: line numbers, scrolling, wrapping, search with highlighted matches and a
// status row with the position. The application owns where the text comes from.
//
// Control characters and escape sequences are always escaped, and the view cannot be told otherwise: the
// text is foreign content. A tab is drawn as spaces. Sections with a role are drawn in the syntax colours of
// the palette; they take no attribute, so without colour the text is plain. Masked sections stay hidden
// until the application reveals them, in every drawing, wrapped or clipped, with or without colour, and
// while they are hidden the search finds nothing in them.
type TextView struct {
	spec TextViewSpec
	keys *Keymap
	text Translator

	lines []textViewLine
	total int // lines the text had before the limit
	short int // lines cut at the limit of their length

	top int // first shown line

	query     textLine
	needle    string
	searching bool
	hits      []int
	cur       int
	hasCur    bool

	wrap, numbers bool
	revealed      bool
	askedReveal   bool
	hasMasked     bool

	// What the last drawing found, for the keys and the wheel, and for a selection with the mouse.
	width, body, page int
	layout            selLayout
	maskGlyph         string

	sel textRange
}

// NewTextView checks spec and returns an empty text view. It rejects a negative or too large limit, a
// missing key, a key that names no action of keys and one that two entries share.
func NewTextView(spec TextViewSpec, keys *Keymap, text Translator) (*TextView, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the text view needs a key table and a translator")
	}
	var errs []error
	switch {
	case spec.MaxLines < 0:
		errs = append(errs, fmt.Errorf("text view: the limit is %d lines; it cannot be negative", spec.MaxLines))
	case spec.MaxLines > TextViewMaxLines:
		errs = append(errs, fmt.Errorf("text view: the limit is %d lines; the most is %d", spec.MaxLines, TextViewMaxLines))
	}
	switch {
	case spec.MaxLineRunes < 0:
		errs = append(errs, fmt.Errorf("text view: the limit is %d characters a line; it cannot be negative", spec.MaxLineRunes))
	case spec.MaxLineRunes > TextViewMaxLineRunes:
		errs = append(errs, fmt.Errorf("text view: the limit is %d characters a line; the most is %d", spec.MaxLineRunes, TextViewMaxLineRunes))
	}
	k := spec.Keys
	errs = append(errs, checkKeys("text view", keys, []namedKey{
		{"up", k.Up, true}, {"down", k.Down, true}, {"page up", k.PageUp, true}, {"page down", k.PageDown, true},
		{"first", k.First, false}, {"last", k.Last, false}, {"search", k.Search, true}, {"enter", k.Enter, true},
		{"back", k.Back, true}, {"next match", k.NextMatch, true}, {"previous match", k.PrevMatch, true},
		{"wrap", k.Wrap, true}, {"numbers", k.Numbers, true}, {"reveal", k.Reveal, false},
	})...)
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if spec.MaxLines == 0 {
		spec.MaxLines = TextViewDefaultLines
	}
	if spec.MaxLineRunes == 0 {
		spec.MaxLineRunes = TextViewDefaultLineRunes
	}
	return &TextView{spec: spec, keys: keys, text: text, wrap: spec.Wrap, numbers: spec.Numbers,
		width: 80, body: 23, page: 22}, nil
}

// SetText replaces the text. A line break is "\n" or "\r\n", and one at the very end of the text ends its
// last line instead of adding an empty one. spans[i] are the sections of line i, as TextSpan describes; a
// list shorter than the lines leaves the rest plain, and entries past the last line are ignored. The view
// starts at the top, hides every masked section again and keeps the search text.
func (v *TextView) SetText(text string, spans [][]TextSpan) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	v.lines, v.total, v.short = nil, 0, 0
	v.top, v.hasCur, v.revealed, v.askedReveal, v.hasMasked = 0, false, false, false, false
	v.sel = textRange{}
	if text != "" {
		v.total = strings.Count(text, "\n") + 1
		for i := 0; i < v.total && len(v.lines) < v.spec.MaxLines; i++ {
			var line string
			line, text, _ = strings.Cut(text, "\n")
			ln := textViewLine{text: line}
			if cut, ok := cutRunes(line, v.spec.MaxLineRunes); ok {
				ln.text, ln.cut = cut, true
				v.short++
			}
			if i < len(spans) {
				ln.runs = normalizeSpans(ln.text, spans[i])
				for _, r := range ln.runs {
					v.hasMasked = v.hasMasked || r.masked
				}
			}
			v.lines = append(v.lines, ln)
		}
	}
	v.queryChanged()
}

// normalizeSpans turns the spans of a line into runs that cover it from the start, one per change of role
// or mask, on character boundaries; it returns nil where nothing is set.
func normalizeSpans(line string, spans []TextSpan) []textRun {
	n := len(line)
	var masked []int // change of the number of masked spans at a byte
	type section struct {
		start, end int
		role       TextRole
	}
	var roles []section
	for _, sp := range spans {
		start, end := max(sp.Start, 0), min(sp.End, n)
		if start >= end {
			continue
		}
		if sp.Masked {
			if masked == nil {
				masked = make([]int, n+1)
			}
			masked[start]++
			masked[end]--
		}
		if sp.Role != TextRolePlain && sp.Role <= TextRoleKeyword {
			roles = append(roles, section{start, end, sp.Role})
		}
	}
	if masked == nil && roles == nil {
		return nil
	}
	slices.SortStableFunc(roles, func(a, b section) int { return a.start - b.start })
	// A section keeps only what the sections that start before it leave.
	var placed []section
	for _, s := range roles {
		if len(placed) > 0 {
			s.start = max(s.start, placed[len(placed)-1].end)
		}
		if s.start < s.end {
			placed = append(placed, s)
		}
	}
	isMasked := make([]bool, n)
	for b, count := 0, 0; b < n; b++ {
		if masked != nil {
			count += masked[b]
		}
		isMasked[b] = count > 0
	}
	var runs []textRun
	next := 0
	for i := 0; i < n; {
		_, size := utf8.DecodeRuneInString(line[i:])
		m := slices.Contains(isMasked[i:i+size], true)
		for next < len(placed) && placed[next].end <= i {
			next++
		}
		role := TextRolePlain
		if next < len(placed) && placed[next].start <= i {
			role = placed[next].role
		}
		if k := len(runs) - 1; k >= 0 && runs[k].role == role && runs[k].masked == m {
			runs[k].end = i + size
		} else {
			runs = append(runs, textRun{end: i + size, role: role, masked: m})
		}
		i += size
	}
	if len(runs) == 1 && runs[0].role == TextRolePlain && !runs[0].masked {
		return nil
	}
	return runs
}

// cellsOf are the characters ln draws. A masked run is drawn as textMaskCells mask glyphs while the sections
// are hidden. The text of a line is escaped here, character by character, so that no boundary of a span moves.
func (v *TextView) cellsOf(ln textViewLine) []textCell {
	out := make([]textCell, 0, len(ln.text))
	run := 0
	for i := 0; i < len(ln.text); {
		r, size := utf8.DecodeRuneInString(ln.text[i:])
		for run < len(ln.runs) && ln.runs[run].end <= i {
			run++
		}
		role, masked := TextRolePlain, false
		if run < len(ln.runs) {
			role, masked = ln.runs[run].role, ln.runs[run].masked
		}
		switch {
		case masked && !v.revealed:
			if len(out) == 0 || !out[len(out)-1].hidden {
				for range textMaskCells {
					out = append(out, textCell{hidden: true, masked: true})
				}
			}
		case r == '\t':
			for range textTabCells {
				out = append(out, textCell{r: ' ', role: role, masked: masked})
			}
		default:
			for _, e := range Escape(ln.text[i : i+size]) {
				out = append(out, textCell{r: e, role: role, masked: masked})
			}
		}
		i += size
	}
	return out
}

// hay is the text a search looks in: the folded characters of the cells, and for a mask glyph a character the
// search text cannot hold, so no match reaches into a masked section.
func hay(cells []textCell) []rune {
	out := make([]rune, len(cells))
	for i, c := range cells {
		if !c.hidden {
			out[i] = unicode.ToLower(c.r)
		}
	}
	return out
}

func (v *TextView) matches(ln textViewLine) bool {
	return v.needle != "" && slices.ContainsFunc(hitMask(hay(v.cellsOf(ln)), []rune(v.needle)), func(b bool) bool { return b })
}

// queryChanged finds the matches of the typed search text again, over the text as it is drawn now.
func (v *TextView) queryChanged() {
	v.needle = string(foldRunes(Escape(v.query.String())))
	v.hits, v.hasCur = v.hits[:0], false
	if v.needle == "" {
		return
	}
	for i, ln := range v.lines {
		if v.matches(ln) {
			v.hits = append(v.hits, i)
		}
	}
}

// Len is the number of lines held.
func (v *TextView) Len() int { return len(v.lines) }

// Truncated reports whether a limit cut the text: lines past MaxLines, or a line past MaxLineRunes.
func (v *TextView) Truncated() bool { return v.total > len(v.lines) || v.short > 0 }

// HasMasked reports whether any line holds a masked section.
func (v *TextView) HasMasked() bool { return v.hasMasked }

// Revealed reports whether the masked sections are shown.
func (v *TextView) Revealed() bool { return v.revealed }

// SetRevealed shows or hides the masked sections; the search is made again over what is drawn then. Showing
// them is the application's decision, after whatever check it requires.
func (v *TextView) SetRevealed(on bool) {
	if v.revealed == on {
		return
	}
	v.revealed, v.sel = on, textRange{}
	v.queryChanged()
}

// TakeRevealRequest returns true once after the user asked to reveal the masked sections, and then false
// until the next request. The view reveals nothing itself; the application decides and calls SetRevealed.
func (v *TextView) TakeRevealRequest() bool {
	asked := v.askedReveal
	v.askedReveal = false
	return asked
}

// Wrapping and LineNumbers report the state of the two switches.
func (v *TextView) Wrapping() bool    { return v.wrap }
func (v *TextView) LineNumbers() bool { return v.numbers }

// Query is the search text.
func (v *TextView) Query() string { return v.query.String() }

// Searching reports whether the search text is being typed.
func (v *TextView) Searching() bool { return v.searching }

// Input is the input focus the view reads keys in: text while a search is typed, navigation otherwise.
func (v *TextView) Input() InputFocus {
	if v.searching {
		return InputText
	}
	return InputNavigation
}

type textGeometry struct {
	mark, num, text int
}

// geometry is how a line is laid out in width: the mark column, the line numbers, which give way when the
// text would get too narrow, and the text.
func (v *TextView) geometry(width int) textGeometry {
	g := textGeometry{}
	if width >= 6 {
		g.mark = logGutter
	}
	if v.numbers {
		g.num = len(strconv.Itoa(max(len(v.lines), 1))) + 1
	}
	g.text = width - g.mark - g.num
	if g.text < logMinText {
		g.num = 0
		g.text = width - g.mark
	}
	g.text = max(g.text, 1)
	return g
}

func (v *TextView) mark() []textCell {
	var out []textCell
	for _, r := range " " + Escape(v.text.Lookup(TextLogCut)) {
		out = append(out, textCell{r: r})
	}
	return out
}

func runesOf(cells []textCell) []rune {
	out := make([]rune, len(cells))
	for i, c := range cells {
		out[i] = c.r
		if c.hidden {
			out[i] = ' '
		}
	}
	return out
}

// bodyCells are the cells a line draws: its text and, where it was cut, the mark.
func (v *TextView) bodyCells(ln textViewLine) (cells []textCell, text int) {
	cells = v.cellsOf(ln)
	text = len(cells)
	if ln.cut {
		cells = append(cells, v.mark()...)
	}
	return cells, text
}

func (v *TextView) rowsOf(ln textViewLine, g textGeometry) int {
	if !v.wrap {
		return 1
	}
	cells, _ := v.bodyCells(ln)
	return len(wrapRanges(runesOf(cells), g.text))
}

// maxTop is the index of the first line of the last page.
func (v *TextView) maxTop(g textGeometry) int {
	t, sum := len(v.lines), 0
	for i := len(v.lines) - 1; i >= 0; i-- {
		r := v.rowsOf(v.lines[i], g)
		if sum+r > v.body && t < len(v.lines) {
			break
		}
		sum += r
		t = i
	}
	return t
}

func (v *TextView) topIndex() int {
	return max(min(v.top, v.maxTop(v.geometry(v.width))), 0)
}

func (v *TextView) scroll(n int) {
	v.top = max(min(v.topIndex()+n, v.maxTop(v.geometry(v.width))), 0)
}

// Wheel scrolls by the notches of the wheel, WheelTextLines a notch.
func (v *TextView) Wheel(steps int) { v.scroll(steps * WheelTextLines) }

// goTo shows line idx, about in the middle of the page.
func (v *TextView) goTo(idx int) {
	g := v.geometry(v.width)
	above := max((v.body-v.rowsOf(v.lines[idx], g))/2, 0)
	t, acc := idx, 0
	for t > 0 {
		r := v.rowsOf(v.lines[t-1], g)
		if acc+r > above {
			break
		}
		acc += r
		t--
	}
	v.top = t
	v.cur, v.hasCur = idx, true
}

// stepMatch jumps to the next or previous line with a match, with wrap-around; from no current match it
// starts at the first shown line.
func (v *TextView) stepMatch(forward bool) {
	if len(v.hits) == 0 {
		return
	}
	var i int
	switch {
	case v.hasCur && forward:
		i, _ = slices.BinarySearch(v.hits, v.cur+1)
	case v.hasCur:
		i, _ = slices.BinarySearch(v.hits, v.cur)
		i--
	case forward:
		i, _ = slices.BinarySearch(v.hits, v.topIndex())
	default:
		i, _ = slices.BinarySearch(v.hits, v.topIndex())
		i--
	}
	v.goTo(v.hits[(i%len(v.hits)+len(v.hits))%len(v.hits)])
}

// Update handles a key. handled is false for a key the view leaves to the application, such as back with
// no search to cancel.
func (v *TextView) Update(msg tea.KeyPressMsg) (handled bool) {
	focus := v.Input()
	k := v.spec.Keys
	for _, id := range []ActionID{k.Back, k.Enter, k.Search, k.Up, k.Down, k.PageUp, k.PageDown, k.First, k.Last,
		k.NextMatch, k.PrevMatch, k.Wrap, k.Numbers, k.Reveal} {
		if id != "" && v.keys.Is(msg, id, focus) {
			return v.Act(id)
		}
	}
	if focus == InputText {
		handled, changed := v.query.edit(msg, false)
		if changed {
			v.queryChanged()
		}
		return handled
	}
	return false
}

// Paste puts pasted text into the search text at its cursor, cleaned as a text line cleans it, and reports
// whether the view took it. Nothing is taken unless a search is being typed.
func (v *TextView) Paste(text string) bool {
	if !v.searching || !v.query.insert(text) {
		return false
	}
	v.queryChanged()
	return true
}

// Act runs one of the text view keys as its key does, for a click or a command that stands for the key.
func (v *TextView) Act(id ActionID) (handled bool) {
	k := v.spec.Keys
	if id == "" {
		return false
	}
	switch id {
	case k.Back:
		if !v.searching && v.needle == "" {
			return false
		}
		v.searching = false
		v.query.clear()
		v.queryChanged()
	case k.Enter:
		if !v.searching {
			return false
		}
		v.searching = false
		v.stepMatch(true)
	case k.Search:
		v.searching = true
		v.query.move(len(v.query.runes))
	case k.Up:
		v.scroll(-1)
	case k.Down:
		v.scroll(1)
	case k.PageUp:
		v.scroll(-max(v.page-1, 1))
	case k.PageDown:
		v.scroll(max(v.page-1, 1))
	case k.First:
		v.top = 0
	case k.Last:
		v.top = v.maxTop(v.geometry(v.width))
	case k.NextMatch:
		v.stepMatch(true)
	case k.PrevMatch:
		v.stepMatch(false)
	case k.Wrap:
		v.wrap = !v.wrap
	case k.Numbers:
		v.numbers = !v.numbers
	case k.Reveal:
		switch {
		case v.revealed:
			v.SetRevealed(false)
		case v.hasMasked:
			v.askedReveal = true
		default:
			return false
		}
	default:
		return false
	}
	return true
}

// Click handles a click on a target the view drew, as the MouseMap of its targets reports it: a button of the
// status row does what its key does.
func (v *TextView) Click(r MouseResult) (handled bool) { return r.Handled && v.Act(r.Action) }

// Hints are the keys of the view for a key line, for the input focus Input names.
func (v *TextView) Hints() []Hint {
	k := v.spec.Keys
	if v.searching {
		return []Hint{{Action: k.Enter, Label: TextKeyEnter}, {Action: k.Back, Label: TextKeyBack, Kind: HintAlways}}
	}
	hints := []Hint{{Action: k.Search, Label: TextKeySearch}}
	if v.needle != "" {
		hints = append(hints, Hint{Action: k.NextMatch, Label: TextLogNextMatch, Priority: 1},
			Hint{Action: k.PrevMatch, Label: TextLogPrevMatch, Priority: 1})
	}
	if k.Reveal != "" && v.hasMasked {
		label := TextViewReveal
		if v.revealed {
			label = TextViewHide
		}
		hints = append(hints, Hint{Action: k.Reveal, Label: label, Priority: 1})
	}
	hints = append(hints, Hint{Action: k.Wrap, Label: TextLogWrap, Priority: 2},
		Hint{Action: k.Numbers, Label: TextViewNumbers, Priority: 2},
		Hint{Action: k.Up, Label: TextHelpScroll, Kind: HintNavigation},
		Hint{Action: k.Down, Label: TextHelpScroll, Kind: HintNavigation})
	if v.needle != "" {
		hints = append(hints, Hint{Action: k.Back, Label: TextKeyBack, Priority: 1})
	}
	return hints
}

// View draws the text view with styles in width by height cells: the lines that fit, from the first shown
// line on, and in the last row the buttons, the notice of a limit and the position. It returns a target for
// the whole area and one per button of the status row, counted from the top left of the view.
func (v *TextView) View(styles Styles, width, height int) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if width == 0 || height == 0 {
		return finishPage(nil, nil, width, height)
	}
	v.width, v.body = width, height
	if height >= 2 {
		v.body = height - 1
	}
	v.layout, v.maskGlyph = v.layout[:0], styles.Glyph(GlyphMask)
	g := v.geometry(width)
	var lines []string
	at := v.topIndex()
	v.top = at
	if len(v.lines) == 0 {
		lines = append(lines, styles.Render(StyleHint, styles.Truncate(Escape(v.text.Lookup(TextViewEmpty)), width)))
	}
	shown := 0
	for i := at; i < len(v.lines) && len(lines) < v.body; i++ {
		lines = append(lines, v.rows(v.lines[i], i, g, styles)...)
		shown++
	}
	v.page = max(shown, 1)
	if len(lines) > v.body {
		lines = lines[:v.body]
		v.layout = v.layout[:v.body]
	}
	targets := []Target{{Rect: Rect{Width: width, Height: height}, Zone: ZoneRows},
		{Rect: Rect{Width: width, Height: v.body}, Zone: ZoneRows, selection: v}}
	if height >= 2 {
		for len(lines) < v.body {
			lines = append(lines, "")
		}
		status, st := v.status(styles, width, at, shown)
		targets = append(targets, OffsetTargets(st, 0, v.body)...)
		lines = append(lines, status)
	}
	return finishPage(lines, targets, width, height)
}

// rows draws line idx in the rows it needs.
func (v *TextView) rows(ln textViewLine, idx int, g textGeometry, styles Styles) []string {
	cells, text := v.bodyCells(ln)
	var hit []bool
	if v.needle != "" {
		hit = hitMask(hay(cells[:text]), []rune(v.needle))
	}
	isCur := v.hasCur && idx == v.cur
	lead := strings.Repeat(" ", g.mark)
	switch {
	case g.mark == 0:
		lead = ""
	case isCur:
		lead = styles.Render(StyleSelectionMark, styles.SelectionMark()) + " "
	case slices.Contains(hit, true):
		lead = styles.Render(StyleActive, styles.Glyph(GlyphStatus)) + " "
	}
	number := ""
	if g.num > 0 {
		number = styles.Render(StyleHint, fmt.Sprintf("%*d", g.num-1, idx+1)) + " "
		lead += number
	}
	indent := strings.Repeat(" ", g.mark+g.num)
	sa, sb := v.sel.cells(uint64(idx), text)
	widths := func(from, to int) []int {
		w := make([]int, 0, to-from)
		for _, c := range cells[from:to] {
			w = append(w, runeCells(cellRune(c)))
		}
		return w
	}
	if !v.wrap {
		row, end := v.clipped(cells, text, hit, isCur, sa, sb, g.text, styles)
		v.layout = append(v.layout, selRow{line: uint64(idx), x: g.mark + g.num, widths: widths(0, end), end: true, lineLen: text})
		return []string{lead + row}
	}
	var out []string
	ranges := wrapRanges(runesOf(cells), g.text)
	for i, rg := range ranges {
		prefix := indent
		if i == 0 {
			prefix = lead
		}
		out = append(out, prefix+textSegments(cells, rg[0], rg[1], text, hit, isCur, sa, sb, styles))
		from, to := min(rg[0], text), min(rg[1], text)
		v.layout = append(v.layout, selRow{line: uint64(idx), x: g.mark + g.num, from: from, widths: widths(from, to),
			end: i == len(ranges)-1 || rg[1] >= text, lineLen: text})
	}
	return out
}

// clipped is the text of a line that does not wrap, cut to room cells with the ellipsis glyph, and how many
// characters of it stand; the mark of a cut line stays at the end.
func (v *TextView) clipped(cells []textCell, text int, hit []bool, isCur bool, sa, sb, room int, styles Styles) (string, int) {
	mark := cells[text:]
	room -= cellsWidth(mark)
	tail := textSegments(mark, 0, len(mark), 0, nil, false, 0, 0, styles)
	body := cells[:text]
	if cellsWidth(body) <= room {
		return textSegments(body, 0, len(body), len(body), hit, isCur, sa, sb, styles) + tail, len(body)
	}
	ellipsis := styles.Glyph(GlyphEllipsis)
	room = max(room-lipgloss.Width(ellipsis), 0)
	end, used := 0, 0
	for end < len(body) && used+runeCells(cellRune(body[end])) <= room {
		used += runeCells(cellRune(body[end]))
		end++
	}
	return textSegments(body, 0, end, len(body), hit, isCur, sa, sb, styles) + styles.Render(StyleHint, ellipsis) + tail, end
}

// cellRune is the character a cell takes room for: a mask glyph takes one cell.
func cellRune(c textCell) rune {
	if c.hidden {
		return ' '
	}
	return c.r
}

func cellsWidth(cells []textCell) int {
	w := 0
	for _, c := range cells {
		w += runeCells(cellRune(c))
	}
	return w
}

// textSegments draws cells[from:to]: text in its role, matches, the selection [sa, sb), mask glyphs and,
// from textLen on, the mark of a cut line. A mask glyph is drawn from the styles' glyph, never from the text.
func textSegments(cells []textCell, from, to, textLen int, hit []bool, isCur bool, sa, sb int, styles Styles) string {
	style := func(i int) TextStyle {
		c := cells[i]
		switch {
		case i >= textLen:
			return StyleWarn
		case i >= sa && i < sb:
			return StyleActiveRow
		case c.hidden:
			return StyleHint
		case i < len(hit) && hit[i]:
			if isCur {
				return StyleActiveRow
			}
			return StyleActive
		}
		switch c.role {
		case TextRoleKey:
			return StyleSyntaxKey
		case TextRoleString:
			return StyleSyntaxString
		case TextRoleNumber:
			return StyleSyntaxNumber
		case TextRoleComment:
			return StyleSyntaxComment
		case TextRoleKeyword:
			return StyleSyntaxKeyword
		}
		return StyleText
	}
	glyph := styles.Glyph(GlyphMask)
	var b strings.Builder
	for i := from; i < to; {
		j := i
		var part strings.Builder
		for j < to && style(j) == style(i) {
			if cells[j].hidden {
				part.WriteString(glyph)
			} else {
				part.WriteRune(cells[j].r)
			}
			j++
		}
		b.WriteString(styles.Render(style(i), part.String()))
		i = j
	}
	return b.String()
}

// textNotice names the limits that cut the text, in a long and a short form.
func (v *TextView) textNotice() (long, short string) {
	var parts []string
	if v.total > len(v.lines) {
		parts = append(parts, v.text.Lookup(TextViewLimitLines, len(v.lines), v.total))
	}
	if v.short > 0 {
		parts = append(parts, v.text.Lookup(TextViewLimitLong, v.short))
	}
	if len(parts) == 0 {
		return "", ""
	}
	return strings.Join(parts, ", "), v.text.Lookup(TextLogCut)
}

// matchInfo says how many lines have a match, or which one is current.
func (v *TextView) matchInfo() string {
	switch n := len(v.hits); {
	case n > 0 && v.hasCur:
		i, _ := slices.BinarySearch(v.hits, v.cur)
		return v.text.Lookup(TextLogMatch, i+1, n)
	case n == 1:
		return v.text.Lookup(TextLogOneMatch)
	case n > 0:
		return v.text.Lookup(TextLogMatches, n)
	}
	return v.text.Lookup(TextLogNoMatches)
}

// buttons are the buttons of the status row, in the order they stand. A switch shows its state as the word
// on or off, so the state reads without colour; a switch that is on is drawn as a primary button.
func (v *TextView) buttons(styles Styles) []logButton {
	k := v.spec.Keys
	key := func(a ActionID) string { return v.keys.Label(a) }
	join := func(parts ...string) string {
		var out []string
		for _, p := range parts {
			if p != "" {
				out = append(out, p)
			}
		}
		return strings.Join(out, " ")
	}
	state := func(on bool) string {
		if on {
			return v.text.Lookup(TextLogOn)
		}
		return v.text.Lookup(TextLogOff)
	}
	kind := func(on bool) ButtonKind {
		if on {
			return ButtonPrimary
		}
		return ButtonNormal
	}
	search := logButton{action: k.Search, rank: 0, kind: ButtonNormal,
		full: join(key(k.Search), v.text.Lookup(TextKeySearch)), short: key(k.Search)}
	if v.needle != "" {
		q := logQueryTail(v.query.String(), styles.Glyph(GlyphEllipsis))
		search.full = join(key(k.Search), q, v.matchInfo())
		search.short = search.full
	}
	btns := []logButton{search}
	if k.Reveal != "" && v.hasMasked {
		word := v.text.Lookup(TextViewReveal)
		if v.revealed {
			word = v.text.Lookup(TextViewHide)
		}
		btns = append(btns, logButton{action: k.Reveal, rank: 1, kind: kind(v.revealed),
			full: join(key(k.Reveal), word), short: key(k.Reveal)})
	}
	if v.needle != "" && len(v.hits) > 0 {
		btns = append(btns,
			logButton{action: k.PrevMatch, rank: 3, full: join(key(k.PrevMatch), v.text.Lookup(TextLogPrevMatch)), short: key(k.PrevMatch)},
			logButton{action: k.NextMatch, rank: 2, full: join(key(k.NextMatch), v.text.Lookup(TextLogNextMatch)), short: key(k.NextMatch)})
	}
	return append(btns,
		logButton{action: k.Wrap, rank: 4, kind: kind(v.wrap),
			full: join(key(k.Wrap), v.text.Lookup(TextLogWrap), state(v.wrap)), short: join(key(k.Wrap), state(v.wrap))},
		logButton{action: k.Numbers, rank: 5, kind: kind(v.numbers),
			full: join(key(k.Numbers), v.text.Lookup(TextViewNumbers), state(v.numbers)), short: join(key(k.Numbers), state(v.numbers))})
}

// status is the last row: buttons from the left, and at the right the position with the notice of a limit.
// Where the width runs short the labels go first, then buttons by rank, and the position gives way last, to
// a shorter form; the short form of the notice is a mark, so a cut text never reads as whole.
func (v *TextView) status(styles Styles, width, at, shown int) (string, []Target) {
	var long, short string
	if n := len(v.lines); n > 0 && shown > 0 {
		long = v.text.Lookup(TextScrollPosition, at+1, at+shown, n)
		short = v.text.Lookup(TextLogPositionShort, at+1, at+shown, n)
	}
	if nl, ns := v.textNotice(); nl != "" {
		long, short = strings.TrimSpace(long+" "+nl), strings.TrimSpace(short+" "+ns)
	}
	var items []statusItem
	var pos string
	if v.searching {
		items, pos = searchStatus(styles, width, v.keys.Label(v.spec.Keys.Search), v.spec.Keys.Search, &v.query, v.matchInfo(), long, short)
	} else {
		items, pos = fitButtons(styles, width, v.buttons(styles), long, short)
	}
	return statusLine(styles, width, items, pos)
}

func (v *TextView) selectPos(x, y int) (textPos, bool) { return v.layout.pos(x, y) }
func (v *TextView) setSelection(sel textRange)         { v.sel = sel }

// selectionText is the selected text as it is drawn, a line break at the end of each line of the text: a tab
// is its spaces, a control character its escape, and a masked section the mask of fixed length even while it
// is revealed, so its content and its length never leave the view.
func (v *TextView) selectionText(sel textRange) string {
	var b strings.Builder
	for line := sel.from.line; line <= sel.to.line && line < uint64(len(v.lines)); line++ {
		if line > sel.from.line {
			b.WriteByte('\n')
		}
		cells := v.cellsOf(v.lines[line])
		from, to := sel.cells(line, len(cells))
		for i := from; i < to; i++ {
			switch c := cells[i]; {
			case !c.masked:
				b.WriteRune(c.r)
			case i == from || !cells[i-1].masked:
				b.WriteString(strings.Repeat(v.maskGlyph, textMaskCells))
			}
		}
	}
	return b.String()
}
