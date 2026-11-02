package tui

import (
	"image/color"
	"strconv"
	"strings"
	"testing"
)

// Invented names stand in for the names an application gives its themes.
const (
	exampleDark  = "example dark"
	exampleLight = "example light"
)

func exampleSet(t *testing.T, extra ...Theme) *ThemeSet {
	t.Helper()
	if extra == nil {
		extra = []Theme{DarkTheme(exampleDark), LightTheme(exampleLight)}
	}
	set, err := NewThemeSet(extra)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func hexRGB(t *testing.T, c Color) rgb {
	t.Helper()
	v, ok := rgbOfColor(c)
	if !ok || !c.isHex() {
		t.Fatalf("%q is not a hex colour", c)
	}
	return v
}

func resolve(t *testing.T, set *ThemeSet, name string, profile ColorProfile, terminal *TerminalColors) Styles {
	t.Helper()
	st, known := set.Resolve(name, profile, CharsetUnicode, terminal)
	if !known {
		t.Fatalf("theme %q is unknown", name)
	}
	return st
}

func TestThemeSetOffersTerminalThenApplicationThenBuiltinThemes(t *testing.T) {
	names := exampleSet(t).Names()
	if len(names) != 3+len(builtinSchemes()) {
		t.Fatalf("names = %v", names)
	}
	if strings.Join(names[:3], ",") != ThemeTerminal+","+exampleDark+","+exampleLight {
		t.Errorf("names start with %v", names[:3])
	}
	for i, s := range builtinSchemes() {
		if names[3+i] != s.Name {
			t.Errorf("name %d = %q, want the built-in %q", 3+i, names[3+i], s.Name)
		}
	}
	bare := exampleSet(t, []Theme{}...)
	if got := bare.Names(); got[0] != ThemeTerminal || len(got) != 1+len(builtinSchemes()) {
		t.Errorf("a set without application themes holds %v", got)
	}
}

func TestUnknownThemeFallsBackToTerminal(t *testing.T) {
	set := exampleSet(t)
	for _, name := range []string{"", "purple", "Example Dark", "dracula", ThemeTerminal + " "} {
		st, known := set.Resolve(name, ProfileTrueColor, CharsetUnicode, nil)
		if known || st.Paints() || st.Palette() != plainPalette() {
			t.Errorf("%q: known %v, paints %v, palette %+v; want the terminal theme", name, known, st.Paints(), st.Palette())
		}
	}
	other := exampleSet(t, Theme{Name: "other dark", Profiles: DarkTheme("").Profiles})
	if _, known := other.Resolve(exampleDark, ProfileTrueColor, CharsetUnicode, nil); known {
		t.Error("a set knows a theme of another set")
	}
}

func TestNewThemeSetRejectsBadThemes(t *testing.T) {
	dark := DarkTheme("").Profiles
	with := func(profile ColorProfile, change func(*Palette)) map[ColorProfile]Palette {
		out := map[ColorProfile]Palette{}
		for k, v := range dark {
			out[k] = v
		}
		p := out[profile]
		change(&p)
		out[profile] = p
		return out
	}
	scheme := *builtinSchemes()[0].Scheme
	badScheme := scheme
	badScheme.ANSI[5] = "5"
	faintScheme := scheme
	faintScheme.Foreground, faintScheme.Background = "#657b83", "#fdf6e3"
	for _, tc := range []struct {
		name   string
		themes []Theme
		want   string
	}{
		{"empty name", []Theme{{Name: "", Profiles: dark}}, "theme name"},
		{"upper case", []Theme{{Name: "Dark", Profiles: dark}}, "theme name"},
		{"padded", []Theme{{Name: " dark", Profiles: dark}}, "theme name"},
		{"double space", []Theme{{Name: "a  b", Profiles: dark}}, "theme name"},
		{"control", []Theme{{Name: "a\x1bb", Profiles: dark}}, "theme name"},
		{"terminal", []Theme{{Name: ThemeTerminal, Profiles: dark}}, "twice"},
		{"built-in", []Theme{{Name: builtinSchemes()[0].Name, Profiles: dark}}, "twice"},
		{"repeated", []Theme{{Name: "a", Profiles: dark}, {Name: "a", Profiles: dark}}, "twice"},
		{"neither", []Theme{{Name: "a"}}, "no true-colour palette"},
		{"both", []Theme{{Name: "a", Profiles: dark, Scheme: &scheme}}, "both"},
		{"no true colour", []Theme{{Name: "a", Profiles: map[ColorProfile]Palette{ProfileANSI256: dark[ProfileANSI256]}}}, "no true-colour palette"},
		{"mono palette", []Theme{{Name: "a", Profiles: with(ProfileMono, func(*Palette) {})}}, "takes no palette"},
		{"empty role", []Theme{{Name: "a", Profiles: with(ProfileTrueColor, func(p *Palette) { p.Accent = "" })}}, "accent"},
		{"index in true colour", []Theme{{Name: "a", Profiles: with(ProfileTrueColor, func(p *Palette) { p.Heat[2] = "22" })}}, "heat2"},
		{"short hex", []Theme{{Name: "a", Profiles: with(ProfileTrueColor, func(p *Palette) { p.Text = "#fff" })}}, "text"},
		{"hex in 256", []Theme{{Name: "a", Profiles: with(ProfileANSI256, func(p *Palette) { p.Base = "#000000" })}}, "base"},
		{"index above 255", []Theme{{Name: "a", Profiles: with(ProfileANSI256, func(p *Palette) { p.Base = "256" })}}, "base"},
		{"leading zero", []Theme{{Name: "a", Profiles: with(ProfileANSI256, func(p *Palette) { p.Base = "023" })}}, "base"},
		{"index above 15", []Theme{{Name: "a", Profiles: with(ProfileANSI16, func(p *Palette) { p.Text = "16" })}}, "text"},
		{"brand repeats", []Theme{{Name: "a", Profiles: with(ProfileTrueColor, func(p *Palette) { p.Brand[7] = p.Brand[0] })}}, "repeats"},
		{"faint text", []Theme{{Name: "a", Profiles: with(ProfileTrueColor, func(p *Palette) { p.Text = "#3a4a5a" })}}, "text on base"},
		{"faint selection", []Theme{{Name: "a", Profiles: with(ProfileTrueColor, func(p *Palette) { p.Selection = "#8090a0" })}}, "text on selection"},
		{"faint hint", []Theme{{Name: "a", Profiles: with(ProfileTrueColor, func(p *Palette) { p.TextDim = "#26323e" })}}, "textDim on base"},
		{"faint 256", []Theme{{Name: "a", Profiles: with(ProfileANSI256, func(p *Palette) { p.Text = "236" })}}, "text on"},
		{"scheme slot", []Theme{{Name: "a", Scheme: &badScheme}}, "scheme colour 7"},
		{"faint scheme", []Theme{{Name: "a", Scheme: &faintScheme}}, "reads below"},
	} {
		set, err := NewThemeSet(tc.themes)
		if err == nil || set != nil {
			t.Errorf("%s: accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q lacks %q", tc.name, err, tc.want)
		}
	}
}

func TestThemeSetCopiesItsInput(t *testing.T) {
	theme := DarkTheme(exampleDark)
	set := exampleSet(t, theme)
	p := theme.Profiles[ProfileTrueColor]
	p.Base = "#ffffff"
	theme.Profiles[ProfileTrueColor] = p
	delete(theme.Profiles, ProfileANSI256)
	if got := resolve(t, set, exampleDark, ProfileTrueColor, nil).Palette().Base; got != "#0b141d" {
		t.Errorf("the set follows a change of its input: base %q", got)
	}
	if got := resolve(t, set, exampleDark, ProfileANSI256, nil).Palette().Base; got != "233" {
		t.Errorf("the set lost its 256-colour palette: base %q", got)
	}
}

// Every hand-picked palette draws its own values under its profile and paints the ground with them.
func TestHandPickedThemesFollowTheProfile(t *testing.T) {
	set := exampleSet(t)
	for _, theme := range []Theme{DarkTheme(exampleDark), LightTheme(exampleLight)} {
		for _, profile := range []ColorProfile{ProfileTrueColor, ProfileANSI256, ProfileANSI16} {
			st := resolve(t, set, theme.Name, profile, nil)
			if st.Palette() != theme.Profiles[profile] || !st.Paints() || st.Profile() != profile {
				t.Errorf("%s, profile %d: palette %+v, paints %v", theme.Name, profile, st.Palette(), st.Paints())
			}
		}
	}
	dark := resolve(t, set, exampleDark, ProfileTrueColor, nil)
	if got := dark.Render(StyleActive, "x"); !strings.Contains(got, "38;2;76;154;255") {
		t.Errorf("true colour accent = %q", got)
	}
	if got := resolve(t, set, exampleDark, ProfileANSI256, nil).Render(StyleActive, "x"); !strings.Contains(got, "38;5;75") {
		t.Errorf("256-colour accent = %q", got)
	}
	if got := resolve(t, set, exampleDark, ProfileANSI16, nil).Render(StyleActive, "x"); !strings.Contains(got, "94") {
		t.Errorf("16-colour accent = %q", got)
	}
}

// A hand-picked theme without a 256-colour palette steps its true-colour one down to the nearest indexes,
// or falls back to the plain palette where the steps collapse; one without a 16-colour palette falls back to
// the plain palette. A fallback paints nothing.
func TestHandPickedThemeWithoutLowerProfiles(t *testing.T) {
	only := func(name string, theme Theme) Theme {
		return Theme{Name: name, Profiles: map[ColorProfile]Palette{ProfileTrueColor: theme.Profiles[ProfileTrueColor]}}
	}
	light, dark := only("only light", LightTheme("")), only("only dark", DarkTheme(""))
	set := exampleSet(t, light, dark)
	st := resolve(t, set, light.Name, ProfileANSI256, nil)
	p := st.Palette()
	for _, r := range p.roles() {
		if i, ok := r.color.index(); !ok || i < 16 {
			t.Errorf("stepped role %s = %q, want an index of the cube or ramp", r.name, *r.color)
		}
	}
	if !st.Paints() || contrastError(p) != nil {
		t.Errorf("stepped palette paints %v, contrast %v", st.Paints(), contrastError(p))
	}
	// The dark surfaces lie closer together than the steps of the grey ramp.
	for _, profile := range []ColorProfile{ProfileANSI256, ProfileANSI16} {
		for _, theme := range []Theme{light, dark} {
			if profile == ProfileANSI256 && theme.Name == light.Name {
				continue
			}
			st := resolve(t, set, theme.Name, profile, nil)
			if st.Palette() != plainPalette() || st.Paints() {
				t.Errorf("%s, profile %d: palette %+v, paints %v; want the plain palette", theme.Name, profile, st.Palette(), st.Paints())
			}
		}
	}
}

// A profile without colour wins over every theme: no palette, no ground, and no colour sequence in any
// style; the plain profile drops the attributes as well.
func TestColorlessProfileWinsOverEveryTheme(t *testing.T) {
	set := exampleSet(t)
	term := &TerminalColors{Foreground: "#ffffff", Background: "#000000"}
	for _, profile := range []ColorProfile{ProfileNone, ProfileMono, ProfileTrueColor + 1} {
		for _, name := range set.Names() {
			st, _ := set.Resolve(name, profile, CharsetUnicode, term)
			if st.Palette() != (Palette{}) || st.Paints() || !st.Profile().Colorless() {
				t.Errorf("profile %d, %s: palette %+v, paints %v", profile, name, st.Palette(), st.Paints())
			}
			for ts := StyleText; ts <= StyleBorderFocus; ts++ {
				out := st.Render(ts, "x")
				if strings.Contains(out, "38;") || strings.Contains(out, "48;") || strings.Contains(out, "\x1b[3") || strings.Contains(out, "\x1b[4") {
					t.Errorf("profile %d, %s, style %d: %q names a colour", profile, name, ts, out)
				}
				if st.Profile() != ProfileMono && out != "x" {
					t.Errorf("profile %d, %s, style %d: %q is not plain text", profile, name, ts, out)
				}
			}
			if st.Paint("x", 3, 2) != "x" {
				t.Errorf("profile %d, %s: paints the screen", profile, name)
			}
		}
	}
	if got := (Styles{}).Render(StyleActiveRow, "x"); got != "x" {
		t.Errorf("the zero Styles draw %q", got)
	}
}

func TestTerminalColorsAreAskedOnlyWhereTheyAreUsed(t *testing.T) {
	set := exampleSet(t)
	for _, tc := range []struct {
		name    string
		profile ColorProfile
		want    bool
	}{
		{ThemeTerminal, ProfileTrueColor, true},
		{ThemeTerminal, ProfileANSI256, true},
		{"unknown", ProfileTrueColor, true},
		{ThemeTerminal, ProfileANSI16, false},
		{ThemeTerminal, ProfileMono, false},
		{ThemeTerminal, ProfileNone, false},
		{exampleDark, ProfileTrueColor, false},
		{builtinSchemes()[0].Name, ProfileTrueColor, false},
	} {
		if got := set.AsksTerminalColors(tc.name, tc.profile); got != tc.want {
			t.Errorf("%s, profile %d: asks %v, want %v", tc.name, tc.profile, got, tc.want)
		}
	}
}

// The terminal theme leaves the text to the terminal and names its accents by ANSI slot, so the terminal's
// own scheme colours them; the surfaces are steps between its background and foreground, and it never
// paints the ground.
func TestTerminalThemeTakesTheColorsOfTheTerminal(t *testing.T) {
	set := exampleSet(t)
	if st := resolve(t, set, ThemeTerminal, ProfileTrueColor, nil); st.Palette() != plainPalette() {
		t.Fatalf("before the answer the palette is %+v, want the plain one", st.Palette())
	}
	st := resolve(t, set, ThemeTerminal, ProfileTrueColor, &TerminalColors{Foreground: ColorOf(color.RGBA{0xcd, 0xd6, 0xf4, 0xff}), Background: "#1e1e2e"})
	p := st.Palette()
	if st.Paints() || p.Text != "" || p.Base != "#1e1e2e" {
		t.Errorf("paints %v, text %q, base %q; want no ground, the terminal's text and its background", st.Paints(), p.Text, p.Base)
	}
	for ts, want := range map[TextStyle]string{StyleActive: "\x1b[1;34m", StyleOK: "\x1b[1;32m", StyleFail: "\x1b[1;31m", StyleWarn: "\x1b[1;33m"} {
		if got := st.Render(ts, "x"); !strings.HasPrefix(got, want) {
			t.Errorf("style %d = %q, want the ANSI slot %q", ts, got, want)
		}
	}
	if got := st.Render(StyleTitle, "x"); strings.Contains(got, "38;") || strings.Contains(got, "\x1b[3") {
		t.Errorf("the title names a text colour: %q", got)
	}
	for _, c := range []Color{p.Panel, p.Surface, p.Selection, p.Border, p.TextDim} {
		if c == p.Base || !c.isHex() {
			t.Errorf("role %q is not a step between the colours of the terminal", c)
		}
	}
}

// Without an answer, with 16 colours or with a pair that is itself too faint, the terminal theme has no
// fills: panels stand by their frames, frames and dimmed text take ANSI slot 8, and the selection is
// reverse video behind the ">" mark.
func TestTerminalThemeFallsBackToFrames(t *testing.T) {
	set := exampleSet(t)
	for _, tc := range []struct {
		name     string
		profile  ColorProfile
		terminal *TerminalColors
	}{
		{"no answer", ProfileTrueColor, nil},
		{"only a background", ProfileTrueColor, &TerminalColors{Background: "#000000"}},
		{"a bad answer", ProfileTrueColor, &TerminalColors{Foreground: "white", Background: "#000000"}},
		{"16 colours", ProfileANSI16, &TerminalColors{Foreground: "#ffffff", Background: "#000000"}},
		{"a pair below 4.5:1", ProfileTrueColor, &TerminalColors{Foreground: "#657b83", Background: "#fdf6e3"}},
	} {
		st := resolve(t, set, ThemeTerminal, tc.profile, tc.terminal)
		p := st.Palette()
		if p.Panel != "" || p.Surface != "" || p.Selection != "" || p.Base != "" || p.Border != "8" || p.TextDim != "8" {
			t.Errorf("%s: palette %+v, want no fills and slot 8 for frames and dimmed text", tc.name, p)
		}
		if got := st.Render(StyleActiveRow, "x"); !strings.Contains(got, "7") || strings.Contains(got, "48;") {
			t.Errorf("%s: the selection %q is not reverse video", tc.name, got)
		}
		if st.SelectionMark() != ">" {
			t.Errorf("%s: the selection lost its mark", tc.name)
		}
	}
}

// With 256 colours the mixed fills take their nearest index of the cube or grey ramp, so a near grey never
// becomes a coloured neighbour, and they keep their contrast and their steps.
func TestTerminalThemeWith256ColorsKeepsItsGreys(t *testing.T) {
	set := exampleSet(t)
	for _, pair := range [][2]Color{{"#cdd6f4", "#1e1e2e"}, {"#24292f", "#ffffff"}, {"#ffffff", "#000000"}} {
		fg := hexRGB(t, pair[0])
		st := resolve(t, set, ThemeTerminal, ProfileANSI256, &TerminalColors{Foreground: pair[0], Background: pair[1]})
		p := st.Palette()
		var fills []rgb
		for _, c := range []Color{p.Panel, p.Surface, p.Selection} {
			n, err := strconv.Atoi(string(c))
			if err != nil || n < 16 || n > 255 {
				t.Fatalf("%s on %s: fill %q is not an index of the cube or ramp", pair[0], pair[1], c)
			}
			x := xterm256(n)
			if x.r != x.g || x.g != x.b {
				t.Errorf("%s on %s: fill %d is the coloured %+v", pair[0], pair[1], n, x)
			}
			if contrast(fg, x) < textContrast {
				t.Errorf("%s on %s: text on fill %d is %.2f:1", pair[0], pair[1], n, contrast(fg, x))
			}
			fills = append(fills, x)
		}
		if fills[0] == fills[1] || fills[1] == fills[2] {
			t.Errorf("%s on %s: the fills collapse: %v", pair[0], pair[1], fills)
		}
	}
}

// The mix keeps text readable on the ground and on every fill, and dimmed text on the ground and the panel,
// for dark and light pairs. A pair that is itself below 4.5:1, or so close above it that the fills would
// collapse into one step, gets no fills at all.
func TestMixedPaletteKeepsItsContrast(t *testing.T) {
	for _, tc := range []struct {
		bg, fg Color
		mixes  bool
	}{
		{"#000000", "#ffffff", true},
		{"#ffffff", "#000000", true},
		{"#282a36", "#f8f8f2", true},
		{"#1e1e2e", "#cdd6f4", true},
		{"#2e3440", "#d8dee9", true},
		{"#002b36", "#839496", true},
		{"#fdf6e3", "#657b83", false},
		{"#eff1f5", "#4c4f69", true},
		{"#ffffff", "#24292f", true},
		{"#757575", "#000000", false},
		{"#747474", "#000000", false},
	} {
		bg, fg := hexRGB(t, tc.bg), hexRGB(t, tc.fg)
		p, ok := mixPalette(fg, bg, nil)
		if ok != tc.mixes {
			t.Errorf("%s on %s (%.2f:1): mixes %v, want %v", tc.fg, tc.bg, contrast(fg, bg), ok, tc.mixes)
			continue
		}
		if !ok {
			continue
		}
		withText := p
		withText.Text = tc.fg
		if err := contrastError(withText); err != nil {
			t.Errorf("%s on %s: %v", tc.fg, tc.bg, err)
		}
		if p.Panel == p.Base || p.Surface == p.Panel || p.Selection == p.Surface {
			t.Errorf("%s on %s: the steps collapse: base %s panel %s surface %s selection %s",
				tc.fg, tc.bg, p.Base, p.Panel, p.Surface, p.Selection)
		}
	}
}

// A scheme with known ANSI colours takes them as hex values and its foreground as the text, so it can paint
// the ground; the roles keep the meaning of their slots.
func TestMixedPaletteTakesKnownANSIColors(t *testing.T) {
	var ansi [16]rgb
	for i := range ansi {
		ansi[i] = rgb{uint8(i * 16), uint8(255 - i*16), uint8(i * 8)}
	}
	fg, bg := rgb{0xf8, 0xf8, 0xf2}, rgb{0x28, 0x2a, 0x36}
	p, ok := mixPalette(fg, bg, &ansi)
	if !ok {
		t.Fatal("the pair does not mix")
	}
	for role, c := range map[Color]rgb{p.Text: fg, p.Accent: ansi[4], p.OK: ansi[2], p.Fail: ansi[1], p.Warn: ansi[3], p.Brand[0]: ansi[brandSlots[0]]} {
		if role != c.hex() {
			t.Errorf("role %q, want %q", role, c.hex())
		}
	}
	if p.Heat[4] != ansi[2].hex() {
		t.Errorf("the strongest heat level is %q, want the green %q", p.Heat[4], ansi[2].hex())
	}
}

func TestColorOf(t *testing.T) {
	if got := ColorOf(color.RGBA{0x1e, 0x1e, 0x2e, 0xff}); got != "#1e1e2e" {
		t.Errorf("ColorOf = %q", got)
	}
	if got := ColorOf(nil); got != "" {
		t.Errorf("ColorOf(nil) = %q", got)
	}
}
