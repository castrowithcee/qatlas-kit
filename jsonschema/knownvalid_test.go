package jsonschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// corpus builds a fixed set of text-distinct valid schemas shaped like the input and output schemas of
// command-line tools: mostly small and some large, every object closed with additionalProperties false,
// most leaves holding only a type, and patterns drawn from a shared set.
func corpus() []json.RawMessage {
	g := corpusGen{r: rand.New(rand.NewPCG(1, 2)), words: []string{"name", "path", "mode", "limit", "offset",
		"query", "target", "label", "value", "count", "size", "owner", "state", "kind", "scope", "format", "level",
		"start", "end", "filter", "sort", "page", "items", "source", "region", "zone", "group", "color", "title",
		"note"}}
	templates := []string{
		`^[a-z][a-z0-9-]{0,%d}$`, `^\d{%d}$`, `^[A-Z]{2}\d{1,%d}$`, `^\w+(\.\w+){0,%d}$`, `^[^\s/]{1,%d}$`,
		`^v\d+\.\d+\.\d{1,%d}$`, `^#[0-9a-fA-F]{%d}$`, `^(\*|[0-5]?\d)(,[0-5]?\d){0,%d}$`,
		`^\S+@\S+\.[a-z]{1,%d}$`, `^[a-z]{1,%d}:\/\/\S+$`,
	}
	for _, template := range templates {
		for k := 1; k <= 25; k++ {
			quoted, _ := json.Marshal(fmt.Sprintf(template, k))
			g.patterns = append(g.patterns, string(quoted))
		}
	}
	schemas := make([]json.RawMessage, 4000)
	for i := range schemas {
		var nodes int
		switch u := g.r.IntN(1000); {
		case u < 3:
			nodes = 1000 + g.r.IntN(700)
		case u < 20:
			nodes = 100 + g.r.IntN(300)
		case u < 120:
			nodes = 30 + g.r.IntN(70)
		case u < 400:
			nodes = 10 + g.r.IntN(16)
		default:
			nodes = 3 + g.r.IntN(7)
		}
		g.b.Reset()
		fmt.Fprintf(&g.b, `{"description":"Input of example command %d.",`, i)
		g.object(1, 2+g.r.IntN(4), nodes)
		g.b.WriteByte('}')
		schemas[i] = json.RawMessage(bytes.Clone(g.b.Bytes()))
	}
	return schemas
}

type corpusGen struct {
	r        *rand.Rand
	b        bytes.Buffer
	words    []string
	patterns []string
}

// object writes the members of an object schema after its opening brace, budget nodes in all. depth is
// the level of the object, the root being 1.
func (g *corpusGen) object(depth, maxDepth, budget int) {
	g.b.WriteString(`"type":"object","properties":{`)
	var names []string
	for budget--; budget > 0 || len(names) == 0; {
		name := g.words[g.r.IntN(len(g.words))]
		if g.r.IntN(3) == 0 {
			name += "_" + g.words[g.r.IntN(len(g.words))]
		}
		if slices.Contains(names, name) {
			name += strconv.Itoa(len(names))
		}
		if len(names) > 0 {
			g.b.WriteByte(',')
		}
		names = append(names, name)
		fmt.Fprintf(&g.b, `"%s":{`, name)
		if depth+1 < maxDepth && budget >= 3 && g.r.IntN(4) == 0 {
			share := 2 + g.r.IntN(budget-1)
			if g.r.IntN(5) == 0 {
				g.b.WriteString(`"type":"array","maxItems":` + strconv.Itoa(1+g.r.IntN(100)) + `,"items":{`)
				g.object(depth+2, maxDepth, share-1)
				g.b.WriteByte('}')
			} else {
				g.object(depth+1, maxDepth, share)
			}
			budget -= share
		} else {
			budget -= g.leaf()
		}
		g.b.WriteByte('}')
	}
	g.b.WriteString(`},"additionalProperties":false`)
	if g.r.IntN(2) == 0 {
		fmt.Fprintf(&g.b, `,"required":["%s"]`, names[0])
	}
	if g.r.IntN(40) == 0 {
		g.b.WriteString(`,"propertyNames":{"pattern":` + g.patterns[g.r.IntN(len(g.patterns))] + `}`)
	}
}

// leaf writes the members of a leaf schema and returns the number of nodes it holds.
func (g *corpusGen) leaf() int {
	kinds := []string{"string", "string", "string", "integer", "boolean", "number"}
	if g.r.IntN(4) != 0 {
		fmt.Fprintf(&g.b, `"type":"%s"`, kinds[g.r.IntN(len(kinds))])
		return 1
	}
	if g.r.IntN(5) == 0 {
		fmt.Fprintf(&g.b, `"description":"Sets the %s of the request.",`, g.words[g.r.IntN(len(g.words))])
	}
	switch g.r.IntN(10) {
	case 0, 1, 2, 3:
		g.b.WriteString(`"type":"string"`)
		if g.r.IntN(2) == 0 {
			fmt.Fprintf(&g.b, `,"minLength":%d,"maxLength":%d`, g.r.IntN(2), 8+g.r.IntN(250))
		}
		switch g.r.IntN(12) {
		case 0, 1, 2, 3:
			g.b.WriteString(`,"pattern":` + g.patterns[g.r.IntN(len(g.patterns))])
		case 4:
			g.b.WriteString(`,"format":"date-time"`)
		case 5:
			g.b.WriteString(`,"format":"uuid"`)
		case 6, 7:
			g.b.WriteString(`,"enum":["alpha","beta","gamma"]`)
		}
	case 4, 5:
		fmt.Fprintf(&g.b, `"type":"integer","minimum":%d,"maximum":%d`, g.r.IntN(2), 10+g.r.IntN(100000))
	case 6:
		g.b.WriteString(`"type":"number","minimum":0,"maximum":1.5`)
	case 7, 8:
		g.b.WriteString(`"type":"array","items":{"type":"string"},"maxItems":` + strconv.Itoa(1+g.r.IntN(50)))
		if g.r.IntN(8) == 0 {
			g.b.WriteString(`,"uniqueItems":true`)
		}
		return 2
	case 9:
		if g.r.IntN(2) == 0 {
			g.b.WriteString(`"anyOf":[{"type":"string"},{"type":"null"}]`)
			return 3
		}
		g.b.WriteString(`"type":"integer","enum":[1,2,3,5,8]`)
	}
	return 1
}

func TestCorpusShape(t *testing.T) {
	schemas := corpus()
	distinct := map[string]bool{}
	total, patterns, largest, nodes := 0, 0, 0, 0
	for _, schema := range schemas {
		distinct[string(schema)] = true
		total += len(schema)
		largest = max(largest, len(schema))
		patterns += bytes.Count(schema, []byte(`"pattern"`))
		nodes += bytes.Count(schema, []byte("{")) - bytes.Count(schema, []byte(`"properties":{`))
		if !json.Valid(schema) || !knownValid(schema, nil) || CheckSchema(schema) != nil {
			t.Fatalf("knownValid did not recognize a valid corpus schema: %s", schema)
		}
	}
	sizes := make([]int, len(schemas))
	for i, schema := range schemas {
		sizes[i] = len(schema)
	}
	slices.Sort(sizes)
	if len(distinct) != len(schemas) {
		t.Errorf("%d of %d schemas are distinct", len(distinct), len(schemas))
	}
	t.Logf("%d schemas, %d bytes, median %d, largest %d, %.1f nodes and %.2f patterns per schema",
		len(schemas), total, sizes[len(sizes)/2], largest, float64(nodes)/float64(len(schemas)),
		float64(patterns)/float64(len(schemas)))
}

// sameVerdict fails the test if CheckSchema, or the checker on a first or a repeated call, disagrees with
// checkSchemaTree on a schema.
func sameVerdict(t *testing.T, checker *Checker, schema []byte) {
	t.Helper()
	want := checkSchemaTree(schema)
	for name, got := range map[string]error{
		"CheckSchema": CheckSchema(schema), "Checker": checker.CheckSchema(schema),
		"Checker again": checker.CheckSchema(schema),
	} {
		if (got == nil) != (want == nil) || got != nil && got.Error() != want.Error() {
			t.Errorf("%q: %s = %v, tree = %v", schema, name, got, want)
		}
	}
}

// testLiterals returns every string literal and every concatenation of string literals in the other tests
// of the package, which covers each schema they reject.
func testLiterals(t testing.TB) []string {
	t.Helper()
	var literals []string
	var fold func(ast.Expr) (string, bool)
	fold = func(e ast.Expr) (string, bool) {
		switch e := e.(type) {
		case *ast.BasicLit:
			if e.Kind == token.STRING {
				s, err := strconv.Unquote(e.Value)
				return s, err == nil
			}
		case *ast.BinaryExpr:
			if e.Op == token.ADD {
				x, okX := fold(e.X)
				y, okY := fold(e.Y)
				return x + y, okX && okY
			}
		case *ast.ParenExpr:
			return fold(e.X)
		}
		return "", false
	}
	for _, name := range []string{"jsonschema_test.go", "checkschema_test.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if e, ok := n.(ast.Expr); ok {
				if s, ok := fold(e); ok {
					// Messages and names are no schemas and only slow the test down.
					if strings.ContainsAny(s, "{[") || json.Valid([]byte(s)) {
						literals = append(literals, s)
					}
					return false
				}
			}
			return true
		})
	}
	if len(literals) < 50 {
		t.Fatalf("found only %d literals", len(literals))
	}
	for name := range nullMessages {
		literals = append(literals, `{"`+name+`":null}`)
	}
	return literals
}

// replacements and insertions are the bytes a mutation puts into a schema. Each can make a schema
// malformed in syntax or in a keyword.
const (
	replacements = "09-e\"\\{}[,:nx\xff"
	insertions   = "9\\x,"
)

// mutate returns schema with the byte at i replaced by c, or removed if c is 0, or with c inserted before
// it if insert is set.
func mutate(schema []byte, i int, c byte, insert bool) []byte {
	out := make([]byte, 0, len(schema)+1)
	out = append(out, schema[:i]...)
	if c != 0 {
		out = append(out, c)
	}
	if insert {
		return append(out, schema[i:]...)
	}
	return append(out, schema[i+1:]...)
}

// TestCheckSchemaMatchesTree holds the fast path and the checker to the tree's verdict and error text on every schema of
// the other tests, on their one-byte mutations, and on one-byte mutations of the corpus.
func TestCheckSchemaMatchesTree(t *testing.T) {
	// One checker for all schemas, so a remembered schema or pattern meets the mutations of its neighbours.
	checker := new(Checker)
	everyMutation := func(schema []byte) {
		sameVerdict(t, checker, schema)
		for i := range schema {
			sameVerdict(t, checker, mutate(schema, i, 0, false))
			for _, c := range []byte(replacements) {
				sameVerdict(t, checker, mutate(schema, i, c, false))
			}
			for _, c := range []byte(insertions) {
				sameVerdict(t, checker, mutate(schema, i, c, true))
			}
		}
	}
	for _, literal := range testLiterals(t) {
		everyMutation([]byte(literal))
	}

	schemas := corpus()
	small := 0
	for _, schema := range schemas {
		if len(schema) < 600 && small < 8 {
			everyMutation(schema)
			small++
		}
	}
	r := rand.New(rand.NewPCG(3, 4))
	for _, schema := range schemas {
		for range 4 {
			i := r.IntN(len(schema))
			c := replacements[r.IntN(len(replacements))]
			if r.IntN(2) == 0 {
				// A byte from elsewhere in the schema keeps most mutations syntactically valid.
				c = schema[r.IntN(len(schema))]
			}
			sameVerdict(t, checker, mutate(schema, i, c, r.IntN(3) == 0))
		}
	}
}

func FuzzCheckSchema(f *testing.F) {
	for _, literal := range testLiterals(f) {
		f.Add([]byte(literal))
	}
	for _, schema := range corpus()[:50] {
		f.Add([]byte(schema))
	}
	checker := new(Checker)
	f.Fuzz(func(t *testing.T, schema []byte) {
		sameVerdict(t, checker, schema)
	})
}

// TestCheckSchemaAllocationsPerSchema counts allocations over the corpus, so the bound holds on any
// machine.
func TestCheckSchemaAllocationsPerSchema(t *testing.T) {
	schemas := corpus()
	allocs := testing.AllocsPerRun(2, func() {
		for _, schema := range schemas {
			_ = CheckSchema(schema)
		}
	})
	if perSchema := allocs / float64(len(schemas)); perSchema > 20 {
		t.Errorf("CheckSchema costs %.1f allocations per corpus schema, want at most 20", perSchema)
	}
}

// BenchmarkCheckSchemaCorpus measures CheckSchema and a new Checker against the tree alone, over the corpus
// and over the corpus with a third of its schemas repeated, as tools repeat a schema for several commands.
func BenchmarkCheckSchemaCorpus(b *testing.B) {
	distinct := corpus()
	repeated := append(slices.Clone(distinct), distinct[:len(distinct)/3]...)
	r := rand.New(rand.NewPCG(5, 6))
	r.Shuffle(len(repeated), func(i, j int) { repeated[i], repeated[j] = repeated[j], repeated[i] })
	for _, set := range []struct {
		name    string
		schemas []json.RawMessage
	}{{"distinct", distinct}, {"repeated", repeated}} {
		for _, bench := range []struct {
			name  string
			check func() func(json.RawMessage) error
		}{
			{"tree", func() func(json.RawMessage) error { return checkSchemaTree }},
			{"CheckSchema", func() func(json.RawMessage) error { return CheckSchema }},
			{"Checker", func() func(json.RawMessage) error { return new(Checker).CheckSchema }},
		} {
			b.Run(set.name+"/"+bench.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					check := bench.check()
					for _, schema := range set.schemas {
						_ = check(schema)
					}
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(set.schemas)), "ns/schema")
			})
		}
	}
}
