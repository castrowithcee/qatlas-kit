package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func logKeys() LogViewKeys {
	return LogViewKeys{Up: "up", Down: "down", PageUp: "pageup", PageDown: "pagedown", First: "first", Last: "last",
		Search: "search", Enter: "enter", Back: "back", Follow: ActionLogFollow, NextMatch: ActionLogNextMatch,
		PrevMatch: ActionLogPrevMatch, Wrap: ActionLogWrap, Timestamps: ActionLogTimestamps}
}

func logKeymap(t *testing.T) *Keymap {
	t.Helper()
	k, err := NewKeymap(append(sampleActions(), LogViewActions()...), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newLog(t *testing.T, spec LogViewSpec, lang i18n.Lang) *LogView {
	t.Helper()
	keys := logKeymap(t)
	spec.Keys = logKeys()
	l, err := NewLogView(spec, keys, newTranslator(t, lang, nil, keys))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func typed(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func typeText(l *LogView, text string) {
	for _, r := range text {
		l.Update(typed(r))
	}
}

func numbered(n int) []LogLine {
	out := make([]LogLine, n)
	for i := range out {
		out[i] = LogLine{Text: fmt.Sprintf("line %03d", i+1)}
	}
	return out
}

func plainLog(t *testing.T) Styles { t.Helper(); return looks(t)["noneascii"] }

func viewLines(t *testing.T, l *LogView, w, h int) []string {
	t.Helper()
	out, _ := l.View(plainLog(t), w, h)
	return strings.Split(out, "\n")
}

func TestLogViewActionsFitATable(t *testing.T) {
	keys := logKeymap(t)
	if err := CheckCatalogs(ModuleCatalog()); err != nil {
		t.Fatal(err)
	}
	for _, a := range LogViewActions() {
		if _, ok := keys.KeyLabel(a.ID); !ok {
			t.Errorf("%s is not in the table", a.ID)
		}
	}
}

func TestNewLogViewRejectsWhatCannotWork(t *testing.T) {
	keys := logKeymap(t)
	text := newTranslator(t, i18n.English, nil, keys)
	cases := []struct {
		name string
		edit func(*LogViewSpec)
		want string
	}{
		{"negative buffer", func(s *LogViewSpec) { s.Lines = -1 }, "cannot be negative"},
		{"buffer over the limit", func(s *LogViewSpec) { s.Lines = LogViewMaxLines + 1 }, "the most is"},
		{"unknown action", func(s *LogViewSpec) { s.Keys.Wrap = "fold" }, "no action of the key table"},
		{"missing key", func(s *LogViewSpec) { s.Keys.Follow = "" }, "follow key is missing"},
		{"shared action", func(s *LogViewSpec) { s.Keys.Wrap = ActionLogFollow }, "also the follow key"},
		{"shared with a movement", func(s *LogViewSpec) { s.Keys.Last = "down" }, "also the down key"},
	}
	for _, tt := range cases {
		spec := LogViewSpec{Keys: logKeys()}
		tt.edit(&spec)
		if _, err := NewLogView(spec, keys, text); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := NewLogView(LogViewSpec{Keys: logKeys(), Lines: LogViewMaxLines}, keys, text); err != nil {
		t.Errorf("the largest buffer: %v", err)
	}
	if _, err := NewLogView(LogViewSpec{Keys: logKeys()}, nil, text); err == nil {
		t.Error("a missing key table is accepted")
	}
	spec := LogViewSpec{Keys: logKeys()}
	spec.Keys.First = ""
	if _, err := NewLogView(spec, keys, text); err != nil {
		t.Errorf("First is optional: %v", err)
	}
}

func TestLogViewFollowsNewLines(t *testing.T) {
	l := newLog(t, LogViewSpec{}, i18n.English)
	l.Append(numbered(50)...)
	rows := viewLines(t, l, 40, 6)
	if !strings.Contains(rows[4], "line 050") || !strings.Contains(rows[5], "[ L on ]") {
		t.Fatalf("a following view shows the newest line:\n%s", strings.Join(rows, "\n"))
	}
	l.Append(LogLine{Text: "line 051"})
	if rows = viewLines(t, l, 40, 6); !strings.Contains(rows[4], "line 051") {
		t.Errorf("new line does not follow:\n%s", strings.Join(rows, "\n"))
	}
	// Scrolling up stops following; new lines count up and the position stays.
	if !l.Update(tea.KeyPressMsg{Code: tea.KeyPgUp}) || l.Following() {
		t.Fatal("page up keeps following")
	}
	before := viewLines(t, l, 120, 6)[0]
	l.Append(LogLine{Text: "line 052"}, LogLine{Text: "line 053"})
	rows = viewLines(t, l, 120, 6)
	if rows[0] != before || !strings.Contains(rows[5], "L paused, 2 new") {
		t.Errorf("a paused view moved or misses the count:\n%s", strings.Join(rows, "\n"))
	}
	// The end key resumes.
	l.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	rows = viewLines(t, l, 40, 6)
	if !l.Following() || !strings.Contains(rows[4], "line 053") || !strings.Contains(rows[5], "[ L on ]") {
		t.Errorf("end does not resume:\n%s", strings.Join(rows, "\n"))
	}
	// The follow action pauses and resumes.
	l.Update(typed('L'))
	if l.Following() {
		t.Error("the follow key does not pause")
	}
	l.Update(typed('L'))
	if !l.Following() {
		t.Error("the follow key does not resume")
	}
	// Down at the end of a paused view leaves it paused; only the end key resumes.
	l.Act("up")
	for range 5 {
		l.Act("down")
	}
	viewLines(t, l, 40, 6)
	if l.Following() {
		t.Error("down resumes following")
	}
}

func TestLogViewWheel(t *testing.T) {
	l := newLog(t, LogViewSpec{}, i18n.English)
	l.Append(numbered(100)...)
	viewLines(t, l, 40, 11)
	l.Wheel(-1)
	if l.Following() {
		t.Fatal("wheel up keeps following")
	}
	rows := viewLines(t, l, 40, 11)
	if !strings.Contains(rows[9], "line 097") {
		t.Errorf("a notch is %d lines:\n%s", WheelTextLines, strings.Join(rows, "\n"))
	}
	l.Wheel(-1000)
	if rows = viewLines(t, l, 40, 11); !strings.Contains(rows[0], "line 001") {
		t.Errorf("wheel stops short of the first line:\n%s", strings.Join(rows, "\n"))
	}
	l.Wheel(1000)
	if !l.Following() {
		t.Error("wheel down to the end does not resume")
	}
}

func TestLogViewSearch(t *testing.T) {
	l := newLog(t, LogViewSpec{}, i18n.English)
	for i := range 40 {
		text := fmt.Sprintf("entry %02d ok", i)
		if i%10 == 3 {
			text = fmt.Sprintf("entry %02d ERROR disk", i)
		}
		l.Append(LogLine{Text: text})
	}
	viewLines(t, l, 60, 8)
	if l.Input() != InputNavigation {
		t.Fatal("navigation expected")
	}
	l.Update(typed('/'))
	if l.Input() != InputText || !l.Searching() {
		t.Fatal("the search key does not start typing")
	}
	// Letters are text now, also those that are actions.
	typeText(l, "error")
	if l.Query() != "error" {
		t.Fatalf("query = %q", l.Query())
	}
	rows := viewLines(t, l, 60, 8)
	if !strings.Contains(rows[7], "4 matches") {
		t.Errorf("status line:\n%s", strings.Join(rows, "\n"))
	}
	l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if l.Searching() || l.Input() != InputNavigation {
		t.Fatal("enter does not keep the search")
	}
	rows = viewLines(t, l, 60, 8)
	if l.Following() || !strings.Contains(rows[7], "match 4/4") {
		t.Errorf("the match at the first shown line is not selected:\n%s", strings.Join(rows, "\n"))
	}
	current := func() string {
		for _, r := range viewLines(t, l, 60, 8)[:7] {
			if strings.HasPrefix(r, ">") {
				return strings.TrimSpace(r[1:])
			}
		}
		return ""
	}
	want := []string{"entry 03 ERROR", "entry 13 ERROR", "entry 23 ERROR", "entry 33 ERROR"}
	for i, w := range want {
		l.Update(typed(']'))
		if got := current(); !strings.HasPrefix(got, w) {
			t.Errorf("next %d: current line %q, want %q", i, got, w)
		}
	}
	l.Update(typed('['))
	if got := current(); !strings.HasPrefix(got, "entry 23 ERROR") {
		t.Errorf("previous: %q", got)
	}
	// Other matching lines are marked without colour.
	marked := 0
	for _, r := range viewLines(t, l, 60, 30)[:29] {
		if strings.HasPrefix(r, "* ") {
			marked++
		}
	}
	if marked == 0 {
		t.Error("matching lines carry no mark")
	}
	// Back clears the kept search, then is left to the application.
	if !l.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) || l.Query() != "" {
		t.Error("back does not clear the search")
	}
	if l.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) {
		t.Error("back with nothing to clear is taken")
	}
	// Back while typing cancels.
	l.Update(typed('/'))
	typeText(l, "zzz")
	if rows = viewLines(t, l, 60, 8); !strings.Contains(rows[7], "no matches") {
		t.Errorf("no matches missing:\n%s", strings.Join(rows, "\n"))
	}
	l.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if l.Searching() || l.Query() != "" {
		t.Error("back does not cancel the typing")
	}
}

func TestLogViewSearchIgnoresCaseAndFollowsTheBuffer(t *testing.T) {
	l := newLog(t, LogViewSpec{Lines: 10}, i18n.English)
	l.Append(LogLine{Text: "Straße mark"}, LogLine{Text: "other"}, LogLine{Text: "ÄRGER mark"})
	l.Update(typed('/'))
	typeText(l, "ärger")
	if len(l.hits) != 1 {
		t.Fatalf("hits = %v, want the umlaut line", l.hits)
	}
	l.Act("enter")
	l.Act("back")
	l.Update(typed('/'))
	typeText(l, "mark")
	l.Act("enter")
	rows := viewLines(t, l, 40, 6)
	if !strings.Contains(rows[5], "match 1/2") {
		t.Fatalf("status: %q", rows[5])
	}
	// Old lines fall out: the hit and the current match go with them, the other stays.
	for i := range 9 {
		l.Append(LogLine{Text: fmt.Sprintf("filler %d", i)})
	}
	if len(l.hits) != 1 || l.hasCur {
		t.Fatalf("hits %v, current %v after the first line left", l.hits, l.hasCur)
	}
	if rows = viewLines(t, l, 120, 6); !strings.Contains(rows[5], "1 matches") {
		t.Errorf("status after drop: %q", rows[5])
	}
	l.Append(LogLine{Text: "new MARK"})
	if len(l.hits) != 1 || l.hits[0] != l.nextSeq-1 {
		t.Errorf("a new matching line is not found: %v", l.hits)
	}
	l.Update(typed(']'))
	if !l.hasCur || l.cur < l.first() {
		t.Error("next does not select a held line")
	}
}

func TestLogViewRingKeepsItsLimitAndDropsTheOldest(t *testing.T) {
	l := newLog(t, LogViewSpec{Lines: 5}, i18n.English)
	l.Append(numbered(12)...)
	if l.Len() != 5 {
		t.Fatalf("len = %d", l.Len())
	}
	rows := viewLines(t, l, 30, 7)
	if !strings.Contains(rows[0], "line 008") || !strings.Contains(rows[4], "line 012") {
		t.Errorf("the oldest lines stay:\n%s", strings.Join(rows, "\n"))
	}
	// A paused view whose first line left moves to the oldest line that remains.
	l.Act("first")
	l.Append(numbered(20)...)
	if rows = viewLines(t, l, 30, 7); !strings.Contains(rows[0], "line 016") {
		t.Errorf("paused position after the drop:\n%s", strings.Join(rows, "\n"))
	}
	d := newLog(t, LogViewSpec{}, i18n.English)
	d.Append(numbered(LogViewDefaultLines + 10)...)
	if d.Len() != LogViewDefaultLines {
		t.Errorf("default buffer holds %d lines", d.Len())
	}
	if l.nextSeq != 32 || d.Len() > LogViewMaxLines {
		t.Error("counters off")
	}
}

func TestLogViewWrapAndTimestamps(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	l := newLog(t, LogViewSpec{}, i18n.English)
	l.Append(LogLine{Text: "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda", Time: at, Source: "web"})
	rows := viewLines(t, l, 30, 5)
	if !strings.Contains(rows[0], "~") || strings.Contains(rows[0], "03:04:05") {
		t.Errorf("without wrap the line is shortened with the glyph, without time:\n%s", strings.Join(rows, "\n"))
	}
	l.Update(typed('w'))
	l.Update(typed('t'))
	rows = viewLines(t, l, 40, 6)
	if !strings.Contains(rows[0], "03:04:05 web") || strings.TrimSpace(rows[1]) == "" || strings.Contains(rows[1], "03:04") {
		t.Errorf("wrapped rows:\n%s", strings.Join(rows, "\n"))
	}
	joined := strings.Join(strings.Fields(strings.Join(rows[:4], " ")), " ")
	if !strings.Contains(joined, "kappa") {
		t.Errorf("wrapping lost text: %q", joined)
	}
	l.Update(typed('w'))
	l.Update(typed('t'))
	if rows = viewLines(t, l, 40, 6); strings.Contains(rows[0], "03:04:05") || !strings.Contains(rows[0], "~") {
		t.Errorf("switches do not toggle back:\n%s", strings.Join(rows, "\n"))
	}
	// The time column is blank for a line without time.
	l.Append(LogLine{Text: "plain"})
	l.Act(ActionLogTimestamps)
	if rows = viewLines(t, l, 40, 6); !strings.Contains(rows[1], "         plain") && !strings.Contains(rows[1], "plain") {
		t.Errorf("no-time row: %q", rows[1])
	}
}

func TestLogViewSplitsTextAtLineBreaks(t *testing.T) {
	l := newLog(t, LogViewSpec{}, i18n.English)
	l.Append(LogLine{Text: "one\r\ntwo\nthree\n"}, LogLine{Text: ""})
	if l.Len() != 4 {
		t.Errorf("len = %d, want 4 (three lines and an empty one)", l.Len())
	}
	if got := l.line(1).text; got != "two" {
		t.Errorf("a CRLF break leaves %q", got)
	}
}

// Nothing a line, a source or the search holds reaches the terminal as a control sequence.
func TestLogViewNeverDrawsControlSequences(t *testing.T) {
	hostile := []string{
		"a\x1b[2J\x1b[31mred", "b\x1b]8;;http://x.example\x07link", "bell\x07", "c1\u009b31m\u0085", "carriage\rreturn",
		"tab\there", "dcs\x1bP1;2|data\x1b\\", "bidi\u202eevil", "nul\x00byte", "\x1b",
	}
	l := newLog(t, LogViewSpec{Wrap: true, Timestamps: true}, i18n.English)
	for _, h := range hostile {
		l.Append(LogLine{Text: h, Source: h, Time: time.Unix(0, 0).UTC()})
	}
	l.Update(typed('/'))
	typeText(l, "\x1b[2J")
	l.Update(tea.KeyPressMsg{Code: tea.KeyExtended, Text: "\x1b]0;x\x07"})
	for _, name := range []string{"noneascii", "mono", "dark"} {
		out, _ := l.View(looks(t)[name], 70, 20)
		if name == "noneascii" && strings.Contains(out, "\x1b") {
			t.Errorf("%s draws an escape sequence: %q", name, out)
		}
		stripped := stripStyles(out)
		if strings.ContainsAny(stripped, "\x1b\x07\r\x00\u009b\u0085\u202e\t") {
			t.Errorf("%s: control character in %q", name, stripped)
		}
	}
	out := stripStyles(func() string { s, _ := l.View(plainLog(t), 70, 20); return s }())
	if !strings.Contains(out, `\x1b[31m`) || !strings.Contains(out, `\a`) || !strings.Contains(out, `\r`) || !strings.Contains(out, `\t`) {
		t.Errorf("escaped text is not visible:\n%s", out)
	}
	// The typed search is escaped as well: only printable characters enter the query.
	if strings.ContainsRune(l.Query(), 0x1b) {
		t.Errorf("query = %q", l.Query())
	}
}

func stripStyles(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestLogViewCutsALongLineVisibly(t *testing.T) {
	l := newLog(t, LogViewSpec{}, i18n.English)
	l.Append(LogLine{Text: strings.Repeat("x", 3*LogViewMaxLineRunes)}, LogLine{Text: strings.Repeat("\x1b", LogViewMaxLineRunes)})
	for i := range 2 {
		ln := l.line(i)
		if !ln.cut || utf8.RuneCountInString(ln.text) > LogViewMaxLineRunes {
			t.Errorf("line %d: cut %v, %d characters", i, ln.cut, utf8.RuneCountInString(ln.text))
		}
	}
	for _, wrap := range []bool{false, true} {
		l.wrap = wrap
		rows := viewLines(t, l, 50, 40)
		if wrap {
			for i := range 2 {
				all := l.rows(l.line(i), uint64(i), l.geometry(50), plainLog(t))
				if !strings.Contains(all[len(all)-1], "[cut]") {
					t.Errorf("wrapped line %d: mark missing at the end", i)
				}
			}
			continue
		}
		if !strings.Contains(rows[0], "[cut]") || !strings.Contains(rows[1], "[cut]") {
			t.Errorf("unwrapped long line shows no mark:\n%s", strings.Join(rows[:3], "\n"))
		}
	}
	l.Append(LogLine{Text: strings.Repeat("y", LogViewMaxLineRunes)})
	if l.line(2).cut {
		t.Error("a line of exactly the limit is marked")
	}
	l.Append(LogLine{Source: strings.Repeat("s", 1000), Text: "z"})
	if got := utf8.RuneCountInString(l.line(3).source); got > logMaxSourceRunes {
		t.Errorf("source holds %d characters", got)
	}
	if rows := viewLines(t, l, 60, 10); lipgloss.Width(rows[3]) != 60 {
		t.Errorf("row width %d", lipgloss.Width(rows[3]))
	}
}

func TestLogViewTargets(t *testing.T) {
	l := newLog(t, LogViewSpec{}, i18n.English)
	l.Append(numbered(30)...)
	_, targets := l.View(plainLog(t), 50, 8)
	m := NewMouseMap(targets, true)
	if r := m.Handle(tea.MouseWheelMsg{X: 3, Y: 2, Button: tea.MouseWheelUp}); r.Zone != ZoneRows || r.WheelSteps != -1 {
		t.Errorf("wheel result %+v", r)
	}
	// A click on the state switches following.
	if !l.Click(m.Handle(tea.MouseClickMsg{X: 1, Y: 7, Button: tea.MouseLeft})) || l.Following() {
		t.Error("a click on the state does not pause")
	}
	_, targets = l.View(plainLog(t), 50, 8)
	m = NewMouseMap(targets, true)
	if !l.Click(m.Handle(tea.MouseClickMsg{X: 1, Y: 7, Button: tea.MouseLeft})) || !l.Following() {
		t.Error("a click on the state does not resume")
	}
	// A click on the free area is the zone's and acts on nothing here.
	if l.Click(m.Handle(tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft})) {
		t.Error("a click on the lines acts")
	}
}

// buttonLog is a log view with a kept search that has matches, so every button is drawn.
func buttonLog(t *testing.T) *LogView {
	t.Helper()
	l := newLog(t, LogViewSpec{}, i18n.English)
	for i := range 30 {
		text := fmt.Sprintf("line %03d", i+1)
		if i%7 == 0 {
			text += " hit"
		}
		l.Append(LogLine{Text: text, Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)})
	}
	l.Update(typed('/'))
	typeText(l, "hit")
	l.Act("enter")
	l.Act(ActionLogFollow) // paused, so Follow has something to switch back
	return l
}

type logState struct {
	following, wrap, stamps, searching, hasCur bool
	cur, top                                   uint64
}

func stateOf(l *LogView) logState {
	return logState{l.following, l.wrap, l.stamps, l.searching, l.hasCur, l.cur, l.top}
}

// statusTargets draws the log view and returns its status row and the targets that sit on it.
func statusTargets(t *testing.T, l *LogView, w, h int) (string, []Target) {
	t.Helper()
	out, targets := l.View(plainLog(t), w, h)
	rows := strings.Split(out, "\n")
	var on []Target
	for _, tg := range targets {
		if tg.Rect.Y == h-1 && tg.Action != "" {
			on = append(on, tg)
		}
	}
	return rows[h-1], on
}

func TestLogViewButtonClickIsItsKey(t *testing.T) {
	keys := map[ActionID]tea.KeyPressMsg{
		ActionLogFollow: typed('L'), ActionLogWrap: typed('w'), ActionLogTimestamps: typed('t'),
		"search": typed('/'), ActionLogPrevMatch: typed('['), ActionLogNextMatch: typed(']'),
	}
	_, targets := statusTargets(t, buttonLog(t), 160, 8)
	seen := map[ActionID]bool{}
	for _, tg := range targets {
		seen[tg.Action] = true
	}
	for action := range keys {
		if !seen[action] {
			t.Errorf("no button for %s", action)
		}
	}
	for action, key := range keys {
		// Two views drawn the same way; in the second round with wrap switched first.
		for round := range 2 {
			byKey, byClick := buttonLog(t), buttonLog(t)
			if round == 1 {
				byKey.Act(ActionLogWrap)
				byClick.Act(ActionLogWrap)
			}
			byKey.View(plainLog(t), 160, 8)
			_, targets := byClick.View(plainLog(t), 160, 8)
			m := NewMouseMap(targets, true)
			var hit *Target
			for i := range targets {
				if targets[i].Action == action && targets[i].Rect.Y == 7 {
					hit = &targets[i]
				}
			}
			if hit == nil {
				t.Fatalf("%s round %d: no target", action, round)
			}
			if !byKey.Update(key) {
				t.Fatalf("%s: key not handled", action)
			}
			if !byClick.Click(m.Handle(tea.MouseClickMsg{X: hit.Rect.X + 1, Y: 7, Button: tea.MouseLeft})) {
				t.Fatalf("%s: click not handled", action)
			}
			if a, b := stateOf(byKey), stateOf(byClick); a != b {
				t.Errorf("%s round %d: key gives %+v, click gives %+v", action, round, a, b)
			}
		}
	}
}

func TestLogViewEveryVisibleControlIsATarget(t *testing.T) {
	for _, w := range []int{20, 30, 45, 60, 80, 100, 120, 160} {
		l := buttonLog(t)
		row, targets := statusTargets(t, l, w, 8)
		cells := []rune(row)
		for _, tg := range targets {
			text := string(cells[tg.Rect.X : tg.Rect.X+tg.Rect.Width])
			if !strings.HasPrefix(text, "[ ") || !strings.HasSuffix(text, " ]") {
				t.Errorf("width %d: target %q is no whole button", w, text)
			}
			for i := tg.Rect.X; i < tg.Rect.X+tg.Rect.Width; i++ {
				cells[i] = ' '
			}
		}
		rest := strings.TrimSpace(string(cells))
		if !strings.Contains(rest, "30") || strings.ContainsAny(rest, "[]") && strings.Count(rest, " ") > 3 {
			t.Errorf("width %d: text outside the targets or no position: %q in %q", w, rest, row)
		}
	}
}

func TestLogViewButtonsNameTheKeyAndTheState(t *testing.T) {
	actions := LogViewActions()
	for i := range actions {
		actions[i].Level, actions[i].Rebind = LevelGlobal, RebindReplace
		actions[i].DefaultKeys = []string{[]string{"f3", "f4", "f6", "f7", "f9"}[i]}
	}
	keys, err := NewKeymap(append(sampleActions(), actions...), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.Bind(map[ActionID]string{ActionLogWrap: "f14", ActionLogFollow: "f13"}); err != nil {
		t.Fatal(err)
	}
	l, err := NewLogView(LogViewSpec{Keys: logKeys()}, keys, newTranslator(t, i18n.English, nil, keys))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 30 {
		text := fmt.Sprintf("line %03d", i+1)
		if i%7 == 0 {
			text += " hit"
		}
		l.Append(LogLine{Text: text})
	}
	l.Act("search")
	typeText(l, "hit")
	l.Act("enter")
	row, _ := statusTargets(t, l, 160, 8)
	for _, want := range []string{"[ F13 paused ]", "[ F14 wrap lines off ]", "[ F9 timestamps off ]", "[ F6 previous match ]", "[ F4 next match ]"} {
		if !strings.Contains(row, want) {
			t.Errorf("%q missing in %q", want, row)
		}
	}
	l.Act(ActionLogWrap)
	l.Act(ActionLogFollow)
	row, _ = statusTargets(t, l, 160, 8)
	for _, want := range []string{"[ F13 following ]", "[ F14 wrap lines on ]"} {
		if !strings.Contains(row, want) {
			t.Errorf("%q missing in %q", want, row)
		}
	}
	// Without labels the keys stay and the state is still in words.
	row, _ = statusTargets(t, l, 110, 8)
	if !strings.Contains(row, "[ F14 on ]") || !strings.Contains(row, "[ F9 off ]") {
		t.Errorf("short form: %q", row)
	}
	// Match jumps need a search with matches.
	plain := newLog(t, LogViewSpec{}, i18n.English)
	plain.Append(numbered(5)...)
	if row, _ = statusTargets(t, plain, 160, 4); strings.Contains(row, "match") {
		t.Errorf("match buttons without a search: %q", row)
	}
}

func TestLogViewStatusDegradesByRank(t *testing.T) {
	l := buttonLog(t)
	// Labels go in this order, then the buttons go in this order, the first one first.
	order := []struct {
		action ActionID
		labels []string
	}{
		{ActionLogTimestamps, []string{"timestamps"}}, {ActionLogWrap, []string{"wrap lines"}},
		{ActionLogPrevMatch, []string{"previous match"}}, {ActionLogNextMatch, []string{"next match"}},
		{ActionLogFollow, []string{"following", "paused"}},
	}
	prev := 1 << 30
	var prevKept map[ActionID]bool
	for w := 160; w >= 20; w-- {
		row, targets := statusTargets(t, l, w, 8)
		if got := lipgloss.Width(row); got != w {
			t.Fatalf("width %d: row is %d cells", w, got)
		}
		if !strings.Contains(row, "of 30") && !strings.Contains(row, "/30") {
			t.Fatalf("width %d: position missing: %q", w, row)
		}
		cells := []rune(row)
		kept := map[ActionID]bool{}
		labelled := map[ActionID]bool{}
		for _, tg := range targets {
			kept[tg.Action] = true
			text := string(cells[tg.Rect.X : tg.Rect.X+tg.Rect.Width])
			for _, o := range order {
				if o.action != tg.Action {
					continue
				}
				for _, word := range o.labels {
					labelled[tg.Action] = labelled[tg.Action] || strings.Contains(text, word)
				}
			}
		}
		for i := range order {
			for j := i + 1; j < len(order); j++ {
				hi, lo := order[i].action, order[j].action // hi goes before lo
				if kept[hi] && labelled[hi] && kept[lo] && !labelled[lo] {
					t.Errorf("width %d: %s keeps its label where %s lost it: %q", w, hi, lo, row)
				}
				if kept[lo] && !kept[hi] && labelled[lo] {
					t.Errorf("width %d: %s is gone before %s lost its label: %q", w, hi, lo, row)
				}
			}
		}
		if len(targets) > prev {
			t.Errorf("width %d: %d buttons after %d", w, len(targets), prev)
		}
		for a := range kept {
			if prevKept != nil && !prevKept[a] {
				t.Errorf("width %d: %s is back", w, a)
			}
		}
		prev, prevKept = len(targets), kept
		if w >= 45 && !(kept[ActionLogFollow] && kept["search"]) {
			t.Errorf("width %d: follow and search must stay", w)
		}
	}
}

func TestLogViewHints(t *testing.T) {
	l := newLog(t, LogViewSpec{}, i18n.English)
	keys := logKeymap(t)
	text := newTranslator(t, i18n.English, nil, keys)
	plain := func() string {
		var parts []string
		for _, g := range keys.Hints(l.Hints(), l.Input(), text) {
			parts = append(parts, g.Text())
		}
		return strings.Join(parts, "|")
	}
	if got := plain(); !strings.Contains(got, "/ search") || !strings.Contains(got, "L follow") || strings.Contains(got, "next match") {
		t.Errorf("hints = %q", got)
	}
	l.Append(LogLine{Text: "abc"})
	l.Update(typed('/'))
	typeText(l, "a")
	if got := plain(); strings.Contains(got, "follow") || !strings.Contains(got, "esc") {
		t.Errorf("hints while typing = %q", got)
	}
	l.Act("enter")
	if got := plain(); !strings.Contains(got, "] next match") {
		t.Errorf("hints with a search = %q", got)
	}
}

func TestLogViewSweep(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, look := range []string{"dark", "noneascii", "mono"} {
		st := looks(t)[look]
		for _, wrap := range []bool{false, true} {
			l := newLog(t, LogViewSpec{Lines: 40, Wrap: wrap, Timestamps: true}, i18n.German)
			for i := range 60 {
				l.Append(LogLine{Text: fmt.Sprintf("zeile %d Größe 日本語 wide text %s", i, strings.Repeat("ab ", i%9)), Time: at, Source: []string{"", "web", "a-long-source-name-x"}[i%3]})
			}
			l.Update(typed('/'))
			typeText(l, "ab")
			l.Act("enter")
			for w := 10; w <= 160; w += 5 {
				for h := 1; h <= 30; h++ {
					out, targets := l.View(st, w, h)
					rows := strings.Split(out, "\n")
					if len(rows) != h {
						t.Fatalf("%s wrap=%v %dx%d: %d rows", look, wrap, w, h, len(rows))
					}
					for i, r := range rows {
						if got := lipgloss.Width(r); got != w {
							t.Fatalf("%s wrap=%v %dx%d row %d is %d cells: %q", look, wrap, w, h, i, got, r)
						}
					}
					for _, tg := range targets {
						if tg.Rect.X < 0 || tg.Rect.X+tg.Rect.Width > w || tg.Rect.Y >= h {
							t.Fatalf("%dx%d: target %+v outside", w, h, tg.Rect)
						}
					}
				}
			}
		}
	}
	l := newLog(t, LogViewSpec{}, i18n.English)
	for _, size := range [][2]int{{0, 0}, {0, 5}, {5, 0}, {-3, -3}} {
		l.View(plainLog(t), size[0], size[1])
	}
}

func goldenLog(t *testing.T) *LogView {
	t.Helper()
	at := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	l := newLog(t, LogViewSpec{Timestamps: true}, i18n.English)
	entries := []struct{ src, text string }{
		{"api", "listening on port 8080"}, {"db", "connection pool ready (4 of 4)"},
		{"api", "GET /plants 200 12ms"}, {"api", "GET /plants/7 404 3ms"},
		{"worker", "job 31 failed: timeout after 30s while waiting for the upstream service to answer"},
		{"db", "slow query: select * from beds where soil = 'clay' order by name"},
		{"api", "POST /plants 201 41ms"}, {"worker", "job 32 started"},
		{"worker", "job 32 finished"}, {"api", "GET /beds 200 9ms"},
		{"api", "GET /beds/2 500 17ms error: bed not found"}, {"db", "checkpoint complete"},
		{"api", "GET /plants 200 11ms"}, {"worker", "job 33 failed: Timeout after 30s"},
	}
	for i, e := range entries {
		l.Append(LogLine{Text: e.text, Source: e.src, Time: at.Add(time.Duration(i) * 1500 * time.Millisecond)})
	}
	l.Update(typed('/'))
	typeText(l, "fail")
	l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return l
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("view differs from %s:\ngot:\n%s\nwant:\n%s", golden, got, want)
	}
}

func TestGoldenLogView(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		w, h       int
		wrap       bool
	}{
		{"logview.narrow.golden", "noneascii", 40, 22, true},
		{"logview.narrow.nowrap.golden", "noneascii", 40, 16, false},
		{"logview.wide.golden", "noneascii", 120, 16, false},
		{"logview.full.golden", "noneascii", 170, 16, false},
		{"logview.wide.dark.golden", "dark", 120, 16, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := goldenLog(t)
			if tt.wrap {
				l.Act(ActionLogWrap)
			}
			out, _ := l.View(looks(t)[tt.look], tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}
