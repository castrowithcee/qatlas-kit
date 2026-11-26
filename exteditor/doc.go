// Package exteditor lets a terminal user interface hand a text to the user's own editor in the manner of
// sudoedit: the text goes to a copy in a private directory, the terminal goes to the editor through
// tea.ExecProcess, and when the editor has ended the copy is checked, read and removed, and the program
// gets an UnchangedMsg, a ChangedMsg with the new text or a FailedMsg. Writing the text back to its place,
// and the rights that takes, stay with the application.
//
// Resolve finds the editor without a shell: the preference, then VISUAL, then EDITOR, and without any of
// them the first of micro, nano and vi on PATH. Editor.NoTraces tells the application before the start
// whether the editor writes no backup, swap, undo or history files; for any editor the package does not
// know it is false and the application decides whether to go on.
//
// # Always enforced
//
// Edit refuses an invalid Request with ErrInvalidRequest: the editor path and the private directory must be
// absolute, MaxSize must be positive, Name must carry no path separator and keep an extension of at most 32
// bytes that is valid UTF-8 without control characters, and a Target must name a user and group other than
// root and an absolute home directory. Resolve splits a command line at spaces only, looks its first part
// up on PATH and requires an absolute path; a source that is set but cannot be found is an error
// (ErrNoEditor), not a reason to try the next one. For vim and Neovim, micro and nano Resolve adds the
// options that switch off backup, swap, undo and history files; nano then skips the nanorc files, since it
// has no other way to switch off a backup set there.
//
// The private directory is opened without following a symbolic link and checked on the open descriptor: a
// directory with mode 0700 that belongs to the user the editor runs as (ErrUnsafeDir). The copy is created
// through that descriptor with O_EXCL, without following a link and with mode 0600; its name is random and
// keeps only the extension of the original. It is read and removed through the same descriptor, so renaming
// or replacing the path after the check redirects nothing.
//
// The text is accepted in either direction only up to MaxSize (ErrTooLarge), as valid UTF-8 (ErrNotUTF8)
// and without NUL bytes (ErrBinary). After the editor, only a regular file with a single link that belongs
// to the user the editor ran as is read (ErrUnsafeCopy), since the editor may have replaced the copy. The
// copy is removed in every case, also when the editor fails or is killed, and an error never quotes the
// text. When Edit itself returns an error, nothing is left in the directory.
//
// With effective user ID 0, Edit refuses (ErrRoot) unless the application names a Target that is not root,
// and without root a Target is refused. The copy then belongs to the target, and the editor runs with its
// user and group ID, without supplementary groups, in the private directory and with this environment only:
// HOME of the target, PATH=/usr/local/bin:/usr/bin:/bin, and TERM, COLORTERM, LANG, LC_ALL, LC_CTYPE and
// LC_MESSAGES of the process when set. Without root, the editor inherits the environment of the process.
//
// Boundary: the copy exists while the editor runs, so the private directory should be on memory-backed
// storage such as XDG_RUNTIME_DIR. If the process dies before the editor ends, or the returned command
// never runs, the copy stays. An editor or its plugins may still write elsewhere. Edit works on Linux and
// macOS; on other platforms it returns an error that wraps errors.ErrUnsupported.
//
// # Options
//
// The package has no options.
package exteditor
