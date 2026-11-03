package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The sizes of a LogView. The application chooses the buffer in LogViewSpec.Lines; the zero value takes
// LogViewDefaultLines and anything above LogViewMaxLines is refused.
const (
	LogViewDefaultLines = 5000
	LogViewMaxLines     = 100000
	// LogViewMaxLineRunes is the longest line a LogView keeps, counted in characters of the escaped text. A
	// longer line is cut and carries a visible mark.
	LogViewMaxLineRunes = 4096
)

const (
	logMaxSourceRunes = 256
	logMaxSourceCells = 16
	logMinText        = 12
	logStampLayout    = "15:04:05"
	logStampCells     = 8
	logGutter         = 2
	logSearchFieldMax = 30
)

// LogLine is one line, or a text of several, to append to a LogView.
type LogLine struct {
	// Text may hold line breaks, which split it into several lines that share Time and Source. Control
	// characters are escaped on arrival.
	Text string
	// Time is the time of the line; the zero Time is none.
	Time time.Time
	// Source names where the line came from, such as a stream or a component; empty is none.
	Source string
}

// LogViewKeys are the actions of the key table a log view reads. LogViewActions defines the five that are
// specific to it; the others are the movement and search actions the table already holds for lists.
type LogViewKeys struct {
	// Up and Down scroll by a line, PageUp and PageDown by a page; First, where the table has it, jumps to the
	// oldest line, and Last to the newest, which resumes following.
	Up, Down, PageUp, PageDown, First, Last ActionID
	// Search starts typing a search, Enter keeps it and Back cancels or clears it; without either, Back is left
	// to the application.
	Search, Enter, Back ActionID
	// Follow switches following the newest line on and off.
	Follow ActionID
	// NextMatch and PrevMatch jump to the next and previous line with a match, with wrap-around.
	NextMatch, PrevMatch ActionID
	// Wrap and Timestamps switch the wrapping of long lines and the time column.
	Wrap, Timestamps ActionID
}

// The IDs of the actions LogViewActions defines.
const (
	ActionLogFollow     ActionID = "log.follow"
	ActionLogNextMatch  ActionID = "log.next"
	ActionLogPrevMatch  ActionID = "log.prev"
	ActionLogWrap       ActionID = "log.wrap"
	ActionLogTimestamps ActionID = "log.time"
)

// LogViewActions are the actions only a log view needs, with their default keys, for an application to add
// to its table; LogViewKeys then names them. Scrolling, search, enter and back are the application's existing
// actions, so the table keeps one meaning per key.
func LogViewActions() []Action {
	return []Action{
		{ID: ActionLogFollow, Level: LevelLetter, DefaultKeys: []string{"F"}, Help: TextLogFollow},
		{ID: ActionLogNextMatch, Level: LevelLetter, DefaultKeys: []string{"]"}, Help: TextLogNextMatch},
		{ID: ActionLogPrevMatch, Level: LevelLetter, DefaultKeys: []string{"["}, Help: TextLogPrevMatch},
		{ID: ActionLogWrap, Level: LevelLetter, DefaultKeys: []string{"w"}, Help: TextLogWrap},
		{ID: ActionLogTimestamps, Level: LevelLetter, DefaultKeys: []string{"t"}, Help: TextLogTimestamps},
	}
}

// LogViewSpec is what a log view keeps and the keys it reads.
type LogViewSpec struct {
	// Lines is the size of the ring buffer; when it is full, the oldest lines go. Zero takes
	// LogViewDefaultLines.
	Lines int
	Keys  LogViewKeys
	// Wrap and Timestamps are the start state of the two switches.
	Wrap, Timestamps bool
}

type logLine struct {
	text   string // escaped, at most LogViewMaxLineRunes characters
	source string // escaped
	at     time.Time
	cut    bool
}

// LogView shows lines that arrive over time: a ring buffer, following the newest line, scrolling, search
// with highlighted matches, wrapping and a time column. It holds the lines as escaped text; the application
// owns where they come from.
type LogView struct {
	spec LogViewSpec
	keys *Keymap
	text Translator

	buf      []logLine
	start    int
	nextSeq  uint64 // the sequence number the next line gets; the line at index i of the buffer has first()+i
	srcWidth int

	following bool
	top       uint64 // first shown line while not following
	unseen    int    // lines that arrived since following stopped

	query     textLine
	needle    string
	searching bool
	hits      []uint64
	cur       uint64
	hasCur    bool

	wrap, stamps bool
	// What the last drawing found, for the keys and the wheel.
	width, body, page int
}

// NewLogView checks spec and returns an empty log view that follows. It rejects a negative or too large
// buffer, a missing key (First is optional), a key that names no action of keys and one that two entries share.
func NewLogView(spec LogViewSpec, keys *Keymap, text Translator) (*LogView, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the log view needs a key table and a translator")
	}
	var errs []error
	switch {
	case spec.Lines < 0:
		errs = append(errs, fmt.Errorf("log view: the buffer holds %d lines; it cannot be negative", spec.Lines))
	case spec.Lines > LogViewMaxLines:
		errs = append(errs, fmt.Errorf("log view: the buffer holds %d lines; the most is %d", spec.Lines, LogViewMaxLines))
	}
	k := spec.Keys
	errs = append(errs, checkKeys("log view", keys, []namedKey{
		{"up", k.Up, true}, {"down", k.Down, true}, {"page up", k.PageUp, true}, {"page down", k.PageDown, true},
		{"first", k.First, false}, {"last", k.Last, true}, {"search", k.Search, true}, {"enter", k.Enter, true},
		{"back", k.Back, true}, {"follow", k.Follow, true}, {"next match", k.NextMatch, true},
		{"previous match", k.PrevMatch, true}, {"wrap", k.Wrap, true}, {"timestamps", k.Timestamps, true},
	})...)
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if spec.Lines == 0 {
		spec.Lines = LogViewDefaultLines
	}
	return &LogView{spec: spec, keys: keys, text: text, following: true, wrap: spec.Wrap, stamps: spec.Timestamps,
		width: 80, body: 23, page: 22}, nil
}

// Len is the number of lines held.
func (l *LogView) Len() int { return len(l.buf) }

// Following reports whether the view shows the newest line as lines arrive.
func (l *LogView) Following() bool { return l.following }

// Query is the search text.
func (l *LogView) Query() string { return l.query.String() }

// Searching reports whether the search text is being typed.
func (l *LogView) Searching() bool { return l.searching }

// Input is the input focus the view reads keys in: text while a search is typed, navigation otherwise.
func (l *LogView) Input() InputFocus {
	if l.searching {
		return InputText
	}
	return InputNavigation
}

func (l *LogView) first() uint64 { return l.nextSeq - uint64(len(l.buf)) }

func (l *LogView) line(i int) logLine { return l.buf[(l.start+i)%len(l.buf)] }

// Append adds lines at the end. A text with line breaks becomes several lines; a line break is "\n" or
// "\r\n", and one at the very end of a text ends its last line instead of adding an empty one. Every line is
// escaped before it is stored and cut to LogViewMaxLineRunes characters with a visible mark. When the buffer
// is full, the oldest lines go; the position, the matches and the current match follow what remains.
func (l *LogView) Append(lines ...LogLine) {
	for _, in := range lines {
		text := strings.ReplaceAll(in.Text, "\r\n", "\n")
		text = strings.TrimSuffix(text, "\n")
		source := Escape(truncateRunes(in.Source, logMaxSourceRunes))
		if w := lipgloss.Width(source); w > l.srcWidth {
			l.srcWidth = min(w, logMaxSourceCells)
		}
		for _, part := range strings.Split(text, "\n") {
			l.push(logLine{text: part, source: source, at: in.Time})
		}
	}
	first := l.first()
	if i := sortSearch(l.hits, first); i > 0 {
		l.hits = append(l.hits[:0], l.hits[i:]...)
	}
	if l.hasCur && l.cur < first {
		l.hasCur = false
	}
	l.top = max(l.top, first)
}

func (l *LogView) push(ln logLine) {
	raw, cut := cutRunes(ln.text, LogViewMaxLineRunes)
	esc := Escape(raw)
	if e, c := cutRunes(esc, LogViewMaxLineRunes); c {
		esc, cut = e, true
	}
	ln.text, ln.cut = esc, cut
	if len(l.buf) < l.spec.Lines {
		l.buf = append(l.buf, ln)
	} else {
		l.buf[l.start] = ln
		l.start = (l.start + 1) % len(l.buf)
	}
	if l.matches(ln) {
		l.hits = append(l.hits, l.nextSeq)
	}
	l.nextSeq++
	if !l.following {
		l.unseen++
	}
}

func truncateRunes(s string, n int) string {
	s, _ = cutRunes(s, n)
	return s
}

// cutRunes keeps the first n characters of s and reports whether it dropped any.
func cutRunes(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i], true
		}
		count++
	}
	return s, false
}

func sortSearch(seqs []uint64, v uint64) int {
	i, _ := slices.BinarySearch(seqs, v)
	return i
}

func foldRunes(s string) []rune {
	r := []rune(s)
	for i := range r {
		r[i] = unicode.ToLower(r[i])
	}
	return r
}

func (l *LogView) matches(ln logLine) bool {
	return l.needle != "" && strings.Contains(string(foldRunes(ln.text)), l.needle)
}

// setQuery makes the typed text the search and finds its matches again.
func (l *LogView) queryChanged() {
	l.needle = string(foldRunes(Escape(l.query.String())))
	l.hits, l.hasCur = l.hits[:0], false
	if l.needle == "" {
		return
	}
	for i := range l.buf {
		if l.matches(l.line(i)) {
			l.hits = append(l.hits, l.first()+uint64(i))
		}
	}
}

// geometry is how a line is laid out in a width.
type logGeometry struct {
	gutter, stamp, source, text int
}

func (l *LogView) geometry(width int) logGeometry {
	g := logGeometry{}
	if width >= 6 {
		g.gutter = logGutter
	}
	if l.stamps {
		g.stamp = logStampCells + 1
	}
	if l.srcWidth > 0 {
		g.source = l.srcWidth + 1
	}
	g.text = width - g.gutter - g.stamp - g.source
	if g.text < logMinText {
		g.source = 0
		g.text = width - g.gutter - g.stamp
	}
	if g.text < logMinText {
		g.stamp = 0
		g.text = width - g.gutter
	}
	g.text = max(g.text, 1)
	return g
}

func (l *LogView) mark() string { return " " + Escape(l.text.Lookup(TextLogCut)) }

func runeCells(r rune) int {
	if r < 0x7f {
		return 1
	}
	return ansi.StringWidth(string(r))
}

func cells(runes []rune) int {
	w := 0
	for _, r := range runes {
		w += runeCells(r)
	}
	return w
}

// wrapRanges breaks runes into rows of at most width cells; there is always at least one row.
func wrapRanges(runes []rune, width int) [][2]int {
	var out [][2]int
	from, used := 0, 0
	for i, r := range runes {
		w := runeCells(r)
		if used+w > width && i > from {
			out = append(out, [2]int{from, i})
			from, used = i, 0
		}
		used += w
	}
	return append(out, [2]int{from, len(runes)})
}

// bodyRunes are the characters a line shows: its text and, where it was cut, the mark.
func (l *LogView) bodyRunes(ln logLine) []rune {
	r := []rune(ln.text)
	if ln.cut {
		r = append(r, []rune(l.mark())...)
	}
	return r
}

func (l *LogView) rowsOf(ln logLine, g logGeometry) int {
	if !l.wrap {
		return 1
	}
	return len(wrapRanges(l.bodyRunes(ln), g.text))
}

// followTop is the index of the first line of the page that ends with the newest line.
func (l *LogView) followTop(g logGeometry) int {
	t, sum := len(l.buf), 0
	for i := len(l.buf) - 1; i >= 0; i-- {
		r := l.rowsOf(l.line(i), g)
		if sum+r > l.body && t < len(l.buf) {
			break
		}
		sum += r
		t = i
	}
	return t
}

// topIndex is the index of the first shown line, as View draws it.
func (l *LogView) topIndex() int {
	maxTop := l.followTop(l.geometry(l.width))
	if l.following {
		return maxTop
	}
	return max(min(int(l.top-l.first()), maxTop), 0)
}

// pause stops following where the view stands.
func (l *LogView) pause() {
	if l.following {
		l.top = l.first() + uint64(l.topIndex())
		l.following, l.unseen = false, 0
	}
}

func (l *LogView) resume() { l.following, l.unseen = true, 0 }

// scroll moves the first shown line by n lines and stops at both ends. Up stops following; reaching the end
// resumes it only where resume says so.
func (l *LogView) scroll(n int, resume bool) {
	if n < 0 {
		l.pause()
	}
	if l.following || n == 0 {
		return
	}
	maxTop := l.followTop(l.geometry(l.width))
	at := max(min(l.topIndex()+n, maxTop), 0)
	l.top = l.first() + uint64(at)
	if resume && n > 0 && at >= maxTop {
		l.resume()
	}
}

// Wheel scrolls by the notches of the wheel, WheelTextLines a notch. Up stops following, and the end of
// the lines resumes it.
func (l *LogView) Wheel(steps int) { l.scroll(steps*WheelTextLines, true) }

// goTo shows the line seq, about in the middle of the page, and stops following.
func (l *LogView) goTo(seq uint64) {
	l.pause()
	g := l.geometry(l.width)
	idx := int(seq - l.first())
	above := max((l.body-l.rowsOf(l.line(idx), g))/2, 0)
	t, acc := idx, 0
	for t > 0 {
		r := l.rowsOf(l.line(t-1), g)
		if acc+r > above {
			break
		}
		acc += r
		t--
	}
	l.top = l.first() + uint64(t)
	l.cur, l.hasCur = seq, true
}

// stepMatch jumps to the next or previous line with a match, with wrap-around; from no current match it
// starts at the first shown line.
func (l *LogView) stepMatch(forward bool) {
	if len(l.hits) == 0 {
		return
	}
	from := l.first() + uint64(l.topIndex())
	var i int
	switch {
	case l.hasCur && forward:
		i, _ = slices.BinarySearch(l.hits, l.cur+1)
	case l.hasCur:
		i, _ = slices.BinarySearch(l.hits, l.cur)
		i--
	case forward:
		i, _ = slices.BinarySearch(l.hits, from)
	default:
		i, _ = slices.BinarySearch(l.hits, from)
		i--
	}
	l.goTo(l.hits[(i%len(l.hits)+len(l.hits))%len(l.hits)])
}

// Update handles a key. handled is false for a key the view leaves to the application, such as back with
// no search to cancel.
func (l *LogView) Update(msg tea.KeyPressMsg) (handled bool) {
	focus := l.Input()
	k := l.spec.Keys
	for _, id := range []ActionID{k.Back, k.Enter, k.Search, k.Up, k.Down, k.PageUp, k.PageDown, k.First, k.Last,
		k.Follow, k.NextMatch, k.PrevMatch, k.Wrap, k.Timestamps} {
		if id != "" && l.keys.Is(msg, id, focus) {
			return l.Act(id)
		}
	}
	if focus == InputText && l.query.edit(msg) {
		l.queryChanged()
		return true
	}
	return false
}

// Act runs one of the log view keys as its key does, for a click or a command that stands for the key.
func (l *LogView) Act(id ActionID) (handled bool) {
	k := l.spec.Keys
	if id == "" {
		return false
	}
	switch id {
	case k.Back:
		if !l.searching && l.needle == "" {
			return false
		}
		l.searching = false
		l.query.clear()
		l.queryChanged()
	case k.Enter:
		if !l.searching {
			return false
		}
		l.searching = false
		l.stepMatch(true)
	case k.Search:
		l.searching = true
		l.query.move(len(l.query.runes))
	case k.Up:
		l.scroll(-1, false)
	case k.Down:
		l.scroll(1, false)
	case k.PageUp:
		l.scroll(-max(l.page-1, 1), false)
	case k.PageDown:
		l.scroll(max(l.page-1, 1), false)
	case k.First:
		l.pause()
		l.top = l.first()
	case k.Last:
		l.resume()
	case k.Follow:
		if l.following {
			l.pause()
		} else {
			l.resume()
		}
	case k.NextMatch:
		l.stepMatch(true)
	case k.PrevMatch:
		l.stepMatch(false)
	case k.Wrap:
		l.wrap = !l.wrap
	case k.Timestamps:
		l.stamps = !l.stamps
	default:
		return false
	}
	return true
}

// Click handles a click on a target the view drew, as the MouseMap of its targets reports it: the state
// in the status line switches following and the search part starts the search.
func (l *LogView) Click(r MouseResult) (handled bool) {
	return r.Handled && l.Act(r.Action)
}

// Hints are the keys of the view for a key line, for the input focus Input names.
func (l *LogView) Hints() []Hint {
	k := l.spec.Keys
	if l.searching {
		return []Hint{{Action: k.Enter, Label: TextKeyEnter}, {Action: k.Back, Label: TextKeyBack, Kind: HintAlways}}
	}
	hints := []Hint{{Action: k.Search, Label: TextKeySearch}, {Action: k.Follow, Label: TextLogFollow, Priority: 1}}
	if l.needle != "" {
		hints = append(hints, Hint{Action: k.NextMatch, Label: TextLogNextMatch, Priority: 1},
			Hint{Action: k.PrevMatch, Label: TextLogPrevMatch, Priority: 1})
	}
	hints = append(hints, Hint{Action: k.Wrap, Label: TextLogWrap, Priority: 2},
		Hint{Action: k.Timestamps, Label: TextLogTimestamps, Priority: 2},
		Hint{Action: k.Up, Label: TextHelpScroll, Kind: HintNavigation},
		Hint{Action: k.Down, Label: TextHelpScroll, Kind: HintNavigation})
	if l.needle != "" {
		hints = append(hints, Hint{Action: k.Back, Label: TextKeyBack, Priority: 1})
	}
	return hints
}

// View draws the log view with styles in width by height cells: the lines that fit, from the first shown
// line on, and in the last row the state, the search and the position. It returns a target for the whole area,
// one for the state and one for the search in the status line, counted from the top left of the view.
func (l *LogView) View(styles Styles, width, height int) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if width == 0 || height == 0 {
		return finishPage(nil, nil, width, height)
	}
	l.width, l.body = width, height
	if height >= 2 {
		l.body = height - 1
	}
	g := l.geometry(width)
	var lines []string
	first, at := l.first(), l.topIndex()
	if !l.following {
		l.top = first + uint64(at)
	}
	if len(l.buf) == 0 {
		lines = append(lines, styles.Render(StyleHint, styles.Truncate(Escape(l.text.Lookup(TextLogEmpty)), width)))
	}
	shown := 0
	for i := at; i < len(l.buf) && len(lines) < l.body; i++ {
		lines = append(lines, l.rows(l.line(i), first+uint64(i), g, styles)...)
		shown++
	}
	l.page = max(shown, 1)
	if len(lines) > l.body {
		lines = lines[:l.body]
	}
	targets := []Target{{Rect: Rect{Width: width, Height: height}, Zone: ZoneRows}}
	if height >= 2 {
		for len(lines) < l.body {
			lines = append(lines, "")
		}
		status, st := l.status(styles, width, at, shown)
		targets = append(targets, OffsetTargets(st, 0, l.body)...)
		lines = append(lines, status)
	}
	return finishPage(lines, targets, width, height)
}

// rows draws one line in the rows it needs.
func (l *LogView) rows(ln logLine, seq uint64, g logGeometry, styles Styles) []string {
	runes := []rune(ln.text)
	var mask []bool
	if l.needle != "" {
		mask = hitMask(foldRunes(ln.text), []rune(l.needle))
	}
	isCur := l.hasCur && seq == l.cur
	lead := strings.Repeat(" ", g.gutter)
	switch {
	case g.gutter == 0:
		lead = ""
	case isCur:
		lead = styles.Render(StyleSelectionMark, styles.SelectionMark()) + " "
	case slices.Contains(mask, true):
		lead = styles.Render(StyleActive, styles.Glyph(GlyphMask)) + " "
	}
	if g.stamp > 0 {
		stamp := strings.Repeat(" ", logStampCells)
		if !ln.at.IsZero() {
			stamp = ln.at.Format(logStampLayout)
		}
		lead += styles.Render(StyleHint, stamp) + " "
	}
	if g.source > 0 {
		src := Pad(styles.Truncate(ln.source, g.source-1), g.source-1)
		lead += styles.Render(StyleHint, src) + " "
	}
	indent := strings.Repeat(" ", g.gutter+g.stamp+g.source)
	mark := []rune(nil)
	if ln.cut {
		mark = []rune(l.mark())
	}
	if !l.wrap {
		return []string{lead + l.clipped(runes, mark, mask, isCur, g.text, styles)}
	}
	all := append(slices.Clone(runes), mark...)
	var out []string
	for i, rg := range wrapRanges(all, g.text) {
		prefix := indent
		if i == 0 {
			prefix = lead
		}
		out = append(out, prefix+segments(all, rg[0], rg[1], len(runes), mask, isCur, styles))
	}
	return out
}

// clipped is the text of a line that does not wrap, cut to room cells with the ellipsis glyph; the mark of a
// cut line stays at the end.
func (l *LogView) clipped(runes, mark []rune, mask []bool, isCur bool, room int, styles Styles) string {
	room -= cells(mark)
	tail := segments(mark, 0, len(mark), 0, nil, false, styles)
	if cells(runes) <= room {
		return segments(runes, 0, len(runes), len(runes), mask, isCur, styles) + tail
	}
	ellipsis := styles.Glyph(GlyphEllipsis)
	room = max(room-lipgloss.Width(ellipsis), 0)
	end, used := 0, 0
	for end < len(runes) && used+runeCells(runes[end]) <= room {
		used += runeCells(runes[end])
		end++
	}
	return segments(runes, 0, end, len(runes), mask, isCur, styles) + styles.Render(StyleHint, ellipsis) + tail
}

// segments draws runes[from:to]: text, matches and, from bodyLen on, the mark of a cut line.
func segments(runes []rune, from, to, bodyLen int, mask []bool, isCur bool, styles Styles) string {
	kind := func(i int) int {
		switch {
		case i >= bodyLen:
			return 2
		case i < len(mask) && mask[i]:
			return 1
		}
		return 0
	}
	var b strings.Builder
	for i := from; i < to; {
		j := i
		for j < to && kind(j) == kind(i) {
			j++
		}
		style := StyleText
		switch kind(i) {
		case 1:
			style = StyleActive
			if isCur {
				style = StyleActiveRow
			}
		case 2:
			style = StyleWarn
		}
		b.WriteString(styles.Render(style, string(runes[i:j])))
		i = j
	}
	return b.String()
}

// hitMask marks the characters of hay, folded, that belong to an occurrence of needle.
func hitMask(hay, needle []rune) []bool {
	mask := make([]bool, len(hay))
	for i := 0; i+len(needle) <= len(hay) && len(needle) > 0; i++ {
		if slices.Equal(hay[i:i+len(needle)], needle) {
			for j := i; j < i+len(needle); j++ {
				mask[j] = true
			}
			i += len(needle) - 1
		}
	}
	return mask
}

// status is the last row: the state, the search and the position, from the left; what does not fit goes
// from the right.
func (l *LogView) status(styles Styles, width, at, shown int) (string, []Target) {
	type piece struct {
		text   string
		plain  int
		action ActionID
	}
	var pieces []piece
	add := func(text string, action ActionID) { pieces = append(pieces, piece{text, lipgloss.Width(text), action}) }
	kind, word := StatusOK, l.text.Lookup(TextLogFollowing)
	if !l.following {
		kind, word = StatusWarn, l.text.Lookup(TextLogPaused)
		if l.unseen > 0 {
			word = l.text.Lookup(TextLogPausedNew, l.unseen)
		}
	}
	add(styles.Status(kind, word), l.spec.Keys.Follow)
	if l.searching || l.needle != "" {
		field := Escape(l.keys.Label(l.spec.Keys.Search)) + " "
		if l.searching {
			qw := min(max(lipgloss.Width(Escape(l.query.String()))+1, 8), logSearchFieldMax)
			field += strings.TrimRight(l.query.view(qw, styles, true, ""), " ")
			if styles.profile.Colorless() && styles.profile != ProfileMono {
				field += " "
			}
		} else {
			field += styles.TruncateLeft(Escape(l.query.String()), logSearchFieldMax)
		}
		word := l.text.Lookup(TextLogNoMatches)
		switch n := len(l.hits); {
		case n > 0 && l.hasCur:
			i, _ := slices.BinarySearch(l.hits, l.cur)
			word = l.text.Lookup(TextLogMatch, i+1, n)
		case n > 0:
			word = l.text.Lookup(TextLogMatches, n)
		}
		add(field+" "+styles.Render(StyleHint, Escape(word)), l.spec.Keys.Search)
	}
	if n := len(l.buf); n > 0 && shown > 0 {
		add(styles.Render(StyleHint, Escape(l.text.Lookup(TextScrollPosition, at+1, at+shown, n))), "")
	}
	sep := styles.Render(StyleHint, styles.Glyph(GlyphKeySeparator))
	sepW := lipgloss.Width(sep)
	for len(pieces) > 1 {
		total := -sepW
		for _, p := range pieces {
			total += p.plain + sepW
		}
		if total <= width {
			break
		}
		pieces = pieces[:len(pieces)-1]
	}
	var line strings.Builder
	var targets []Target
	x := 0
	for i, p := range pieces {
		if i > 0 {
			line.WriteString(sep)
			x += sepW
		}
		if p.action != "" && x < width {
			targets = append(targets, Target{Rect: Rect{X: x, Width: min(p.plain, width-x), Height: 1}, Zone: ZoneRows, Action: p.action})
		}
		line.WriteString(p.text)
		x += p.plain
	}
	return line.String(), targets
}
