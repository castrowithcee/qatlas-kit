package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func barSpec(width int) FrameSpec {
	spec := sampleSpec(width, 12)
	spec.Bar = []BarAction{
		{Action: "save", Label: "ex.save", Kind: ButtonPrimary},
		{Action: "new", Label: "ex.new"},
		{Action: "remove", Label: "ex.remove", Kind: ButtonDanger},
	}
	return spec
}

func barRow(f styledFrame, spec FrameSpec) string {
	rows := plainRows(first(f.Render(spec)))
	return rows[f.Workspace(spec).Y-1]
}

// The bar is right-aligned with the first action at the right end. Where the width runs short the last
// actions of the application, at the left, give way; it never wraps.
func TestActionBarCutsFromTheLeft(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	for _, tt := range []struct {
		width int
		want  string
	}{
		{100, "[ F8 Remove ] [ n New ] [ F2 save ]"},
		{35, "[ F8 Remove ] [ n New ] [ F2 save ]"},
		{34, "[ n New ] [ F2 save ]"},
		{21, "[ n New ] [ F2 save ]"},
		{20, "[ F2 save ]"},
		{11, "[ F2 save ]"},
	} {
		if got := barRow(f, barSpec(tt.width)); got != strings.Repeat(" ", tt.width-len(tt.want))+tt.want {
			t.Errorf("width %d: %q, want %q", tt.width, got, tt.want)
		}
	}
	// Where not even the first fits, there is no row.
	spec := barSpec(10)
	if rows := plainRows(first(f.Render(spec))); strings.Contains(strings.Join(rows, "\n"), "F2 save ]") {
		t.Errorf("a button that does not fit stands:\n%s", strings.Join(rows, "\n"))
	}
}

// Primary and danger change the look only, never the place.
func TestActionBarKeepsTheOrderOfTheApplication(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := barSpec(100)
	spec.Bar[0], spec.Bar[2] = spec.Bar[2], spec.Bar[0]
	if got := strings.TrimSpace(barRow(f, spec)); got != "[ F2 save ] [ n New ] [ F8 Remove ]" {
		t.Errorf("got %q", got)
	}
}

// An action in the bar leaves the footer; one the bar cut stays there.
func TestActionBarActionsLeaveTheFooter(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := barSpec(100)
	spec.Footer = append(spec.Footer, Hint{Action: "new", Label: "ex.new", Priority: 5}, Hint{Action: "remove", Label: "ex.remove", Priority: 5})
	if foot := footerOf(f, spec); strings.Contains(foot, "save") || strings.Contains(foot, "New") || strings.Contains(foot, "Remove") {
		t.Errorf("an action of the bar stands in the footer:\n%s", foot)
	}
	spec.Width = 21 // the bar keeps save and new, remove at the left goes to the footer
	foot := footerOf(f, spec)
	if strings.Contains(foot, "save") || strings.Contains(foot, "New") || !strings.Contains(foot, "Remove") {
		t.Errorf("the footer of a cut bar:\n%s", foot)
	}
	spec.Width = 100
	spec.Bar = nil
	if foot := footerOf(f, spec); !strings.Contains(foot, "save") {
		t.Errorf("without a bar the footer keeps save:\n%s", foot)
	}
}

// A rebinding shows its new key in the button.
func TestActionBarShowsTheRebinding(t *testing.T) {
	f, keys := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	if err := keys.Bind(map[ActionID]string{"save": "f3"}); err != nil {
		t.Fatal(err)
	}
	if got := barRow(f, barSpec(100)); !strings.HasSuffix(got, "[ F3 save ]") {
		t.Errorf("got %q", got)
	}
}

// An action without a key that acts in the input focus, an unknown action and one without a label do not
// appear and take no room.
func TestActionBarLeavesOutAnActionWithoutKey(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := barSpec(100)
	spec.Bar = []BarAction{{Action: "new", Label: "ex.new"}, {Action: "nothing", Label: "ex.open"}, {Action: "save", Label: "ex.save"}}
	if got := strings.TrimSpace(barRow(f, spec)); got != "[ F2 save ] [ n New ]" {
		// in InputNavigation the letter key acts
		t.Errorf("navigation: %q", got)
	}
	spec.Input = InputText // the letter key types text there
	if got := strings.TrimSpace(barRow(f, spec)); got != "[ F2 save ]" {
		t.Errorf("text: %q", got)
	}
	spec.Bar = []BarAction{{Action: "new", Label: "ex.new"}}
	ws := f.Workspace(spec)
	spec.Bar = nil
	if ws != f.Workspace(spec) {
		t.Error("a bar without a button takes a row")
	}
}

// Every button is a click target with its action; a click runs it as its key runs it.
func TestActionBarButtonsAreClickTargets(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := barSpec(100)
	out, targets := f.Render(spec)
	y := f.Workspace(spec).Y - 1
	row := plainRows(out)[y]
	m := NewMouseMap(targets, true)
	for action, label := range map[ActionID]string{"save": "F2 save", "new": "n New", "remove": "F8 Remove"} {
		x := strings.Index(row, label)
		for _, dx := range []int{0, len(label) - 1} {
			res := m.Handle(tea.MouseClickMsg{Button: tea.MouseLeft, X: x + dx, Y: y})
			if !res.Handled || res.Action != action || res.Zone != "" {
				t.Errorf("click on %q at +%d: %+v", label, dx, res)
			}
		}
	}
	if res := m.Handle(tea.MouseClickMsg{Button: tea.MouseLeft, X: 60, Y: y}); res.Action != "" {
		t.Errorf("click beside the buttons: %+v", res)
	}
}

// The bar takes one row from the workspace in every form of the navigation and is the first row below the
// rule.
func TestActionBarTakesOneRowInEveryNavForm(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	for _, nav := range []NavForm{NavTabs, NavPathLine, NavPathHeader, NavHeaderChoice} {
		spec := barSpec(100)
		spec.Nav = nav
		with := f.Workspace(spec)
		bar := spec.Bar
		spec.Bar = nil
		without := f.Workspace(spec)
		if with.Height != without.Height-1 || with.Y != without.Y+1 {
			t.Errorf("nav %d: workspace %+v with the bar, %+v without", nav, with, without)
		}
		spec.Bar = bar
		rows := plainRows(first(f.Render(spec)))
		if len(rows) != spec.Height || !strings.HasSuffix(rows[with.Y-1], "[ F2 save ]") || !strings.HasPrefix(rows[with.Y-2], "---") {
			t.Errorf("nav %d: bar and rule misplaced:\n%s", nav, strings.Join(rows, "\n"))
		}
	}
}

// A text that holds control characters is escaped in the button as in the footer.
func TestActionBarEscapesTheLabel(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := barSpec(100)
	spec.Bar = []BarAction{{Action: "save", Label: "bad\x1b[31mtext"}}
	if out := first(f.Render(spec)); strings.Contains(out, "\x1b[31m") {
		t.Error("an escape sequence of a label reached the screen")
	}
}

func TestGoldenActionBar(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		lang       i18n.Lang
		width      int
	}{
		{"frame.bar.wide.golden", "light256", i18n.English, 100},
		{"frame.bar.wide.dark.golden", "dark", i18n.English, 100},
		{"frame.bar.wide.de.golden", "light256", i18n.German, 100},
		{"frame.bar.wide.de.dark.golden", "dark", i18n.German, 100},
		{"frame.bar.narrow.golden", "light256", i18n.English, 40},
		{"frame.bar.narrow.dark.golden", "dark", i18n.English, 40},
		{"frame.bar.narrow.de.golden", "light256", i18n.German, 40},
		{"frame.bar.narrow.de.dark.golden", "dark", i18n.German, 40},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := sampleFrame(t, looks(t)[tt.look], tt.lang)
			spec := barSpec(tt.width)
			spec.Height = 9
			checkGolden(t, tt.name, first(f.Render(spec))+"\n")
		})
	}
}

// Text reads on every fill of a button and the key on its pad, in every theme the kit ships, light and dark,
// at the contrast the palette demands of text. Fills and key move towards the text only as far as that needs.
func TestKeyButtonContrast(t *testing.T) {
	set := exampleSet(t)
	terms := []*TerminalColors{
		{Foreground: "#d0d0d0", Background: "#101010"},
		{Foreground: "#202020", Background: "#f4f4f4"},
	}
	for _, name := range set.Names() {
		for _, term := range terms {
			p := resolve(t, set, name, ProfileTrueColor, term).Palette()
			for _, c := range []struct {
				fg, bg Color
				what   string
			}{
				{p.Text, p.Surface, "text on the button"},
				{p.Base, reachContrast(p.Accent, p.Base, p.Text), "text on primary"},
				{p.Base, reachContrast(p.Fail, p.Base, p.Text), "text on danger"},
				{reachContrast(p.Accent, p.Panel, p.Text), p.Panel, "key on the pad"},
			} {
				fg, okFg := rgbOfColor(c.fg)
				bg, okBg := rgbOfColor(c.bg)
				if !okFg || !okBg {
					continue
				}
				if got := contrast(fg, bg); got < textContrast {
					t.Errorf("%s on %s: %s is %.2f:1", name, term.Background, c.what, got)
				}
			}
		}
	}
}

// A button is a filled block without brackets: a pad for the key and a fill for the label. Without colour
// the label is reversed and the key bold before it; without any style only the characters tell.
func TestKeyButtonLooks(t *testing.T) {
	l := looks(t)
	p := l["dark"].Palette()
	sgr := func(c Color) string { v := hexRGB(t, c); return fmt.Sprintf("48;2;%d;%d;%d", v.r, v.g, v.b) }
	normal := l["dark"].KeyButton(ButtonNormal, "F2", "Save")
	for _, want := range []string{sgr(p.Panel), sgr(p.Surface)} {
		if !strings.Contains(normal, want) {
			t.Errorf("normal button lacks the fill %q: %q", want, normal)
		}
	}
	if got := ansi.Strip(normal); got != " F2  Save " {
		t.Errorf("text %q", got)
	}
	for kind, fill := range map[ButtonKind]Color{ButtonPrimary: p.Accent, ButtonDanger: p.Fail} {
		out := l["dark"].KeyButton(kind, "F2", "Save")
		v := hexRGB(t, reachContrast(fill, p.Base, p.Text))
		if want := fmt.Sprintf("48;2;%d;%d;%d", v.r, v.g, v.b); !strings.Contains(out, want) || !strings.Contains(out, sgr(p.Panel)) {
			t.Errorf("kind %d: fill %q or pad missing: %q", kind, want, out)
		}
	}
	mono := l["mono"].KeyButton(ButtonNormal, "F2", "Save")
	noColorSequence(t, "mono", mono)
	if i, j := strings.Index(mono, "\x1b[1m"), strings.Index(mono, "7m"); i < 0 || j < i {
		t.Errorf("mono: key not bold before the reversed label: %q", mono)
	}
	if danger := l["mono"].KeyButton(ButtonDanger, "F2", "Save"); !strings.Contains(danger, "1;7m") {
		t.Errorf("mono danger is not bold and reversed: %q", danger)
	}
	if got := l["noneascii"].KeyButton(ButtonDanger, "F2", "Speichern"); got != "[ F2 Speichern ]" {
		t.Errorf("none: %q", got)
	}
}

// A click on the pad and on the label of a filled button runs its action.
func TestActionBarFilledButtonClickTargets(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["dark"], i18n.English)
	spec := barSpec(100)
	out, targets := f.Render(spec)
	y := f.Workspace(spec).Y - 1
	row := plainRows(out)[y]
	m := NewMouseMap(targets, true)
	for _, probe := range []string{"F2", "save", "n", "New"} {
		x := strings.Index(row, probe)
		want := ActionID("save")
		if probe == "n" || probe == "New" {
			want = "new"
		}
		if res := m.Handle(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}); res.Action != want {
			t.Errorf("click on %q: %+v", probe, res)
		}
	}
	// The first cell of the pad and the last cell of the label are inside the target.
	x := strings.Index(row, "F2") - 1
	if res := m.Handle(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}); res.Action != "save" {
		t.Errorf("click on the edge of the pad: %+v", res)
	}
}
