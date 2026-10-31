// Package helptopics shares plain-text help topics between a CLI, TUI, and manpage.
package helptopics

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Topic is one guide supplied by the application.
type Topic struct {
	Name  string
	Short string
	Text  string
}

// Lookup returns the named topic without changing the caller's order.
func Lookup(topics []Topic, name string) (Topic, bool) {
	for _, topic := range topics {
		if topic.Name == name {
			return topic, true
		}
	}
	return Topic{}, false
}

// CLIText wraps a topic for the command line.
func CLIText(topic Topic) string { return Wrap(topic.Text, 80) }

var listMarker = regexp.MustCompile(`^ *(\d+\. |- )`)

// Wrap fits plain text to width columns, retaining paragraph and list boundaries.
func Wrap(text string, width int) string {
	width = max(width, 1)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, wrapLine(line, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapLine(line string, width int) []string {
	if utf8.RuneCountInString(line) <= width {
		return []string{line}
	}
	lead := len(line) - len(strings.TrimLeft(line, " "))
	hang := lead
	if marker := listMarker.FindString(line); marker != "" {
		hang = len(marker)
	}
	if lead > width/2 {
		lead = 0
	}
	if hang > width/2 {
		hang = lead
	}
	var lines []string
	current, empty := strings.Repeat(" ", lead), true
	for _, word := range strings.Fields(line) {
		if !empty && utf8.RuneCountInString(current)+1+utf8.RuneCountInString(word) <= width {
			current += " " + word
			continue
		}
		if !empty {
			lines = append(lines, current)
			current = strings.Repeat(" ", hang)
		}
		for utf8.RuneCountInString(current)+utf8.RuneCountInString(word) > width {
			room := width - utf8.RuneCountInString(current)
			runes := []rune(word)
			lines = append(lines, current+string(runes[:room]))
			current, word = strings.Repeat(" ", hang), string(runes[room:])
		}
		current, empty = current+word, false
	}
	return append(lines, current)
}
