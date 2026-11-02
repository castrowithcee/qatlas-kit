package tui

import (
	"fmt"
	"image/color"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// ColorProfile is how much of colour and text attributes the output can show. The zero value shows neither.
type ColorProfile uint8

const (
	// ProfileNone shows plain text only: no terminal, or TERM=dumb.
	ProfileNone ColorProfile = iota
	// ProfileMono shows no colour but keeps bold, faint and reverse: a terminal with NO_COLOR set.
	ProfileMono
	// ProfileANSI16 shows the 16 colours of the terminal's own scheme.
	ProfileANSI16
	// ProfileANSI256 shows the 256 colours of the xterm palette.
	ProfileANSI256
	// ProfileTrueColor shows 24-bit colour.
	ProfileTrueColor
)

// Colorless reports whether the profile shows no colour. Every fallback without colour asks it, so none of
// them depends on a palette field.
func (p ColorProfile) Colorless() bool { return p < ProfileANSI16 || p > ProfileTrueColor }

// DetectColorProfile is the colour profile of the terminal behind out under the environment environ, in the
// form of os.Environ. NO_COLOR with any non-empty value turns colour off and keeps the attributes of a
// terminal; without a terminal or with TERM=dumb nothing but plain text is shown.
func DetectColorProfile(out io.Writer, environ []string) ColorProfile {
	var p ColorProfile
	switch colorprofile.Detect(out, environ) {
	case colorprofile.TrueColor:
		p = ProfileTrueColor
	case colorprofile.ANSI256:
		p = ProfileANSI256
	case colorprofile.ANSI:
		p = ProfileANSI16
	case colorprofile.ASCII:
		p = ProfileMono
	}
	// The library reads NO_COLOR as a boolean; the convention turns colour off with any non-empty value.
	if envValue(environ, "NO_COLOR") != "" && p != ProfileNone {
		return ProfileMono
	}
	return p
}

// CharsetMode is the set of characters the terminal draws. The zero value is the safe ASCII set.
type CharsetMode uint8

const (
	// CharsetASCII draws ASCII only: TERM=dumb or a locale without UTF-8.
	CharsetASCII CharsetMode = iota
	// CharsetUnicode draws block, frame and other Unicode characters.
	CharsetUnicode
)

// DetectCharsetMode is the character set of the terminal under the environment environ, in the form of
// os.Environ: ASCII under TERM=dumb or where the first set of LC_ALL, LC_CTYPE and LANG names no UTF-8,
// Unicode otherwise, including where none of them is set.
func DetectCharsetMode(environ []string) CharsetMode {
	if envValue(environ, "TERM") == "dumb" {
		return CharsetASCII
	}
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.ToLower(envValue(environ, name)); v != "" {
			if strings.Contains(v, "utf-8") || strings.Contains(v, "utf8") {
				return CharsetUnicode
			}
			return CharsetASCII
		}
	}
	return CharsetUnicode
}

// envValue is the last value of name in environ, as the process environment resolves duplicates.
func envValue(environ []string, name string) string {
	value := ""
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			value = v
		}
	}
	return value
}

// Color is one palette value: "#rrggbb", an ANSI index from "0" to "255", or empty for the terminal default.
type Color string

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ColorOf is the "#rrggbb" value of c, such as a colour the terminal reported; empty for nil.
func ColorOf(c color.Color) Color {
	if c == nil {
		return ""
	}
	return rgbOf(c).hex()
}

func (c Color) isHex() bool { return hexColor.MatchString(string(c)) }

// index is the ANSI index of c; false where c is no index.
func (c Color) index() (int, bool) {
	s := string(c)
	if s == "" || len(s) > 3 || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	i, err := strconv.Atoi(s)
	if err != nil || i < 0 || i > 255 {
		return 0, false
	}
	return i, true
}

// rgb is a colour with 8 bits per channel.
type rgb struct{ r, g, b uint8 }

func rgbOf(c color.Color) rgb {
	r, g, b, _ := c.RGBA()
	return rgb{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

func (c rgb) hex() Color { return Color(fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)) }

// rgbOfColor is the colour c names where it is known: a hex value, or an index of the xterm cube or grey
// ramp. The 16 lower indexes are the terminal's own and unknown.
func rgbOfColor(c Color) (rgb, bool) {
	if c.isHex() {
		v, _ := strconv.ParseUint(string(c[1:]), 16, 32)
		return rgb{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
	}
	if i, ok := c.index(); ok && i >= 16 {
		return xterm256(i), true
	}
	return rgb{}, false
}

// mix is the colour share of the way from bg to fg.
func mix(bg, fg rgb, share float64) rgb {
	step := func(a, b uint8) uint8 { return uint8(math.Round(float64(a) + (float64(b)-float64(a))*share)) }
	return rgb{step(bg.r, fg.r), step(bg.g, fg.g), step(bg.b, fg.b)}
}

// luminance is the relative luminance of a colour as WCAG defines it.
func luminance(c rgb) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.r) + 0.7152*channel(c.g) + 0.0722*channel(c.b)
}

// contrast is the WCAG contrast ratio of two colours, from 1 to 21.
func contrast(a, b rgb) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// xterm256 is the colour of an index of the xterm cube (16 to 231) or grey ramp (232 to 255).
func xterm256(i int) rgb {
	if i >= 232 {
		v := uint8(8 + 10*(i-232))
		return rgb{v, v, v}
	}
	i -= 16
	level := func(v int) uint8 {
		if v == 0 {
			return 0
		}
		return uint8(55 + 40*v)
	}
	return rgb{level(i / 36), level(i / 6 % 6), level(i % 6)}
}

// nearest256 is the index of the cube or grey ramp closest to c. The 16 slots below are left out: the
// terminal's scheme defines them.
func nearest256(c rgb) int {
	best, bestDistance := 16, math.MaxInt
	for i := 16; i < 256; i++ {
		x := xterm256(i)
		dr, dg, db := int(c.r)-int(x.r), int(c.g)-int(x.g), int(c.b)-int(x.b)
		if d := dr*dr + dg*dg + db*db; d < bestDistance {
			best, bestDistance = i, d
		}
	}
	return best
}
