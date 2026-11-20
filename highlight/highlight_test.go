package highlight

import (
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/tui"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		name, text string
		want       Format
	}{
		{"compose.yaml", "", FormatYAML},
		{"/srv/app/docker-compose.YML", "", FormatYAML},
		{`C:\app\data.json`, "", FormatJSON},
		{"app.ini", "", FormatINI},
		{"setup.cfg", "", FormatINI},
		{"config.toml", "", FormatTOML},
		{".env", "", FormatEnv},
		{".env.local", "", FormatEnv},
		{"prod.env", "", FormatEnv},
		{"/app/.envrc", "", FormatShell},
		{"run.sh", "", FormatShell},
		{"x.bash", "", FormatShell},
		{".bashrc", "", FormatShell},
		{"Dockerfile", "", FormatDockerfile},
		{"dockerfile.dev", "", FormatDockerfile},
		{"app.dockerfile", "", FormatDockerfile},
		{"Containerfile", "", FormatDockerfile},
		{"deploy", "#!/bin/bash\necho hi\n", FormatShell},
		{"deploy", "#!/usr/bin/env -S bash -eu\n", FormatShell},
		{"deploy", "#! /bin/sh\n", FormatShell},
		{"deploy", "#!/usr/bin/env python3\n", FormatNone},
		{"deploy", "#!\n", FormatNone},
		{"deploy", "#!/usr/bin/env\n", FormatNone},
		{"notes.txt", "key: value\n", FormatNone},
		{"data.json.bak", "{}", FormatNone},
		{"", "", FormatNone},
		{"run.py", "#!/bin/sh\n", FormatShell}, // the shebang decides where the name does not
	}
	for _, c := range cases {
		if got := Detect(c.name, c.text); got != c.want {
			t.Errorf("Detect(%q, %q) = %v, want %v", c.name, c.text, got, c.want)
		}
	}
	if Highlight("a.json", `{"a": 1}`) == nil {
		t.Error("Highlight found no sections in JSON")
	}
	if Highlight("a.txt", `{"a": 1}`) != nil {
		t.Error("Highlight found sections in an unknown format")
	}
}

func TestFormatNames(t *testing.T) {
	seen := map[string]Format{}
	for f := FormatNone; f < formatCount; f++ {
		n := f.String()
		if other, dup := seen[n]; dup {
			t.Errorf("%v and %v share the name %q", f, other, n)
		}
		seen[n] = f
	}
	if formatCount.String() != "none" {
		t.Error("a format outside the list is not none")
	}
}

func TestYAML(t *testing.T) {
	runCases(t, FormatYAML, []lexCase{
		{"key and plain value", "name: web", "K(name)"},
		{"comment", "name: web # note\n# whole", "K(name) C(# note)\nC(# whole)"},
		{"hash inside a plain value", "url: http://h/a#b", "K(url)"},
		{"quoted values", `a: "x: y"` + "\n" + `b: 'it''s'`, `K(a) S("x: y")` + "\n" + `K(b) S('it''s')`},
		{"escaped quote", `a: "x\"y" # c`, `K(a) S("x\"y") C(# c)`},
		{"quoted key", `"a b": 1`, `K("a b") N(1)`},
		{"numbers and keywords", "a: 1\nb: -2.5e3\nc: 0x1F\nd: true\ne: Null\nf: ~\ng: 1.2.3", "K(a) N(1)\nK(b) N(-2.5e3)\nK(c) N(0x1F)\nK(d) W(true)\nK(e) W(Null)\nK(f) W(~)\nK(g)"},
		{"sequence", "- a\n- 3\n- \"s\"\n  - x: 1", "\nN(3)\nS(\"s\")\nK(x) N(1)"},
		{"document markers", "---\na: 1\n...", "W(---)\nK(a) N(1)\nW(...)"},
		{"marker with value", "--- !tag\na: 1", "W(---) W(!tag)\nK(a) N(1)"},
		{"anchor alias", "a: &x 1\nb: *x\nc: !!str 5", "K(a) W(&x) N(1)\nK(b) W(*x)\nK(c) W(!!str) N(5)"},
		{"flow", `l: [a, 1, "b", true]` + "\n" + `m: {k: v, n: null}`, `K(l) N(1) S("b") W(true)` + "\n" + `K(m) K(k) K(n) W(null)`},
		{"literal block", "a: |\n  x: 1\n  # no\n\n  y\nb: 2", "K(a) W(|)\nS(x: 1)\nS(# no)\n\nS(y)\nK(b) N(2)"},
		{"folded block with indicators", "a: >-2\n   text\nb: 1", "K(a) W(>-2)\nS(text)\nK(b) N(1)"},
		{"block in sequence", "- |\n  text\n- x", "W(|)\nS(text)\n"},
		{"block in sequence mapping", "- k: |\n    text\n  o: 1\n", "K(k) W(|)\nS(text)\nK(o) N(1)"},
		{"nested block ends on dedent", "a:\n  b: |\n    t\n  c: 1", "K(a)\nK(b) W(|)\nS(t)\nK(c) N(1)"},
		{"block marker is not a block", "a: |x\nb: 1", "K(a)\nK(b) N(1)"},
		{"colon without space", "a:b: c", "K(a:b)"},
		{"empty and blank", "\n  \na:", "\n\nK(a)"},
		{"unterminated quote", `a: "open`, `K(a) S("open)`},
	})
}

func TestEnv(t *testing.T) {
	runCases(t, FormatEnv, []lexCase{
		{"plain", "A=1\nB=hello world\nC=", "K(A) N(1)\nK(B) S(hello world)\nK(C)"},
		{"comment", "# c\n  # d\nA=1 # e\nB=x#y\nC= # f", "C(# c)\nC(# d)\nK(A) N(1) C(# e)\nK(B) S(x#y)\nK(C) C(# f)"},
		{"export", "export A=1\nexportA=2", "W(export) K(A) N(1)\nK(exportA) N(2)"},
		{"spaces around the name", " F = 3", "K(F) N(3)"},
		{"not an assignment", "just text\na b=c\n=x", "\n\n"},
		{"quotes", `A="two words" # c` + "\n" + `B='it "is"'` + "\n" + `C="a\"b"`, `K(A) S("two words") C(# c)` + "\n" + `K(B) S('it "is"')` + "\n" + `K(C) S("a\"b")`},
		{"quote inside an unquoted value", `A=it's`, `K(A) S(it's)`},
		{"multi-line quote", "A=\"one\ntwo # no\nthree\" # c\nB=1", "K(A) S(\"one)\nS(two # no)\nS(three\") C(# c)\nK(B) N(1)"},
		{"multi-line single quote has no escapes", "A='x\\\n'\nB=2", "K(A) S('x\\)\nS(')\nK(B) N(2)"},
		{"unclosed quote takes the rest", "A=\"x\nB=1", "K(A) S(\"x)\nS(B=1)"},
	})
}

func TestJSON(t *testing.T) {
	runCases(t, FormatJSON, []lexCase{
		{"object", "{\n  \"a\": 1,\n  \"b\" : \"x\"\n}", "\nK(\"a\") N(1)\nK(\"b\") S(\"x\")\n"},
		{"array and keywords", `[1, -2.5e3, true, false, null, "s"]`, `N(1) N(-2.5e3) W(true) W(false) W(null) S("s")`},
		{"escape", `{"a\"b": "c\\"}`, `K("a\"b") S("c\\")`},
		{"bare words are not keywords", `[truth, nullable, 12ab]`, `N(12)`},
		{"bad number", `[1.2.3, -, 1e]`, ``},
		{"unterminated string", `{"a": "x`, `K("a") S("x)`},
		{"string with colon inside", `["a: b"]`, `S("a: b")`},
	})
}

func TestINI(t *testing.T) {
	runCases(t, FormatINI, []lexCase{
		{"sections and keys", "[main]\nk = v\nn=3", "W([main])\nK(k)\nK(n) N(3)"},
		{"comments", "; a\n# b\nk = v ; c\nu = http://h/#f\nw = x # d", "C(; a)\nC(# b)\nK(k) C(; c)\nK(u)\nK(w) C(# d)"},
		{"keywords", "a = yes\nb = Off\nc = true\nd = say yes", "K(a) W(yes)\nK(b) W(Off)\nK(c) W(true)\nK(d)"},
		{"quoted value", `k = "a ; b" ; c`, `K(k) S("a ; b") C(; c)`},
		{"colon separator", "k: 1", "K(k) N(1)"},
		{"key alone", "flag", "K(flag)"},
		{"section with comment", "[a] ; c", "W([a]) C(; c)"},
		{"unclosed section", "[a", "W([a)"},
		{"quoted key", `"a b" = 1`, `K("a b") N(1)`},
		{"no multi-line strings", `k = """` + "\nx", `K(k) S("") S(")` + "\n" + `K(x)`},
	})
}

func TestTOML(t *testing.T) {
	runCases(t, FormatTOML, []lexCase{
		{"tables", "[a.b]\n[[arr]]\n[ x ] # c", "W([a.b])\nW([[arr]])\nW([ x ]) C(# c)"},
		{"values", "n = 1_000\nf = 2.5\nt = true\nd = 1979-05-27T07:32:00Z\ns = \"v\" # c\nl = 'lit'", "K(n) N(1_000)\nK(f) N(2.5)\nK(t) W(true)\nK(d) N(1979-05-27T07:32:00Z)\nK(s) S(\"v\") C(# c)\nK(l) S('lit')"},
		{"array and inline table", "a = [1, 2.5, \"x\", false]\nb = {k = 1, j = 'z'}", "K(a) N(1) N(2.5) S(\"x\") W(false)\nK(b) N(1) S('z')"},
		{"hash in string", `a = "x # y"`, `K(a) S("x # y")`},
		{"escape", `a = "x\"y"`, `K(a) S("x\"y")`},
		{"literal string has no escape", `a = 'x\' # c`, `K(a) S('x\') C(# c)`},
		{"dotted and quoted keys", "a.b = 1\n\"q k\" = 2", "K(a.b) N(1)\nK(\"q k\") N(2)"},
		{"multi-line string", "m = \"\"\"\nhello # no\n\"\"\" # c\nn = 1", "K(m) S(\"\"\")\nS(hello # no)\nS(\"\"\") C(# c)\nK(n) N(1)"},
		{"multi-line literal", "m = '''x\ny'''\nn = 1", "K(m) S('''x)\nS(y''')\nK(n) N(1)"},
		{"single-line triple quote", `m = """a""" # c`, `K(m) S("""a""") C(# c)`},
		{"semicolon is no comment", "a = x;y", "K(a)"},
		{"bare key without value", "flag", ""},
	})
}

func TestShell(t *testing.T) {
	runCases(t, FormatShell, []lexCase{
		{"comment and shebang", "#!/bin/sh\necho hi # c", "C(#!/bin/sh)\nC(# c)"},
		{"assignments", "FOO=bar\nexport A=\"x y\" N=3\nB+=1 cmd", "K(FOO)\nW(export) K(A) S(\"x y\") K(N)\nK(B)"},
		{"strings", `echo 'a b' "c $d" e"f"g`, `S('a b') S("c $d") S("f")`},
		{"escaped quote", `echo \' "a\"b"`, `S("a\"b")`},
		{"numbers", "sleep 5\nexit 0\necho 1x", "N(5)\nW(exit) N(0)\n"},
		{"if", "if [ -f x ]; then echo 'hi'; fi", "W(if) W(then) S('hi') W(fi)"},
		{"keywords only in command position", "echo if then done", ""},
		{"for loop", "for f in a b; do\n  echo $f\ndone", "W(for) W(in) W(do)\n\nW(done)"},
		{"case", "case $x in\n  a) echo;;\nesac", "W(case) W(in)\n\nW(esac)"},
		{"in outside a head", "echo in", ""},
		{"pipes and lists", "a | b && c || d; e & f", ""},
		{"here-document", "cat <<EOF\nhello 'x\n# no\nEOF\necho 1", "W(EOF)\nS(hello 'x)\nS(# no)\nW(EOF)\nN(1)"},
		{"here-document quoted and stripped", "cat <<-'END'\n\tbody\n\tEND\nx", "W('END')\nS(\tbody)\nW(END)\n"},
		{"here-document delimiter must match the whole line", "cat <<EOF\nEOF \nEOFX\nEOF", "W(EOF)\nS(EOF )\nS(EOFX)\nW(EOF)"},
		{"here-string is no here-document", "cat <<< word\necho 1", "\nN(1)"},
		{"only the first here-document", "cat <<A <<B\nx\nA\ny", "W(A)\nS(x)\nW(A)\n"},
		{"multi-line string", "x=\"a\nb # c\nd\" y\nz=1", "K(x) S(\"a)\nS(b # c)\nS(d\")\nK(z)"},
		{"multi-line single quote", "echo 'a\n$b\n' c", "S('a)\nS($b)\nS(')"},
		{"here-document without a delimiter", "cat <<\necho", "\n"},
		{"function", "function f { echo; }\nf() { :; }", "W(function) W(})\nW({) W(})"},
	})
}

func TestDockerfile(t *testing.T) {
	runCases(t, FormatDockerfile, []lexCase{
		{"comment and directive", "# syntax=docker/dockerfile:1\n  # c", "C(# syntax=docker/dockerfile:1)\nC(# c)"},
		{"from", "FROM --platform=linux/amd64 golang:1.22 AS build\nfrom x as y", "W(FROM) K(--platform) W(AS)\nW(from) W(as)"},
		{"env", "ENV A=1 B=\"x y\"\nENV C d\nARG V=2\nLABEL k=v", "W(ENV) K(A) K(B) S(\"x y\")\nW(ENV) K(C)\nW(ARG) K(V)\nW(LABEL) K(k)"},
		{"run with continuation", "RUN go build \\\n  -o /out \\\n  # a comment\n  ./...\nEXPOSE 8080", "W(RUN)\n\nC(# a comment)\n\nW(EXPOSE) N(8080)"},
		{"continuation does not start an instruction", "RUN a \\\n  run b", "W(RUN)\n"},
		{"json form", `CMD ["curl", "-f"]`, `W(CMD) S("curl") S("-f")`},
		{"healthcheck", "HEALTHCHECK --interval=30s CMD curl -f x", "W(HEALTHCHECK) K(--interval) W(CMD)"},
		{"onbuild", "ONBUILD RUN x\nonbuild", "W(ONBUILD) W(RUN)\nW(onbuild)"},
		{"copy flags", "COPY --from=build --chown=1:1 /a /b", "W(COPY) K(--from) K(--chown)"},
		{"hash is no comment inside an instruction", "RUN echo a # b", "W(RUN)"},
		{"unknown word", "FROMAGE x", ""},
		{"blank line keeps the continuation", "RUN a \\\n\nrun b", "W(RUN)\n\n"},
	})
}

func TestUnknownAndEmptyHaveNoSections(t *testing.T) {
	for _, f := range []Format{FormatNone, formatCount, 200} {
		if got := Spans(f, "a: 1\n"); got != nil {
			t.Errorf("Spans(%v) = %v", f, got)
		}
	}
	for f := FormatYAML; f < formatCount; f++ {
		for _, text := range []string{"", "\n", "\r\n"} {
			if got := Spans(f, text); got != nil {
				t.Errorf("Spans(%v, %q) = %v", f, text, got)
			}
		}
	}
}

func TestBinaryContentHasNoSections(t *testing.T) {
	valid := strings.Repeat("a: 1\n", 100)
	cases := []struct {
		name, text string
		binary     bool
	}{
		{"text", valid, false},
		{"NUL at the start", "\x00" + valid, true},
		{"NUL at the end", valid + "\x00", true},
		{"NUL after the probe", strings.Repeat("a", binaryProbe+10) + "\x00", true},
		{"valid non-ASCII", strings.Repeat("ä: ö\n", 100), false},
		{"a few damaged bytes", valid + "\xff\xfe", false},
		{"mostly damaged", strings.Repeat("\xff\xfe\xfd", 100) + "a: 1", true},
		{"just over a tenth", strings.Repeat("a", 90) + strings.Repeat("\xff", 11), true},
		{"just under a tenth", strings.Repeat("a", 90) + strings.Repeat("\xff", 9), false},
		{"long, all damaged", strings.Repeat("\xff", binaryProbe+100), true},
		{"long, mostly damaged", strings.Repeat("\xff\xfe\xfd\xfc", binaryProbe/4) + strings.Repeat("a", 100), true},
		{"long, just over a tenth", strings.Repeat("a", binaryProbe-binaryProbe/10-1) + strings.Repeat("\xff", binaryProbe/10+1) + strings.Repeat("a", 100), true},
		{"long, just under a tenth", strings.Repeat("a", binaryProbe-binaryProbe/10) + strings.Repeat("\xff", binaryProbe/10) + strings.Repeat("a", 100), false},
	}
	for _, c := range cases {
		if got := binary(c.text); got != c.binary {
			t.Errorf("%s: binary = %v, want %v", c.name, got, c.binary)
		}
		if got := Spans(FormatYAML, c.text); (got == nil) != c.binary {
			t.Errorf("%s: spans nil = %v, want %v", c.name, got == nil, c.binary)
		}
	}
	// A multi-byte character cut by the end of the probe is not damage.
	cut := strings.Repeat("a", binaryProbe-1) + "ä" + strings.Repeat("a", 10)
	if binary(cut) {
		t.Error("a character cut by the probe counts as damage")
	}
}

func TestLongLinesHaveNoSectionsAndKeepTheState(t *testing.T) {
	long := strings.Repeat("x", MaxLineBytes+1)
	got := Spans(FormatYAML, "a: 1\nb: "+long+"\nc: 2")
	if len(got) != 3 || len(got[0]) != 2 || got[1] != nil || len(got[2]) != 2 {
		t.Errorf("sections = %v", got)
	}
	// A line of exactly the limit is still read.
	exact := "k: " + strings.Repeat("y", MaxLineBytes-3)
	if len(exact) != MaxLineBytes || len(Spans(FormatYAML, exact)[0]) != 1 {
		t.Error("a line of exactly MaxLineBytes is not read")
	}
	// The long line does not leave a block scalar or here-document.
	block := Spans(FormatYAML, "a: |\n  "+long+"\n  x\nb: 1")
	if len(block[2]) != 1 || block[2][0].Role != tui.TextRoleString || block[1] != nil {
		t.Errorf("block = %v", block)
	}
	heredoc := Spans(FormatShell, "cat <<E\n"+long+"\nx\nE")
	if len(heredoc[2]) != 1 || heredoc[2][0].Role != tui.TextRoleString || len(heredoc[3]) != 1 {
		t.Errorf("heredoc = %v", heredoc)
	}
}

func TestSpansNeverSplitACharacter(t *testing.T) {
	cases := map[Format]string{
		FormatYAML:       "ключ: значение # 注釈\n  \x80x: \x80 1\n- \xc3 \"é\"\n",
		FormatEnv:        "名前=値 # é\nA= \x80\n",
		FormatJSON:       "{\"é\": \"ü\", \x80 1}",
		FormatINI:        "k\x80 = \x80 v ; \xc3\n",
		FormatTOML:       "\"é\" = [\x80, 'ü']\n",
		FormatShell:      "echo \x80 'é' é$x # ü\ncat <<\xc3\xa9\nx\n\xc3\xa9\n",
		FormatDockerfile: "RUN \x80 é\nENV é=\x80\n",
	}
	for f, text := range cases {
		checkSpans(t, f, text)
	}
	// The cases above must hold sections, or the test checks nothing.
	if len(Spans(FormatYAML, cases[FormatYAML])[0]) == 0 {
		t.Error("no sections in the YAML case")
	}
}

// Lines are the ones the text view keeps, whatever the line breaks of the text.
func TestSpansMatchTheLinesOfTheTextView(t *testing.T) {
	texts := []string{
		"a: 1\nb: 2\n",
		"a: 1\r\nb: 2\r\n",
		"a: 1\r\nb: 2",
		"a: 1\n\n\nb: 2\n\n",
		"\n\na: 1\n",
		"a: 1\n\n",
		"a: 1\rb: 2\n",
		"a: 1\r\r\nb: 2\r\n",
		"a: \"é\"\r\n\r\nb: 2\r\n",
		"\n",
		"",
	}
	view := newView(t)
	for _, text := range texts {
		spans := Spans(FormatYAML, text)
		view.SetText(text, spans)
		if text != "" && text != "\n" && len(spans) != view.Len() {
			t.Errorf("%q: %d entries for %d lines of the view", text, len(spans), view.Len())
		}
		if (text == "" || text == "\n") && (spans != nil || view.Len() != 0) {
			t.Errorf("%q: spans %v, view lines %d", text, spans, view.Len())
		}
	}
	// The same text with other line breaks draws the same.
	st := styles(t)
	draw := func(text string) string {
		view.SetText(text, Spans(FormatYAML, text))
		out, _ := view.View(st, 40, 8)
		return out
	}
	lf := draw("a: 1\nb: \"x\" # c\n\nc: true\n")
	if crlf := draw("a: 1\r\nb: \"x\" # c\r\n\r\nc: true\r\n"); crlf != lf {
		t.Errorf("CRLF text draws differently:\n%s\n%s", crlf, lf)
	}
	if noEnd := draw("a: 1\nb: \"x\" # c\n\nc: true"); noEnd != lf {
		t.Errorf("text without a final line break draws differently:\n%s\n%s", noEnd, lf)
	}
}

func TestSpansAreLimitedByLines(t *testing.T) {
	text := strings.Repeat("a: 1\n", tui.TextViewMaxLines+5)
	got := Spans(FormatYAML, text)
	if len(got) != tui.TextViewMaxLines {
		t.Fatalf("%d entries, want %d", len(got), tui.TextViewMaxLines)
	}
	if len(got[tui.TextViewMaxLines-1]) != 2 {
		t.Error("the last listed line has no sections")
	}
}

// The time to read a text grows with its length. Each case is one shape that could make a lexer rescan.
func TestRuntimeIsLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	shapes := map[string]string{
		"quotes":     strings.Repeat(`"'`, 4000) + "\n",
		"open quote": strings.Repeat(`a: "x `, 2000) + "\n",
		"colons":     strings.Repeat("a:b ", 4000) + "\n",
		"brackets":   strings.Repeat("[{", 4000) + "\n",
		"spaces":     strings.Repeat(" ", 8000) + "x\n",
		"hash":       strings.Repeat("a #", 4000) + "\n",
		"equals":     strings.Repeat("=a ", 4000) + "\n",
		"backslash":  strings.Repeat(`\`, 8000) + "\n",
		"heredoc":    "cat <<E\n" + strings.Repeat("<<x ", 2000) + "\n",
		"block":      "a: |\n" + strings.Repeat("  x\n", 2000),
	}
	for name, unit := range shapes {
		for f := FormatYAML; f < formatCount; f++ {
			start := time.Now()
			text := strings.Repeat(unit, 1<<20/len(unit)+1)
			Spans(f, text)
			if d := time.Since(start); d > 10*time.Second {
				t.Errorf("%s as %v: %v for %d bytes", name, f, d, len(text))
			}
		}
	}
}
