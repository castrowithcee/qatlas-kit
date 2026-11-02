package tui

import (
	"hash/fnv"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// StatusKind is the state a status mark shows.
type StatusKind string

const (
	StatusOK   StatusKind = "ok"
	StatusWarn StatusKind = "warn"
	StatusFail StatusKind = "fail"
	StatusBusy StatusKind = "busy"
)

// Escape makes untrusted text visible without letting it move the cursor, change the terminal or start a
// line: every control or otherwise unprintable character is written as its Go escape. Widgets escape the
// plain-text arguments they take, such as titles, words and labels; content lines are styled text, so an
// application escapes the data in them before it styles them.
func Escape(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			quoted := strconv.QuoteRune(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Status is a status mark: the status glyph in the colour of kind, then the word. The word stays without
// colour, so the state never depends on colour alone.
func (s Styles) Status(kind StatusKind, word string) string {
	return s.Render(statusStyle(kind), s.Glyph(GlyphStatus)) + " " + Escape(word)
}

func statusStyle(kind StatusKind) TextStyle {
	switch kind {
	case StatusOK:
		return StyleOK
	case StatusWarn:
		return StyleWarn
	case StatusFail:
		return StyleFail
	}
	return StyleHint
}

// MessageKind is the kind of a message line.
type MessageKind string

const (
	MessageBusy    MessageKind = "busy"
	MessageError   MessageKind = "error"
	MessageWarning MessageKind = "warning"
	MessageStatus  MessageKind = "status"
	MessageResult  MessageKind = "result"
)

// Message draws a message in the style of its kind, wrapped into width cells and never cut, so the way out
// at its end stays on screen. The text carries its own word, such as TextError or TextWarning, since the
// style alone carries no meaning. It is plain text: line breaks start new lines, every other control
// character is escaped.
func (s Styles) Message(kind MessageKind, text string, width int) string {
	style := StyleText
	switch kind {
	case MessageError:
		style = StyleFail
	case MessageWarning:
		style = StyleWarn
	case MessageResult:
		style = StyleOK
	case MessageBusy:
		style = StyleHint
	}
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		lines = append(lines, Wrap(Escape(l), width)...)
	}
	for i, l := range lines {
		lines[i] = s.Render(style, l)
	}
	return strings.Join(lines, "\n")
}

// frameChars are the frame characters: rounded, or double for a focused frame without colour, where the
// colour of the frame cannot show the focus; plain ASCII in an ASCII terminal.
func (s Styles) frameChars(focused bool) (h, v string, corners [4]string) {
	double := focused && s.profile.Colorless()
	switch {
	case s.charset != CharsetUnicode && double:
		return "=", "|", [4]string{"#", "#", "#", "#"}
	case s.charset != CharsetUnicode:
		return "-", "|", [4]string{"+", "+", "+", "+"}
	case double:
		return "═", "║", [4]string{"╔", "╗", "╚", "╝"}
	}
	return "─", "│", [4]string{"╭", "╮", "╰", "╯"}
}

// Panel draws lines in a frame of width by height cells: the title bold in the top border, the note dimmed
// at its right end where it fits, one cell of padding inside, the inner area in the panel colour. Every line
// is exactly width cells and the frame stays closed at any size; content that does not fit is cut. Below 4
// by 2 cells there is no room for a frame, and the lines are drawn without one.
func (s Styles) Panel(title, note string, lines []string, width, height int, focused bool) []string {
	width, height = max(width, 0), max(height, 0)
	if width < 4 || height < 2 {
		out := make([]string, height)
		for i := range out {
			if i < len(lines) {
				out[i] = Pad(Clip(lines[i], width), width)
			} else {
				out[i] = strings.Repeat(" ", width)
			}
		}
		return out
	}
	inner, rows := width-4, height-2
	frame := StyleBorder
	if focused {
		frame = StyleBorderFocus
	}
	h, v, c := s.frameChars(focused)
	title, note = Escape(title), Escape(note)
	label := ""
	if title != "" {
		label = s.Truncate(" "+title+" ", inner+1)
	}
	tail := ""
	if note != "" {
		if t := " " + note + " "; lipgloss.Width(t) <= inner+1-lipgloss.Width(label)-2 {
			tail = t
		}
	}
	rule := inner + 1 - lipgloss.Width(label) - lipgloss.Width(tail)
	top := s.Render(frame, c[0]+h) + s.Render(StyleTitle, label)
	if tail != "" {
		top += s.Render(frame, strings.Repeat(h, rule-1)) + s.Render(StyleHint, tail) + s.Render(frame, h+c[1])
	} else {
		top += s.Render(frame, strings.Repeat(h, rule)+c[1])
	}
	out := []string{top}
	body, opening := lipgloss.NewStyle(), ""
	if bg := s.palette.Panel; bg != "" && !s.profile.Colorless() {
		body = body.Background(lipgloss.Color(string(bg)))
		opening = openingOf(body)
	}
	for i := 0; i < rows; i++ {
		text := ""
		if i < len(lines) {
			text = lines[i]
		}
		text = keepOpen(Pad(Clip(text, inner), inner), opening)
		out = append(out, s.Render(frame, v)+body.Render(" "+text+" ")+s.Render(frame, v))
	}
	return append(out, s.Render(frame, c[2]+strings.Repeat(h, inner+2)+c[3]))
}

// TitleLine is the title line of a screen of width cells: the title at the left and, dimmed, the note at the
// right edge where it fits beside the title.
func (s Styles) TitleLine(title, note string, width int) string {
	title, note = s.Truncate(Escape(title), max(width, 1)), Escape(note)
	line := s.Render(StyleTitle, title)
	if note == "" {
		return line
	}
	if gap := width - lipgloss.Width(title) - lipgloss.Width(note); gap >= 2 {
		line += strings.Repeat(" ", gap) + s.Render(StyleHint, note)
	}
	return line
}

// Row draws one row of a table of width cells. A selected row is filled across its width behind the
// selection mark; every other row leads with a blank of the same width. text is shortened to fit with the
// ellipsis glyph, never wrapped.
func (s Styles) Row(text string, width int, selected bool) string {
	width = max(width, 0)
	if width < 2 {
		return Pad(s.Truncate(text, width), width)
	}
	body := Pad(s.Truncate(text, width-2), width-2)
	if !selected {
		return "  " + body
	}
	fill := s.Style(StyleActiveRow)
	return s.Render(StyleSelectionMark, s.SelectionMark()) + fill.Render(keepOpen(" "+body, openingOf(fill)))
}

// Span is the range of cells from Start up to End that one element of a drawn line takes.
type Span struct{ Start, End int }

// TabStrip draws a row of tabs and returns the cells each takes. The active tab is a fill, the accent fill
// while the strip has the focus; the others are dimmed. Without colour the active tab stands in brackets,
// in reverse video while the strip has the focus, so colour is never the only mark. A word equal to the tab
// separator glyph separates groups and is no tab. Every tab takes the same cells whether active or not.
func (s Styles) TabStrip(words []string, active int, focused bool) (string, []Span) {
	var b strings.Builder
	spans := make([]Span, 0, len(words))
	x := 0
	plain := s.profile.Colorless()
	for i, word := range words {
		word = Escape(word)
		text, style := " "+word+" ", s.Style(StyleHint)
		switch {
		case word == s.Glyph(GlyphTabSeparator):
			text = word
		case i == active && plain:
			text, style = "["+word+"]", s.Style(StyleActive)
			if focused {
				style = s.Style(StyleActiveRow)
			}
		case i == active && focused:
			style = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color(string(s.palette.Accent))).
				Foreground(lipgloss.Color(string(s.palette.Base)))
		case i == active:
			style = s.Style(StyleActiveRow)
		}
		if i > 0 {
			b.WriteString(" ")
			x++
		}
		b.WriteString(style.Render(text))
		w := lipgloss.Width(text)
		spans = append(spans, Span{x, x + w})
		x += w
	}
	return b.String(), spans
}

// ButtonKind is the weight of a button.
type ButtonKind uint8

const (
	ButtonNormal ButtonKind = iota
	ButtonPrimary
	ButtonDanger
)

// Button draws a label as a button: "[ label ]", or "> label <" in reverse video while it has the focus, so
// the focus reads without colour. Primary and danger buttons are bold and filled where there is colour.
func (s Styles) Button(kind ButtonKind, label string, focused bool) string {
	label = Escape(label)
	text := "[ " + label + " ]"
	if focused {
		text = "> " + label + " <"
	}
	if s.profile == ProfileNone || s.profile > ProfileTrueColor {
		return text
	}
	style := lipgloss.NewStyle()
	p := s.palette
	colored := !s.profile.Colorless()
	switch kind {
	case ButtonPrimary, ButtonDanger:
		style = style.Bold(true)
		fill := p.Accent
		if kind == ButtonDanger {
			fill = p.Fail
		}
		if colored && fill != "" {
			style = style.Background(lipgloss.Color(string(fill)))
			if p.Base != "" {
				style = style.Foreground(lipgloss.Color(string(p.Base)))
			}
		}
	default:
		if colored && p.Text != "" {
			style = style.Foreground(lipgloss.Color(string(p.Text)))
		}
	}
	if focused {
		style = style.Bold(true).Reverse(true)
	}
	return style.Render(text)
}

// TickBox is the box of an entry of a multiple choice: "[x]" in the OK colour or "[ ]". The characters alone
// tell the states apart.
func (s Styles) TickBox(ticked bool) string {
	if ticked {
		return s.Render(StyleOK, "[x]")
	}
	return "[ ]"
}

// CategoryColor is the brand colour of a name: always the same one for the same name.
func (s Styles) CategoryColor(name string) Color {
	h := fnv.New32a()
	h.Write([]byte(name))
	return s.palette.Brand[h.Sum32()%uint32(len(s.palette.Brand))]
}

// CategoryMark is the category glyph in the colour of name. It stands beside the name, never in its place.
func (s Styles) CategoryMark(name string) string {
	style := lipgloss.NewStyle()
	if c := s.CategoryColor(name); c != "" && !s.profile.Colorless() {
		style = style.Foreground(lipgloss.Color(string(c)))
	}
	return style.Render(s.Glyph(GlyphCategory))
}

// Truncate shortens text at its right end to width cells and marks the cut with the ellipsis glyph.
func (s Styles) Truncate(text string, width int) string {
	switch {
	case lipgloss.Width(text) <= width:
		return text
	case width <= 0:
		return ""
	case width == 1:
		return s.Glyph(GlyphEllipsis)
	}
	return Clip(text, width-1) + s.Glyph(GlyphEllipsis)
}

// TruncateLeft keeps the end of text, such as a path, within width cells and marks the cut at its start.
// text is plain, without escape sequences.
func (s Styles) TruncateLeft(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	mark := s.Glyph(GlyphEllipsis)
	if width <= 1 {
		return Clip(mark, max(width, 0))
	}
	runes := []rune(text)
	for i := range runes {
		if tail := string(runes[i:]); lipgloss.Width(tail) <= width-1 {
			return mark + tail
		}
	}
	return mark
}

// Clip cuts text to width visible cells. Escape sequences take no cells and are never split; when the cut
// leaves a style open, a reset ends it. Text without escapes stays a true prefix of its input.
func Clip(text string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	parser := ansi.NewParser()
	state := byte(ansi.NormalState)
	used, styled := 0, false
	for rest := text; rest != ""; {
		seq, w, n, next := ansi.DecodeSequence(rest, state, parser)
		state = next
		switch {
		case w == 0 && ansi.HasCsiPrefix(seq) && strings.HasSuffix(seq, "m"):
			b.WriteString(seq)
			styled = seq != "\x1b[m" && seq != "\x1b[0m"
		case w == 0 && strings.HasPrefix(seq, "\x1b"):
			b.WriteString(seq)
		default:
			if used+w > width {
				if styled {
					b.WriteString("\x1b[m")
				}
				return b.String()
			}
			b.WriteString(seq)
			used += w
		}
		rest = rest[n:]
	}
	return b.String()
}

// Pad fills text with spaces to width cells; wider text stays as it is.
func Pad(text string, width int) string {
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
}

// Wrap breaks text into lines of at most width cells, at spaces where it can and inside a word where it
// must. Nothing is cut away. A line that fits stays exactly as it is; the pieces of a wrapped line lose their
// trailing blanks.
func Wrap(text string, width int) []string {
	width = max(width, 1)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) <= width {
			out = append(out, line)
			continue
		}
		for _, piece := range strings.Split(lipgloss.Wrap(line, width, ""), "\n") {
			out = append(out, strings.TrimRight(piece, " "))
		}
	}
	return out
}
