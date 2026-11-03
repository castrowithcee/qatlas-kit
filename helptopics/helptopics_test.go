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

func sampleTerms() []Term {
	return []Term{
		{ID: "term.bed", Title: "Bed", Text: "A bed is one patch of soil. Several plants can share a bed."},
		{ID: "term.pump", Title: "Pump", Text: "The pump waters a bed. It runs for a minute? It stops by itself!"},
	}
}

func TestLookupTerm(t *testing.T) {
	terms := sampleTerms()
	if got, ok := LookupTerm(terms, "term.pump"); !ok || got != terms[1] {
		t.Fatalf("LookupTerm(term.pump) = %#v, %v", got, ok)
	}
	if _, ok := LookupTerm(terms, "term.nothing"); ok {
		t.Fatal("an unknown ID was found")
	}
}

func TestCheckTermsRejectsBrokenTerms(t *testing.T) {
	if err := CheckTerms(sampleTerms()); err != nil {
		t.Fatal(err)
	}
	if err := CheckTerms(nil); err != nil {
		t.Fatalf("no terms: %v", err)
	}
	for _, tt := range []struct {
		name string
		edit func(*Term)
		want string
	}{
		{"empty ID", func(term *Term) { term.ID = "" }, "an ID"},
		{"blank in ID", func(term *Term) { term.ID = "term bed" }, "an ID"},
		{"control in ID", func(term *Term) { term.ID = "term.\x1bbed" }, "an ID"},
		{"repeated ID", func(term *Term) { term.ID = "term.pump" }, "listed twice"},
		{"empty title", func(term *Term) { term.Title = " " }, "a title"},
		{"title of two lines", func(term *Term) { term.Title = "Bed\nSoil" }, "a title"},
		{"empty text", func(term *Term) { term.Text = "" }, "a text"},
		{"control in text", func(term *Term) { term.Text = "A bed\x1b[2J is soil." }, "a text"},
		{"line break in text", func(term *Term) { term.Text = "A bed.\nIt is soil." }, "a text"},
		{"four sentences", func(term *Term) { term.Text = "One. Two. Three. Four." }, "4 sentences"},
	} {
		terms := sampleTerms()
		tt.edit(&terms[0])
		if err := CheckTerms(terms); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
}
