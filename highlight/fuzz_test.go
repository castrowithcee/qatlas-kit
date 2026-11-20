package highlight

import (
	"strings"
	"testing"
)

// FuzzSpans checks for every format that nothing panics and that the sections keep the contract of Spans on
// any text. The seeds run in a normal test.
func FuzzSpans(f *testing.F) {
	seeds := []string{
		"---\nname: app # c\nl: [a, 1, \"b\"]\nm: {k: v}\nb: &x ~\nc: *x\nt: |\n  text: 1\n  # no\nu: >-\n  x\n'q': 'it''s'\n",
		"# c\nexport A=1\nB=\"two words\" # c\nC='x\nmore\n'\nD=hello world # note\n F = 3\nnot an assign\n",
		"{\n  \"a\": [1, -2.5e3, true, null],\n  \"b\" : \"x\\\"y\",\n  \"c\": {\"d\": false}\n}\n",
		"; c\n[sec]\nk = v\nn=3 ; note\nflag\nq = \"a b\"\nb = yes\nu: 1\n",
		"# c\n[a.b]\n[[arr]]\nk = \"v\" # c\nd = 1979-05-27T07:32:00Z\nl = [1, 2.5, \"x\"]\ni = {a = 1, b = 'z'}\nm = \"\"\"\nhello\n\"\"\"\n",
		"#!/bin/sh\nFOO=bar\nexport A=\"a b\" N=3\nif [ -f x ]; then echo 'hi' 42; fi\nfor f in a b; do echo $f; done\ncat <<-'EOF'\n\tbody\n\tEOF\nx=\"multi\nline\" y\ncase $x in\n a) :;;\nesac\n",
		"# syntax=docker/dockerfile:1\nFROM --platform=linux/amd64 golang:1.22 AS build\nENV A=1 B=\"x y\"\nRUN go build \\\n  -o /out \\\n  # c\n  ./...\nHEALTHCHECK --interval=30s CMD [\"curl\"]\nONBUILD RUN x\n",
		"ключ: значение # 注釈\r\n\x80x: \x80 1\r\n\xc3 \"é\"",
		strings.Repeat("\"'", 50), "\n\n", "",
	}
	for format := FormatNone; format <= formatCount; format++ {
		for _, seed := range seeds {
			f.Add(uint8(format), seed)
		}
	}
	f.Fuzz(func(t *testing.T, format uint8, text string) {
		checkSpans(t, Format(format), text)
		Detect(text, text)
	})
}
