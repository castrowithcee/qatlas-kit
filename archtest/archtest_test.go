package archtest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const goMod = "module example.com/m\n\ngo 1.26\n"

func moduleFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{"go.mod": {Data: []byte(goMod)}}
	for name, src := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(src)}
	}
	return fsys
}

// items lists the findings as "file:line rule item".
func items(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, fmt.Sprintf("%s:%02d %s %s", f.File, f.Line, f.Rule, f.Item))
	}
	return out
}

func find(t *testing.T, fsys fstest.MapFS, rules ...Rule) []string {
	t.Helper()
	findings, err := Find(fsys, rules...)
	if err != nil {
		t.Fatal(err)
	}
	return items(findings)
}

func wantItems(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

var all = Files{Under: []string{"."}}

func TestImportRules(t *testing.T) {
	fsys := moduleFS(map[string]string{
		"ui/a.go": "package ui\n\nimport (\n\t_ \"example.com/m/store\"\n\tv \"example.com/m/provider/alpha/sub\"\n" +
			"\t_ \"example.com/m/core\"\n\t_ \"net/http\"\n)\n\nvar _ = v.X\n",
		"ui/a_test.go": "package ui\n\nimport _ \"example.com/m/store\"\n",
		"ui/sub/b.go":  "package sub\n\nimport _ \"example.com/m/provider\"\n",
		"provider/alpha/a.go": "package alpha\n\nimport (\n\t_ \"example.com/m/provider\"\n\t_ \"example.com/m/provider/beta\"\n" +
			"\t_ \"example.com/m/store\"\n\t_ \"os\"\n)\n",
		"provider/p.go": "package provider\n\nimport _ \"example.com/m/store\"\n",
	})
	got := find(t, fsys,
		Rule{Name: "surface", Files: Files{Under: []string{"ui"}}, Check: ForbidImports("store", "provider/...")},
		Rule{Name: "provider", Files: Files{Under: []string{"provider/*"}}, Check: AllowImports("provider", "internal/...")},
	)
	wantItems(t, got,
		"provider/alpha/a.go:05 provider provider/beta",
		"provider/alpha/a.go:06 provider store",
		"ui/a.go:04 surface store",
		"ui/a.go:05 surface provider/alpha/sub",
		"ui/sub/b.go:03 surface provider",
	)
}

func TestForbidImportPathsSkipsTheModule(t *testing.T) {
	fsys := moduleFS(map[string]string{
		"a.go": "package a\n\nimport (\n\t_ \"example.com/m/beta\"\n\t_ \"example.com/other/beta\"\n\t_ \"example.com/m\"\n)\n",
	})
	got := find(t, fsys, Rule{Name: "names", Files: all, Check: ForbidImportPaths(Words("beta", "m"))})
	wantItems(t, got, "a.go:05 names beta")
}

func TestForbidSelectorsFollowsAliasesAndDotImports(t *testing.T) {
	const pkg = "example.com/lib/gloss/v2"
	fsys := moduleFS(map[string]string{
		"ui/color.go": "package ui\n\nimport \"example.com/lib/gloss/v2\"\n\nvar c = gloss.Color(\"1\")\n",
		"ui/a.go":     "package ui\n\nimport g \"example.com/lib/gloss/v2\"\n\nvar c = g.Color(\"1\")\nvar s = g.Style{}\n",
		"ui/b.go":     "package ui\n\nimport . \"example.com/lib/gloss/v2\"\n\nvar c = Color(\"1\")\n",
		"ui/c.go":     "package ui\n\nimport gloss \"example.com/other/gloss\"\n\nvar c = gloss.Color(\"1\")\n",
		"ui/d.go":     "package ui\n\nfunc f(gloss struct{ Color int }) int { return gloss.Color }\n",
	})
	got := find(t, fsys, Rule{Name: "colour", Files: Files{Under: []string{"ui"}, Except: []string{"ui/color.go"}},
		Check: ForbidSelectors(pkg, "Color")})
	wantItems(t, got, "ui/a.go:05 colour gloss.Color", "ui/b.go:03 colour gloss.*")
}

func TestForbidConstruction(t *testing.T) {
	fsys := moduleFS(map[string]string{
		"a.go": "package a\n\nimport \"net/http\"\n\nvar a = &http.Client{}\nvar b = new(http.Client)\nvar c *http.Client\nvar d = http.Request{}\n",
		"b.go": "package a\n\nimport . \"net/http\"\n\nvar e = Client{}\n",
		"c.go": "package a\n\ntype Client struct{}\n\nvar f = Client{}\n",
	})
	got := find(t, fsys, Rule{Name: "client", Files: all, Check: ForbidConstruction("net/http", "Client")})
	wantItems(t, got, "a.go:05 client http.Client", "a.go:06 client http.Client", "b.go:05 client http.Client")
}

func TestForbidDeclarations(t *testing.T) {
	fsys := moduleFS(map[string]string{
		"a.go": "package a\n\nfunc retryAfter() {}\n\ntype T struct{}\n\nfunc (T) capHold() {}\n\ntype validToken int\n\n" +
			"var statusError = 1\n\nfunc f() { var retryAfter = 1; _ = retryAfter }\n",
	})
	got := find(t, fsys, Rule{Name: "helper", Files: all,
		Check: ForbidDeclarations("retryAfter", "capHold", "validToken", "statusError")})
	wantItems(t, got, "a.go:03 helper retryAfter", "a.go:07 helper capHold", "a.go:09 helper validToken",
		"a.go:11 helper statusError")
}

func TestForbidLiteralsAndIdentifiers(t *testing.T) {
	fsys := moduleFS(map[string]string{
		"a.go": "package a\n\nimport _ \"example.com/alpha\"\n\nconst Hint = \"Use Alpha here\"\nconst ok = \"alphabet\"\n" +
			"const raw = `Alpha`\nvar AlphaClient = 1\nconst n = 'A'\n",
	})
	got := find(t, fsys,
		Rule{Name: "literal", Files: all, Check: ForbidLiterals(Words("Alpha"))},
		Rule{Name: "ident", Files: all, Check: ForbidIdentifiers(Pattern(regexp.MustCompile(`(?i)alpha`)))},
	)
	wantItems(t, got, "a.go:05 literal Alpha", "a.go:07 literal Alpha", "a.go:08 ident Alpha")
}

func TestMatchers(t *testing.T) {
	for _, c := range []struct {
		m     Matcher
		value string
		want  []string
	}{
		{Exact("Retry-After"), "Retry-After", []string{"Retry-After"}},
		{Exact("Retry-After"), "Retry-After:", nil},
		{Words("make", "Alpha"), "make sure Alpha-x alpha", []string{"make", "Alpha"}},
		{Words("make"), "remake makes", nil},
		{Words("a.b"), "x a.b.c", []string{"a.b"}},
		{Pattern(regexp.MustCompile(`(?i)beta`)), "Beta beta BETA beta", []string{"Beta", "beta", "BETA"}},
	} {
		if got := c.m(c.value); !slices.Equal(got, c.want) {
			t.Errorf("match %q = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestForbidKeyLiterals(t *testing.T) {
	fsys := moduleFS(map[string]string{
		"ui/keymap.go": "package ui\n\nfunc k(s string) bool { switch s {\ncase \"q\":\n}\nreturn s == \"esc\" }\n",
		"ui/a.go": "package ui\n\nfunc f(m msg, s string, v any) {\n\tswitch m.String() {\n\tcase \"x\", \"enter\":\n\t}\n" +
			"\tif m.String() == \"y\" || \"z\" != m.String() {\n\t}\n\tif s == \"esc\" || s == \"other\" {\n\t}\n" +
			"\tswitch v.(type) {\n\tcase string:\n\t}\n\tif m.Text() == \"w\" {\n\t}\n}\n",
		"ui/a_test.go": "package ui\n\nfunc g(s string) bool { return s == \"esc\" }\n",
		"core/c.go":    "package core\n\nfunc g(s string) bool { return s == \"esc\" }\n",
	})
	got := find(t, fsys, Rule{Name: "keys", Files: Files{Under: []string{"ui"}, Except: []string{"ui/keymap.go"}},
		Check: ForbidKeyLiterals([]string{"esc", "enter"}, "String")})
	wantItems(t, got,
		"ui/a.go:05 keys enter",
		"ui/a.go:05 keys x",
		"ui/a.go:07 keys y",
		"ui/a.go:07 keys z",
		"ui/a.go:09 keys esc",
	)
}

func TestFilesSelection(t *testing.T) {
	fsys := moduleFS(map[string]string{
		"ui/a.go":            "package ui\n\nconst A = \"Alpha\"\n",
		"ui/a_test.go":       "package ui\n\nconst A = \"Alpha\"\n",
		"ui/testdata/x.go":   "package x\n\nconst A = \"Alpha\"\n",
		"ui/_old/x.go":       "package x\n\nconst A = \"Alpha\"\n",
		"ui/.hidden/x.go":    "package x\n\nconst A = \"Alpha\"\n",
		"ui/vendor/x.go":     "package x\n\nconst A = \"Alpha\"\n",
		"ui/gen/x.go":        "package gen\n\nconst A = \"Alpha\"\n",
		"uiextra/x.go":       "package uiextra\n\nconst A = \"Alpha\"\n",
		"ui/broken.txt":      "not go",
		"ui/sub/deep/y.go":   "package deep\n\nconst A = \"Alpha\"\n",
		"ui/sub/deep/y_test": "not go",
	})
	check := ForbidLiterals(Exact("Alpha"))
	for _, c := range []struct {
		name  string
		files Files
		want  []string
	}{
		{"sources", Files{Under: []string{"ui"}}, []string{"ui/a.go", "ui/gen/x.go", "ui/sub/deep/y.go"}},
		{"with tests", Files{Under: []string{"ui"}, Tests: WithTests}, []string{"ui/a.go", "ui/a_test.go", "ui/gen/x.go", "ui/sub/deep/y.go"}},
		{"only tests", Files{Under: []string{"ui"}, Tests: OnlyTests}, []string{"ui/a_test.go"}},
		{"except", Files{Under: []string{"ui"}, Except: []string{"ui/gen", "ui/sub/*/y.go"}}, []string{"ui/a.go"}},
		{"pattern", Files{Under: []string{"ui/*"}}, []string{"ui/gen/x.go", "ui/sub/deep/y.go"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			findings, err := Find(fsys, Rule{Name: "r", Files: c.files, Check: check})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range findings {
				got = append(got, f.File)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("files = %q, want %q", got, c.want)
			}
		})
	}
}

func TestFindRefusesRulesThatCannotCheck(t *testing.T) {
	fsys := moduleFS(map[string]string{"ui/a.go": "package ui\n"})
	check := ForbidLiterals(Exact("x"))
	for _, c := range []struct {
		name  string
		fsys  fstest.MapFS
		rules []Rule
		want  string
	}{
		{"no rule", fsys, nil, "no rule"},
		{"no name", fsys, []Rule{{Name: " ", Files: all, Check: check}}, "no name"},
		{"twice", fsys, []Rule{{Name: "r", Files: all, Check: check}, {Name: "r", Files: all, Check: check}}, "twice"},
		{"no checker", fsys, []Rule{{Name: "r", Files: all}}, "no checker"},
		{"no directory", fsys, []Rule{{Name: "r", Check: check}}, "no directory"},
		{"bad pattern", fsys, []Rule{{Name: "r", Files: Files{Under: []string{"ui["}}, Check: check}}, "bad pattern"},
		{"empty pattern", fsys, []Rule{{Name: "r", Files: Files{Under: []string{"ui"}, Except: []string{""}}, Check: check}}, "bad pattern"},
		{"renamed directory", fsys, []Rule{{Name: "r", Files: Files{Under: []string{"web"}}, Check: check}}, "selects no Go file"},
		{"stale except", fsys, []Rule{{Name: "r", Files: Files{Under: []string{"ui"}, Except: []string{"ui/gone.go"}}, Check: check}}, "leaves out no file"},
		{"except of a skipped test", moduleFS(map[string]string{"ui/a.go": "package ui\n", "ui/a_test.go": "package ui\n"}),
			[]Rule{{Name: "r", Files: Files{Under: []string{"ui"}, Except: []string{"ui/a_test.go"}}, Check: check}}, "leaves out no file"},
		{"no go.mod", fstest.MapFS{"a.go": {Data: []byte("package a\n")}}, []Rule{{Name: "r", Files: all, Check: check}}, "go.mod"},
		{"no module", fstest.MapFS{"go.mod": {Data: []byte("go 1.26\n")}}, []Rule{{Name: "r", Files: all, Check: check}}, "no module"},
		{"parse error", moduleFS(map[string]string{"a.go": "package a\n\nfunc {\n"}), []Rule{{Name: "r", Files: all, Check: check}}, "parse a.go"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Find(c.fsys, c.rules...)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want one with %q", err, c.want)
			}
		})
	}
}

func TestProblemsKeepTheExceptionsShrinking(t *testing.T) {
	findings := []Finding{
		{Rule: "names", File: "a.go", Line: 3, Item: "Alpha", Hint: "read it from the registry"},
		{Rule: "names", File: "a.go", Line: 9, Item: "Alpha", Hint: "read it from the registry"},
		{Rule: "imports", File: "b.go", Line: 4, Item: "store"},
	}
	problems := Problems(findings, nil)
	want := []string{
		`a.go:3: rule names: "Alpha"; read it from the registry`,
		`a.go:9: rule names: "Alpha"; read it from the registry`,
		`b.go:4: rule imports: "store"`,
	}
	if !slices.Equal(problems, want) {
		t.Errorf("problems = %q, want %q", problems, want)
	}
	excepted := []Exception{{"names", "a.go", "Alpha", "existing debt"}, {"imports", "b.go", "store", "entry point"}}
	if problems := Problems(findings, excepted); len(problems) != 0 {
		t.Errorf("excepted findings still reported: %q", problems)
	}
	for name, c := range map[string]struct {
		exceptions []Exception
		want       string
	}{
		"stale":           {[]Exception{{"names", "gone.go", "Alpha", "r"}}, "stale"},
		"other rule":      {[]Exception{{"imports", "a.go", "Alpha", "r"}}, "stale"},
		"other item":      {[]Exception{{"names", "a.go", "Beta", "r"}}, "stale"},
		"no reason":       {[]Exception{{"names", "a.go", "Alpha", " "}}, "no reason"},
		"listed twice":    {[]Exception{{"names", "a.go", "Alpha", "r"}, {"names", "a.go", "Alpha", "r"}}, "listed twice"},
		"reason only one": {[]Exception{{"names", "a.go", "Alpha", ""}, {"names", "a.go", "Alpha", "r"}}, "no reason"},
	} {
		problems := Problems(findings[:2], c.exceptions)
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, c.want) }) {
			t.Errorf("%s: problems = %q, want one with %q", name, problems, c.want)
		}
	}
	// An exception without a reason does not allow its finding.
	if problems := Problems(findings[:1], []Exception{{"names", "a.go", "Alpha", ""}}); len(problems) != 2 {
		t.Errorf("an exception without a reason allows the finding: %q", problems)
	}
}

// recorder captures what Check reports instead of failing the test that runs it.
type recorder struct {
	testing.TB
	errors []string
	fatal  string
}

func (r *recorder) Helper() {}

func (r *recorder) Error(args ...any) { r.errors = append(r.errors, args[0].(string)) }

func (r *recorder) Fatal(args ...any) {
	r.fatal = args[0].(error).Error()
	panic(r)
}

func TestCheckReportsEveryProblem(t *testing.T) {
	fsys := moduleFS(map[string]string{"a.go": "package a\n\nconst A, B = \"Alpha\", \"Beta\"\n"})
	rules := []Rule{{Name: "names", Files: all, Check: ForbidLiterals(Exact("Alpha", "Beta"))}}
	r := &recorder{TB: t}
	Check(r, fsys, rules, []Exception{{"names", "a.go", "Beta", "kept"}})
	if len(r.errors) != 1 || !strings.Contains(r.errors[0], `"Alpha"`) {
		t.Errorf("errors = %q, want the one unexcepted finding", r.errors)
	}

	r = &recorder{TB: t}
	func() {
		defer func() {
			if v := recover(); v != r {
				panic(v)
			}
		}()
		Check(r, fsys, []Rule{{Name: "names", Files: Files{Under: []string{"gone"}}, Check: rules[0].Check}}, nil)
	}()
	if !strings.Contains(r.fatal, "selects no Go file") {
		t.Errorf("fatal = %q, want the broken rule", r.fatal)
	}
}

func TestModuleRootAndPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := ModuleRoot(deep)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(root); mustEval(t, got) != want {
		t.Errorf("ModuleRoot = %q, want %q", got, root)
	}
	if module, err := ModulePath(os.DirFS(got)); err != nil || module != "example.com/m" {
		t.Errorf("ModulePath = %q, %v", module, err)
	}
	if _, err := ModuleRoot(filepath.VolumeName(root) + string(filepath.Separator)); err == nil {
		t.Error("ModuleRoot found a go.mod at the file system root")
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPackageName(t *testing.T) {
	for path, want := range map[string]string{
		"net/http":                 "http",
		"example.com/lib/gloss/v2": "gloss",
		"gopkg.in/yaml.v3":         "yaml",
		"example.com/m/vault":      "vault",
	} {
		if got := PackageName(path); got != want {
			t.Errorf("PackageName(%q) = %q, want %q", path, got, want)
		}
	}
}
