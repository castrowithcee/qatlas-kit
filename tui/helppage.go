package tui

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/helppages"
)

// pageLook is how one character of a page is drawn; url is the address it links to.
type pageLook struct {
	style TextStyle
	url   string
}

// pageRows is the page shown in width cells: its title, its blocks and its link under TextHelpMore. Key
// placeholders resolve to the keys bound now, so a new binding shows at once.
func (h *Help) pageRows(width int, styles Styles) []textRow {
	h.doc.reset("")
	c := h.pages[h.page].page.In(h.spec.Lang)
	// The title never counts as the place a search found.
	find := h.find
	h.find = nil
	rows := h.pageBlock(nil, []helppages.Span{{Text: c.Title}}, StyleActive, 0, "", width, styles)
	h.find = find
	rows = append(rows, textRow{})
	bullet := "-"
	if styles.charset == CharsetUnicode {
		bullet = "•"
	}
	prev := helppages.Heading
	for _, b := range helppages.Blocks(c.Text) {
		if prev != helppages.Heading && (prev != helppages.Item || b.Kind != helppages.Item) {
			rows = append(rows, textRow{})
		}
		prev = b.Kind
		switch b.Kind {
		case helppages.Heading:
			rows = h.pageBlock(rows, b.Spans, StyleTitle, 0, "", width, styles)
		case helppages.Item:
			mark := bullet
			if b.Ordered {
				mark = strconv.Itoa(b.Number) + "."
			}
			rows = h.pageBlock(rows, b.Spans, StyleText, 2*b.Level, mark+" ", width, styles)
		default:
			rows = h.pageBlock(rows, b.Spans, StyleText, 0, "", width, styles)
		}
	}
	if find != nil {
		h.scroll.offset, h.find = max(h.findRow, 0), nil
	}
	if c.Link != "" {
		rows = append(rows, textRow{})
		rows = h.pageBlock(rows, []helppages.Span{{Text: h.text.Lookup(TextHelpMore)}}, StyleTitle, 0, "", width, styles)
		rows = h.pageBlock(rows, []helppages.Span{{Kind: helppages.Link, Text: c.Link, URL: c.Link}}, StyleText, 0, "", width, styles)
	}
	return rows
}

// pageBlock appends spans as one paragraph in the base style, indented by indent cells and wrapped to the
// rest of width: mark leads the first row and its width indents the others. A link shows its text and then
// its address, which alone links where the terminal takes hyperlinks.
func (h *Help) pageBlock(rows []textRow, spans []helppages.Span, base TextStyle, indent int, mark string, width int, styles Styles) []textRow {
	var text []rune
	var looks []pageLook
	add := func(s string, look pageLook) {
		for _, r := range s {
			// One blank between words, as the wrap draws them, so the selection keeps to the text.
			if r == ' ' && (len(text) == 0 || text[len(text)-1] == ' ') {
				continue
			}
			text, looks = append(text, r), append(looks, look)
		}
	}
	plain := pageLook{style: base}
	for _, sp := range spans {
		s, _ := resolvePlaceholders(sp.Text, h.keys)
		s = Escape(s)
		switch sp.Kind {
		case helppages.Bold:
			add(s, pageLook{style: StyleTitle})
		case helppages.Code:
			add(s, pageLook{style: StyleSyntaxString})
		case helppages.Link:
			link := pageLook{style: StyleHint, url: sp.URL}
			if s == "" || s == sp.URL {
				add(Escape(sp.URL), link)
				continue
			}
			add(s+" (", plain)
			add(Escape(sp.URL), link)
			add(")", plain)
		default:
			add(s, plain)
		}
	}
	for len(text) > 0 && text[len(text)-1] == ' ' {
		text, looks = text[:len(text)-1], looks[:len(looks)-1]
	}
	x := indent + lipgloss.Width(mark)
	room := max(width-x, 1)
	var pieces []string
	for _, p := range wrapWords(string(text), room) {
		pieces = append(pieces, Clip(p, room))
	}
	sels := h.doc.para(string(text), pieces, x, false)
	if h.find != nil && h.findRow < 0 {
		if at := foldIndex(text, h.find); at >= 0 {
			h.findRow = len(rows)
			for i, sel := range sels {
				if at < sel.from+len(sel.widths) {
					h.findRow += i
					break
				}
			}
		}
	}
	linked := styles.hyperlinks()
	for i, piece := range pieces {
		prefix := strings.Repeat(" ", indent) + mark
		if i > 0 {
			prefix = strings.Repeat(" ", x)
		}
		drawn := styles.Render(base, piece)
		if r := sels[i]; len(r.widths) == utf8.RuneCountInString(piece) {
			to := r.from + len(r.widths)
			drawn = drawLooks(text[r.from:to], looks[r.from:to], styles, linked)
		}
		rows = append(rows, textRow{text: h.doc.mark(Clip(prefix+drawn, width), sels[i], styles), sel: &sels[i]})
	}
	return rows
}

// drawLooks draws each run of characters that look alike in its style.
func drawLooks(text []rune, looks []pageLook, styles Styles, linked bool) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		j := i + 1
		for j < len(text) && looks[j] == looks[i] {
			j++
		}
		s := styles.Render(looks[i].style, string(text[i:j]))
		if looks[i].url != "" && linked {
			// Each piece links to the whole address; the address also stands as text for a terminal without
			// hyperlinks.
			s = ansi.SetHyperlink(looks[i].url) + s + ansi.ResetHyperlink()
		}
		b.WriteString(s)
		i = j
	}
	return b.String()
}
