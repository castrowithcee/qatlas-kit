package tui

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
)

// ThemeTerminal is the name of the default theme, which paints no ground and derives its roles from the
// colours the terminal reports.
const ThemeTerminal = "terminal"

// TerminalColors are the foreground, background and 16 ANSI colours of a terminal scheme, each "#rrggbb".
// For the terminal theme only Foreground and Background count, as the terminal answers OSC 10 and 11; its
// accents name the ANSI slots, so the terminal colours them itself.
type TerminalColors struct {
	Foreground, Background Color
	ANSI                   [16]Color
}

// Theme is one theme as data. A hand-picked theme sets Profiles, which must hold a true-colour palette and
// may hold a 256- and a 16-colour one; a missing 256-colour palette is stepped down from the true-colour one,
// a missing 16-colour palette falls back to the plain palette. A scheme theme sets Scheme instead and is
// derived from it as the terminal theme is derived from the terminal. Every theme paints its ground.
type Theme struct {
	Name     string
	Profiles map[ColorProfile]Palette
	Scheme   *TerminalColors
}

// ThemeSet holds the themes an application offers: the terminal theme first, then the application's own,
// then the built-in schemes. It never changes after NewThemeSet.
type ThemeSet struct {
	themes []themeEntry
}

type themeEntry struct {
	name     string
	terminal bool
	profiles map[ColorProfile]Palette
	scheme   *schemeColors
}

type schemeColors struct {
	fg, bg rgb
	ansi   [16]rgb
}

// themeName is the form of a theme name: it is stored in preferences and shown untranslated.
var themeName = regexp.MustCompile(`^[a-z0-9]+(?:[ ._-][a-z0-9]+)*$`)

// NewThemeSet builds the themes of an application from its own themes extra and the built-in ones. It
// rejects an invalid or repeated name, a theme with both or neither of Profiles and Scheme, a colour that is
// not of its profile's form, a hand-picked palette whose text does not reach 4.5:1 on its ground and fills or
// whose dimmed text does not reach 3:1, and a scheme that cannot be derived. The themes are copied.
func NewThemeSet(extra []Theme) (*ThemeSet, error) {
	set := &ThemeSet{themes: []themeEntry{{name: ThemeTerminal, terminal: true}}}
	var errs []error
	seen := map[string]bool{ThemeTerminal: true}
	for _, t := range builtinSchemes() {
		seen[t.Name] = true
	}
	for _, t := range extra {
		if seen[t.Name] {
			errs = append(errs, fmt.Errorf("theme %q is defined twice", t.Name))
			continue
		}
		seen[t.Name] = true
		e, err := newThemeEntry(t)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		set.themes = append(set.themes, e)
	}
	for _, t := range builtinSchemes() {
		e, err := newThemeEntry(t)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		set.themes = append(set.themes, e)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return set, nil
}

func newThemeEntry(t Theme) (themeEntry, error) {
	if !themeName.MatchString(t.Name) {
		return themeEntry{}, fmt.Errorf("theme name %q is not lower-case letters and digits in words", t.Name)
	}
	switch {
	case t.Scheme != nil && t.Profiles != nil:
		return themeEntry{}, fmt.Errorf("theme %q has both palettes and a scheme", t.Name)
	case t.Scheme != nil:
		s, err := schemeOf(*t.Scheme)
		if err != nil {
			return themeEntry{}, fmt.Errorf("theme %q: %w", t.Name, err)
		}
		if _, ok := mixPalette(s.fg, s.bg, &s.ansi); !ok {
			return themeEntry{}, fmt.Errorf("theme %q: the scheme's text reads below %.1f:1 on its background or its fills", t.Name, textContrast)
		}
		return themeEntry{name: t.Name, scheme: &s}, nil
	}
	if _, ok := t.Profiles[ProfileTrueColor]; !ok {
		return themeEntry{}, fmt.Errorf("theme %q has no true-colour palette", t.Name)
	}
	for _, profile := range slices.Sorted(maps.Keys(t.Profiles)) {
		if err := checkPalette(t.Profiles[profile], profile); err != nil {
			return themeEntry{}, fmt.Errorf("theme %q: %w", t.Name, err)
		}
	}
	return themeEntry{name: t.Name, profiles: maps.Clone(t.Profiles)}, nil
}

// checkPalette requires every role of a hand-picked palette in the form of its profile, distinct category
// colours, and the contrast of text and dimmed text where both colours are known.
func checkPalette(p Palette, profile ColorProfile) error {
	var form func(Color) bool
	switch profile {
	case ProfileTrueColor:
		form = Color.isHex
	case ProfileANSI256:
		form = func(c Color) bool { _, ok := c.index(); return ok }
	case ProfileANSI16:
		form = func(c Color) bool { i, ok := c.index(); return ok && i < 16 }
	default:
		return fmt.Errorf("profile %d takes no palette", profile)
	}
	for _, r := range p.roles() {
		if !form(*r.color) {
			return fmt.Errorf("profile %d: role %s = %q is not a colour of the profile", profile, r.name, *r.color)
		}
	}
	if profile != ProfileANSI16 {
		seen := map[Color]bool{}
		for _, c := range p.Brand {
			if seen[c] {
				return fmt.Errorf("profile %d: category colour %s repeats", profile, c)
			}
			seen[c] = true
		}
	}
	if err := contrastError(p); err != nil {
		return fmt.Errorf("profile %d: %w", profile, err)
	}
	return nil
}

func schemeOf(c TerminalColors) (schemeColors, error) {
	var s schemeColors
	for i, v := range append([]Color{c.Foreground, c.Background}, c.ANSI[:]...) {
		if !v.isHex() {
			return s, fmt.Errorf("scheme colour %d = %q is not #rrggbb", i, v)
		}
	}
	s.fg, _ = rgbOfColor(c.Foreground)
	s.bg, _ = rgbOfColor(c.Background)
	for i, v := range c.ANSI {
		s.ansi[i], _ = rgbOfColor(v)
	}
	return s, nil
}

// Names are the names of the themes in the order a settings screen offers them, the default first.
func (s *ThemeSet) Names() []string {
	names := make([]string, len(s.themes))
	for i, t := range s.themes {
		names[i] = t.name
	}
	return names
}

func (s *ThemeSet) named(name string) (themeEntry, bool) {
	for _, t := range s.themes {
		if t.name == name {
			return t, true
		}
	}
	return s.themes[0], false
}

// Resolve returns the styles of the theme name under the colour profile and character set of the terminal.
// terminal holds the colours the terminal reported, or nil before it has answered. A name the set does not
// hold yields the terminal theme and false; the application shows TextThemeUnknown then. A profile without
// colour wins over every theme.
func (s *ThemeSet) Resolve(name string, profile ColorProfile, charset CharsetMode, terminal *TerminalColors) (Styles, bool) {
	t, known := s.named(name)
	if charset != CharsetUnicode {
		charset = CharsetASCII
	}
	st := Styles{profile: profile, charset: charset}
	if profile.Colorless() {
		st.profile = min(profile, ProfileMono)
		return st, known
	}
	st.palette = t.palette(profile, terminal)
	st.paints = !t.terminal && st.palette.Base != ""
	return st, known
}

// AsksTerminalColors reports whether the theme name derives its colours from the terminal under profile, so
// the application asks the terminal for its foreground and background. A terminal whose colours the theme
// could not use, and one without colour, is asked nothing.
func (s *ThemeSet) AsksTerminalColors(name string, profile ColorProfile) bool {
	t, _ := s.named(name)
	return t.terminal && (profile == ProfileTrueColor || profile == ProfileANSI256)
}

func (t themeEntry) palette(profile ColorProfile, terminal *TerminalColors) Palette {
	derived := func(fg, bg rgb, ansi *[16]rgb) Palette {
		if profile != ProfileTrueColor && profile != ProfileANSI256 {
			return plainPalette()
		}
		p, ok := mixPalette(fg, bg, ansi)
		if ok && profile == ProfileANSI256 {
			p, ok = steppedTo256(p, fg, bg)
		}
		if !ok {
			return plainPalette()
		}
		return p
	}
	switch {
	case t.terminal:
		if terminal == nil || !terminal.Foreground.isHex() || !terminal.Background.isHex() {
			return plainPalette()
		}
		fg, _ := rgbOfColor(terminal.Foreground)
		bg, _ := rgbOfColor(terminal.Background)
		return derived(fg, bg, nil)
	case t.scheme != nil:
		return derived(t.scheme.fg, t.scheme.bg, &t.scheme.ansi)
	}
	if p, ok := t.profiles[profile]; ok {
		return p
	}
	if profile == ProfileANSI256 {
		p := t.profiles[ProfileTrueColor]
		fg, _ := rgbOfColor(p.Text)
		bg, _ := rgbOfColor(p.Base)
		if stepped, ok := steppedTo256(p, fg, bg); ok {
			return stepped
		}
	}
	return plainPalette()
}
