// Package projectpath expands home-relative paths, resolves canonical paths, finds Git working tree roots,
// checks whether one path lies within another, and suggests completions for a typed file or directory path.
// It reads the file system directly and does not start a process.
//
// A [Suggester] turns typed text into an ordered list of suggestions so that a user interface can offer
// path completion without reading the file system itself. It reads only the directory the typed text
// names, once per instance. Directories come first, each with a trailing separator of the running platform;
// files follow in [Files] mode. [Directories] mode completes only absolute and "~/" paths, [Files] mode also
// paths relative to the working directory given to [NewSuggester]. Hidden entries appear once the typed last
// segment starts with a dot, names match with their case, a suggestion keeps the typed spelling of the
// directory part, and a link to a directory counts as a directory. A name that is not valid UTF-8 or holds a
// control or format character, such as a right-to-left override, is never suggested, because file names are
// untrusted data. A missing or unreadable directory yields no suggestions and no error.
package projectpath
