package exteditor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

var (
	// ErrUnsupported is returned by Edit on a platform without support.
	ErrUnsupported = fmt.Errorf("the external editor is unsupported on this platform: %w", errors.ErrUnsupported)
	// ErrNoEditor reports that no editor could be resolved.
	ErrNoEditor = errors.New("no editor found")
	// ErrInvalidRequest reports a request that is incomplete or contradicts itself.
	ErrInvalidRequest = errors.New("invalid edit request")
	// ErrRoot reports a process with effective user ID 0 and no target user.
	ErrRoot = errors.New("refusing to run the editor as root; a target user is required")
	// ErrUnsafeDir reports a private directory that is no directory, a symbolic link, belongs to another
	// user or is open to others.
	ErrUnsafeDir = errors.New("unsafe private directory")
	// ErrUnsafeCopy reports a copy the editor left as something other than a private regular file.
	ErrUnsafeCopy = errors.New("unsafe copy")
	// ErrTooLarge reports a text above the size limit.
	ErrTooLarge = errors.New("text too large")
	// ErrBinary reports a text that holds a NUL byte.
	ErrBinary = errors.New("binary content")
	// ErrNotUTF8 reports a text that is not valid UTF-8.
	ErrNotUTF8 = errors.New("text is not valid UTF-8")
	// ErrEditorFailed reports an editor that could not start or did not exit with status 0.
	ErrEditorFailed = errors.New("the editor failed")
)

// maxExtension bounds the extension the copy keeps.
const maxExtension = 32

// Editor is a resolved editor command line.
type Editor struct {
	// Path is the absolute path of the program.
	Path string
	// Args come before the file name; for a known editor they start with the options that switch off its
	// backup, swap, undo and history files.
	Args []string
	// NoTraces reports that Args switch off the files a known editor writes beside or about the text.
	// It is false for an editor this package does not know; the application decides whether to go on.
	NoTraces bool
}

// Resolve finds the editor: the preference, then VISUAL, then EDITOR, read through getenv (os.Getenv
// when nil). The first of them that is set decides; one that cannot be found is an error, not a reason
// to try the next. When none is set, the first of micro, nano and vi on the PATH of the process is used.
// A command line is split at spaces only, without a shell; its first part is looked up on PATH and must
// resolve to an absolute path.
func Resolve(preference string, getenv func(string) string) (Editor, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	for _, source := range []struct{ name, value string }{
		{"the editor preference", preference}, {"VISUAL", getenv("VISUAL")}, {"EDITOR", getenv("EDITOR")},
	} {
		if strings.TrimSpace(source.value) == "" {
			continue
		}
		e, err := lookup(splitSpaces(source.value))
		if err != nil {
			return Editor{}, fmt.Errorf("%w: %s: %w", ErrNoEditor, source.name, err)
		}
		return e, nil
	}
	fallbacks := []string{"micro", "nano", "vi"}
	for _, name := range fallbacks {
		if e, err := lookup([]string{name}); err == nil {
			return e, nil
		}
	}
	return Editor{}, fmt.Errorf("%w: neither the preference, VISUAL nor EDITOR is set and none of %q is on PATH",
		ErrNoEditor, fallbacks)
}

func splitSpaces(s string) []string {
	var parts []string
	for _, p := range strings.Split(s, " ") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func lookup(fields []string) (Editor, error) {
	path, err := exec.LookPath(fields[0])
	if err != nil {
		return Editor{}, err
	}
	if !filepath.IsAbs(path) {
		return Editor{}, fmt.Errorf("%q does not resolve to an absolute path", fields[0])
	}
	e := Editor{Path: path}
	e.Args, e.NoTraces = noTraceArgs(path)
	e.Args = append(e.Args, fields[1:]...)
	return e, nil
}

// noTraceArgs returns the options that keep a known editor from writing backup, swap, undo or history
// files. The editor is recognized by the name of the file the path finally points to, so a vi that links
// to vim counts as vim and one that links to anything else stays unknown.
func noTraceArgs(path string) ([]string, bool) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(path)), ".exe")
	switch {
	case name == "vim" || strings.HasPrefix(name, "vim.") || name == "nvim":
		return []string{"-n", "-i", "NONE", "-c", "set nobackup nowritebackup noundofile"}, true
	case name == "micro":
		return []string{"-backup", "false", "-saveundo", "false"}, true
	case name == "nano":
		// nano has no option that switches off backups or history set in a nanorc, only one that skips
		// the nanorc files; its defaults write neither.
		return []string{"--ignorercfiles"}, true
	}
	return nil, false
}

// Target is the user the editor runs as when the process has effective user ID 0. Neither ID may be 0.
type Target struct {
	UID, GID uint32
	// Home is the absolute home directory of the user; the editor gets it as HOME.
	Home string
}

// Request describes one edit.
type Request struct {
	Editor Editor
	// Dir is the absolute private directory the copy is made in, such as XDG_RUNTIME_DIR. It must belong
	// to the user the editor runs as, have mode 0700 and not be a symbolic link.
	Dir string
	// Name is the name of the original file; only its extension is kept, so the editor recognizes the
	// format. It must not contain a path separator.
	Name string
	// Text is the content to edit; it must be valid UTF-8 without NUL bytes and at most MaxSize bytes.
	Text []byte
	// MaxSize is the limit in bytes for the text in both directions; it must be positive.
	MaxSize int64
	// Target is required when the process has effective user ID 0 and refused otherwise.
	Target *Target

	// geteuid replaces os.Geteuid in tests.
	geteuid func() int
}

// UnchangedMsg reports that the editor ended with status 0 and the text unchanged.
type UnchangedMsg struct{}

// ChangedMsg carries the changed text. It passed the size limit, is valid UTF-8 and holds no NUL byte.
type ChangedMsg struct {
	Text []byte
}

// FailedMsg reports a failed edit; any change is discarded. Err wraps ErrEditorFailed, ErrUnsafeCopy,
// ErrTooLarge, ErrBinary or ErrNotUTF8, or reports that the copy could not be read or removed.
type FailedMsg struct {
	Err error
}

// Edit checks the request, makes the private copy and returns a command that hands the terminal to the
// editor through tea.ExecProcess. When the editor has ended, the copy is checked, read and removed in
// every case, and the command yields an UnchangedMsg, a ChangedMsg or a FailedMsg. When Edit returns an
// error, nothing is left in Dir. The application runs the returned command exactly once; until then the
// copy stays in Dir.
func Edit(r Request) (tea.Cmd, error) {
	s, err := prepare(r)
	if err != nil {
		return nil, err
	}
	return tea.ExecProcess(s.cmd, s.finish), nil
}

func (r Request) check() error {
	var errs []error
	if r.Editor.Path == "" || !filepath.IsAbs(r.Editor.Path) {
		errs = append(errs, errors.New("the editor path is not absolute"))
	}
	if !filepath.IsAbs(r.Dir) {
		errs = append(errs, errors.New("the private directory is not absolute"))
	}
	if r.MaxSize <= 0 {
		errs = append(errs, errors.New("the size limit is not positive"))
	}
	if _, err := extension(r.Name); err != nil {
		errs = append(errs, err)
	}
	if t := r.Target; t != nil {
		if t.UID == 0 || t.GID == 0 {
			errs = append(errs, errors.New("the target user or group is root"))
		}
		if !filepath.IsAbs(t.Home) {
			errs = append(errs, errors.New("the home of the target user is not absolute"))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return checkText(r.Text, r.MaxSize)
}

// extension returns the extension of name the copy keeps.
func extension(name string) (string, error) {
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("the file name %q carries a path", name)
	}
	ext := filepath.Ext(name)
	if len(ext) > maxExtension {
		return "", fmt.Errorf("the extension of %q is longer than %d bytes", name, maxExtension)
	}
	if !utf8.ValidString(ext) || strings.IndexFunc(ext, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("the extension of %q holds a control character or invalid UTF-8", name)
	}
	return ext, nil
}

// checkText names the first reason text cannot be edited or returned; it never quotes the text.
func checkText(text []byte, limit int64) error {
	switch {
	case int64(len(text)) > limit:
		return fmt.Errorf("%w: %d bytes, the limit is %d", ErrTooLarge, len(text), limit)
	case bytes.IndexByte(text, 0) >= 0:
		return ErrBinary
	case !utf8.Valid(text):
		return ErrNotUTF8
	}
	return nil
}
