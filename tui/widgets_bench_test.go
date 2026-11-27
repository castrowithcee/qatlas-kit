package tui

import "testing"

func BenchmarkClip(b *testing.B) {
	line := "\x1b[1;38;2;200;100;50mstatus\x1b[0m " + "\x1b]8;;https://example.com/docs\x07a linked word\x1b]8;;\x07" + " and some plain text after it"
	b.ReportAllocs()
	for b.Loop() {
		Clip(line, 20)
	}
}
