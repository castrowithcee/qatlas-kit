// Package uiprefs loads and saves the device-local preferences of a terminal user interface in tui.yaml in
// the folder the application passes. Preferences apply to one device only, are never exported and need no
// elevated session to change. The application supplies a Schema with the theme names, areas and key table
// it accepts; this package holds no application content and no package state.
//
// The file is a flat YAML mapping. Load never fails: a missing file yields the defaults, an unreadable or
// malformed one the defaults and a warning, and a value that is not valid the default for that part and a
// warning that names it. Keys this version does not know, action IDs under keys included, are ignored, so a
// later version can add fields. The keys section holds only the bindings that differ from the default; one
// that breaks a rule of the key table or collides keeps the default of its action with a warning, so the
// loaded keys never collide.
//
// Save refuses a value that is not valid and writes nothing then. It writes through the configfile
// package: atomically under a lock file beside the target, with a backup of the replaced bytes, through a
// symlink to its target and private to the user for a new file. The last writer wins.
package uiprefs
