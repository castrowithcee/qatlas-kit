package kit

import (
	"os"
	"regexp"
	"testing"

	"github.com/castrowithcee/qatlas-kit/archtest"
)

func TestTechnicalNamesAreApplicationOwned(t *testing.T) {
	forbidden := archtest.Pattern(regexp.MustCompile("(?i)(" + "qat" + "las|" + "cr" + "ew|" + "sys" + "min)"))
	module := archtest.Files{Under: []string{"."}, Tests: archtest.WithTests}
	const hint = "names, prefixes, paths, keys and labels come from the application"
	archtest.Check(t, os.DirFS("."), []archtest.Rule{
		{Name: "identifiers", Files: module, Check: archtest.ForbidIdentifiers(forbidden), Hint: hint},
		{Name: "literals", Files: module, Check: archtest.ForbidLiterals(forbidden), Hint: hint},
		{Name: "imports", Files: module, Check: archtest.ForbidImportPaths(forbidden), Hint: hint},
	}, nil)
}
