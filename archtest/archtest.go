package archtest

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestFiles selects whether a rule checks test files.
type TestFiles int

const (
	// SkipTests checks only files without the _test.go suffix.
	SkipTests TestFiles = iota
	// WithTests checks test files as well.
	WithTests
	// OnlyTests checks only test files.
	OnlyTests
)

// Files selects the Go files a rule checks. Paths are slash-separated and relative to the module root.
type Files struct {
	// Under holds path.Match patterns of directories. A file counts when its directory or one of the
	// directories above it matches; "." selects the whole module.
	Under []string
	// Except holds path.Match patterns of files or directories that the rule leaves out, such as the one
	// file that owns a key table. Every pattern must leave out at least one file.
	Except []string
	Tests  TestFiles
}

// Rule is one declarative check. Name identifies it in findings and exceptions and is unique within one
// Find call.
type Rule struct {
	Name  string
	Files Files
	Check Checker
	// Hint tells the reader of a finding what to do instead.
	Hint string
}

// Checker inspects one file and returns its hits.
type Checker func(file *File) []Hit

// Hit is one violation inside a file: the node it was found at and the item an exception names.
type Hit struct {
	Node ast.Node
	Item string
}

// File is one parsed Go file handed to a Checker.
type File struct {
	// Path is slash-separated and relative to the module root.
	Path string
	// Module is the module path from go.mod.
	Module string
	Test   bool
	Syntax *ast.File
	Fset   *token.FileSet
}

// Imports returns the import paths of the file by the name the file uses for them. A blank import is left
// out, and a dot import is listed under ".". Without an alias the name is the last element of the path
// without a major version suffix, so a package whose name differs from its path needs an alias to be found.
func (f *File) Imports() map[string][]string {
	byName := map[string][]string{}
	for _, spec := range f.Syntax.Imports {
		target, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := PackageName(target)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" {
			continue
		}
		byName[name] = append(byName[name], target)
	}
	return byName
}

// PackageName returns the package name a Go file assumes for an import path without an alias: the last
// element of the path, skipping a major version element such as "v2" and cutting a ".vN" suffix.
func PackageName(importPath string) string {
	elems := strings.Split(importPath, "/")
	name := elems[len(elems)-1]
	if len(elems) > 1 && isMajorVersion(name) {
		name = elems[len(elems)-2]
	}
	if i := strings.LastIndex(name, ".v"); i > 0 && isMajorVersion(name[i+1:]) {
		name = name[:i]
	}
	return name
}

func isMajorVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	_, err := strconv.Atoi(s[1:])
	return err == nil
}

// Finding is one violation of a rule. Line is only for the reader; an exception matches Rule, File and Item.
type Finding struct {
	Rule string
	File string
	Line int
	Item string
	Hint string
}

// Exception allows every finding of one rule with one item in one file. Reason must say why.
type Exception struct {
	Rule   string
	File   string
	Item   string
	Reason string
}

// Find parses every Go file below the root of fsys, which must hold go.mod, and returns the findings of
// rules sorted by file, line, rule and item. Build constraints are ignored, so every platform checks the
// same files. Directories the go tool ignores (testdata, vendor and names starting with "." or "_") are
// skipped. A rule without a name, checker or directory, a rule name used twice, a rule that selects no file
// and an Except pattern that leaves out no file are errors, so a renamed directory cannot silence a rule.
func Find(fsys fs.FS, rules ...Rule) ([]Finding, error) {
	if err := validRules(rules); err != nil {
		return nil, err
	}
	module, err := ModulePath(fsys)
	if err != nil {
		return nil, err
	}
	var paths []string
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	selected := make([]int, len(rules))
	excepted := make([][]bool, len(rules))
	for i, r := range rules {
		excepted[i] = make([]bool, len(r.Files.Except))
	}
	var findings []Finding
	for _, p := range paths {
		test := strings.HasSuffix(p, "_test.go")
		var file *File
		for i, r := range rules {
			if !r.Files.selects(p, test, excepted[i]) {
				continue
			}
			selected[i]++
			if file == nil {
				if file, err = parseFile(fsys, p, module, test); err != nil {
					return nil, err
				}
			}
			for _, hit := range r.Check(file) {
				line := 0
				if hit.Node != nil {
					line = file.Fset.Position(hit.Node.Pos()).Line
				}
				findings = append(findings, Finding{Rule: r.Name, File: p, Line: line, Item: hit.Item, Hint: r.Hint})
			}
		}
	}
	var errs []error
	for i, r := range rules {
		if selected[i] == 0 {
			errs = append(errs, fmt.Errorf("rule %q selects no Go file", r.Name))
		}
		for j, used := range excepted[i] {
			if !used {
				errs = append(errs, fmt.Errorf("rule %q: except pattern %q leaves out no file", r.Name, r.Files.Except[j]))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	slices.SortFunc(findings, compareFindings)
	return slices.Compact(findings), nil
}

func validRules(rules []Rule) error {
	if len(rules) == 0 {
		return errors.New("no rule given")
	}
	var errs []error
	seen := map[string]bool{}
	for i, r := range rules {
		switch {
		case strings.TrimSpace(r.Name) == "":
			errs = append(errs, fmt.Errorf("rule %d has no name", i))
		case seen[r.Name]:
			errs = append(errs, fmt.Errorf("rule %q is given twice", r.Name))
		}
		seen[r.Name] = true
		if r.Check == nil {
			errs = append(errs, fmt.Errorf("rule %q has no checker", r.Name))
		}
		if len(r.Files.Under) == 0 {
			errs = append(errs, fmt.Errorf("rule %q selects no directory", r.Name))
		}
		for _, pattern := range slices.Concat(r.Files.Under, r.Files.Except) {
			if _, err := path.Match(pattern, ""); err != nil || pattern == "" {
				errs = append(errs, fmt.Errorf("rule %q: bad pattern %q", r.Name, pattern))
			}
		}
	}
	return errors.Join(errs...)
}

func (f Files) selects(p string, test bool, excepted []bool) bool {
	switch {
	case f.Tests == SkipTests && test, f.Tests == OnlyTests && !test:
		return false
	}
	if !slices.ContainsFunc(ancestors(path.Dir(p)), func(dir string) bool { return matchAny(f.Under, dir) }) {
		return false
	}
	out := false
	for i, pattern := range f.Except {
		if slices.ContainsFunc(append(ancestors(path.Dir(p)), p), func(name string) bool { return match(pattern, name) }) {
			excepted[i], out = true, true
		}
	}
	return !out
}

// ancestors returns dir and every directory above it up to ".".
func ancestors(dir string) []string {
	dirs := []string{dir}
	for dir != "." {
		dir = path.Dir(dir)
		dirs = append(dirs, dir)
	}
	return dirs
}

func matchAny(patterns []string, name string) bool {
	return slices.ContainsFunc(patterns, func(pattern string) bool { return match(pattern, name) })
}

func match(pattern, name string) bool {
	ok, _ := path.Match(pattern, name)
	return ok
}

func parseFile(fsys fs.FS, p, module string, test bool) (*File, error) {
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	syntax, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &File{Path: p, Module: module, Test: test, Syntax: syntax, Fset: fset}, nil
}

func compareFindings(a, b Finding) int {
	if c := strings.Compare(a.File, b.File); c != 0 {
		return c
	}
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	if c := strings.Compare(a.Rule, b.Rule); c != 0 {
		return c
	}
	return strings.Compare(a.Item, b.Item)
}

// skipDir reports the directories the go tool ignores, which hold no module code.
func skipDir(name string) bool {
	return name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// Problems turns findings into failure messages: every finding without an exception, and every exception
// without a reason, listed twice or without a finding. The last keeps the list of exceptions from growing
// stale, so it can only shrink. The messages are sorted.
func Problems(findings []Finding, exceptions []Exception) []string {
	type key struct{ rule, file, item string }
	found := map[key]bool{}
	for _, f := range findings {
		found[key{f.Rule, f.File, f.Item}] = true
	}
	excepted := map[key]bool{}
	var problems []string
	for _, e := range exceptions {
		k := key{e.Rule, e.File, e.Item}
		switch {
		case strings.TrimSpace(e.Reason) == "":
			problems = append(problems, fmt.Sprintf("%s: rule %s: exception for %q has no reason", e.File, e.Rule, e.Item))
			continue
		case excepted[k]:
			problems = append(problems, fmt.Sprintf("%s: rule %s: exception for %q is listed twice", e.File, e.Rule, e.Item))
		case !found[k]:
			problems = append(problems, fmt.Sprintf("%s: rule %s: exception for %q is stale; remove it", e.File, e.Rule, e.Item))
		}
		excepted[k] = true
	}
	for _, f := range findings {
		if excepted[key{f.Rule, f.File, f.Item}] {
			continue
		}
		msg := fmt.Sprintf("%s:%d: rule %s: %q", f.File, f.Line, f.Rule, f.Item)
		if f.Hint != "" {
			msg += "; " + f.Hint
		}
		problems = append(problems, msg)
	}
	slices.Sort(problems)
	return problems
}

// Check runs Find and reports every problem as a test error. An error of Find fails the test at once.
func Check(t testing.TB, fsys fs.FS, rules []Rule, exceptions []Exception) {
	t.Helper()
	findings, err := Find(fsys, rules...)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range Problems(findings, exceptions) {
		t.Error(problem)
	}
}

// ModuleRoot returns dir or the nearest directory above it that holds go.mod.
func ModuleRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod in or above the directory")
		}
		dir = parent
	}
}

// ModulePath reads the module path from go.mod at the root of fsys.
func ModulePath(fsys fs.FS) (string, error) {
	data, err := fs.ReadFile(fsys, "go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			if module := strings.Trim(strings.TrimSpace(rest), `"`); module != "" {
				return module, nil
			}
		}
	}
	return "", errors.New("go.mod declares no module")
}
