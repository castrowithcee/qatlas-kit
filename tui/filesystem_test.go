package tui_test

import (
	"os"
	"testing"

	"github.com/castrowithcee/qatlas-kit/archtest"
)

// The TUI never reads the file system: the application hands it paths and their suggestions.
func TestTUIReadsNoFileSystem(t *testing.T) {
	archtest.Check(t, os.DirFS(".."), []archtest.Rule{{
		Name:  "file system",
		Files: archtest.Files{Under: []string{"tui"}},
		Check: archtest.ForbidImportPaths(archtest.Exact("os", "path/filepath")),
		Hint:  "let the application read the file system and pass the result, such as through DialogInput.Suggest",
	}}, nil)
}
