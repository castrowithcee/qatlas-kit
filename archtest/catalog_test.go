package archtest

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/tui"
)

const tuiImport = "github.com/castrowithcee/" + "qat" + "las-kit/tui"

func catalogFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, src := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(src)}
	}
	return fsys
}

func TestConstantsFindsTheTypedIDs(t *testing.T) {
	fsys := catalogFS(map[string]string{
		"ui/keys.go": "package ui\n\nimport kt \"" + tuiImport + "\"\n\nconst (\n\ttA kt.TextID = \"a\"\n\ttB = kt.TextID(\"b\")\n" +
			"\tplain = \"c\"\n\ttN kt.TextID = \"n\" + \"m\"\n)\n\ntype local string\n\nconst tL local = \"l\"\n",
		"ui/dot.go":       "package ui\n\nimport . \"" + tuiImport + "\"\n\nconst tD TextID = \"d\"\n",
		"ui/other.go":     "package ui\n\nconst tO TextID = \"o\"\n",
		"ui/keys_test.go": "package ui\n\nimport \"" + tuiImport + "\"\n\nconst tT tui.TextID = \"t\"\n",
		"ui/sub/s.go":     "package sub\n\nimport \"" + tuiImport + "\"\n\nconst tS tui.TextID = \"s\"\n",
	})
	constants, err := Constants(fsys, "ui", Type{tuiImport, "TextID"}, Type{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range constants {
		got = append(got, c.File+" "+c.Name+"="+c.Value)
	}
	want := []string{"ui/dot.go tD=d", "ui/keys.go tA=a", "ui/keys.go tB=b", "ui/keys.go tL=l"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("constants:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if constants[1].Line != 6 {
		t.Errorf("line of tA = %d, want 6", constants[1].Line)
	}
	if _, err := Constants(fsys, "gone"); err == nil {
		t.Error("a missing directory gave no error")
	}
}

func complete() i18n.Catalogs[tui.TextID] {
	return i18n.Catalogs[tui.TextID]{
		i18n.English: {"a": "A %d", "b": "B"},
		i18n.German:  {"a": "Ä %d", "b": "Be"},
	}
}

func TestCheckCatalog(t *testing.T) {
	fsys := catalogFS(map[string]string{
		"ui/keys.go": "package ui\n\nimport \"" + tuiImport + "\"\n\nconst (\n\ttA tui.TextID = \"a\"\n\ttB tui.TextID = \"b\"\n)\n",
	})
	if err := CheckCatalog(fsys, "ui", complete()); err != nil {
		t.Fatalf("complete catalog: %v", err)
	}
	for _, c := range []struct {
		name    string
		files   map[string]string
		catalog func(i18n.Catalogs[tui.TextID])
		want    string
	}{
		{"declared without English", nil, func(c i18n.Catalogs[tui.TextID]) { delete(c[i18n.English], "b"); delete(c[i18n.German], "b") },
			`tB ("b") has no English text`},
		{"missing German", nil, func(c i18n.Catalogs[tui.TextID]) { delete(c[i18n.German], "a") }, "has no de text"},
		{"empty text", nil, func(c i18n.Catalogs[tui.TextID]) { c[i18n.German]["b"] = " " }, "is empty"},
		{"other verbs", nil, func(c i18n.Catalogs[tui.TextID]) { c[i18n.German]["a"] = "Ä %s" }, "format verbs"},
		{"repeated ID", map[string]string{"ui/more.go": "package ui\n\nimport \"" + tuiImport + "\"\n\nconst tC tui.TextID = \"a\"\n"},
			nil, `tC repeats the text ID "a" of tA`},
		{"local type", map[string]string{"ui/more.go": "package ui\n\ntype id string\n\nconst tC id = \"c\"\n"}, nil, `tC ("c") has no English text`},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := catalogFS(map[string]string{"ui/keys.go": string(fsys["ui/keys.go"].Data)})
			for name, src := range c.files {
				files[name] = &fstest.MapFile{Data: []byte(src)}
			}
			catalogs := complete()
			if c.catalog != nil {
				c.catalog(catalogs)
			}
			err := CheckCatalog(files, "ui", catalogs, Type{Name: "id"})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want one with %q", err, c.want)
			}
		})
	}
	empty := catalogFS(map[string]string{"ui/keys.go": "package ui\n\nconst a = \"a\"\n"})
	if err := CheckCatalog(empty, "ui", complete()); err == nil || !strings.Contains(err.Error(), "no text ID constant") {
		t.Errorf("a package without IDs: err = %v", err)
	}
}

// The kit's own module catalog passes the check its applications run.
func TestModuleCatalogIsComplete(t *testing.T) {
	root, err := ModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckCatalog(os.DirFS(root), "tui", tui.ModuleCatalog(), Type{Name: "TextID"}); err != nil {
		t.Error(err)
	}
}
