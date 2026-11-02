package tui

import (
	"fmt"
	"strconv"
)

// Palette names one colour per role. Screen code takes colours only from styles built on a palette, never
// as a literal. An empty role is one the theme leaves to the terminal; the palette of a profile without
// colour is the zero value.
type Palette struct {
	Base, Surface, Panel, Border, BorderFocus        Color
	Text, TextDim, Accent, Selection, OK, Warn, Fail Color
	// Heat are the levels of a heatmap: 0 is an empty cell, 4 the strongest.
	Heat [5]Color
	// Brand is a fixed row of category colours; CategoryColor picks one per name.
	Brand [8]Color
}

// roles lists the roles of p with their names, for checks that treat every role alike.
func (p *Palette) roles() []namedColor {
	out := []namedColor{
		{"base", &p.Base}, {"surface", &p.Surface}, {"panel", &p.Panel}, {"border", &p.Border},
		{"borderFocus", &p.BorderFocus}, {"text", &p.Text}, {"textDim", &p.TextDim}, {"accent", &p.Accent},
		{"selection", &p.Selection}, {"ok", &p.OK}, {"warn", &p.Warn}, {"fail", &p.Fail},
	}
	for i := range p.Heat {
		out = append(out, namedColor{"heat" + strconv.Itoa(i), &p.Heat[i]})
	}
	for i := range p.Brand {
		out = append(out, namedColor{"brand" + strconv.Itoa(i), &p.Brand[i]})
	}
	return out
}

type namedColor struct {
	name  string
	color *Color
}

// The least contrast of text and of dimmed text on the colours a palette puts behind them (WCAG).
const (
	textContrast = 4.5
	dimContrast  = 3.0
)

// contrastError reports the first role pair of p that is known on both sides and reads below its limit:
// text on the ground and every fill, dimmed text on the ground and the panel.
func contrastError(p Palette) error {
	pairs := []struct {
		fg, bg   Color
		fgn, bgn string
		least    float64
	}{
		{p.Text, p.Base, "text", "base", textContrast},
		{p.Text, p.Surface, "text", "surface", textContrast},
		{p.Text, p.Panel, "text", "panel", textContrast},
		{p.Text, p.Selection, "text", "selection", textContrast},
		{p.TextDim, p.Base, "textDim", "base", dimContrast},
		{p.TextDim, p.Panel, "textDim", "panel", dimContrast},
	}
	for _, pair := range pairs {
		fg, okFg := rgbOfColor(pair.fg)
		bg, okBg := rgbOfColor(pair.bg)
		if !okFg || !okBg {
			continue
		}
		if got := contrast(fg, bg); got < pair.least {
			return fmt.Errorf("%s on %s is %.2f:1, below %.1f:1", pair.fgn, pair.bgn, got, pair.least)
		}
	}
	return nil
}

// The shares of the foreground in the mixed roles. The fills are the most a pair with a contrast well above
// textContrast keeps; a tighter pair scales them down together.
const (
	sharePanel     = 0.06
	shareSurface   = 0.12
	shareSelection = 0.20
	shareBorder    = 0.28
	shareTextDim   = 0.60
)

// shareHeat are the shares of the heat levels; level 0, the empty cell, is a step of the foreground, the
// others lead to the green of the scheme where it is known and to the foreground where it is not.
var shareHeat = [5]float64{0.15, 0.35, 0.55, 0.78, 1}

// The ANSI slots of the roles a derived palette takes from the colours of a scheme or the terminal.
const (
	slotFail, slotOK, slotWarn, slotAccent, slotDim = 1, 2, 3, 4, 8
)

// brandSlots avoid the bright yellow and green, which vanish on a light background.
var brandSlots = [8]int{3, 4, 2, 6, 5, 1, 13, 12}

// mixPalette is the palette of foreground fg on background bg: the surfaces, the borders and the dimmed text
// are steps between the two, the accents the ANSI colours. Without ansi the accents name the ANSI slots, so
// the terminal colours them, and the text keeps the terminal's own foreground. It reports false for a pair
// whose own contrast is below textContrast, since every fill would lower it further, and for one so close to
// it that the fills could not be told apart.
func mixPalette(fg, bg rgb, ansi *[16]rgb) (Palette, bool) {
	if contrast(fg, bg) < textContrast {
		return Palette{}, false
	}
	fill := func(share float64) rgb { return mix(bg, fg, share) }
	var scale float64
	for step := 20; step >= 0; step-- {
		scale = float64(step) / 20
		if contrast(fg, fill(sharePanel*scale)) >= textContrast && contrast(fg, fill(shareSurface*scale)) >= textContrast &&
			contrast(fg, fill(shareSelection*scale)) >= textContrast {
			break
		}
	}
	panel, surface, selection := fill(sharePanel*scale), fill(shareSurface*scale), fill(shareSelection*scale)
	if panel == bg || surface == panel || selection == surface {
		return Palette{}, false
	}
	// The dimmed text moves towards the text until it reads; at the text itself it always does.
	var dim rgb
	for step := int(shareTextDim * 100); step <= 100; step++ {
		dim = fill(float64(step) / 100)
		if contrast(dim, bg) >= dimContrast && contrast(dim, panel) >= dimContrast {
			break
		}
	}
	slot := func(i int) Color {
		if ansi == nil {
			return Color(strconv.Itoa(i))
		}
		return ansi[i].hex()
	}
	p := Palette{
		Base: bg.hex(), Surface: surface.hex(), Panel: panel.hex(),
		Border: fill(shareBorder).hex(), BorderFocus: slot(slotAccent),
		TextDim: dim.hex(),
		Accent:  slot(slotAccent), OK: slot(slotOK), Fail: slot(slotFail), Warn: slot(slotWarn),
		Selection: selection.hex(),
	}
	green := fg
	if ansi != nil {
		p.Text = fg.hex()
		green = ansi[slotOK]
	}
	p.Heat[0] = fill(shareHeat[0]).hex()
	for i := 1; i < len(p.Heat); i++ {
		p.Heat[i] = mix(bg, green, shareHeat[i]).hex()
	}
	for i, s := range brandSlots {
		p.Brand[i] = slot(s)
	}
	return p, true
}

// plainPalette is the palette where colours are unknown or cannot be mixed: no ground and no fills, panels
// stand by their frames, and the selection is reverse video. The marks keep telling focus and selection
// apart, as they do without colour.
func plainPalette() Palette {
	slot := func(i int) Color { return Color(strconv.Itoa(i)) }
	p := Palette{
		Border: slot(slotDim), BorderFocus: slot(slotAccent), TextDim: slot(slotDim),
		Accent: slot(slotAccent), OK: slot(slotOK), Fail: slot(slotFail), Warn: slot(slotWarn),
	}
	for i, s := range brandSlots {
		p.Brand[i] = slot(s)
	}
	return p
}

// steppedTo256 names every hex role of p, a palette of foreground fg on background bg, by its nearest index,
// since a generic step-down may give a grey a coloured neighbour. It reports false where the steps lose
// their contrast, on the fills and on bg itself, or can no longer be told apart.
func steppedTo256(p Palette, fg, bg rgb) (Palette, bool) {
	for _, r := range p.roles() {
		if v, ok := rgbOfColor(*r.color); ok && r.color.isHex() {
			*r.color = Color(strconv.Itoa(nearest256(v)))
		}
	}
	check := p
	if check.Text == "" {
		// The text is the terminal's own foreground, which the terminal draws unchanged.
		check.Text = fg.hex()
	}
	text, _ := rgbOfColor(check.Text)
	dim, _ := rgbOfColor(check.TextDim)
	if contrastError(check) != nil || contrast(text, bg) < textContrast || contrast(dim, bg) < dimContrast ||
		p.Panel == p.Base || p.Surface == p.Panel || p.Selection == p.Surface {
		return Palette{}, false
	}
	return p, true
}

// DarkTheme is the hand-picked dark theme of this package, with a palette for true colour, 256 and 16
// colours, under the name the application gives it.
func DarkTheme(name string) Theme {
	return Theme{Name: name, Profiles: map[ColorProfile]Palette{
		ProfileTrueColor: {
			Base: "#0b141d", Surface: "#1a2b3c", Panel: "#14212f",
			Border: "#2a3d52", BorderFocus: "#3b82f6",
			Text: "#d6e2ee", TextDim: "#7f93a8",
			Accent: "#4c9aff", OK: "#3fb56b", Fail: "#f0525a", Warn: "#e7b84c",
			Selection: "#14336b",
			Heat:      [5]Color{"#16222d", "#1f5a3a", "#2a8a50", "#35a95a", "#4fd47a"},
			Brand:     [8]Color{"#f08c3a", "#4c9aff", "#e7b84c", "#2fb5a8", "#a35cf0", "#5fd07f", "#e0629a", "#ef5b5b"},
		},
		ProfileANSI256: {
			Base: "233", Surface: "236", Panel: "235",
			Border: "240", BorderFocus: "69",
			Text: "253", TextDim: "246",
			Accent: "75", OK: "71", Fail: "203", Warn: "179",
			Selection: "24",
			Heat:      [5]Color{"236", "22", "29", "35", "41"},
			Brand:     [8]Color{"208", "75", "179", "37", "135", "78", "168", "203"},
		},
		// The 16-colour surfaces stay black: a dim grey slot would fall into the text colours.
		ProfileANSI16: {
			Base: "0", Surface: "0", Panel: "0",
			Border: "8", BorderFocus: "12",
			Text: "15", TextDim: "7",
			Accent: "12", OK: "10", Fail: "9", Warn: "11",
			Selection: "4",
			Heat:      [5]Color{"8", "2", "2", "10", "10"},
			Brand:     [8]Color{"3", "12", "11", "6", "5", "10", "13", "9"},
		},
	}}
}

// LightTheme is the hand-picked light theme of this package, with a palette for true colour, 256 and 16
// colours, under the name the application gives it.
func LightTheme(name string) Theme {
	return Theme{Name: name, Profiles: map[ColorProfile]Palette{
		ProfileTrueColor: {
			Base: "#f6f8fb", Surface: "#e0e7ef", Panel: "#edf1f6",
			Border: "#b8c4d2", BorderFocus: "#1d5fd6",
			Text: "#14202e", TextDim: "#566778",
			Accent: "#1d5fd6", OK: "#1a7f43", Fail: "#c62828", Warn: "#9a6700",
			Selection: "#cfe0fb",
			Heat:      [5]Color{"#e1e8ef", "#b7e1c4", "#7cc794", "#3fa365", "#1b7a44"},
			Brand:     [8]Color{"#c2570c", "#1d5fd6", "#9a6700", "#0f7f79", "#7b3fc4", "#1a7f43", "#b8346f", "#c62828"},
		},
		ProfileANSI256: {
			Base: "255", Surface: "253", Panel: "254",
			Border: "250", BorderFocus: "26",
			Text: "234", TextDim: "241",
			Accent: "26", OK: "28", Fail: "160", Warn: "130",
			Selection: "153",
			Heat:      [5]Color{"254", "151", "114", "71", "28"},
			Brand:     [8]Color{"166", "26", "130", "30", "91", "28", "125", "160"},
		},
		ProfileANSI16: {
			Base: "15", Surface: "15", Panel: "15",
			Border: "8", BorderFocus: "4",
			Text: "0", TextDim: "8",
			Accent: "4", OK: "2", Fail: "1", Warn: "3",
			Selection: "7",
			Heat:      [5]Color{"7", "2", "2", "10", "10"},
			Brand:     [8]Color{"3", "4", "3", "6", "5", "2", "5", "1"},
		},
	}}
}
