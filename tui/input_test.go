package tui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// fieldModel is a text field that types every character it is handed, as a field or a passphrase prompt
// does, and counts the escapes and mouse events it meets.
type fieldModel struct {
	mu     sync.Mutex
	text   strings.Builder
	escape int
}

func (m *fieldModel) Init() tea.Cmd { return nil }

func (m *fieldModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handle(msg)
	return m, nil
}

func (m *fieldModel) handle(msg tea.Msg) {
	switch msg := msg.(type) {
	case InputBatch:
		for _, one := range msg {
			m.handle(one)
		}
	case tea.PasteMsg:
		m.text.WriteString(msg.Content)
	case tea.KeyPressMsg:
		switch {
		case msg.Code == tea.KeyEscape:
			m.escape++
		case msg.Text != "":
			m.text.WriteString(msg.Text)
		case msg.Mod == tea.ModAlt && msg.Code > ' ' && msg.Code < 0x7f:
			// A text field types alt with a character as that character.
			m.text.WriteRune(msg.Code)
		}
	}
}

func (m *fieldModel) View() tea.View { return tea.NewView("") }

func (m *fieldModel) value() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.text.String()
}

// chunkReader hands out its chunks one per Read, the way a terminal behind SSH or tmux delivers a short read,
// then reports the end of the input. It waits gap before each chunk after the first.
type chunkReader struct {
	mu     sync.Mutex
	chunks [][]byte
	gap    time.Duration
	paused int
	reads  int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	if r.reads > 0 && r.reads < r.paused {
		time.Sleep(r.gap)
	}
	r.reads++
	n := copy(p, r.chunks[0])
	if n < len(r.chunks[0]) {
		r.chunks[0] = r.chunks[0][n:]
	} else {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

// readerPause is a gap between two reads longer than the input reader waits for the rest of a sequence; the
// reader then reports what it has, the way it meets a report a slow connection split. splitPause is barely
// longer: should the reader still wait, it gets the report whole, which makes no text either.
const (
	readerPause = 2 * uv.DefaultEscTimeout
	splitPause  = uv.DefaultEscTimeout + 10*time.Millisecond
)

// testClock is the clock of the filter in a test; a pause in the input moves it on.
type testClock struct{ offset atomic.Int64 }

func (c *testClock) now() time.Time { return time.Now().Add(time.Duration(c.offset.Load())) }

// pause is a key that stands for a gap in the input: when the event loop meets it, the real timers run out
// and the clock of the filter moves on by a second. It is handled there, not by the reader, which runs ahead
// of the loop. inputEnd ends the program, so every key before it has been handled when Run returns.
const (
	pause    = "\x1b[23~"
	inputEnd = "\x1b[24~"
)

func runInput(t *testing.T, model *fieldModel, chunks ...string) []string {
	t.Helper()
	return runPaused(t, model, readerPause, chunks...)
}

// runPaused feeds the chunks gap apart to a real program around model through the filter and returns the
// messages that reached the model.
func runPaused(t *testing.T, model *fieldModel, gap time.Duration, chunks ...string) []string {
	t.Helper()
	clock := &testClock{}
	reader := &chunkReader{gap: gap, paused: len(chunks)}
	for _, c := range chunks {
		reader.chunks = append(reader.chunks, []byte(c))
	}
	reader.chunks = append(reader.chunks, []byte(inputEnd))
	filter := NewInputFilter(clock.now)
	var seen []string
	var record func(tea.Msg)
	record = func(msg tea.Msg) {
		switch msg := msg.(type) {
		case tea.MouseMsg:
			seen = append(seen, "mouse")
		case tea.KeyPressMsg:
			seen = append(seen, msg.String())
		case tea.PasteMsg:
			seen = append(seen, "paste")
		case InputBatch:
			for _, one := range msg {
				record(one)
			}
		}
	}
	observe := func(m tea.Model, msg tea.Msg) tea.Msg {
		if key, ok := msg.(tea.KeyPressMsg); ok && key.Code == tea.KeyF11 {
			clock.offset.Add(int64(time.Second))
			time.Sleep(2 * bracketHold)
			return nil
		}
		msg = filter.Filter(m, msg)
		if key, ok := msg.(tea.KeyPressMsg); ok && key.Code == tea.KeyF12 {
			return tea.QuitMsg{}
		}
		record(msg)
		return msg
	}
	p := tea.NewProgram(model, tea.WithInput(reader), tea.WithOutput(io.Discard), tea.WithFilter(observe),
		tea.WithoutSignals())
	filter.Attach(p.Send)
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("the program did not end")
	}
	return seen
}

// splitReports are mouse reports as a terminal sends them: wheel, left click press and release, motion, and a
// release alone.
var splitReports = []string{"\x1b[<65;12;5M", "\x1b[<64;3;40M", "\x1b[<0;70;9M\x1b[<0;70;9m", "\x1b[<35;120;40M", "\x1b[<0;7;19m"}

// A report split between two reads, with a pause after which the input reader gives up waiting for its rest,
// makes no text at any byte of the split.
func TestSplitMouseReportsMakeNoTextInAField(t *testing.T) {
	for _, report := range splitReports {
		stream := strings.Repeat(report, 3)
		for i := 0; i <= len(stream); i++ {
			t.Run(fmt.Sprintf("%q/%d", report, i), func(t *testing.T) {
				t.Parallel()
				m := &fieldModel{}
				m.text.WriteString("abc")
				keys := runPaused(t, m, splitPause, stream[:i], stream[i:])
				if got := m.value(); got != "abc" {
					t.Errorf("field %q after keys %q", got, keys)
				}
			})
		}
	}
}

// A report cut at any byte reaches the model as a mouse event when the pieces follow each other within the
// escape wait of the input reader: the reader joins them, and no key and no text comes of it. A machine that
// stalls the reader for longer than that wait splits the report for real, so a run is repeated twice before it
// counts as failed; a missing join fails every run.
func TestMouseReportsSplitWithinTheEscapeWaitStayMouseEvents(t *testing.T) {
	const quick = 5 * time.Millisecond
	for _, report := range splitReports {
		stream := report + report
		want := 2 * strings.Count(report, "\x1b")
		if strings.HasPrefix(report, "\x1b[<6") {
			// Two equal notches back to back are one notch the terminal reported twice.
			want = 1
		}
		for i := 1; i < len(report); i++ {
			t.Run(fmt.Sprintf("%q/%d", report, i), func(t *testing.T) {
				var keys []string
				for try := 0; try < 3; try++ {
					m := &fieldModel{}
					keys = runPaused(t, m, quick, stream[:i], stream[i:])
					if got := m.value(); got != "" {
						t.Fatalf("field %q after keys %q", got, keys)
					}
					if len(keys) == want && strings.Count(strings.Join(keys, ","), "mouse") == want {
						return
					}
				}
				t.Errorf("keys %q, want %d mouse events", keys, want)
			})
		}
	}
}

// An escape with nothing after it is a key and reaches the model at once.
func TestALoneEscapeIsAKey(t *testing.T) {
	m := &fieldModel{}
	if keys := runInput(t, m, "\x1b", pause); len(keys) != 1 || keys[0] != "esc" || m.escape != 1 {
		t.Errorf("keys %q, escapes %d, want one esc", keys, m.escape)
	}
}

// The characters of a report typed by hand, pasted, or following a report too late reach the field.
func TestTypedCharactersOfAReportReachTheField(t *testing.T) {
	t.Parallel()
	const paste = "\x1b[200~[<65;12;5M\x1b[201~"
	for _, c := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{"typed in one read", []string{"[<65;12;5M"}, "[<65;12;5M"},
		{"typed one by one", []string{"[", "<", "6", "5", ";", "M"}, "[<65;M"},
		{"after a pause", []string{"\x1b[", pause, "<65;12;5M"}, "[<65;12;5M"},
		{"alt+[ on its own", []string{"\x1b[", pause, "x"}, "[x"},
		{"alt+[ and a letter", []string{"\x1b[", "x"}, "[x"},
		{"alt+[ twice", []string{"\x1b[", "\x1b[", pause, "x"}, "[[x"},
		{"digits after a report", []string{"\x1b[<35;12;5M", "12;5M"}, "12;5M"},
		{"paste of a report", []string{paste}, "[<65;12;5M"},
		{"paste after a piece", []string{"\x1b[", "<65", paste}, "[<65;12;5M"},
		{"text after a report", []string{"\x1b[", "<65;12;5Mxy"}, "xy"},
		{"text after a pause", []string{"\x1b[", "<65;1", pause, "2;5M"}, "2;5M"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m := &fieldModel{}
			runInput(t, m, append(c.chunks, pause)...)
			if got := m.value(); got != c.want {
				t.Errorf("field = %q, want %q", got, c.want)
			}
		})
	}
}

// A paste reaches the field whole through the filter.
func TestAPasteReachesTheField(t *testing.T) {
	m := &fieldModel{}
	keys := runInput(t, m, "\x1b[200~ q enter\x1b[201~")
	if got := m.value(); got != " q enter" || len(keys) != 1 || keys[0] != "paste" {
		t.Errorf("field %q after %q", got, keys)
	}
}

// heldFilter is a filter on a clock the test moves; the release of a held "[" is sent by hand, so the order of
// the timer and the rest of the report is the test's choice.
func heldFilter() (*InputFilter, *time.Time) {
	now := time.Unix(1000, 0)
	f := NewInputFilter(func() time.Time { return now })
	f.Attach(func(tea.Msg) {})
	return f, &now
}

// keyTexts lists what a filter result delivers, in order.
func keyTexts(msg tea.Msg) []string {
	switch msg := msg.(type) {
	case nil:
		return nil
	case tea.KeyPressMsg:
		return []string{msg.String()}
	case tea.PasteMsg:
		return []string{"paste " + msg.Content}
	case InputBatch:
		var out []string
		for _, one := range msg {
			out = append(out, keyTexts(one)...)
		}
		return out
	}
	return []string{fmt.Sprintf("%T", msg)}
}

// The rest of a report that arrives while the "[" is still held, even late in the window, makes no key.
func TestHeldBracketSwallowsTheRestWithinTheWindow(t *testing.T) {
	f, now := heldFilter()
	if got := f.Filter(nil, tea.KeyPressMsg{Code: '[', Mod: tea.ModAlt}); got != nil {
		t.Fatalf("alt+[ passed at once: %v", keyTexts(got))
	}
	*now = now.Add(bracketHold - time.Millisecond)
	if got := f.Filter(nil, textKey("<65;12;5M")); got != nil {
		t.Errorf("the rest made keys %v", keyTexts(got))
	}
	if got := f.Filter(nil, heldKeyMsg{f.id}); got != nil {
		t.Errorf("the release made keys %v", keyTexts(got))
	}
}

// A head that comes after the hold ended is text, whether or not the window would still take it.
func TestHeadAfterTheReleaseIsText(t *testing.T) {
	f, now := heldFilter()
	f.Filter(nil, tea.KeyPressMsg{Code: tea.KeyEscape})
	f.Filter(nil, textKey("["))
	*now = now.Add(bracketHold)
	if got := keyTexts(f.Filter(nil, heldKeyMsg{f.id})); len(got) != 1 || got[0] != "[" {
		t.Fatalf("release: %v", got)
	}
	*now = now.Add(time.Millisecond)
	if got := keyTexts(f.Filter(nil, textKey("<65;12;5M"))); len(got) != 1 || got[0] != "<65;12;5M" {
		t.Errorf("head after the release: %v", got)
	}
}

// A head after the window is text too, and the held "[" goes first.
func TestHeadAfterTheWindowIsText(t *testing.T) {
	f, now := heldFilter()
	f.Filter(nil, tea.KeyPressMsg{Code: '[', Mod: tea.ModAlt})
	*now = now.Add(fragmentWindow + time.Millisecond)
	if got := keyTexts(f.Filter(nil, textKey("<65;12;5M"))); len(got) != 2 || got[0] != "alt+[" {
		t.Errorf("keys %v, want alt+[ then the text", got)
	}
}

// An alt+[ typed alone reaches the model once the hold ends; a stale release does nothing.
func TestLoneAltBracketArrivesAfterTheHold(t *testing.T) {
	f, _ := heldFilter()
	f.Filter(nil, tea.KeyPressMsg{Code: '[', Mod: tea.ModAlt})
	if got := f.Filter(nil, heldKeyMsg{f.id - 1}); got != nil {
		t.Errorf("a stale release made %v", keyTexts(got))
	}
	if got := keyTexts(f.Filter(nil, heldKeyMsg{f.id})); len(got) != 1 || got[0] != "alt+[" {
		t.Errorf("keys %v, want alt+[", got)
	}
}

// Without the Send of a program the "[" is still held, so a split report makes no text; the next key that
// is no piece brings it.
func TestAnUnattachedFilterStillHoldsTheBracket(t *testing.T) {
	now := time.Unix(1000, 0)
	f := NewInputFilter(func() time.Time { return now })
	if got := f.Filter(nil, tea.KeyPressMsg{Code: '[', Mod: tea.ModAlt}); got != nil {
		t.Fatalf("alt+[ passed at once: %v", keyTexts(got))
	}
	if got := f.Filter(nil, textKey("<65;12;5M")); got != nil {
		t.Errorf("the rest made keys %v", keyTexts(got))
	}
	f.Filter(nil, tea.KeyPressMsg{Code: '[', Mod: tea.ModAlt})
	if got := keyTexts(f.Filter(nil, textKey("x"))); len(got) != 2 || got[0] != "alt+[" || got[1] != "x" {
		t.Errorf("keys %v, want alt+[ then x", got)
	}
}

// A paste after a held "[" comes after it, and a piece after the paste is text.
func TestAPasteReleasesTheHeldBracket(t *testing.T) {
	f, _ := heldFilter()
	f.Filter(nil, tea.KeyPressMsg{Code: '[', Mod: tea.ModAlt})
	if got := keyTexts(f.Filter(nil, tea.PasteMsg{Content: "x"})); len(got) != 2 || got[0] != "alt+[" || got[1] != "paste x" {
		t.Errorf("keys %v, want alt+[ then the paste", got)
	}
	if got := keyTexts(f.Filter(nil, textKey("<65;12;5M"))); len(got) != 1 {
		t.Errorf("a head after the paste: %v", got)
	}
}

// The Linux console sends F1 to F5 as "\x1b[[" and a letter, which the reader reports apart.
func TestLinuxConsoleFunctionKeys(t *testing.T) {
	f, now := heldFilter()
	f.Filter(nil, uv.UnknownEvent("\x1b[["))
	if got := keyTexts(f.Filter(nil, textKey("C"))); len(got) != 1 || got[0] != "f3" {
		t.Errorf("keys %v, want f3", got)
	}
	f.Filter(nil, uv.UnknownEvent("\x1b[["))
	*now = now.Add(fragmentWindow + time.Millisecond)
	if got := keyTexts(f.Filter(nil, textKey("C"))); len(got) != 1 || got[0] != "C" {
		t.Errorf("late letter: %v, want C", got)
	}
}

// A notch the terminal reports as a press and a release arrives as two equal events at once: one step.
// Notches at the pace of a hand, at another cell or in the other direction are each a step.
func TestOneNotchIsOneStepWhateverTheTerminalReports(t *testing.T) {
	f, now := heldFilter()
	down := tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelDown}
	steps := func(msgs ...tea.MouseWheelMsg) int {
		n := 0
		for _, m := range msgs {
			if f.Filter(nil, m) != nil {
				n++
			}
		}
		return n
	}
	if n := steps(down, down); n != 1 {
		t.Errorf("a doubled notch is %d steps", n)
	}
	if n := steps(down, down, down); n != 2 {
		t.Errorf("a doubled notch and one more is %d steps", n)
	}
	*now = now.Add(wheelMerge)
	if n := steps(down); n != 1 {
		t.Errorf("a notch after the merge gap is %d steps", n)
	}
	up := tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelUp}
	other := tea.MouseWheelMsg{X: 6, Y: 3, Button: tea.MouseWheelUp}
	if n := steps(up, other); n != 2 {
		t.Errorf("notches up and at another cell are %d steps", n)
	}
}
