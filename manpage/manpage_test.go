package manpage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/helptopics"
)

func TestGenerateUsesCallerTreeAndName(t *testing.T) {
	dir := t.TempDir()
	topics := []helptopics.Topic{{Name: "start", Short: "Start here", Text: ".hidden\na\\b --flag"}}
	intro := `The same text is printed by \fBexample help\fP \fItopic\fP.`
	err := Generate(dir, "example", "v1.0.0", intro, topics, func(output, version string) error {
		if version != "v1.0.0" {
			t.Errorf("generator got version %q", version)
		}
		return os.WriteFile(filepath.Join(output, "example.1"), []byte(".SH NAME\nexample\n.SH SEE ALSO\nmore\n"), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(dir, "example.1"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	for _, want := range []string{".SH HELP TOPICS\n.PP\n" + intro + "\n", ".SS start", `\&.hidden`, `a\eb \-\-flag`} {
		if !strings.Contains(s, want) {
			t.Errorf("manpage missing %q", want)
		}
	}
	if strings.Index(s, ".SH HELP TOPICS") > strings.Index(s, ".SH SEE ALSO") {
		t.Error("topics follow SEE ALSO")
	}
	if _, err := os.Stat(filepath.Join(dir, "example-start.1")); !os.IsNotExist(err) {
		t.Error("topic got a separate page")
	}
}

func TestGenerateRejectsUnsafeProgram(t *testing.T) {
	for _, program := range []string{"", "../other", "bad\n.SH FAKE"} {
		err := Generate(t.TempDir(), program, "v1.0.0", "", nil, func(string, string) error { return nil })
		if err == nil {
			t.Errorf("program %q accepted", program)
		}
	}
}

func TestWithHelpTopicsRejectsUnsafeText(t *testing.T) {
	for _, intro := range []string{"Text\n.SH FAKE", ".SH FAKE", "'br", "Text\rmore", "Text\x00", "bad\xff"} {
		if _, err := WithHelpTopics("", intro, nil); err == nil {
			t.Errorf("intro %q accepted", intro)
		}
	}
	if _, err := WithHelpTopics("", "", []helptopics.Topic{{Name: "x\n.SH FAKE"}}); err == nil {
		t.Error("unsafe topic accepted")
	}
}

func TestWithHelpTopicsWithoutIntro(t *testing.T) {
	page, err := WithHelpTopics(".SH NAME\nexample\n", "", []helptopics.Topic{{Name: "start", Short: "Start here"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, ".SH HELP TOPICS\n.SS start\n") {
		t.Errorf("page %q", page)
	}
}

func TestRoffKeepsTextAsText(t *testing.T) {
	for in, want := range map[string]string{
		`.hidden`:      `\&.hidden`,
		`'quoted'`:     `\&'quoted'`,
		`a\b`:          `a\eb`,
		`--connection`: `\-\-connection`,
	} {
		if got := roff(in); got != want {
			t.Errorf("roff(%q) = %q, want %q", in, got, want)
		}
	}
}
