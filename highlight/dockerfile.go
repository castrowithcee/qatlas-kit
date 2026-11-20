package highlight

import (
	"strings"

	"github.com/castrowithcee/qatlas-kit/tui"
)

var dockerInstructions = [...]string{"from", "run", "cmd", "label", "maintainer", "expose", "env", "add", "copy",
	"entrypoint", "volume", "user", "workdir", "arg", "onbuild", "stopsignal", "healthcheck", "shell"}

func dockerInstruction(w string) (string, bool) {
	for _, in := range dockerInstructions {
		if strings.EqualFold(w, in) {
			return in, true
		}
	}
	return "", false
}

// lexDockerfile marks the instruction at the start of each logical line, comment lines, flags, the names of
// ENV, ARG and LABEL, the AS of FROM, quoted strings and numbers. A line that ends in a backslash continues
// the instruction. A comment only counts as one on a line of its own. A here-document is not followed.
func lexDockerfile(line string, st *lineState, e *emitter) {
	i := skipSpace(line, 0)
	if i >= len(line) {
		return
	}
	if line[i] == '#' {
		e.comment(i)
		return
	}
	continued := st.cont
	st.cont = strings.HasSuffix(line[:trimEnd(line, 0, len(line))], `\`)
	if !continued {
		st.instr = ""
	}
	args := 0
	if !continued {
		i = dockerHead(line, i, st, e)
	}
	for i < len(line) {
		i = skipSpace(line, i)
		if i >= len(line) {
			return
		}
		c := line[i]
		if c == '"' || c == '\'' {
			end, _ := closeQuote(line, i+1, c, c == '"', false)
			e.add(i, end, tui.TextRoleString)
			i = end
			args++
			continue
		}
		j := i
		for j < len(line) && !isSpace(line[j]) && line[j] != '"' && line[j] != '\'' {
			j++
		}
		word := line[i:j]
		eq := strings.IndexByte(word, '=')
		switch {
		case strings.HasPrefix(word, "--"):
			if eq > 0 {
				e.add(i, i+eq, tui.TextRoleKey)
			} else {
				e.add(i, j, tui.TextRoleKey)
			}
		case (st.instr == "env" || st.instr == "arg" || st.instr == "label") && (eq > 0 || args == 0 && eq < 0):
			if eq < 0 {
				eq = len(word)
			}
			e.add(i, i+eq, tui.TextRoleKey)
			args++
		case st.instr == "from" && args > 0 && strings.EqualFold(word, "as"):
			e.add(i, j, tui.TextRoleKeyword)
			args++
		case st.instr == "healthcheck" && (word == "CMD" || word == "NONE"):
			e.add(i, j, tui.TextRoleKeyword)
			args++
		case isNumber(word):
			e.add(i, j, tui.TextRoleNumber)
			args++
		default:
			args++
		}
		i = j
	}
}

// dockerHead marks the instruction word at i, if it is one, and returns the index after it.
func dockerHead(line string, i int, st *lineState, e *emitter) int {
	j := i
	for j < len(line) && !isSpace(line[j]) {
		j++
	}
	in, ok := dockerInstruction(line[i:j])
	if !ok {
		return i
	}
	e.add(i, j, tui.TextRoleKeyword)
	st.instr = in
	if in == "onbuild" {
		k := skipSpace(line, j)
		m := k
		for m < len(line) && !isSpace(line[m]) {
			m++
		}
		if inner, ok := dockerInstruction(line[k:m]); ok && inner != "onbuild" {
			e.add(k, m, tui.TextRoleKeyword)
			st.instr = inner
			return m
		}
	}
	return j
}
