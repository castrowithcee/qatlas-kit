package highlight

import (
	"strings"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// lexEnv knows NAME=value lines with an optional export, comments, and quoted values that run over several
// lines until their closing quote. An unquoted value is a string, or a number if it reads as one. A quote
// that is never closed turns the rest of the text into a string, as dotenv readers do.
func lexEnv(line string, st *lineState, e *emitter) {
	if st.mode == modeQuote {
		end, closed := closeQuote(line, 0, st.quote, st.quote == '"', false)
		e.add(0, end, tui.TextRoleString)
		if !closed {
			return
		}
		st.mode = modeNone
		envTail(line, end, e)
		return
	}
	i := skipSpace(line, 0)
	if i >= len(line) {
		return
	}
	if line[i] == '#' {
		e.comment(i)
		return
	}
	if strings.HasPrefix(line[i:], "export") && i+6 < len(line) && isSpace(line[i+6]) {
		e.add(i, i+6, tui.TextRoleKeyword)
		i = skipSpace(line, i+7)
	}
	eq := strings.IndexByte(line[i:], '=')
	if eq < 0 {
		return
	}
	eq += i
	k := trimEnd(line, i, eq)
	if k == i || strings.ContainsAny(line[i:k], " \t") {
		return
	}
	e.add(i, k, tui.TextRoleKey)
	v := skipSpace(line, eq+1)
	if v >= len(line) {
		return
	}
	switch c := line[v]; {
	case c == '#' && v > eq+1:
		e.comment(v)
	case c == '"' || c == '\'':
		end, closed := closeQuote(line, v+1, c, c == '"', false)
		e.add(v, end, tui.TextRoleString)
		if !closed {
			st.mode, st.quote = modeQuote, c
			return
		}
		envTail(line, end, e)
	default:
		j := v
		for ; j < len(line); j++ {
			if line[j] == '#' && j > v && isSpace(line[j-1]) {
				break
			}
		}
		end := trimEnd(line, v, j)
		role := tui.TextRoleString
		if isNumber(line[v:end]) {
			role = tui.TextRoleNumber
		}
		e.add(v, end, role)
		if j < len(line) {
			e.comment(j)
		}
	}
}

// envTail marks a comment after a quoted value.
func envTail(line string, i int, e *emitter) {
	if j := skipSpace(line, i); j < len(line) && line[j] == '#' && j > i {
		e.comment(j)
	}
}
