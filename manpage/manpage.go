package manpage

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/helptopics"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Generate calls generateTree for the application's real command tree, then adds its
// help topics to the root page. The callback must write <program>.1 in outputDir.
func Generate(outputDir, program, version, intro string, topics []helptopics.Topic, generateTree func(string, string) error) error {
	if !namePattern.MatchString(program) {
		return fmt.Errorf("invalid program name %q", program)
	}
	if generateTree == nil {
		return fmt.Errorf("manpage tree generator is required")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	if err := generateTree(outputDir, version); err != nil {
		return err
	}
	root := filepath.Join(outputDir, program+".1")
	page, err := os.ReadFile(root)
	if err != nil {
		return err
	}
	withTopics, err := WithHelpTopics(string(page), intro, topics)
	if err != nil {
		return err
	}
	return os.WriteFile(root, []byte(withTopics), 0o644)
}

// WithHelpTopics inserts the application topics before SEE ALSO when present. intro is the sentence that
// opens the section, one line of roff source such as "The same text is printed by \fBexample help\fP."; an
// empty intro opens it without one.
func WithHelpTopics(page, intro string, topics []helptopics.Topic) (string, error) {
	if !validIntro(intro) {
		return "", fmt.Errorf("invalid help topics introduction %q", intro)
	}
	var b strings.Builder
	b.WriteString(".SH HELP TOPICS\n")
	if intro != "" {
		b.WriteString(".PP\n" + intro + "\n")
	}
	for _, topic := range topics {
		if !namePattern.MatchString(topic.Name) {
			return "", fmt.Errorf("invalid help topic name %q", topic.Name)
		}
		b.WriteString(".SS " + roff(topic.Name) + "\n.PP\n")
		for _, line := range strings.Split(topic.Short, "\n") {
			b.WriteString(roff(line) + "\n")
		}
		b.WriteString(".PP\n.nf\n")
		for _, line := range strings.Split(helptopics.Wrap(topic.Text, 72), "\n") {
			b.WriteString(roff(line) + "\n")
		}
		b.WriteString(".fi\n\n")
	}
	if before, after, found := strings.Cut(page, ".SH SEE ALSO"); found {
		return before + b.String() + "\n.SH SEE ALSO" + after, nil
	}
	return page + "\n" + b.String(), nil
}

// validIntro keeps the caller's roff source to one text line: a line break, another control character, or a
// leading roff control character would let it start a request of its own.
func validIntro(intro string) bool {
	if strings.HasPrefix(intro, ".") || strings.HasPrefix(intro, "'") || !utf8.ValidString(intro) {
		return false
	}
	for _, r := range intro {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func roff(line string) string {
	line = strings.ReplaceAll(line, `\`, `\e`)
	line = strings.ReplaceAll(line, "-", `\-`)
	if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
		line = `\&` + line
	}
	return line
}
