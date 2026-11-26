package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// FileKind says what an entry of a folder is. The zero value is a plain file, which is never opened as a
// folder.
type FileKind uint8

const (
	FileRegular FileKind = iota
	FileDir
	// FileLink is a link; the browser never follows it, so enter reports it like a file.
	FileLink
)

// FileEntry is one entry of a folder as the application read it. Every text in it is untrusted plain text:
// control characters are escaped when it is drawn.
type FileEntry struct {
	// Name is the name of the entry alone, one part of a path. SetEntries refuses a name that is empty, ".",
	// "..", or holds "/", "\" or NUL, so no entry can lead out of the folder it was listed in.
	Name string
	Kind FileKind
	// LinkTarget is where a link points, shown behind the name.
	LinkTarget string
	// Size is the size in bytes of a regular file; a negative size and any other kind show none.
	Size int64
	// Rights are the permissions as text, such as "rwxr-xr-x".
	Rights string
	Owner  string
	// Modified is the time of the last change; the zero Time shows none.
	Modified time.Time
	// Unreadable marks an entry the application could not read, so it is told by a word, not by colour.
	Unreadable bool
}

// FileBrowserKeys are the actions of the key table a file browser reads.
type FileBrowserKeys struct {
	Up, Down, PageUp, PageDown, First, Last ActionID
	// Left and Right must name actions too; a folder has no groups to fold, so they do nothing.
	Left, Right ActionID
	// Enter opens the marked folder; on a file or a link it reports the action Enter.
	Enter ActionID
	// Search starts typing a filter. A filter that starts with "*" and goes on asks for matches below the
	// shown folder instead, see FileSearch.
	Search ActionID
	// Back leaves the list of matches, then clears the filter, then goes up one folder; at the root it is left to
	// the application.
	Back ActionID
	// Reload asks for the entries of the shown folder again; empty for none.
	Reload ActionID
}

// FileBrowserSpec is what a file browser shows and the keys it reads. The application supplies the entries.
type FileBrowserSpec struct {
	Keys FileBrowserKeys
	// Actions are what the application offers on the marked entry or the folder, such as open, edit, new,
	// rename, copy and delete. Each is an action of the key table with a label, stands as a button below the
	// list and acts by its key or a click.
	Actions []FormButton
	// RootLabel is the plain text the path line shows for the root; empty takes TextFileRoot.
	RootLabel string
}

// FileLoad asks the application for the entries of one folder. Dir is the folder as the names of its parts
// below the root, empty for the root itself; the application joins them below its own root and checks the
// result against it, since the browser only guarantees that no part is ".", ".." or holds a separator.
// Token identifies the request: the answer must carry the FileLoad it answers.
type FileLoad struct {
	Dir   []string
	Token uint64
}

// FileAction reports that the action Action was triggered. Dir is the shown folder and Entry the marked
// entry, valid where HasEntry is true; the application runs the action and checks the names against its root.
type FileAction struct {
	Action   ActionID
	Dir      []string
	Entry    FileEntry
	HasEntry bool
}

// FileSearch asks the application for the entries below the folder Dir, at any depth, whose name holds Query.
// Dir is the shown folder as in FileLoad, Query the text behind the "*" of the filter with the white space
// around it cut off, never empty. Token identifies the request: the answer must carry the FileSearch it
// answers. A new FileSearch or a FileLoad makes the application's running search superfluous; Searching says
// when the browser no longer waits for one.
type FileSearch struct {
	Dir   []string
	Query string
	Token uint64
}

// FileMatch is one entry found by a search. Path is the entry as the names of its parts below the root, not
// below the searched folder, so a match is a place in the same tree as a FileLoad. Like a FileEntry name, each
// part must be a plain name, and the path must lead below the searched folder.
type FileMatch struct {
	Path []string
	Kind FileKind
}

// FileSearchEnd says why a search ended.
type FileSearchEnd uint8

// The reasons a search ended; the zero value is a search that read everything below the folder. An unknown
// value shows the generic text.
const (
	FileSearchComplete FileSearchEnd = iota
	// FileSearchEntryLimit: the limit of entries to read was reached.
	FileSearchEntryLimit
	// FileSearchTimeLimit: the time limit ran out.
	FileSearchTimeLimit
	// FileSearchMatchLimit: the limit of matches was reached.
	FileSearchMatchLimit
	// FileSearchCanceled: the search was stopped before its end.
	FileSearchCanceled
)

// FileFound is the answer to a FileSearch: the matches and, unless the search ran to its end, the reason it
// stopped. Limit is the limit that was reached, in entries, seconds or matches by End, and shown in the text
// for it; zero or less shows the text without a number.
type FileFound struct {
	Matches []FileMatch
	End     FileSearchEnd
	Limit   int
}

// FileResult is what a key press or a click asks the application to do: Load, Search, or the Action to run.
type FileResult struct {
	Load   *FileLoad
	Search *FileSearch
	Action *FileAction
}

// FileBrowser shows the folders and files below a root as a table with a clickable path line and the
// actions of the application as buttons. It reads no file system: the application answers a FileLoad with
// SetEntries or SetError, a FileSearch with SetMatches or SetSearchError, and runs the actions. The browser walks only within the root, as a list of name
// parts, never as a joined string. Update, Act and Click return the result.
type FileBrowser struct {
	spec FileBrowserSpec
	keys *Keymap
	text Translator
	list *List

	dir     []string
	token   uint64
	byID    map[ItemID]FileEntry
	keep    ItemID
	loading bool

	items []ListItem // the entries of the shown folder, also while the matches are shown
	empty string
	// The search: hits is true from the request until the list of matches is left, waiting while the answer
	// is due, and seen is the filter text the browser last decided on.
	hits    bool
	waiting bool
	stoken  uint64
	seen    string
	matches map[ItemID]FileMatch
	note    string
}

// The target items of a file browser.
const (
	fileCrumbRef  = "\x00p"
	fileButtonRef = "\x00F"
	fileUpRef     = "\x00U"
)

// NewFileBrowser checks spec and returns a browser at the root with nothing loaded; Load asks for the first
// folder. It rejects a missing key or one that names no action of keys or that two keys share, and an action
// without label, whose key is no action of keys, is a key of the browser or is on another button.
func NewFileBrowser(spec FileBrowserSpec, keys *Keymap, text Translator) (*FileBrowser, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the file browser needs a key table and a translator")
	}
	k := spec.Keys
	named := []namedKey{{"up", k.Up, true}, {"down", k.Down, true}, {"page up", k.PageUp, true},
		{"page down", k.PageDown, true}, {"first", k.First, true}, {"last", k.Last, true}, {"left", k.Left, true},
		{"right", k.Right, true}, {"enter", k.Enter, true}, {"search", k.Search, true}, {"back", k.Back, true},
		{"reload", k.Reload, false}}
	errs := checkKeys("file browser", keys, named)
	for i, b := range spec.Actions {
		switch {
		case !keys.known(b.Action):
			errs = append(errs, fmt.Errorf("file browser: action %d: %q is no action of the key table", i, b.Action))
		case slices.ContainsFunc(named, func(n namedKey) bool { return n.id == b.Action }):
			errs = append(errs, fmt.Errorf("file browser: action %d: %q is a key of the browser", i, b.Action))
		case slices.ContainsFunc(spec.Actions[:i], func(o FormButton) bool { return o.Action == b.Action }):
			errs = append(errs, fmt.Errorf("file browser: action %d: %q is on another button", i, b.Action))
		}
		if b.Label == "" {
			errs = append(errs, fmt.Errorf("file browser: action %d has no label", i))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	list, err := NewList(ListSpec{
		Columns: []Column{
			{Title: TextFileColName},
			{Title: TextFileColSize, Optional: true, Right: true},
			{Title: TextFileColModified, Optional: true},
			{Title: TextFileColRights, Optional: true, Dimmed: true},
			{Title: TextFileColOwner, Optional: true, Dimmed: true},
		},
		Keys: ListKeys{Up: k.Up, Down: k.Down, PageUp: k.PageUp, PageDown: k.PageDown, First: k.First, Last: k.Last,
			Left: k.Left, Right: k.Right, Enter: k.Enter, Search: k.Search, Back: k.Back},
		Empty: TextFileEmpty,
	}, keys, text)
	if err != nil {
		return nil, err
	}
	spec.Actions = slices.Clone(spec.Actions)
	return &FileBrowser{spec: spec, keys: keys, text: text, list: list, byID: map[ItemID]FileEntry{}}, nil
}

// validFileName reports whether name is one part of a path that stays in the folder it is joined to.
func validFileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

// entryID is the ID of an entry in the list: the quoted name, so a name with control characters is a valid ID.
func entryID(name string) ItemID { return ItemID(strconv.Quote(name)) }

// Dir is the shown folder as the names of its parts below the root, empty at the root.
func (b *FileBrowser) Dir() []string { return slices.Clone(b.dir) }

// Loading reports whether the answer to the last FileLoad is still due.
func (b *FileBrowser) Loading() bool { return b.loading }

// Searching reports whether a search is asked and its list of matches not left, whether the answer is due or
// shown. An application that runs the search stops it when this turns false or a new FileSearch comes.
func (b *FileBrowser) Searching() bool { return b.hits }

// Load asks for the entries of the folder dir, the names of its parts below the root, and empties the list
// until the answer comes. It returns no request for a part that is not a plain name.
func (b *FileBrowser) Load(dir []string) FileResult {
	if slices.ContainsFunc(dir, func(n string) bool { return !validFileName(n) }) {
		return FileResult{}
	}
	return b.request(slices.Clone(dir), "")
}

func (b *FileBrowser) request(dir []string, keep ItemID) FileResult {
	b.dir, b.keep = dir, keep
	b.hits, b.waiting, b.seen, b.matches, b.note = false, false, "", nil, ""
	b.token++
	b.loading = true
	b.show(nil, b.text.Lookup(TextFileLoading))
	return FileResult{Load: &FileLoad{Dir: slices.Clone(dir), Token: b.token}}
}

// show replaces the entries, and the sentence an empty list shows, keeping the selection where it can.
// While the matches are shown it only keeps them for when the list is left.
func (b *FileBrowser) show(items []ListItem, empty string) {
	b.byID = map[ItemID]FileEntry{}
	b.items, b.empty = items, empty
	if b.hits {
		return
	}
	b.draw(items, empty)
	if len(items) == 0 {
		b.list.Reset()
	}
}

func (b *FileBrowser) draw(items []ListItem, empty string) {
	_ = b.list.SetItems(items) // the items are checked before
	b.list.SetEmpty(empty)
}

// SetEntries shows the entries answering load. current is false, and nothing changes, when load is not the
// last request: a late answer for a folder left in the meantime never replaces the shown one. An entry with an
// invalid name, or a name listed twice, refuses the whole folder: it is shown as failed and err says why.
func (b *FileBrowser) SetEntries(load FileLoad, entries []FileEntry) (current bool, err error) {
	if load.Token != b.token || !b.loading {
		return false, nil
	}
	b.loading = false
	seen := map[string]bool{}
	for _, e := range entries {
		switch {
		case !validFileName(e.Name):
			err = fmt.Errorf("the entry %q is not a plain name", e.Name)
		case seen[e.Name]:
			err = fmt.Errorf("the entry %q is listed twice", e.Name)
		}
		if err != nil {
			b.show(nil, b.text.Lookup(TextFileBadName, strconv.Quote(e.Name)))
			return true, err
		}
		seen[e.Name] = true
	}
	items := make([]ListItem, len(entries))
	byID := make(map[ItemID]FileEntry, len(entries))
	for i, e := range entries {
		id := entryID(e.Name)
		byID[id] = e
		items[i] = ListItem{ID: id, Cells: b.cells(e)}
	}
	b.show(items, b.text.Lookup(TextFileEmpty))
	b.byID = byID
	if b.keep != "" && !b.hits {
		b.list.Select(b.keep)
	}
	return true, nil
}

// SetError shows that the folder of load could not be read, with the reason cause gives; like SetEntries it
// ignores a load that is not the last request. cause is untrusted text and shown escaped.
func (b *FileBrowser) SetError(load FileLoad, cause error) (current bool) {
	if load.Token != b.token || !b.loading {
		return false
	}
	b.loading = false
	reason := ""
	if cause != nil {
		reason = cause.Error()
	}
	b.show(nil, b.text.Lookup(TextFileFailed, reason))
	return true
}

// cells are the cells of an entry in the columns of the table.
func (b *FileBrowser) cells(e FileEntry) []string {
	name := e.Name
	switch e.Kind {
	case FileDir:
		name += "/"
	case FileLink:
		name += " -> " + e.LinkTarget
	}
	if e.Unreadable {
		name += " (" + b.text.Lookup(TextFileUnreadable) + ")"
	}
	size, modified := "", ""
	if e.Kind == FileRegular && e.Size >= 0 {
		size = fileSize(e.Size)
	}
	if !e.Modified.IsZero() {
		modified = e.Modified.Format("2006-01-02 15:04")
	}
	return []string{name, size, modified, e.Rights, e.Owner}
}

// fileSize writes n bytes with a binary unit and at most one decimal.
func fileSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	size := float64(n)
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB", "PiB"} {
		size /= 1024
		if size < 1024 || unit == "PiB" {
			return fmt.Sprintf("%.1f %s", size, unit)
		}
	}
	return ""
}

// Selected is the marked entry; none while the list is empty, loading or failed.
func (b *FileBrowser) Selected() (FileEntry, bool) {
	id, ok := b.list.Selected()
	if !ok {
		return FileEntry{}, false
	}
	e, ok := b.byID[id]
	return e, ok
}

// SetFocused says whether the rows have the focus; a click on the marked row opens it only then.
func (b *FileBrowser) SetFocused(on bool) { b.list.SetFocused(on) }

// Input is the input focus the browser reads keys in: text while a filter is typed.
func (b *FileBrowser) Input() InputFocus { return b.list.Input() }

// Wheel moves the selection by the notches of the wheel and stops at the ends.
func (b *FileBrowser) Wheel(steps int) { b.list.Wheel(steps) }

// up goes one folder up; at the root there is none.
func (b *FileBrowser) up() FileResult {
	if len(b.dir) == 0 {
		return FileResult{}
	}
	return b.request(slices.Clone(b.dir[:len(b.dir)-1]), "")
}

// activate opens the entry id: a folder is loaded, anything else is reported as the action Enter.
func (b *FileBrowser) activate(id ItemID) FileResult {
	if b.hits {
		m, ok := b.matches[id]
		if !ok {
			return FileResult{}
		}
		last := len(m.Path) - 1
		return b.request(slices.Clone(m.Path[:last]), entryID(m.Path[last]))
	}
	e, ok := b.byID[id]
	if !ok {
		return FileResult{}
	}
	if e.Kind == FileDir && validFileName(e.Name) {
		return b.request(append(slices.Clone(b.dir), e.Name), "")
	}
	return b.report(b.spec.Keys.Enter)
}

func (b *FileBrowser) report(action ActionID) FileResult {
	a := &FileAction{Action: action, Dir: slices.Clone(b.dir)}
	a.Entry, a.HasEntry = b.Selected()
	return FileResult{Action: a}
}

// Update handles a key. handled is false for a key the browser leaves to the application, such as back at the
// root.
func (b *FileBrowser) Update(msg tea.KeyPressMsg) (FileResult, bool) {
	focus := b.list.Input()
	if focus != InputText {
		for _, a := range b.actionIDs() {
			if b.keys.Is(msg, a, focus) {
				return b.Act(a)
			}
		}
	}
	for _, id := range []ActionID{b.spec.Keys.Back, b.spec.Keys.Enter} {
		if b.keys.Is(msg, id, focus) {
			if res, ok := b.hitKey(id); ok {
				return res, true
			}
		}
	}
	open, handled := b.list.Update(msg)
	switch {
	case open != "":
		return b.activate(open), true
	case handled:
		return b.syncSearch(), true
	case b.keys.Is(msg, b.spec.Keys.Back, focus) && len(b.dir) > 0:
		return b.up(), true
	}
	return FileResult{}, false
}

// actionIDs are the actions of the buttons and the reload key.
func (b *FileBrowser) actionIDs() []ActionID {
	var ids []ActionID
	for _, a := range b.spec.Actions {
		ids = append(ids, a.Action)
	}
	if b.spec.Keys.Reload != "" {
		ids = append(ids, b.spec.Keys.Reload)
	}
	return ids
}

// Act runs an action of the browser as its key does, for a click or a command that stands for the key.
func (b *FileBrowser) Act(id ActionID) (FileResult, bool) {
	k := b.spec.Keys
	switch {
	case id == "":
		return FileResult{}, false
	case id == k.Reload:
		keep, _ := b.list.Selected()
		return b.request(slices.Clone(b.dir), keep), true
	case slices.ContainsFunc(b.spec.Actions, func(a FormButton) bool { return a.Action == id }):
		return b.report(id), true
	}
	if res, ok := b.hitKey(id); ok {
		return res, true
	}
	open, handled := b.list.Act(id)
	switch {
	case open != "":
		return b.activate(open), true
	case handled:
		return b.syncSearch(), true
	case id == k.Back && len(b.dir) > 0:
		return b.up(), true
	}
	return FileResult{}, false
}

// Click handles a click on a target the browser drew, as the MouseMap of its targets reports it: a part of
// the path line goes to that folder, a button acts as its key does, and a row is the list's own click.
func (b *FileBrowser) Click(r MouseResult) (FileResult, bool) {
	if !r.Handled {
		return FileResult{}, false
	}
	if n, ok := strings.CutPrefix(r.Item, fileCrumbRef); ok {
		depth, err := strconv.Atoi(n)
		if err != nil || depth < 0 || depth > len(b.dir) {
			return FileResult{}, false
		}
		if depth == len(b.dir) {
			return FileResult{}, true
		}
		return b.request(slices.Clone(b.dir[:depth]), ""), true
	}
	if r.Item == fileUpRef {
		return b.up(), true
	}
	if n, ok := strings.CutPrefix(r.Item, fileButtonRef); ok {
		i, err := strconv.Atoi(n)
		if err != nil || i < 0 || i >= len(b.spec.Actions) {
			return FileResult{}, false
		}
		return b.Act(b.spec.Actions[i].Action)
	}
	open, handled := b.list.Click(r)
	if open != "" {
		return b.activate(open), true
	}
	if handled {
		return b.syncSearch(), true
	}
	return FileResult{}, false
}

// Hints are the keys of the browser for the footer: the actions of the application first, then the keys of
// the list and the reload key.
func (b *FileBrowser) Hints() []Hint {
	var hints []Hint
	for _, a := range b.spec.Actions {
		hints = append(hints, Hint{Action: a.Action, Label: a.Label})
	}
	if b.spec.Keys.Reload != "" {
		hints = append(hints, Hint{Action: b.spec.Keys.Reload, Label: TextKeyReload})
	}
	return append(hints, b.list.Hints()...)
}

// pathLine draws the path as parts a click leads to, the deepest folder last. Where the width runs short the
// upper parts give way to a mark that leads to the nearest hidden folder.
func (b *FileBrowser) pathLine(styles Styles, width int) (string, []Target) {
	label := b.spec.RootLabel
	if label == "" {
		label = b.text.Lookup(TextFileRoot)
	}
	parts := append([]string{label}, b.dir...)
	const sep = " / "
	ell := lipgloss.Width(styles.Glyph(GlyphEllipsis))
	room := max(width-2, 0)
	first := len(parts) - 1
	used := lipgloss.Width(Escape(parts[first]))
	for i := first - 1; i >= 0; i-- {
		w := lipgloss.Width(Escape(parts[i])) + len(sep)
		hidden := 0
		if i > 0 {
			hidden = ell + len(sep)
		}
		if used+w+hidden > room {
			break
		}
		first, used = i, used+w
	}
	limit := room
	if first > 0 {
		limit -= ell + len(sep)
	}
	var out strings.Builder
	var targets []Target
	x := 2
	out.WriteString("  ")
	add := func(text string, style TextStyle, depth int) {
		w := lipgloss.Width(text)
		out.WriteString(styles.Render(style, text))
		targets = append(targets, Target{Rect: Rect{X: x, Width: w, Height: 1}, Zone: ZoneRows, Item: fileCrumbRef + strconv.Itoa(depth)})
		x += w
	}
	if first > 0 {
		add(styles.Glyph(GlyphEllipsis), StyleHint, first-1)
		out.WriteString(sep)
		x += len(sep)
	}
	for i := first; i < len(parts); i++ {
		text := Escape(parts[i])
		if i == first {
			text = styles.Truncate(text, max(limit, 0))
		}
		style := StyleHint
		if i == len(parts)-1 {
			style = StyleTitle
		}
		add(text, style, i)
		if i < len(parts)-1 {
			out.WriteString(sep)
			x += len(sep)
		}
	}
	return out.String(), targets
}

// View draws the browser with styles in width by height cells: the path line, the table and, where the height
// allows, the buttons. It returns a target for every part of the path, every row and every button, counted
// from the top left.
func (b *FileBrowser) View(styles Styles, width, height int) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if width == 0 || height == 0 {
		return finishPage(nil, nil, width, height)
	}
	path, targets := b.pathLine(styles, width)
	lines := []string{path}
	var rows []string
	var bt []Target
	if height >= 6 {
		rows, bt = b.buttonRows(width, styles)
		if height-1-len(rows) < 5 {
			rows, bt = nil, nil
		}
	}
	var note []string
	if b.hits && b.note != "" {
		note = []string{"  " + styles.Render(StyleHint, styles.Truncate(Escape(b.note), max(width-2, 0)))}
	}
	body, ts := b.list.View(styles, width, max(height-1-len(rows)-len(note), 1))
	lines = append(lines, strings.Split(body, "\n")...)
	lines = append(lines, note...)
	targets = append(targets, OffsetTargets(ts, 0, 1)...)
	targets = append(targets, OffsetTargets(bt, 0, len(lines))...)
	lines = append(lines, rows...)
	return finishPage(lines, targets, width, height)
}

// buttonRows draws the buttons of the application behind the key now in force, and the one that goes up while
// a folder is open.
func (b *FileBrowser) buttonRows(width int, styles Styles) ([]string, []Target) {
	var cells []buttonCell
	draw := func(kind ButtonKind, action ActionID, label TextID, ref string) {
		text := b.text.Lookup(label)
		if key := b.keys.Label(action); key != "" {
			text = key + " " + text
		}
		cells = append(cells, buttonCell{styles.Button(kind, text, false), Target{Zone: ZoneRows, Action: action, Item: ref}})
	}
	for i, a := range b.spec.Actions {
		kind := ButtonNormal
		if a.Primary {
			kind = ButtonPrimary
		}
		draw(kind, a.Action, a.Label, fileButtonRef+strconv.Itoa(i))
	}
	if len(b.dir) > 0 {
		draw(ButtonNormal, b.spec.Keys.Back, TextFileUp, fileUpRef)
	}
	return flowButtons(cells, width)
}
