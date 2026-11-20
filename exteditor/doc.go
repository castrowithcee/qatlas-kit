// Package exteditor lets a terminal user interface hand a text to the user's own editor in the manner of
// sudoedit: the text goes to a copy in a private directory, the terminal goes to the editor through
// tea.ExecProcess, and when the editor has ended the copy is checked, read and removed, and the program
// gets an UnchangedMsg, a ChangedMsg with the new text or a FailedMsg. Writing the text back to its place,
// and the rights that takes, stay with the application.
//
// Resolve finds the editor without a shell. For vim and Neovim, micro and nano it adds the options that
// switch off backup, swap, undo and history files; nano then skips the nanorc files, since it has no
// other way to switch off a backup set there. Editor.NoTraces tells the application before the start
// whether that holds; for any other editor it is false and the application decides whether to go on.
//
// Security boundary:
//
//   - The private directory is opened without following a symbolic link and checked on the open
//     descriptor: a directory with mode 0700 that belongs to the user the editor runs as. The copy is
//     created through that descriptor with O_EXCL, without following a link and with mode 0600; its name
//     is random and keeps only the extension of the original. It is read and removed through the same
//     descriptor, so renaming or replacing the path after the check redirects nothing.
//   - The text is accepted in either direction only up to the size limit, as valid UTF-8 and without NUL
//     bytes. After the editor, only a regular file with a single link that belongs to the user the editor
//     ran as is read, since the editor may have replaced the copy. The copy is removed in every case,
//     also when the editor fails or is killed, and an error never quotes the text.
//   - With effective user ID 0, Edit refuses unless the application names a Target that is not root. The
//     copy then belongs to the target, and the editor runs with its user and group ID, without
//     supplementary groups, in the private directory and with this environment only: HOME of the target,
//     PATH=/usr/local/bin:/usr/bin:/bin, and TERM, COLORTERM, LANG, LC_ALL, LC_CTYPE and LC_MESSAGES of
//     the process when set. Without root, the editor inherits the environment of the process.
//
// Limits: the copy exists while the editor runs, so the private directory should be on memory-backed
// storage such as XDG_RUNTIME_DIR. If the process dies before the editor ends, or the returned command
// never runs, the copy stays. An editor or its plugins may still write elsewhere. Edit works on Linux and
// macOS; on other platforms it returns an error that wraps errors.ErrUnsupported.
package exteditor
