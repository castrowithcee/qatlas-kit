package tui

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestDetectColorProfile(t *testing.T) {
	tty := []string{"TTY_FORCE=1", "TERM=xterm", "COLORTERM=truecolor"}
	for _, tc := range []struct {
		name    string
		environ []string
		want    ColorProfile
	}{
		{"no terminal", []string{"TERM=xterm", "COLORTERM=truecolor"}, ProfileNone},
		{"true colour", tty, ProfileTrueColor},
		{"NO_COLOR=1", append(tty, "NO_COLOR=1"), ProfileMono},
		{"NO_COLOR with any value", append(tty, "NO_COLOR=yes"), ProfileMono},
		{"NO_COLOR=0 still turns colour off", append(tty, "NO_COLOR=0"), ProfileMono},
		{"empty NO_COLOR", append(tty, "NO_COLOR="), ProfileTrueColor},
		{"dumb", []string{"TTY_FORCE=1", "TERM=dumb", "NO_COLOR=yes"}, ProfileNone},
	} {
		if got := DetectColorProfile(&bytes.Buffer{}, tc.environ); got != tc.want {
			t.Errorf("%s: profile %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestColorlessProfiles(t *testing.T) {
	for p, want := range map[ColorProfile]bool{
		ProfileNone: true, ProfileMono: true, ProfileANSI16: false, ProfileANSI256: false, ProfileTrueColor: false, ProfileTrueColor + 1: true,
	} {
		if got := p.Colorless(); got != want {
			t.Errorf("profile %d: colorless %v, want %v", p, got, want)
		}
	}
}

func TestDetectCharsetMode(t *testing.T) {
	for _, tc := range []struct {
		environ []string
		want    CharsetMode
	}{
		{nil, CharsetUnicode},
		{[]string{"TERM=xterm-256color", "LANG=de_DE.UTF-8"}, CharsetUnicode},
		{[]string{"LC_ALL=en_US.utf8", "LANG=C"}, CharsetUnicode},
		{[]string{"TERM=dumb", "LANG=de_DE.UTF-8"}, CharsetASCII},
		{[]string{"LC_ALL=C", "LANG=de_DE.UTF-8"}, CharsetASCII},
		{[]string{"LC_CTYPE=POSIX"}, CharsetASCII},
		{[]string{"LANG=de_DE.ISO-8859-1"}, CharsetASCII},
		{[]string{"LANG=C", "LANG=C.UTF-8"}, CharsetUnicode},
	} {
		if got := DetectCharsetMode(tc.environ); got != tc.want {
			t.Errorf("%v: charset %d, want %d", tc.environ, got, tc.want)
		}
	}
}

// Colours stand only in the palettes: no other source file of the package holds a colour literal, so
// screen code cannot draw a colour that no theme chose.
func TestColorLiteralsStayInThePalettes(t *testing.T) {
	hex := regexp.MustCompile(`#[0-9a-fA-F]{6}\b`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "palette.go" || name == "themes_builtin_gen.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if v, _ := strconv.Unquote(lit.Value); hex.MatchString(v) {
					t.Errorf("%s holds the colour literal %q outside a palette", name, v)
				}
			}
			return true
		})
	}
	if checked < 5 {
		t.Fatalf("checked %d files, the scan is broken", checked)
	}
}
