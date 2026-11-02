package tui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// CheckIcon reports why glyph cannot be an icon: icons are one emoji that draws two cells by itself or one
// symbol of one cell, so every terminal agrees on their width. It rejects an empty glyph, variation
// selectors, joiners, skin tone modifiers and regional indicators (flags), control characters, and a glyph
// whose width differs between grapheme and per-rune measurement. An application checks its icon table with
// it in a test; an icon stands beside its name, never in its place, and is left out in an ASCII terminal.
func CheckIcon(glyph string) error {
	if glyph == "" {
		return errors.New("icon is empty")
	}
	for _, r := range glyph {
		switch {
		case r == 0xFE0E || r == 0xFE0F:
			return fmt.Errorf("icon %q holds the variation selector U+%04X", glyph, r)
		case r == 0x200D:
			return fmt.Errorf("icon %q holds a zero width joiner", glyph)
		case r >= 0x1F3FB && r <= 0x1F3FF:
			return fmt.Errorf("icon %q holds the skin tone modifier U+%04X", glyph, r)
		case r >= 0x1F1E6 && r <= 0x1F1FF:
			return fmt.Errorf("icon %q holds the regional indicator U+%04X", glyph, r)
		case r < 0x20 || (r >= 0x7f && r < 0xa0):
			return fmt.Errorf("icon %q holds the control character U+%04X", glyph, r)
		}
	}
	if n := len([]rune(glyph)); n != 1 {
		return fmt.Errorf("icon %q is %d code points, want one", glyph, n)
	}
	grapheme, wc := ansi.StringWidth(glyph), ansi.StringWidthWc(glyph)
	if grapheme != wc || grapheme < 1 || grapheme > 2 {
		return fmt.Errorf("icon %q is %d cells by grapheme and %d by rune, want the same 1 or 2", glyph, grapheme, wc)
	}
	return nil
}
