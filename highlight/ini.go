package highlight

import (
	"strings"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// lexINI reads INI and, with toml set, TOML. A section header is a keyword. In INI, ';' and '#' start a
// comment at the start of a line or after a space, a value is one unit and is a number or keyword only if it
// is nothing else, and a key may stand alone. In TOML '#' starts a comment outside a string, the elements of
// arrays and inline tables are read one by one, dates count as numbers and a multi-line string is followed to
// its end. Nested keys of an inline table are not marked.
func lexINI(line string, st *lineState, e *emitter, toml bool) {
	if st.mode == modeQuote {
		end := strings.Index(line, st.delim)
		if end < 0 {
			e.add(0, len(line), tui.TextRoleString)
			return
		}
		e.add(0, end+3, tui.TextRoleString)
		st.mode = modeNone
		iniValue(line, end+3, st, e, toml)
		return
	}
	i := skipSpace(line, 0)
	if i >= len(line) {
		return
	}
	if c := line[i]; c == '#' || c == ';' && !toml {
		e.comment(i)
		return
	}
	if line[i] == '[' {
		end := strings.IndexByte(line[i:], ']')
		if end < 0 {
			end = len(line)
		} else {
			end += i + 1
			if toml && line[i+1:i+2] == "[" && end < len(line) && line[end] == ']' {
				end++
			}
		}
		e.add(i, end, tui.TextRoleKeyword)
		if j := skipSpace(line, end); j < len(line) && (line[j] == '#' || line[j] == ';' && !toml) {
			e.comment(j)
		}
		return
	}
	keyEnd, sep := i, -1
	if c := line[i]; c == '"' || c == '\'' {
		keyEnd, _ = closeQuote(line, i+1, c, c == '"' && toml, false)
		if j := skipSpace(line, keyEnd); j < len(line) && (line[j] == '=' || line[j] == ':' && !toml) {
			sep = j
		}
	} else {
		for j := i; j < len(line); j++ {
			if line[j] == '=' || line[j] == ':' && !toml {
				sep = j
				keyEnd = trimEnd(line, i, j)
				break
			}
		}
		if sep < 0 {
			keyEnd = trimEnd(line, i, len(line))
			if !toml {
				e.add(i, keyEnd, tui.TextRoleKey)
			}
			return
		}
	}
	if sep < 0 {
		return
	}
	e.add(i, keyEnd, tui.TextRoleKey)
	iniValue(line, sep+1, st, e, toml)
}

func iniValue(line string, i int, st *lineState, e *emitter, toml bool) {
	start := i
	for {
		i = skipSpace(line, i)
		if i >= len(line) {
			return
		}
		c := line[i]
		spaceBefore := i == 0 || isSpace(line[i-1])
		switch {
		case c == '#' && (toml || spaceBefore && i > start), c == ';' && !toml && spaceBefore && i > start:
			e.comment(i)
			return
		case toml && (strings.HasPrefix(line[i:], `"""`) || strings.HasPrefix(line[i:], `'''`)):
			delim := line[i : i+3]
			end := strings.Index(line[i+3:], delim)
			if end < 0 {
				e.add(i, len(line), tui.TextRoleString)
				st.mode, st.delim = modeQuote, delim
				return
			}
			end += i + 6
			e.add(i, end, tui.TextRoleString)
			i = end
		case c == '"' || c == '\'':
			end, _ := closeQuote(line, i+1, c, c == '"' && toml, false)
			e.add(i, end, tui.TextRoleString)
			i = end
		case toml && strings.IndexByte(",[]{}=", c) >= 0:
			i++
		default:
			j := i
			for ; j < len(line); j++ {
				ch := line[j]
				if toml && (isSpace(ch) || strings.IndexByte(",[]{}=#", ch) >= 0) {
					break
				}
				if !toml && (ch == '#' || ch == ';') && j > i && isSpace(line[j-1]) {
					break
				}
			}
			end := j
			if !toml {
				end = trimEnd(line, i, j)
			}
			word := line[i:end]
			switch {
			case isNumber(word) || toml && isDate(word):
				e.add(i, end, tui.TextRoleNumber)
			case toml && (word == "true" || word == "false" || word == "inf" || word == "nan"),
				!toml && isAnyFold(word, "true", "false", "yes", "no", "on", "off"):
				e.add(i, end, tui.TextRoleKeyword)
			}
			i = j
		}
	}
}

// isDate reports whether s looks like a TOML date, time or date-time.
func isDate(s string) bool {
	if len(s) < 8 || !isDigit(s[0]) {
		return false
	}
	seps := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case isDigit(c) || c == '.':
		case c == '-' || c == ':':
			seps++
		case c == 'T' || c == 'Z' || c == '+':
		default:
			return false
		}
	}
	return seps >= 2
}
