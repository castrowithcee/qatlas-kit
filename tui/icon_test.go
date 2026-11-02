package tui

import (
	"strings"
	"testing"
)

func TestCheckIcon(t *testing.T) {
	for _, glyph := range []string{"\U0001F3E0", "\U0001F517", "✅", "✓", "■"} {
		if err := CheckIcon(glyph); err != nil {
			t.Errorf("%q: %v", glyph, err)
		}
	}
	for _, tc := range []struct{ glyph, want string }{
		{"", "empty"},
		{"❤️", "variation selector"},
		{"☺︎", "variation selector"},
		{"\U0001F468‍\U0001F4BB", "joiner"},
		{"\U0001F44D\U0001F3FD", "skin tone"},
		{"\U0001F1E9\U0001F1EA", "regional indicator"},
		{"\x07", "control"},
		{"\u009b", "control"},
		{"ab", "code points"},
		{"é", "code points"},
		{"́", "cells"},
	} {
		err := CheckIcon(tc.glyph)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %v, want one about %q", tc.glyph, err, tc.want)
		}
	}
}
