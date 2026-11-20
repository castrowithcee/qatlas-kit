// Package highlight turns a text and its file name into the sections a tui.TextView draws in its syntax
// roles. It is a small line-by-line lexer for the formats that are common on a host, not a parser: it judges
// the shape of each line and never whether the text is valid, and a text it cannot read stays plain.
//
// Detect names the format from the file name and, failing that, from the interpreter of a shebang line.
// Spans returns the sections of a text in a given format, one list per line, to pass to
// tui.TextView.SetText next to the text itself; Highlight combines the two. The lines are split exactly as
// the text view splits them, so a list fits the line of the same index.
//
// # Formats and roles
//
// Each format marks the same few things with the roles of the text view; everything else stays plain.
//
//   - YAML (.yaml, .yml): keys and flow keys are keys; quoted scalars and the lines of a literal or folded
//     block scalar are strings; numbers; true, false, null and ~, anchors, aliases, tags, block scalar
//     indicators and document markers are keywords; comments.
//   - Env (.env, .env.*, *.env): names are keys, as is the keyword export; values are strings, or numbers if
//     they read as one; comments. A quoted value may run over several lines.
//   - JSON (.json): keys; strings; numbers; true, false and null are keywords.
//   - INI (.ini, .cfg) and TOML (.toml): keys; section headers are keywords; quoted strings, and in TOML
//     multi-line strings; numbers, and in TOML dates; true and false are keywords, and in INI also yes, no,
//     on and off; comments.
//   - Shell (.sh, .bash, .zsh, .ksh, .dash, .ash, dotfiles of these shells, shebang): names of assignments are
//     keys; quoted strings, strings over several lines and here-document bodies; numbers; reserved words and
//     common builtins in command position and the delimiters of a here-document are keywords; comments.
//   - Dockerfile (Dockerfile, Dockerfile.*, *.dockerfile, Containerfile): instructions are keywords, as is
//     the AS of FROM and the CMD of HEALTHCHECK; flags and the names of ENV, ARG and LABEL are keys; quoted
//     strings; numbers; comment lines.
//
// # Contract
//
// The sections of a line are ascending, do not overlap, lie inside the line and start and end on UTF-8
// character boundaries. The package never sets Masked: what to hide is for the application to decide.
// There is no package state, and the time is linear in the length of the text. The state between lines is
// small and holds nothing but the mode of the block, quote or here-document that is open.
//
// # Limits
//
// Spans returns nil, so the text view draws plain text, for an unknown format, an empty text and binary text,
// which is one with a NUL byte or more than a tenth of invalid UTF-8 in its first 64 KiB. A line longer than
// MaxLineBytes has no sections and leaves the state of the lines after it as it was. Lines past
// tui.TextViewMaxLines are not listed. Values that continue over several lines are followed only where
// the format has a block or quote that is closed explicitly: YAML block scalars, quoted values of Env, TOML
// multi-line strings, and in shell quotes and the first here-document of a line. A plain or quoted YAML scalar
// that continues on the next line is not.
package highlight
