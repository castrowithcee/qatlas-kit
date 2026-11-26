// Package projectpath expands home-relative paths, resolves canonical paths, finds Git working tree roots,
// checks whether one path lies within another, suggests completions for a typed file or directory path, and
// searches below a start path for names that hold a text.
// It reads the file system directly and does not start a process.
//
// A [Suggester] turns typed text into an ordered list of suggestions so that a user interface can offer
// path completion without reading the file system itself. It reads only the directory the typed text
// names, once per instance. Directories come first, each with a trailing separator of the running platform;
// files follow in [Files] mode. Hidden entries appear once the typed last segment starts with a dot, names
// match with their case, a suggestion keeps the typed spelling of the directory part, and a link to a
// directory counts as a directory.
//
// [Search] finds, from a start path, the entries that are directories, files or both and whose name holds
// a text, ignoring case, over any number of levels. [SplitSearch] splits a typed "<start>*<text>" into its
// parts; without a "*" the text stays a plain completion for a [Suggester]. Four directories are read at
// the same time. Matches are ordered by name quality (the same name, then a name that starts with the text,
// then any other), then by depth, then by path. A link can match but is never followed, so it is reported
// as a file. The result says why the search ended; a limit or a canceled context ends it at once and is not
// an error, and then the matches are those found so far.
//
// # Always enforced
//
// A name that is not valid UTF-8 or holds a control or format character, such as a right-to-left override,
// is never suggested, because file names are untrusted data. A missing or unreadable directory yields no
// suggestions and no error. At most [MaxSuggestions] suggestions are returned for one typed text. A
// Suggester is not safe for concurrent use.
//
// A search reads at most 200,000 entries, runs for at most three seconds and returns at most 50 matches
// unless the options set lower or higher limits. It stays on the device of the start path on Unix, does not
// enter /proc, /sys, /dev and /run on Linux, and skips a directory it cannot read, counting it. Names that
// are not valid UTF-8 or hold a control or format character never match. A start that is not a readable
// directory, an empty text and an unknown [Kind] are errors.
//
// # Options
//
//   - [Mode]: [Directories] completes only directories and only absolute and "~/" paths; [Files] also offers
//     files and completes paths relative to the working directory. Required by [NewSuggester], which has no
//     default.
//   - The workDir argument of [NewSuggester]: what a relative path is resolved against in [Files] mode.
//     Default: the process working directory.
//   - [SearchOptions].Kind: [Both], [OnlyDirectories] or [OnlyFiles]. Default: [Both].
//   - [SearchOptions].MaxEntries, Timeout and MaxMatches: the limits of one search. Default:
//     [DefaultMaxEntries], [DefaultTimeout] and [DefaultMaxMatches].
package projectpath
