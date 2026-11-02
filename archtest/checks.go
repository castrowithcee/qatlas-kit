package archtest

import (
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ForbidImports reports every import of a package of the module that matches one of patterns. A pattern is
// a path relative to the module root, such as "internal/vault"; a trailing "/..." also matches every package
// below it. The item is the module-relative import path.
func ForbidImports(patterns ...string) Checker {
	return func(file *File) []Hit {
		return moduleImports(file, func(rel string) bool { return matchImport(patterns, rel) })
	}
}

// AllowImports reports every import of a package of the module that matches none of patterns, which take
// the form of ForbidImports. Imports from outside the module stay free.
func AllowImports(patterns ...string) Checker {
	return func(file *File) []Hit {
		return moduleImports(file, func(rel string) bool { return !matchImport(patterns, rel) })
	}
}

func moduleImports(file *File, report func(rel string) bool) []Hit {
	var hits []Hit
	for _, spec := range file.Syntax.Imports {
		target, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if rel, ok := strings.CutPrefix(target, file.Module+"/"); ok && report(rel) {
			hits = append(hits, Hit{spec, rel})
		}
	}
	return hits
}

func matchImport(patterns []string, rel string) bool {
	for _, p := range patterns {
		if base, ok := strings.CutSuffix(p, "/..."); ok {
			if rel == base || strings.HasPrefix(rel, base+"/") {
				return true
			}
		} else if rel == p {
			return true
		}
	}
	return false
}

// ForbidImportPaths reports every import path from outside the module that m matches, with the items m
// returns. The module's own packages are not checked.
func ForbidImportPaths(m Matcher) Checker {
	return func(file *File) []Hit {
		var hits []Hit
		for _, spec := range file.Syntax.Imports {
			target, err := strconv.Unquote(spec.Path.Value)
			if err != nil || target == file.Module || strings.HasPrefix(target, file.Module+"/") {
				continue
			}
			for _, item := range m(target) {
				hits = append(hits, Hit{spec, item})
			}
		}
		return hits
	}
}

// ForbidSelectors reports every use of the named members of the package with importPath, such as the
// constructor of a colour outside the palette or the opening of a store outside the core. A use counts
// whatever alias the file gives the import, and a dot import of the package counts as a use of all its
// members. The item is "<package name>.<member>", or "<package name>.*" for a dot import.
func ForbidSelectors(importPath string, names ...string) Checker {
	pkg := PackageName(importPath)
	return func(file *File) []Hit {
		var hits []Hit
		local := map[string]bool{}
		for _, spec := range file.Syntax.Imports {
			if spec.Name != nil && spec.Name.Name == "." && importOf(spec) == importPath {
				hits = append(hits, Hit{spec, pkg + ".*"})
			}
		}
		for name, paths := range file.Imports() {
			if slices.Contains(paths, importPath) {
				local[name] = true
			}
		}
		if len(local) == 0 {
			return hits
		}
		ast.Inspect(file.Syntax, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && local[id.Name] && slices.Contains(names, sel.Sel.Name) {
					hits = append(hits, Hit{sel, pkg + "." + sel.Sel.Name})
				}
			}
			return true
		})
		return hits
	}
}

// ForbidConstruction reports every composite literal and every new() of the type typeName of the package
// with importPath, such as an HTTP client built outside the shared constructor. The item is
// "<package name>.<type>".
func ForbidConstruction(importPath, typeName string) Checker {
	pkg := PackageName(importPath)
	return func(file *File) []Hit {
		local := map[string]bool{}
		for name, paths := range file.Imports() {
			if slices.Contains(paths, importPath) {
				local[name] = true
			}
		}
		if len(local) == 0 {
			return nil
		}
		isType := func(e ast.Expr) bool {
			if sel, ok := e.(*ast.SelectorExpr); ok && sel.Sel.Name == typeName {
				id, ok := sel.X.(*ast.Ident)
				return ok && local[id.Name]
			}
			// A dot import names the type without a qualifier.
			id, ok := e.(*ast.Ident)
			return ok && local["."] && id.Name == typeName
		}
		var hits []Hit
		ast.Inspect(file.Syntax, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if isType(n.Type) {
					hits = append(hits, Hit{n, pkg + "." + typeName})
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "new" && len(n.Args) == 1 && isType(n.Args[0]) {
					hits = append(hits, Hit{n, pkg + "." + typeName})
				}
			}
			return true
		})
		return hits
	}
}

// ForbidDeclarations reports every top-level function, method, type, variable or constant with one of
// names, such as a local copy of a shared helper. The item is the name.
func ForbidDeclarations(names ...string) Checker {
	return func(file *File) []Hit {
		var hits []Hit
		add := func(id *ast.Ident) {
			if slices.Contains(names, id.Name) {
				hits = append(hits, Hit{id, id.Name})
			}
		}
		for _, decl := range file.Syntax.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				add(decl.Name)
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						add(spec.Name)
					case *ast.ValueSpec:
						for _, id := range spec.Names {
							add(id)
						}
					}
				}
			}
		}
		return hits
	}
}

// ForbidLiterals reports every string literal outside the import declarations that m matches, with the
// items m returns.
func ForbidLiterals(m Matcher) Checker {
	return func(file *File) []Hit {
		var hits []Hit
		ast.Inspect(file.Syntax, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ImportSpec:
				return false
			case *ast.BasicLit:
				if value, ok := stringValue(n); ok {
					for _, item := range m(value) {
						hits = append(hits, Hit{n, item})
					}
				}
			}
			return true
		})
		return hits
	}
}

// ForbidIdentifiers reports every identifier, including package, field and label names, that m matches,
// with the items m returns.
func ForbidIdentifiers(m Matcher) Checker {
	return func(file *File) []Hit {
		var hits []Hit
		ast.Inspect(file.Syntax, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				for _, item := range m(id.Name) {
					hits = append(hits, Hit{id, item})
				}
			}
			return true
		})
		return hits
	}
}

// ForbidKeyLiterals keeps key values in the key table. It reports every string literal that is an item of
// an expression switch case, every string literal compared with == or != to the result of a call of one of
// methods (such as "String" of a key event), and every key of keys compared with == or != to anything.
// Only the file that owns the table should be left out. The item is the literal value.
func ForbidKeyLiterals(keys []string, methods ...string) Checker {
	return func(file *File) []Hit {
		var hits []Hit
		ast.Inspect(file.Syntax, func(n ast.Node) bool {
			hits = append(hits, keyHits(n, keys, methods)...)
			return true
		})
		return hits
	}
}

func keyHits(n ast.Node, keys, methods []string) []Hit {
	var hits []Hit
	switch n := n.(type) {
	case *ast.SwitchStmt:
		for _, stmt := range n.Body.List {
			for _, e := range stmt.(*ast.CaseClause).List {
				if lit, ok := e.(*ast.BasicLit); ok {
					if value, ok := stringValue(lit); ok {
						hits = append(hits, Hit{lit, value})
					}
				}
			}
		}
	case *ast.BinaryExpr:
		if n.Op != token.EQL && n.Op != token.NEQ {
			break
		}
		for _, pair := range [][2]ast.Expr{{n.X, n.Y}, {n.Y, n.X}} {
			lit, ok := pair[0].(*ast.BasicLit)
			if !ok {
				continue
			}
			value, ok := stringValue(lit)
			if ok && (slices.Contains(keys, value) || isMethodCall(pair[1], methods)) {
				hits = append(hits, Hit{lit, value})
			}
		}
	}
	return hits
}

func isMethodCall(e ast.Expr, methods []string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && slices.Contains(methods, sel.Sel.Name)
}

func importOf(spec *ast.ImportSpec) string {
	target, _ := strconv.Unquote(spec.Path.Value)
	return target
}

func stringValue(lit *ast.BasicLit) (string, bool) {
	if lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

// Matcher returns the distinct items a value holds, or none.
type Matcher func(value string) []string

// Exact matches a value that equals one of values.
func Exact(values ...string) Matcher {
	return func(value string) []string {
		if slices.Contains(values, value) {
			return []string{value}
		}
		return nil
	}
}

// Words matches each of words that stands in a value as a whole token: the characters around it are no
// ASCII letters or digits. Case is significant.
func Words(words ...string) Matcher {
	patterns := make([]*regexp.Regexp, len(words))
	for i, w := range words {
		patterns[i] = regexp.MustCompile(`(?:^|[^A-Za-z0-9])` + regexp.QuoteMeta(w) + `(?:$|[^A-Za-z0-9])`)
	}
	return func(value string) []string {
		var items []string
		for i, re := range patterns {
			if re.MatchString(value) && !slices.Contains(items, words[i]) {
				items = append(items, words[i])
			}
		}
		return items
	}
}

// Pattern matches every text re finds in a value; the items are the distinct matched texts.
func Pattern(re *regexp.Regexp) Matcher {
	return func(value string) []string {
		var items []string
		for _, item := range re.FindAllString(value, -1) {
			if !slices.Contains(items, item) {
				items = append(items, item)
			}
		}
		return items
	}
}
