// Package helppages loads Markdown help pages from an fs.FS, checks them and splits their text into blocks
// a user interface can draw. The application supplies the files; the package reads no operating system
// path and draws nothing.
//
// # Page model
//
// Every folder in the root is a topic and its name is the topic ID. A topic holds pages and no folder. The
// file index.md is the page of the topic itself: it carries the title and order of the topic and is
// Topic.Index, not part of Topic.Pages. Every other page is <name>.md in English, which is required, and
// optionally <name>.<lang>.md for another language of package i18n, written as its code such as "de".
//
// A page starts with YAML front matter between two "---" lines. The keys are title (required, one line),
// order (a number), keywords (a list of strings), screens (a list of screen IDs without blank or control
// characters) and link (an https address). The text after the front matter is the body.
//
// Topics sort by the order of their index page and pages by their own order, ascending. Pages without
// order follow those with one. Ties, and pages without order, sort by English title and then by name,
// compared byte by byte.
//
// A language is chosen per file: Page.In returns the content of the translation when it exists and the
// English content otherwise. Only the link falls back to the English one when the translation names none.
// Title, keywords and body of a translation replace the English ones as a whole.
//
// # Markdown subset
//
// Blocks splits a body into headings (#, ## and ###), paragraphs (consecutive lines are joined by one
// space), bullet items ("- ") and numbered items ("1." with any number). An item is indented at level 0
// or, with two to four spaces (a tab counts as four), at level 1. A deeper marker is plain text, and an
// indented line right after an item continues it. Inline, the text knows **bold**, `code` and
// [text](address). Everything else, such as tables, images, quotes, HTML, code fences, further heading
// levels and marks that are not closed, stays plain text. Nothing is dropped. The split adds no control
// characters, but it passes those of the source on: the user interface masks them. The address of a link
// ends at the first closing parenthesis.
//
// # Always enforced
//
// Load reports every breach at once, each with its path. It refuses a file in the root, a folder below a
// topic, a file without the .md suffix, an unknown language suffix or an explicit ".en", a translation
// without its English file, a topic without index.md and an empty page set. It decodes the front matter
// strictly: it must exist, an unknown key is an error, and the title must be present, a single line
// without control characters. Keywords may not be empty or hold control characters. A translation may
// carry only title, keywords and link; order or screens there is an error. A screen ID that two pages
// carry, or one page lists twice, is an error. The link of the front matter and the address of every
// inline link must be an absolute https address of printable ASCII without blanks, with a host and
// without user information, in every language version.
//
// # Options
//
// Options.Placeholder: when set, every placeholder {name} in a body that it does not know is an error, in
// every language version. A placeholder name starts with a lower-case letter and continues with lower-case
// letters, digits, ".", "_" or "-". Placeholders are not resolved here. Default: nil, no placeholder is
// checked. Placeholders lists those of a text, so an application can check its pages in its own tests.
package helppages
