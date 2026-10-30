package kit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestTechnicalNamesAreApplicationOwned(t *testing.T) {
	forbidden := regexp.MustCompile("(?i)(" + "qat" + "las|" + "cr" + "ew|" + "sys" + "min)")
	modulePath := "github.com/castrowithcee/" + "qat" + "las-kit"
	files := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != "." && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			return err
		}
		moduleImports := make(map[token.Pos]bool)
		for _, spec := range file.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if value == modulePath || strings.HasPrefix(value, modulePath+"/") {
				moduleImports[spec.Path.Pos()] = true
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			var value string
			switch node := node.(type) {
			case *ast.Ident:
				value = node.Name
			case *ast.BasicLit:
				if node.Kind != token.STRING || moduleImports[node.Pos()] {
					return true
				}
				value, _ = strconv.Unquote(node.Value)
			}
			if forbidden.MatchString(value) {
				t.Errorf("%s: application-specific technical name %q", files.Position(node.Pos()), value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
