package highlight

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// MaxLineBytes is the longest line that gets sections. A longer line stays plain and does not change the
// state of the lines after it.
const MaxLineBytes = 16384

// binaryProbe is how much of a text is inspected for invalid UTF-8; a NUL byte is looked for in all of it.
const binaryProbe = 65536

// Format is a text format with a lexer.
type Format uint8

// The formats. FormatNone has no lexer.
const (
	FormatNone Format = iota
	FormatYAML
	FormatEnv
	FormatJSON
	FormatINI
	FormatTOML
	FormatShell
	FormatDockerfile
	formatCount
)

// String is the lower-case name of the format, "none" for FormatNone and for a value outside the list.
func (f Format) String() string {
	switch f {
	case FormatYAML:
		return "yaml"
	case FormatEnv:
		return "env"
	case FormatJSON:
		return "json"
	case FormatINI:
		return "ini"
	case FormatTOML:
		return "toml"
	case FormatShell:
		return "shell"
	case FormatDockerfile:
		return "dockerfile"
	}
	return "none"
}

// Detect names the format of a file from its name, which may carry a directory, and failing that from the
// interpreter in a shebang line of text. It reads nothing else of text and returns FormatNone for an
// unknown format.
func Detect(name, text string) Format {
	base := name
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.ToLower(base)
	switch {
	case base == "dockerfile", base == "containerfile",
		strings.HasPrefix(base, "dockerfile."), strings.HasPrefix(base, "containerfile."),
		strings.HasSuffix(base, ".dockerfile"), strings.HasSuffix(base, ".containerfile"):
		return FormatDockerfile
	case base == ".env", strings.HasPrefix(base, ".env."), strings.HasSuffix(base, ".env"):
		return FormatEnv
	}
	switch base {
	case ".envrc", ".bashrc", ".bash_profile", ".bash_login", ".bash_logout", ".profile", ".zshrc", ".zshenv",
		".zprofile", ".zlogin", ".kshrc":
		return FormatShell
	}
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		switch base[i+1:] {
		case "yaml", "yml":
			return FormatYAML
		case "json":
			return FormatJSON
		case "ini", "cfg":
			return FormatINI
		case "toml":
			return FormatTOML
		case "sh", "bash", "zsh", "ksh", "dash", "ash":
			return FormatShell
		}
	}
	return shebang(text)
}

func shebang(text string) Format {
	if !strings.HasPrefix(text, "#!") {
		return FormatNone
	}
	line, _, _ := strings.Cut(text[2:], "\n")
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return FormatNone
	}
	prog := fields[0]
	if i := strings.LastIndexByte(prog, '/'); i >= 0 {
		prog = prog[i+1:]
	}
	if prog == "env" {
		prog = ""
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "-") && !strings.Contains(f, "=") {
				prog = f
				break
			}
		}
	}
	switch prog {
	case "sh", "bash", "zsh", "ksh", "dash", "ash":
		return FormatShell
	}
	return FormatNone
}

// Highlight is Spans of the format Detect finds for name and text.
func Highlight(name, text string) [][]tui.TextSpan {
	return Spans(Detect(name, text), text)
}

// Spans returns the sections of text in format, one entry per line, to hand to tui.TextView.SetText together
// with text. Lines are those the text view keeps: a line break is "\n" or "\r\n", and one at the very end of
// the text ends its last line instead of adding an empty one. Offsets are bytes from the start of the line.
//
// The sections of a line are ascending and do not overlap, lie inside the line and start and end on UTF-8
// character boundaries; Masked is never set. The result is nil for FormatNone, for a value outside the
// formats, for an empty text and for binary text. A text is binary if it holds a NUL byte or if more than a
// tenth of its first 64 KiB is invalid UTF-8. Lines longer than MaxLineBytes have no sections, and lines past
// tui.TextViewMaxLines are not listed. Nothing is judged beyond the shape of each format; the text need not be
// valid.
func Spans(format Format, text string) [][]tui.TextSpan {
	if format == FormatNone || format >= formatCount || text == "" || binary(text) {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	n := min(strings.Count(text, "\n")+1, tui.TextViewMaxLines)
	out := make([][]tui.TextSpan, n)
	var st lineState
	e := &emitter{}
	for i := 0; i < n; i++ {
		var line string
		line, text, _ = strings.Cut(text, "\n")
		if len(line) > MaxLineBytes {
			continue
		}
		e.begin(line)
		lex(format, line, &st, e)
		if len(e.out) > 0 {
			out[i] = slices.Clone(e.out)
		}
	}
	return out
}

func binary(text string) bool {
	if strings.IndexByte(text, 0) >= 0 {
		return true
	}
	probe := text[:min(len(text), binaryProbe)]
	bad := 0
	for i := 0; i < len(probe); {
		r, w := utf8.DecodeRuneInString(probe[i:])
		// Only a character cut by the end of the probe, while the text goes on, is not damage.
		if r == utf8.RuneError && w == 1 && (len(probe) == len(text) || utf8.FullRuneInString(probe[i:])) {
			bad++
		}
		i += w
	}
	return bad*10 > len(probe)
}

// mode is the multi-line state a lexer carries to the next line.
type mode uint8

const (
	modeNone mode = iota
	modeBlock
	modeQuote
	modeHeredoc
)

// lineState is the whole state between two lines. It holds no reference to more than one line of text.
type lineState struct {
	mode   mode
	indent int
	quote  byte
	delim  string
	strip  bool
	cont   bool
	instr  string
}

func lex(format Format, line string, st *lineState, e *emitter) {
	switch format {
	case FormatYAML:
		lexYAML(line, st, e)
	case FormatEnv:
		lexEnv(line, st, e)
	case FormatJSON:
		lexJSON(line, e)
	case FormatINI:
		lexINI(line, st, e, false)
	case FormatTOML:
		lexINI(line, st, e, true)
	case FormatShell:
		lexShell(line, st, e)
	case FormatDockerfile:
		lexDockerfile(line, st, e)
	}
}

// emitter collects the sections of one line and keeps them valid whatever a lexer asks for.
type emitter struct {
	line string
	out  []tui.TextSpan
	last int
}

func (e *emitter) begin(line string) {
	e.line, e.out, e.last = line, e.out[:0], 0
}

func (e *emitter) add(start, end int, role tui.TextRole) {
	start, end = max(start, e.last), min(end, len(e.line))
	for start < end && !boundary(e.line, start) {
		start++
	}
	for end > start && !boundary(e.line, end) {
		end--
	}
	if start >= end {
		return
	}
	e.out = append(e.out, tui.TextSpan{Start: start, End: end, Role: role})
	e.last = end
}

func boundary(s string, i int) bool { return i <= 0 || i >= len(s) || utf8.RuneStart(s[i]) }

func (e *emitter) comment(start int) { e.add(start, len(e.line), tui.TextRoleComment) }

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isWordByte(c byte) bool {
	return c == '_' || isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func skipSpace(s string, i int) int {
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	return i
}

func trimEnd(s string, from, end int) int {
	for end > from && isSpace(s[end-1]) {
		end--
	}
	return end
}

// closeQuote finds the quote q from index from on. It returns the index after it, or len(s) and false if
// the line ends first. With backslash a backslash protects the next byte; with doubled two quotes in a row
// are one character of the string.
func closeQuote(s string, from int, q byte, backslash, doubled bool) (int, bool) {
	for j := from; j < len(s); j++ {
		switch c := s[j]; {
		case backslash && c == '\\':
			j++
		case c == q:
			if doubled && j+1 < len(s) && s[j+1] == q {
				j++
				continue
			}
			return j + 1, true
		}
	}
	return len(s), false
}

// isNumber reports whether s is a decimal integer or float with optional sign, underscores between digits
// and exponent, or a hexadecimal or octal integer.
func isNumber(s string) bool {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	if len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X' || s[1] == 'o') {
		for i := 2; i < len(s); i++ {
			c := s[i]
			ok := isDigit(c) || s[1] != 'o' && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
			if !ok || s[1] == 'o' && c > '7' {
				return false
			}
		}
		return true
	}
	i, digits := 0, 0
	for i < len(s) && (isDigit(s[i]) || s[i] == '_' && digits > 0) {
		if s[i] != '_' {
			digits++
		}
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && (isDigit(s[i]) || s[i] == '_' && digits > 0) {
			if s[i] != '_' {
				digits++
			}
			i++
		}
	}
	if digits == 0 {
		return false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		exp := 0
		for i < len(s) && isDigit(s[i]) {
			i++
			exp++
		}
		if exp == 0 {
			return false
		}
	}
	return i == len(s)
}

func isAnyFold(s string, words ...string) bool {
	for _, w := range words {
		if strings.EqualFold(s, w) {
			return true
		}
	}
	return false
}
