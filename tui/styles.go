package tui

import (
	"regexp"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Styles are the resolved look of one theme under one terminal: its palette, colour profile and character
// set. Styles is a value; the zero value draws plain ASCII text without colour or attributes. Build one with
// ThemeSet.Resolve.
type Styles struct {
	palette Palette
	profile ColorProfile
	charset CharsetMode
	paints  bool
}

// Palette is the palette of the styles, the zero value without colour.
func (s Styles) Palette() Palette { return s.palette }

// Profile is the colour profile the styles draw for.
func (s Styles) Profile() ColorProfile { return s.profile }

// Charset is the character set the styles draw with.
func (s Styles) Charset() CharsetMode { return s.charset }

// Paints reports whether every cell of the screen carries the ground and text colour; see Paint.
func (s Styles) Paints() bool { return s.paints }

// TextStyle names a text style built from the palette roles. None of them carries meaning alone: focus has
// its mark and frame, a result its word, so a terminal without colour loses nothing.
type TextStyle uint8

const (
	// StyleText is content text: names, values, descriptions.
	StyleText TextStyle = iota
	// StyleTitle is a title: bold.
	StyleTitle
	// StyleActive is the active element: bold in the accent colour.
	StyleActive
	// StyleHint is secondary text: hints, side columns, the action of a key.
	StyleHint
	// StyleOK, StyleWarn and StyleFail are results and states: bold in their colour.
	StyleOK
	StyleWarn
	StyleFail
	// StyleKey is a key in a hint: bold.
	StyleKey
	// StyleFooterKey is a key in the footer: bold in the accent colour.
	StyleFooterKey
	// StyleActiveRow is the selected row across its full width: the selection fill, reverse without one.
	StyleActiveRow
	// StyleSelectionMark is the mark that leads the selected row.
	StyleSelectionMark
	// StyleBorder and StyleBorderFocus are the quiet frame and the frame of the zone with the focus.
	StyleBorder
	StyleBorderFocus
)

// Style is the Lip Gloss style of t, for composing with styles of an application's own roles.
func (s Styles) Style(t TextStyle) lipgloss.Style {
	plain := lipgloss.NewStyle()
	if s.profile == ProfileNone || s.profile > ProfileTrueColor {
		return plain
	}
	bold := plain.Bold(true)
	if s.profile == ProfileMono {
		switch t {
		case StyleTitle, StyleActive, StyleOK, StyleWarn, StyleFail, StyleKey, StyleFooterKey:
			return bold
		case StyleHint, StyleBorder:
			return plain.Faint(true)
		case StyleActiveRow, StyleSelectionMark:
			return bold.Reverse(true)
		}
		return plain
	}
	p := s.palette
	fg := func(st lipgloss.Style, c Color) lipgloss.Style {
		if c == "" {
			return st
		}
		return st.Foreground(lipgloss.Color(string(c)))
	}
	switch t {
	case StyleText:
		return fg(plain, p.Text)
	case StyleTitle, StyleKey:
		return fg(bold, p.Text)
	case StyleActive, StyleFooterKey:
		return fg(bold, p.Accent)
	case StyleHint:
		return fg(plain, p.TextDim)
	case StyleOK:
		return fg(bold, p.OK)
	case StyleWarn:
		return fg(bold, p.Warn)
	case StyleFail:
		return fg(bold, p.Fail)
	case StyleBorder:
		return fg(plain, p.Border)
	case StyleBorderFocus:
		return fg(plain, p.BorderFocus)
	case StyleActiveRow, StyleSelectionMark:
		if p.Selection == "" {
			return bold.Reverse(true)
		}
		c := p.Text
		if t == StyleSelectionMark {
			c = p.Accent
		}
		return fg(bold, c).Background(lipgloss.Color(string(p.Selection)))
	}
	return plain
}

// Render draws text in the style t.
func (s Styles) Render(t TextStyle, text string) string { return s.Style(t).Render(text) }

// Glyph names a character that carries meaning; each has a Unicode and an ASCII form.
type Glyph uint8

const (
	GlyphStatus       Glyph = iota // leads a status word
	GlyphOK                        // a result that succeeded
	GlyphFail                      // a result that failed
	GlyphWarning                   // a warning or question
	GlyphCollapsed                 // a closed group
	GlyphExpanded                  // an open group, and the end of a choice field
	GlyphCategory                  // a category mark
	GlyphEllipsis                  // the end of a shortened text
	GlyphKeySeparator              // between groups of key hints
	GlyphTabSeparator              // between groups of tabs
	GlyphInfo                      // more explanation on request
	GlyphMask                      // one typed character of a masked field
	glyphCount
)

// glyphs are the Unicode and ASCII forms of every glyph, in one place.
var glyphs = [glyphCount][2]string{
	GlyphStatus:       {"●", "*"},
	GlyphOK:           {"✓", "+"},
	GlyphFail:         {"✗", "x"},
	GlyphWarning:      {"⚠", "!"},
	GlyphCollapsed:    {"▸", ">"},
	GlyphExpanded:     {"▾", "v"},
	GlyphCategory:     {"■", "#"},
	GlyphEllipsis:     {"…", "~"},
	GlyphKeySeparator: {" · ", " - "},
	GlyphTabSeparator: {"|", "|"},
	GlyphInfo:         {" (i)", " (i)"},
	GlyphMask:         {"•", "*"},
}

// Glyph is the form of g in the character set of the styles.
func (s Styles) Glyph(g Glyph) string {
	if g >= glyphCount {
		return ""
	}
	if s.charset == CharsetUnicode {
		return glyphs[g][0]
	}
	return glyphs[g][1]
}

// SelectionMark leads the selected row: a narrow bar where colour shows it on a selection fill, and ">"
// where it does not, so the selection never depends on colour alone.
func (s Styles) SelectionMark() string {
	if s.charset != CharsetUnicode || s.profile.Colorless() || s.palette.Selection == "" {
		return ">"
	}
	return "▌"
}

// Paint gives every cell of a width by height screen the ground and text colour of a painting theme: the
// colours open each line and follow every reset inside it, lines are padded with spaces to width, and
// missing lines are added. Only escape sequences and spaces are added, so no cell moves. Styles that do not
// paint return view unchanged.
func (s Styles) Paint(view string, width, height int) string {
	prefix := s.canvasPrefix()
	if prefix == "" {
		return view
	}
	lines := strings.Split(view, "\n")
	for height > len(lines) {
		lines = append(lines, "")
	}
	for i, line := range lines {
		line = sgrSequence.ReplaceAllStringFunc(line, func(seq string) string {
			if resetsColor(seq) {
				return seq + prefix
			}
			return seq
		})
		if pad := width - ansi.StringWidth(line); pad > 0 {
			line += prefix + strings.Repeat(" ", pad)
		}
		lines[i] = prefix + line + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
}

// canvasPrefix is the escape sequence that sets the ground and text colour; empty where the styles do not
// paint.
func (s Styles) canvasPrefix() string {
	if !s.paints || s.profile.Colorless() {
		return ""
	}
	return openingOf(lipgloss.NewStyle().Foreground(lipgloss.Color(string(s.palette.Text))).
		Background(lipgloss.Color(string(s.palette.Base))))
}

// openingOf is the escape sequence style opens its text with.
func openingOf(style lipgloss.Style) string {
	const probe = "x"
	prefix, _, _ := strings.Cut(style.Render(probe), probe)
	return prefix
}

var sgrSequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

// resetsColor reports whether an SGR sequence drops the background or the foreground: a full reset (also
// the empty form), or the default-colour codes. The extended colour codes carry arguments that may be 0.
func resetsColor(seq string) bool {
	params := strings.Split(seq[2:len(seq)-1], ";")
	for i := 0; i < len(params); i++ {
		switch p := params[i]; {
		case slices.Contains([]string{"", "0", "39", "49"}, p):
			return true
		case p == "38" || p == "48":
			if i+1 < len(params) && params[i+1] == "5" {
				i += 2
			} else {
				i += 4
			}
		}
	}
	return false
}

// keepOpen makes a finished line keep the style opening open across the resets of its own styles, so a
// filled area stays filled behind styled text.
func keepOpen(text, opening string) string {
	if opening == "" || !strings.Contains(text, "\x1b[") {
		return text
	}
	return strings.NewReplacer("\x1b[0m", "\x1b[0m"+opening, "\x1b[m", "\x1b[m"+opening).Replace(text)
}
