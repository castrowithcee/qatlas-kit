package highlight

import (
	"strings"

	"github.com/castrowithcee/qatlas-kit/tui"
)

const shellOperators = ";&|()<>"

// lexShell marks comments, quoted strings, reserved words and builtins in command position, the names of
// assignments, numbers, here-document bodies and the strings that run over several lines. It does not expand
// anything: a quote inside "$(...)" is read as part of the string, and only the first here-document of a line
// is followed.
func lexShell(line string, st *lineState, e *emitter) {
	i, cmd := 0, true
	switch st.mode {
	case modeHeredoc:
		body := line
		if st.strip {
			body = strings.TrimLeft(line, "\t")
		}
		if body == st.delim {
			e.add(len(line)-len(body), len(line), tui.TextRoleKeyword)
			st.mode, st.delim = modeNone, ""
			return
		}
		e.add(0, len(line), tui.TextRoleString)
		return
	case modeQuote:
		end, closed := closeQuote(line, 0, st.quote, st.quote == '"', false)
		e.add(0, end, tui.TextRoleString)
		if !closed {
			return
		}
		st.mode = modeNone
		i, cmd = end, false
	}
	expectIn := 0
	for i < len(line) {
		c := line[i]
		switch {
		case isSpace(c):
			i++
			continue
		case c == '#':
			e.comment(i)
			return
		case strings.HasPrefix(line[i:], "<<") && !strings.HasPrefix(line[i:], "<<<") &&
			(i == 0 || line[i-1] != '<') && st.mode != modeHeredoc:
			i = shellHeredoc(line, i, st, e)
			continue
		case strings.IndexByte(shellOperators, c) >= 0:
			i++
			cmd = true
			if c == '<' || c == '>' {
				cmd = false
			}
			continue
		}
		start := i
		assigned := false
		if cmd {
			if j := assignmentEnd(line, i); j > i {
				e.add(i, j, tui.TextRoleKey)
				assigned = true
			}
		}
		quoted := false
		for i < len(line) {
			ch := line[i]
			if isSpace(ch) || strings.IndexByte(shellOperators, ch) >= 0 {
				break
			}
			if ch == '\\' {
				i = min(i+2, len(line))
				continue
			}
			if ch == '\'' || ch == '"' {
				quoted = true
				end, closed := closeQuote(line, i+1, ch, ch == '"', false)
				e.add(i, end, tui.TextRoleString)
				i = end
				if !closed {
					if st.mode != modeHeredoc {
						st.mode, st.quote = modeQuote, ch
					}
					return
				}
				continue
			}
			i++
		}
		word := line[start:i]
		switch {
		case assigned:
			// A command or another assignment may follow.
		case quoted:
			cmd = false
		case expectIn > 0:
			if expectIn == 1 && word == "in" {
				e.add(start, i, tui.TextRoleKeyword)
				expectIn = 0
			} else {
				expectIn--
			}
			cmd = false
		case cmd && shellKeyword(word) != kwNone:
			e.add(start, i, tui.TextRoleKeyword)
			switch shellKeyword(word) {
			case kwHead:
				expectIn, cmd = 2, false
			case kwEnd, kwFunction:
				cmd = false
			}
		case isNumber(word):
			e.add(start, i, tui.TextRoleNumber)
			cmd = false
		default:
			cmd = false
		}
	}
}

type keywordKind uint8

const (
	kwNone keywordKind = iota
	kwCommand
	kwHead
	kwEnd
	kwFunction
)

// shellKeyword classifies a word in command position: kwCommand is a reserved word or builtin that a command
// or an assignment may follow, kwHead opens a "for x in" or "case x in" head, kwEnd closes a construct.
func shellKeyword(w string) keywordKind {
	switch w {
	case "if", "then", "else", "elif", "while", "until", "do", "time", "!", "{", "[[", "export", "local",
		"readonly", "declare", "typeset", "unset", "return", "break", "continue", "exit":
		return kwCommand
	case "for", "select", "case":
		return kwHead
	case "fi", "done", "esac", "}", "]]":
		return kwEnd
	case "function":
		return kwFunction
	}
	return kwNone
}

// assignmentEnd returns the end of NAME in NAME= or NAME+= at i, or i if the word is not an assignment.
func assignmentEnd(line string, i int) int {
	j := i
	for j < len(line) && isWordByte(line[j]) {
		j++
	}
	if j == i || isDigit(line[i]) || j >= len(line) {
		return i
	}
	if line[j] == '=' || line[j] == '+' && j+1 < len(line) && line[j+1] == '=' {
		return j
	}
	return i
}

// shellHeredoc reads the introducer at i and starts the here-document if it names a delimiter. It returns
// the index to go on from.
func shellHeredoc(line string, i int, st *lineState, e *emitter) int {
	j := i + 2
	strip := j < len(line) && line[j] == '-'
	if strip {
		j++
	}
	j = skipSpace(line, j)
	if j >= len(line) {
		return i + 2
	}
	var delim string
	end := j
	switch c := line[j]; c {
	case '\'', '"':
		n := strings.IndexByte(line[j+1:], c)
		if n < 0 {
			return i + 2
		}
		delim, end = line[j+1:j+1+n], j+n+2
	default:
		if c == '\\' {
			j++
		}
		end = j
		for end < len(line) && !isSpace(line[end]) && strings.IndexByte(shellOperators, line[end]) < 0 {
			end++
		}
		delim = line[j:end]
	}
	if delim == "" {
		return i + 2
	}
	e.add(j, end, tui.TextRoleKeyword)
	st.mode, st.delim, st.strip = modeHeredoc, delim, strip
	return end
}
