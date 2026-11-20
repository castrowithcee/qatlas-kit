package highlight

import "github.com/castrowithcee/qatlas-kit/tui"

// lexJSON works on one line at a time and keeps no state, because a JSON string never spans lines. A string
// followed by a colon on the same line is a key.
func lexJSON(line string, e *emitter) {
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == '"':
			end, _ := closeQuote(line, i+1, '"', true, false)
			role := tui.TextRoleString
			if j := skipSpace(line, end); j < len(line) && line[j] == ':' {
				role = tui.TextRoleKey
			}
			e.add(i, end, role)
			i = end
		case c == '-' || isDigit(c):
			j := i + 1
			for j < len(line) && (isDigit(line[j]) || line[j] == '.' || line[j] == 'e' || line[j] == 'E' ||
				line[j] == '+' || line[j] == '-') {
				j++
			}
			if isNumber(line[i:j]) {
				e.add(i, j, tui.TextRoleNumber)
			}
			i = j
		case isWordByte(c):
			j := i + 1
			for j < len(line) && isWordByte(line[j]) {
				j++
			}
			if w := line[i:j]; w == "true" || w == "false" || w == "null" {
				e.add(i, j, tui.TextRoleKeyword)
			}
			i = j
		default:
			i++
		}
	}
}
