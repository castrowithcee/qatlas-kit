package highlight

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/tui"
)

var roleTag = map[tui.TextRole]string{
	tui.TextRoleKey: "K", tui.TextRoleString: "S", tui.TextRoleNumber: "N",
	tui.TextRoleComment: "C", tui.TextRoleKeyword: "W", tui.TextRolePlain: "P",
}

// describe writes the sections of every line as TAG(text), the lines joined by a newline.
func describe(text string, spans [][]tui.TextSpan) string {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		var parts []string
		if i < len(spans) {
			for _, s := range spans[i] {
				parts = append(parts, roleTag[s.Role]+"("+l[s.Start:s.End]+")")
			}
		}
		out[i] = strings.Join(parts, " ")
	}
	return strings.Join(out, "\n")
}

type lexCase struct {
	name, in, want string
}

func runCases(t *testing.T, f Format, cases []lexCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := describe(c.in, Spans(f, c.in))
			if got != c.want {
				t.Errorf("input:\n%s\ngot:\n%s\nwant:\n%s", c.in, got, c.want)
			}
		})
	}
}

// checkSpans verifies the contract of Spans on text: the entries match the lines, and each line's sections
// are ascending, apart, inside the line, on character boundaries, with a role and unmasked.
func checkSpans(t *testing.T, format Format, text string) {
	t.Helper()
	spans := Spans(format, text)
	norm := strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := strings.Split(norm, "\n")
	if len(spans) > len(lines) {
		t.Fatalf("%v: %d entries for %d lines", format, len(spans), len(lines))
	}
	for i, ss := range spans {
		line, last := lines[i], 0
		for _, s := range ss {
			switch {
			case s.Start < last || s.End <= s.Start || s.End > len(line):
				t.Fatalf("%v: line %d %q: bad span %+v after %d", format, i, line, s, last)
			case !boundary(line, s.Start) || !boundary(line, s.End):
				t.Fatalf("%v: line %d %q: span %+v is not on a character boundary", format, i, line, s)
			case s.Role == tui.TextRolePlain || s.Role > tui.TextRoleKeyword || s.Masked:
				t.Fatalf("%v: line %d: bad role or mask in %+v", format, i, s)
			}
			last = s.End
		}
	}
}
