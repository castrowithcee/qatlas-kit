// Package archtest checks the architecture of a Go module by reading its source, so a broken layer rule
// fails the test run instead of waiting for a review. An application declares its rules in a test and runs
// them with Check; this package knows no application, layer or name of its own.
//
// A Rule selects files by directory pattern and applies one Checker to each of them. The checkers cover
// import boundaries between the module's packages (ForbidImports, AllowImports), import paths from outside
// it (ForbidImportPaths), qualified members such as the constructor of a colour or the opening of a store
// (ForbidSelectors), construction of a type (ForbidConstruction), local copies of shared helpers
// (ForbidDeclarations), names in string literals and identifiers (ForbidLiterals, ForbidIdentifiers, with
// the matchers Exact, Words and Pattern) and key values outside the key table of a terminal user interface
// (ForbidKeyLiterals). An application can add its own Checker. CheckCatalog compares the text IDs a TUI
// package declares with its catalogs and adds the checks of tui.CheckCatalogs.
//
// Violations that exist today are listed as exceptions, each with a reason. The list can only shrink: a new
// violation fails, and so does an exception that no longer matches anything, one listed twice or one
// without a reason. A rule that selects no file and an Except pattern that leaves out nothing are errors, so
// renaming a directory cannot silence a rule.
//
// The checks read syntax, not types. Build constraints are ignored, so every platform checks the same
// files, and directories the go tool ignores are skipped. A package is recognised by its import path
// through any alias; a package whose name differs from the last element of its path is recognised only
// under an alias. Reflection, code generation at run time and values built from pieces stay out of
// reach; the checks keep honest code in its layer and are no defence against code that hides on purpose.
package archtest
