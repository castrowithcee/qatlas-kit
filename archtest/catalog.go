package archtest

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// Type names a named type by its import path and name. An empty Package means a type declared in the
// package that is read.
type Type struct {
	Package string
	Name    string
}

// Constant is one string constant of a Type.
type Constant struct {
	Name  string
	Value string
	File  string
	Line  int
}

// Constants returns the string constants of one of types declared in the non-test Go files directly in dir
// of fsys, in file and source order. A constant counts when its declared type or the conversion of its value
// names the type, whatever alias the file gives the import.
func Constants(fsys fs.FS, dir string, types ...Type) ([]Constant, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var constants []Constant
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parseFile(fsys, path.Join(dir, name), "", false)
		if err != nil {
			return nil, err
		}
		isType := typeMatcher(file, types)
		for _, decl := range file.Syntax.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if i >= len(vs.Values) {
						break
					}
					value := vs.Values[i]
					if call, ok := value.(*ast.CallExpr); ok && vs.Type == nil && len(call.Args) == 1 && isType(call.Fun) {
						value = call.Args[0]
					} else if vs.Type == nil || !isType(vs.Type) {
						continue
					}
					lit, ok := value.(*ast.BasicLit)
					if !ok {
						continue
					}
					if text, ok := stringValue(lit); ok {
						constants = append(constants, Constant{id.Name, text, file.Path, file.Fset.Position(id.Pos()).Line})
					}
				}
			}
		}
	}
	return constants, nil
}

func typeMatcher(file *File, types []Type) func(ast.Expr) bool {
	imports := file.Imports()
	return func(e ast.Expr) bool {
		for _, t := range types {
			switch e := e.(type) {
			case *ast.Ident:
				if e.Name == t.Name && (t.Package == "" || slices.Contains(imports["."], t.Package)) {
					return true
				}
			case *ast.SelectorExpr:
				if id, ok := e.X.(*ast.Ident); ok && t.Package != "" && e.Sel.Name == t.Name &&
					slices.Contains(imports[id.Name], t.Package) {
					return true
				}
			}
		}
		return false
	}
}

// CheckCatalog reports every gap between the text IDs a TUI package declares and its catalogs: no constant
// declared at all, which means the scan is broken; two constants with the same ID; a constant whose ID has
// no English text; and everything tui.CheckCatalogs reports, so every text exists in English and German and
// in every other language present. Constants of tui.TextID in the non-test files directly in dir count, and
// so do constants of local, which names the types an application derives from it.
func CheckCatalog(fsys fs.FS, dir string, catalogs i18n.Catalogs[tui.TextID], local ...Type) error {
	textID := reflect.TypeFor[tui.TextID]()
	constants, err := Constants(fsys, dir, append(local, Type{textID.PkgPath(), textID.Name()})...)
	if err != nil {
		return err
	}
	if len(constants) == 0 {
		return fmt.Errorf("%s declares no text ID constant", dir)
	}
	var errs []error
	byID := map[string]Constant{}
	for _, c := range constants {
		if first, ok := byID[c.Value]; ok {
			errs = append(errs, fmt.Errorf("%s:%d: %s repeats the text ID %q of %s", c.File, c.Line, c.Name, c.Value, first.Name))
			continue
		}
		byID[c.Value] = c
		if _, ok := catalogs[i18n.English][tui.TextID(c.Value)]; !ok {
			errs = append(errs, fmt.Errorf("%s:%d: %s (%q) has no English text", c.File, c.Line, c.Name, c.Value))
		}
	}
	errs = append(errs, tui.CheckCatalogs(catalogs))
	return errors.Join(errs...)
}
