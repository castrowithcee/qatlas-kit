package helppages

import (
	"strconv"
	"strings"
)

// BlockKind is the kind of a Block.
type BlockKind int

// The kinds of block.
const (
	Paragraph BlockKind = iota
	Heading
	Item
)

// SpanKind is the kind of a Span.
type SpanKind int

// The kinds of span.
const (
	Text SpanKind = iota
	Bold
	Code
	Link
)

// Span is a run of inline text. URL is set for a Link only. The split does not check it: Load does.
type Span struct {
	Kind SpanKind
	Text string
	URL  string
}

// Block is a heading, a paragraph or a list item.
type Block struct {
	Kind BlockKind
	// Level is 1 to 3 for a heading, 0 or 1 for an item and 0 for a paragraph.
	Level int
	// Ordered tells a numbered item from a bullet; Number is the number as written.
	Ordered bool
	Number  int
	Spans   []Span
}

// Blocks splits a Markdown body into blocks. See the package documentation for the subset it knows.
func Blocks(text string) []Block {
	var blocks []Block
	var cur *Block
	var raw string
	flush := func() {
		if cur != nil {
			cur.Spans = inline(raw)
			blocks = append(blocks, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			flush()
			continue
		}
		body := strings.TrimLeft(line, " \t")
		indent := 0
		for _, c := range line[:len(line)-len(body)] {
			if c == '\t' {
				indent += 4
			} else {
				indent++
			}
		}
		if indent == 0 {
			if level, rest, ok := heading(body); ok {
				flush()
				blocks = append(blocks, Block{Kind: Heading, Level: level, Spans: inline(rest)})
				continue
			}
		}
		if ordered, number, rest, ok := listItem(body); ok && indent <= 4 {
			flush()
			cur, raw = &Block{Kind: Item, Level: min(indent/2, 1), Ordered: ordered, Number: number}, rest
			continue
		}
		if cur != nil && (cur.Kind == Paragraph || indent >= 2) {
			raw += " " + body
			continue
		}
		flush()
		cur, raw = &Block{Kind: Paragraph}, body
	}
	flush()
	return blocks
}

// PlainText returns the text of a body without its marks: one line per block, links as their text.
func PlainText(text string) string {
	var lines []string
	for _, b := range Blocks(text) {
		var sb strings.Builder
		for _, s := range b.Spans {
			sb.WriteString(s.Text)
		}
		lines = append(lines, sb.String())
	}
	return strings.Join(lines, "\n")
}

func heading(line string) (level int, rest string, ok bool) {
	for level = 1; level <= 3; level++ {
		if after, found := strings.CutPrefix(line, strings.Repeat("#", level)+" "); found {
			rest = strings.TrimSpace(after)
			return level, rest, rest != ""
		}
	}
	return 0, "", false
}

func listItem(line string) (ordered bool, number int, rest string, ok bool) {
	if after, found := strings.CutPrefix(line, "- "); found {
		rest = strings.TrimSpace(after)
		return false, 0, rest, rest != ""
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	// nine digits cannot overflow an int
	if i == 0 || i > 9 || !strings.HasPrefix(line[i:], ". ") {
		return false, 0, "", false
	}
	number, _ = strconv.Atoi(line[:i])
	rest = strings.TrimSpace(line[i+2:])
	return true, number, rest, rest != ""
}

func inline(s string) []Span {
	var spans []Span
	var plain strings.Builder
	add := func(kind SpanKind, text, url string) {
		if plain.Len() > 0 {
			spans = append(spans, Span{Kind: Text, Text: plain.String()})
			plain.Reset()
		}
		spans = append(spans, Span{Kind: kind, Text: text, URL: url})
	}
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "**"):
			if j := strings.Index(s[i+2:], "**"); j > 0 {
				add(Bold, s[i+2:i+2+j], "")
				i += j + 4
				continue
			}
		case s[i] == '`':
			if j := strings.IndexByte(s[i+1:], '`'); j > 0 {
				add(Code, s[i+1:i+1+j], "")
				i += j + 2
				continue
			}
		case s[i] == '[':
			if j := i + strings.IndexByte(s[i:], ']'); j > i+1 && strings.HasPrefix(s[j+1:], "(") {
				if k := strings.IndexByte(s[j+2:], ')'); k >= 0 {
					add(Link, s[i+1:j], s[j+2:j+2+k])
					i = j + 3 + k
					continue
				}
			}
		case s[i] == '!' && strings.HasPrefix(s[i+1:], "["):
			// an image is not supported and must not turn into a link
			plain.WriteString("![")
			i += 2
			continue
		}
		plain.WriteByte(s[i])
		i++
	}
	if plain.Len() > 0 {
		spans = append(spans, Span{Kind: Text, Text: plain.String()})
	}
	return spans
}

// Placeholders returns the names of the placeholders {name} in text, in order and with repeats. Its
// grammar is that of the placeholders the terminal package resolves.
func Placeholders(text string) []string {
	var names []string
	for {
		open := strings.IndexByte(text, '{')
		if open < 0 {
			return names
		}
		end := strings.IndexByte(text[open+1:], '}')
		if end < 0 {
			return names
		}
		name := text[open+1 : open+1+end]
		if !isPlaceholderName(name) {
			text = text[open+1:]
			continue
		}
		names = append(names, name)
		text = text[open+end+2:]
	}
}

func isPlaceholderName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}
