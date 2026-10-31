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
	err := Generate(dir, "example", "v1.0.0", topics, func(output, version string) error {
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
	for _, want := range []string{"example help", ".SS start", `\&.hidden`, `a\eb \-\-flag`} {
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

func TestWithHelpTopicsRejectsUnsafeNames(t *testing.T) {
	for _, program := range []string{"", "../other", "bad\n.SH FAKE"} {
		if _, err := WithHelpTopics("", program, nil); err == nil {
			t.Errorf("program %q accepted", program)
		}
	}
	if _, err := WithHelpTopics("", "example", []helptopics.Topic{{Name: "x\n.SH FAKE"}}); err == nil {
		t.Error("unsafe topic accepted")
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
