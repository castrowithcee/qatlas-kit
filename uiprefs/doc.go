// Package uiprefs loads and saves the device-local preferences of a terminal user interface in tui.yaml in
// the folder the application passes. Preferences apply to one device only, are never exported and need no
// elevated session to change. The application supplies a Schema with the theme names, areas and key table
// it accepts; this package holds no application content and no package state.
//
// The file is a flat YAML mapping. Keys this version does not know, action IDs under keys included, are
// ignored, so a later version can add fields. The keys section holds only the bindings that differ from the
// default. The editor is an optional command line for the exteditor package; a file without it loads as before,
// and a save leaves it out while it is empty. The navigation is the form of tui.NavForm: the tab line, the path
// line, the path in the header or the choice field in the header. A file without the key loads as before, and a
// save leaves the key out while the value is empty. Validate is the check of Save on its own, for preferences
// taken from flags or held only in memory.
//
// # Always enforced
//
// An invalid Schema is refused by Load, Validate and Save: the themes must include tui.ThemeTerminal, theme and
// area names are not empty or listed twice, DefaultArea is one of the areas, every navigation is one of
// NavigationTabs, NavigationPathLine, NavigationPathHeader and NavigationHeaderChoice, and DefaultNavigation is
// one of the offered navigations and empty where there are none.
//
// Load never fails. A missing file yields the defaults. An unreadable or malformed file yields the defaults
// and a warning. A value that is not valid yields the default for that part and a warning that names it:
// a theme or area outside the schema, an icons value other than IconsUnicode, IconsNerdFont or IconsNone, a
// language outside the languages the package knows, a mouse value that is no boolean and an editor command
// line that is not valid UTF-8 or holds a control character. The defaults are tui.ThemeTerminal, the
// default area of the schema, IconsUnicode, LanguageEnglish and the mouse enabled. A binding in the keys
// section that breaks a rule of the key table or collides keeps the default of its action with a warning,
// so the loaded keys never collide.
//
// Save refuses a value that is not valid, with the same checks as Validate, and writes nothing then. It
// writes through the configfile package: atomically under a lock file beside the target, with a backup of
// the replaced bytes, through a symlink to its target and private to the user for a new file. When another
// writer replaced the file meanwhile, Save starts over, at most three attempts in all, so the last writer
// wins. A Prefs value with keys is valid only if the schema has a key table and the key table accepts them.
//
// # Options
//
//   - Schema.Keys: the key table whose rebindable actions the keys section binds. Default: nil, the
//     application has no keys to rebind; Load ignores a keys section with a warning, Validate and Save
//     refuse Prefs.Keys.
//   - Schema.Navigations: the navigation forms the application offers. Default: empty, the setting does not
//     exist: Prefs.Navigation stays empty, Validate and Save refuse a value, and Load ignores a value in the
//     file with a warning.
//   - Schema.DefaultNavigation: the form that holds without a choice, one of Schema.Navigations. Default:
//     empty, which is valid only where no navigation is offered.
package uiprefs
