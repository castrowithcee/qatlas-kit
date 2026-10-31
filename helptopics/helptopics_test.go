package helptopics

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWrap(t *testing.T) {
	for _, tt := range []struct {
		line  string
		width int
		want  string
	}{
		{"one two three four", 9, "one two\nthree\nfour"},
		{"1. first item text", 12, "1. first\n   item text"},
		{"  - bullet item here", 14, "  - bullet\n    item here"},
		{"abcdefghij", 4, "abcd\nefgh\nij"},
	} {
		if got := Wrap(tt.line, tt.width); got != tt.want {
			t.Errorf("Wrap(%q, %d) = %q, want %q", tt.line, tt.width, got, tt.want)
		}
	}
	for _, width := range []int{1, 2, 20, 80} {
		for _, line := range strings.Split(Wrap("a long list item with unicode ü and more words", width), "\n") {
			if utf8.RuneCountInString(line) > width {
				t.Errorf("line %q exceeds %d columns", line, width)
			}
		}
	}
}

func TestSharedTopicText(t *testing.T) {
	topics := []Topic{{Name: "start", Short: "Start here", Text: strings.Repeat("word ", 20)}}
	topic, ok := Lookup(topics, "start")
	if !ok || topic != topics[0] {
		t.Fatalf("Lookup(start) = %#v, %v", topic, ok)
	}
	if _, ok := Lookup(topics, "missing"); ok {
		t.Fatal("unknown topic found")
	}
	for _, line := range strings.Split(CLIText(topic), "\n") {
		if utf8.RuneCountInString(line) > 80 {
			t.Fatalf("CLI line exceeds 80 columns: %q", line)
		}
	}
}
