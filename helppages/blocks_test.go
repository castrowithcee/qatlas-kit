package helppages

import (
	"reflect"
	"testing"
)

func TestBlocks(t *testing.T) {
	text := "# One\n## Two\n### Three\n#### Four\n\nline a\nline b\n\n- first\n  - nested **bold**\n    - deep\n- second `code`\n1. num\n7. seven\n   continued\n\nSee [docs](https://example.org/x) now."
	want := []Block{
		{Kind: Heading, Level: 1, Spans: []Span{{Text: "One"}}},
		{Kind: Heading, Level: 2, Spans: []Span{{Text: "Two"}}},
		{Kind: Heading, Level: 3, Spans: []Span{{Text: "Three"}}},
		{Kind: Paragraph, Spans: []Span{{Text: "#### Four"}}},
		{Kind: Paragraph, Spans: []Span{{Text: "line a line b"}}},
		{Kind: Item, Spans: []Span{{Text: "first"}}},
		{Kind: Item, Level: 1, Spans: []Span{{Text: "nested "}, {Kind: Bold, Text: "bold"}}},
		{Kind: Item, Level: 1, Spans: []Span{{Text: "deep"}}},
		{Kind: Item, Spans: []Span{{Text: "second "}, {Kind: Code, Text: "code"}}},
		{Kind: Item, Ordered: true, Number: 1, Spans: []Span{{Text: "num"}}},
		{Kind: Item, Ordered: true, Number: 7, Spans: []Span{{Text: "seven continued"}}},
		{Kind: Paragraph, Spans: []Span{{Text: "See "}, {Kind: Link, Text: "docs", URL: "https://example.org/x"}, {Text: " now."}}},
	}
	got := Blocks(text)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

func TestBlocksKeepForeignAsPlainText(t *testing.T) {
	tests := map[string]string{
		"| a | b |":           "| a | b |",
		"![alt](https://x.y)": "![alt](https://x.y)",
		"> quote":             "> quote",
		"<b>html</b>":         "<b>html</b>",
		"**open":              "**open",
		"`open":               "`open",
		"[open](x":            "[open](x",
		"```go":               "```go",
		"- ":                  "-",
		"      - too deep":    "- too deep",
	}
	for in, want := range tests {
		b := Blocks(in)
		if len(b) != 1 || b[0].Kind != Paragraph || len(b[0].Spans) != 1 || b[0].Spans[0].Kind != Text || b[0].Spans[0].Text != want {
			t.Errorf("Blocks(%q) = %#v, want plain %q", in, b, want)
		}
	}
}

func TestPlainText(t *testing.T) {
	got := PlainText("# Title\n\nSome **bold** and [a link](https://example.org).\n- item")
	if want := "Title\nSome bold and a link.\nitem"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPlaceholders(t *testing.T) {
	got := Placeholders("Press {app.quit} or {help}, not {Bad}, {} or {a b}, {open and {help-2}.")
	if want := []string{"app.quit", "help", "help-2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
