package highlight

import (
	"strings"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// lexYAML knows block mappings and sequences, quoted and plain scalars, anchors, aliases and tags, flow
// collections on one line and literal and folded block scalars, whose lines it keeps as strings. A quoted
// scalar or plain scalar that continues on the next line is not followed.
func lexYAML(line string, st *lineState, e *emitter) {
	if st.mode == modeBlock {
		if strings.TrimLeft(line, " \t") == "" {
			return
		}
		ind := 0
		for ind < len(line) && line[ind] == ' ' {
			ind++
		}
		if ind > st.indent {
			e.add(ind, len(line), tui.TextRoleString)
			return
		}
		st.mode = modeNone
	}
	i := skipSpace(line, 0)
	if i >= len(line) {
		return
	}
	if line[i] == '#' {
		e.comment(i)
		return
	}
	if i == 0 && (strings.HasPrefix(line, "---") || strings.HasPrefix(line, "...")) &&
		(len(line) == 3 || isSpace(line[3])) {
		e.add(0, 3, tui.TextRoleKeyword)
		yamlValue(line, 3, i, st, e, false)
		return
	}
	parent := i
	for i < len(line) && line[i] == '-' && (i+1 == len(line) || isSpace(line[i+1])) {
		parent = i
		i = skipSpace(line, i+1)
	}
	if i >= len(line) {
		return
	}
	if end := yamlKeyEnd(line, i); end >= 0 {
		e.add(i, end, tui.TextRoleKey)
		yamlValue(line, end+1, i, st, e, false)
		return
	}
	yamlValue(line, i, parent, st, e, false)
}

// yamlKeyEnd returns where the key at i ends, the colon being there, or -1 if the line holds no key.
func yamlKeyEnd(line string, i int) int {
	switch line[i] {
	case '"', '\'':
		end, closed := closeQuote(line, i+1, line[i], line[i] == '"', line[i] == '\'')
		if closed && end < len(line) && line[end] == ':' && (end+1 == len(line) || isSpace(line[end+1])) {
			return end
		}
		return -1
	case '[', '{', '|', '>', '#', '*', '&', '!':
		return -1
	}
	for j := i; j < len(line); j++ {
		switch c := line[j]; {
		case c == ':' && (j+1 == len(line) || isSpace(line[j+1])):
			return trimEnd(line, i, j)
		case c == '#' && j > i && isSpace(line[j-1]):
			return -1
		}
	}
	return -1
}

func yamlValue(line string, i, parent int, st *lineState, e *emitter, flow bool) {
	first := true
	for {
		i = skipSpace(line, i)
		if i >= len(line) {
			return
		}
		c := line[i]
		if first && (c == '[' || c == '{') {
			flow = true
		}
		switch {
		case c == '#' && (i == 0 || isSpace(line[i-1])):
			e.comment(i)
			return
		case c == '"' || c == '\'':
			end, _ := closeQuote(line, i+1, c, c == '"', c == '\'')
			role := tui.TextRoleString
			if j := skipSpace(line, end); flow && j < len(line) && line[j] == ':' {
				role = tui.TextRoleKey
			}
			e.add(i, end, role)
			i, first = end, false
		case (c == '|' || c == '>') && first && !flow:
			j := i + 1
			for j < len(line) && (isDigit(line[j]) || line[j] == '+' || line[j] == '-') {
				j++
			}
			if k := skipSpace(line, j); k == len(line) || line[k] == '#' && k > j {
				e.add(i, j, tui.TextRoleKeyword)
				st.mode, st.indent = modeBlock, parent
				i = j
				continue
			}
			i = yamlPlain(line, i, e, flow)
			first = false
		case c == '&' || c == '*' || c == '!':
			j := i
			for j < len(line) && !isSpace(line[j]) && !(flow && strings.IndexByte(",]}", line[j]) >= 0) {
				j++
			}
			e.add(i, j, tui.TextRoleKeyword)
			i = j
		case flow && strings.IndexByte(",[]{}", c) >= 0:
			i++
			first = false
		default:
			i = yamlPlain(line, i, e, flow)
			first = false
		}
	}
}

// yamlPlain handles the plain scalar at i and returns where it ends.
func yamlPlain(line string, i int, e *emitter, flow bool) int {
	j := i
	for ; j < len(line); j++ {
		c := line[j]
		if c == '#' && j > i && isSpace(line[j-1]) {
			break
		}
		if flow && (strings.IndexByte(",[]{}", c) >= 0 || c == ':' && (j+1 == len(line) || isSpace(line[j+1]))) {
			break
		}
	}
	if j == i {
		return i + 1
	}
	end := trimEnd(line, i, j)
	word := line[i:end]
	switch {
	case flow && j < len(line) && line[j] == ':':
		e.add(i, end, tui.TextRoleKey)
	case isNumber(word):
		e.add(i, end, tui.TextRoleNumber)
	case word == "~" || isAnyFold(word, "true", "false", "null"):
		e.add(i, end, tui.TextRoleKeyword)
	}
	return j
}
