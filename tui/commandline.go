package tui

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Context is the place the command line was opened from, for the availability of a command.
type Context struct {
	Area  AreaID
	Tab   TabID
	Zone  FocusZone
	Focus InputFocus
}

// Command is one entry of the command line: a name for something a key or a tab click already does. Running
// it yields the command, and the application does with Action, Area and Tab exactly what the key or the tab
// click does, so a command is never a second way with rules of its own.
type Command struct {
	// Name is lower-case words separated by single blanks; Aliases are further such names that match it.
	Name    string
	Aliases []string
	// Description says what the command does; it names no key, the key stands beside it.
	Description TextID
	Action      ActionID
	// Key is the position, counted from 1, of the one key of Action that does the same, for an action whose
	// keys each do another thing, such as one digit per area; 0 shows all keys of Action.
	Key  int
	Area AreaID
	Tab  TabID
	// Hidden keeps the command out of the list of the empty line; typed text still finds it.
	Hidden bool
	// Available reports whether the command acts in ctx, which is where its key acts, and the reason it does
	// not otherwise, with the next step.
	Available func(ctx Context) (bool, string)
}

// Always is the availability of a command that acts everywhere.
func Always(Context) (bool, string) { return true, "" }

// CommandLineKeys are the actions of the key table that move through the command line: Enter runs the chosen
// command and Back closes the line.
type CommandLineKeys struct {
	Up, Down, Next, Prev, Enter, Back ActionID
}

// CommandLineSpec is what a command line runs and the keys it reads.
type CommandLineSpec struct {
	Commands []Command
	Keys     CommandLineKeys
}

// maxCommandRows is the most matches the command line shows at once; the list scrolls with the choice.
const maxCommandRows = 12

// commandPrompt leads the input line. It is a prompt, not the key that opens the line, which may be rebound.
const commandPrompt = ": "

// CommandLine finds and runs commands by name. It holds the typed text, the chosen match, the message of a
// refusal and the place it was opened from; it reads keys where text is typed, so a letter is always text.
type CommandLine struct {
	commands []Command
	nav      CommandLineKeys
	keys     *Keymap
	text     Translator

	input  []rune
	cursor int
	msg    string
	ctx    Context
}

// NewCommandLine checks spec and returns a closed, empty command line. It rejects an empty list, a name or
// alias that is not lower-case words separated by single blanks or that two commands share, a command without
// description or availability, one that names neither an action nor an area, a tab without its area, a key
// position outside the keys of a known action, two commands that do the same, and a move key that names no
// action of keys.
func NewCommandLine(spec CommandLineSpec, keys *Keymap, text Translator) (*CommandLine, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the command line needs a key table and a translator")
	}
	commands, nav := spec.Commands, spec.Keys
	var errs []error
	for _, id := range []ActionID{nav.Up, nav.Down, nav.Next, nav.Prev, nav.Enter, nav.Back} {
		if _, ok := keys.index[id]; !ok {
			errs = append(errs, fmt.Errorf("command line key %q is no action of the key table", id))
		}
	}
	if len(commands) == 0 {
		errs = append(errs, errors.New("the command line holds no command"))
	}
	names := map[string]string{}
	// A click finds its command by what it does, so no two commands may do the same.
	type effect struct {
		action ActionID
		area   AreaID
		tab    TabID
	}
	effects := map[effect]string{}
	list := make([]Command, len(commands))
	for i, c := range commands {
		c.Aliases = append([]string(nil), c.Aliases...)
		list[i] = c
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("command %q: "+format, append([]any{c.Name}, args...)...))
		}
		for _, n := range append([]string{c.Name}, c.Aliases...) {
			switch other, dup := names[n]; {
			case !commandName(n):
				fail("%q is not lower-case words separated by single blanks", n)
			case dup:
				fail("%q is also a name of %q", n, other)
			default:
				names[n] = c.Name
			}
		}
		if c.Description == "" {
			fail("no description")
		}
		if c.Available == nil {
			fail("no availability; use Always for a command that acts everywhere")
		}
		switch {
		case c.Action == "" && c.Area == "":
			fail("names neither an action nor an area")
		case c.Action != "" && !isPlaceholderName(string(c.Action)):
			fail("action %q is no valid action ID", c.Action)
		}
		if c.Tab != "" && c.Area == "" {
			fail("tab %q without its area", c.Tab)
		}
		if c.Key != 0 {
			i, ok := keys.index[c.Action]
			if !ok || c.Key < 0 || c.Key > len(keys.actions[i].DefaultKeys) {
				fail("key %d is no key of action %q", c.Key, c.Action)
			}
		}
		e := effect{c.Action, c.Area, c.Tab}
		if other, dup := effects[e]; dup {
			fail("does the same as %q; make it an alias", other)
		}
		effects[e] = c.Name
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &CommandLine{commands: list, nav: nav, keys: keys, text: text}, nil
}

func commandName(n string) bool {
	if n == "" || normalizeCommand(n) != n {
		return false
	}
	for _, r := range n {
		if !unicode.IsPrint(r) || unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// normalizeCommand is typed text in the form names take: lower case, words separated by one blank.
func normalizeCommand(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// Reset empties the line, for a command line closed by other means than its keys.
func (c *CommandLine) Reset() { c.input, c.cursor, c.msg = nil, 0, "" }

// SetContext sets the place the command line is opened from, which decides where a command acts. The
// application sets it when it opens the line.
func (c *CommandLine) SetContext(ctx Context) { c.ctx = ctx }

// Query is the typed text.
func (c *CommandLine) Query() string { return string(c.input) }

// Matches are the commands that match the typed text in their order: those whose name or an alias is the text
// first, then those that begin with it, then those with a word that begins with it, then those that hold it
// inside a word; each group keeps the order of the commands. Case does not count. The empty line lists the
// commands that are not hidden.
func (c *CommandLine) Matches() []Command {
	query := normalizeCommand(string(c.input))
	if query == "" {
		var shown []Command
		for _, cmd := range c.commands {
			if !cmd.Hidden {
				shown = append(shown, cmd)
			}
		}
		return shown
	}
	var groups [4][]Command
	for _, cmd := range c.commands {
		rank := len(groups)
		for _, n := range append([]string{cmd.Name}, cmd.Aliases...) {
			switch {
			case n == query:
				rank = min(rank, 0)
			case strings.HasPrefix(n, query):
				rank = min(rank, 1)
			case strings.Contains(n, " "+query):
				rank = min(rank, 2)
			case strings.Contains(n, query):
				rank = min(rank, 3)
			}
		}
		if rank < len(groups) {
			groups[rank] = append(groups[rank], cmd)
		}
	}
	return append(append(append(groups[0], groups[1]...), groups[2]...), groups[3]...)
}

// Update handles a key of the open command line. done reports that the line closes: with the zero Command for
// the back key, and with the chosen command for the enter key where it is available. An unknown or
// unavailable command keeps the line open with the typed text and says why. Every other key that types text
// edits the line.
func (c *CommandLine) Update(msg tea.KeyPressMsg) (cmd Command, done bool) {
	matches := c.Matches()
	is := func(id ActionID) bool { return c.keys.Is(msg, id, InputText) }
	switch {
	case is(c.nav.Back):
		c.Reset()
		return Command{}, true
	case is(c.nav.Up) || is(c.nav.Prev):
		c.cursor, c.msg = wrap(c.cursor-1, max(len(matches), 1)), ""
		return Command{}, false
	case is(c.nav.Down) || is(c.nav.Next):
		c.cursor, c.msg = wrap(c.cursor+1, max(len(matches), 1)), ""
		return Command{}, false
	case is(c.nav.Enter):
		return c.run(matches)
	}
	before := len(c.input)
	switch {
	case msg.Code == tea.KeyBackspace && msg.Mod == 0 && len(c.input) > 0:
		c.input = c.input[:len(c.input)-1]
	case msg.Text != "" && msg.Mod&^tea.ModShift == 0:
		for _, r := range msg.Text {
			if unicode.IsPrint(r) {
				c.input = append(c.input, r)
			}
		}
	}
	if len(c.input) != before {
		c.cursor, c.msg = 0, ""
	}
	return Command{}, false
}

// Click is a click on a match the command line drew, as the MouseMap of its targets reports it: it chooses
// the match and runs it as the enter key does.
func (c *CommandLine) Click(r MouseResult) (cmd Command, done bool) {
	if !r.Handled || r.Zone != ZoneRows {
		return Command{}, false
	}
	matches := c.Matches()
	for i, m := range matches {
		if m.Action == r.Action && m.Area == r.Area && m.Tab == r.Tab {
			c.cursor = i
			return c.run(matches)
		}
	}
	return Command{}, false
}

func (c *CommandLine) run(matches []Command) (Command, bool) {
	if len(matches) == 0 {
		c.msg = c.text.Lookup(TextCommandUnknown, normalizeCommand(string(c.input)))
		return Command{}, false
	}
	cmd := matches[min(c.cursor, len(matches)-1)]
	if ok, reason := cmd.Available(c.ctx); !ok {
		c.msg = cmd.Name + ": " + reason
		return Command{}, false
	}
	c.Reset()
	cmd.Aliases = append([]string(nil), cmd.Aliases...)
	return cmd, true
}

// View draws the command line with styles as a panel of width by height cells: the input line, the matches
// with name and description at the left and the key in force at the right, where at most twelve show and the
// list scrolls with the choice, the position where not all show, and the message of a refusal. A command that
// does not act where the line was opened is dimmed. It returns one target per drawn match, counted from the
// top left of the panel.
func (c *CommandLine) View(styles Styles, width, height int) (string, []Target) {
	title := c.text.Lookup(TextCommandTitle)
	if width <= 0 || height <= 0 {
		return strings.Join(styles.Panel(title, "", nil, width, height, true), "\n"), nil
	}
	x0, y0, inner, room := 2, 1, width-4, height-2
	if width < 4 || height < 2 {
		// Without room for a frame the panel draws the lines alone.
		x0, y0, inner, room = 0, 0, width, height
	}
	matches := c.Matches()
	lines := []string{c.inputLine(styles, inner), ""}
	var msg []string
	if c.msg != "" {
		msg = strings.Split(styles.Message(MessageError, c.text.Lookup(TextError, c.msg), inner), "\n")
	}
	var targets []Target
	switch {
	case len(matches) == 0 && c.msg != "":
	case len(matches) == 0:
		lines = append(lines, styles.Render(StyleHint, styles.Truncate(Escape(c.text.Lookup(TextCommandNoMatch)), inner)))
	default:
		avail := max(room-len(lines)-len(msg), 1)
		rows := min(avail, maxCommandRows, len(matches))
		if len(matches) > rows && rows == avail && rows > 1 {
			// A list that goes on shows its position in a line of its own.
			rows--
		}
		cursor := min(c.cursor, len(matches)-1)
		start := max(min(cursor-rows/2, len(matches)-rows), 0)
		end := min(start+rows, len(matches))
		keyW, nameW := 0, 0
		for _, m := range matches[start:end] {
			keyW = max(keyW, lipgloss.Width(c.keyLabel(m)))
			nameW = max(nameW, lipgloss.Width(m.Name))
		}
		for i := start; i < end; i++ {
			targets = append(targets, Target{Rect: Rect{X: x0, Y: y0 + len(lines), Width: inner, Height: 1},
				Zone: ZoneRows, Action: matches[i].Action, Area: matches[i].Area, Tab: matches[i].Tab})
			lines = append(lines, c.row(styles, matches[i], i == cursor, nameW, keyW, inner))
		}
		if len(matches) > end-start {
			pos := c.text.Lookup(TextListPosition, start+1, end, len(matches))
			lines = append(lines, styles.Render(StyleHint, styles.Truncate(Escape(pos), inner)))
		}
	}
	lines = append(lines, msg...)
	var kept []Target
	for _, t := range targets {
		if t.Rect.Y-y0 < room {
			kept = append(kept, t)
		}
	}
	return strings.Join(styles.Panel(title, "", lines, width, height, true), "\n"), kept
}

// inputLine is the prompt and the typed text, the end in view where it is long, or the dimmed placeholder.
func (c *CommandLine) inputLine(styles Styles, width int) string {
	room := max(width-len(commandPrompt), 0)
	if len(c.input) == 0 {
		return commandPrompt + styles.Render(StyleHint, styles.Truncate(Escape(c.text.Lookup(TextCommandPrompt)), room))
	}
	return commandPrompt + styles.TruncateLeft(Escape(string(c.input)), room)
}

// keyLabel is the key column of a command: the keys of its action in force, or the one key it names.
func (c *CommandLine) keyLabel(cmd Command) string {
	if cmd.Key == 0 {
		return Escape(c.keys.Label(cmd.Action))
	}
	// A replaced key takes the place of the single default key, and an extra key goes after the defaults, so
	// the position of a default key holds under every binding.
	return Escape(c.keys.keyLabel(c.keys.bound[c.keys.index[cmd.Action]][cmd.Key-1]))
}

func (c *CommandLine) row(styles Styles, cmd Command, chosen bool, nameW, keyW, width int) string {
	key := c.keyLabel(cmd)
	key = strings.Repeat(" ", keyW-lipgloss.Width(key)) + key
	avail := max(width-2-keyW-1, 1)
	left := cmd.Name + strings.Repeat(" ", nameW-lipgloss.Width(cmd.Name)) + "  " + Escape(c.text.Lookup(cmd.Description))
	left = Pad(styles.Truncate(left, avail), avail)
	if ok, _ := cmd.Available(c.ctx); !ok {
		left = styles.Render(StyleHint, left)
	}
	return styles.Row(left+" "+styles.Render(StyleHint, key), width, chosen)
}
