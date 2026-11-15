package jsonschema_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/castrowithcee/qatlas-kit/jsonschema"
)

// benchmarkSchemas builds a fixed corpus of valid schemas of depth 2 to 21. Each level nests through
// properties, items, anyOf or a schema under additionalProperties and carries leaf members with recurring
// patterns, bounds and enums.
func benchmarkSchemas() []json.RawMessage {
	patterns := []string{`^[a-z][a-z0-9-]{0,62}$`, `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`, `^[A-Z]{2,3}[0-9]{1,6}$`}
	var level func(depth, seed int) map[string]any
	level = func(depth, seed int) map[string]any {
		node := map[string]any{
			"type":        "object",
			"description": fmt.Sprintf("Level %d of example %d.", depth, seed),
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "pattern": patterns[(depth+seed)%len(patterns)],
					"minLength": 1, "maxLength": 63, "x-form": "a short name"},
				"size":  map[string]any{"type": "integer", "minimum": 0, "maximum": 1000},
				"kind":  map[string]any{"enum": []any{"alpha", "beta", "gamma", 1}},
				"tags":  map[string]any{"type": "array", "uniqueItems": true, "items": map[string]any{"type": "string"}},
				"start": map[string]any{"type": "string", "format": "date-time"},
			},
			"required":             []any{"name"},
			"additionalProperties": false,
		}
		if depth <= 1 {
			return node
		}
		child := level(depth-1, seed)
		properties := node["properties"].(map[string]any)
		switch (depth + seed) % 4 {
		case 0:
			properties["child"] = child
		case 1:
			properties["children"] = map[string]any{"type": "array", "minItems": 1, "items": child}
		case 2:
			properties["either"] = map[string]any{"anyOf": []any{map[string]any{"type": "null"}, child}}
		case 3:
			properties["labels"] = map[string]any{"type": "object", "additionalProperties": child,
				"propertyNames": map[string]any{"pattern": patterns[0]}}
		}
		return node
	}
	var schemas []json.RawMessage
	for seed := 0; seed < 100; seed++ {
		raw, err := json.Marshal(level(2+seed%20, seed))
		if err != nil {
			panic(err)
		}
		schemas = append(schemas, raw)
	}
	return schemas
}

func BenchmarkCheckSchema(b *testing.B) {
	schemas := benchmarkSchemas()
	for _, schema := range schemas {
		if err := jsonschema.CheckSchema(schema); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, schema := range schemas {
			_ = jsonschema.CheckSchema(schema)
		}
	}
}

// TestCheckSchemaEdgeCases pins how CheckSchema reads each keyword: as encoding/json would decode it into
// the keyword's Go type, with the last of duplicate members winning.
func TestCheckSchemaEdgeCases(t *testing.T) {
	for _, tt := range []struct{ schema, want string }{
		{`{"type":"string"} `, ""},
		{"{\"type\":\"string\"}\n\t\r", ""},
		{`{"type":"string"} x`, "schema is invalid"},
		{`{} {}`, "schema is invalid"},
		{`{"type":"string"`, "schema is invalid"},
		{``, "schema is invalid"},
		{`"x"`, "schema is invalid"},
		{`{"properties":{"a":{"type":}}}`, "schema is invalid"},
		{`{"type":"x"}`, "schema at $ has an invalid type"},

		{`{"type":"string","type":"bogus"}`, "schema at $ has an invalid type"},
		{`{"type":"bogus","type":"string"}`, ""},
		{`{"type":null,"type":"string"}`, ""},
		{`{"type":"string","type":null}`, "schema at $ has an invalid type"},
		{`{"properties":{"a":{"pattern":"("},"a":{}}}`, ""},
		{`{"properties":{"a":{}},"properties":{"b":{"pattern":"("}}}`, "schema at $.b has an invalid pattern"},

		{`{"minItems":1.0}`, "schema at $ has an invalid minItems"},
		{`{"maxLength":1e1}`, "schema at $ has an invalid maxLength"},
		{`{"minProperties":-0}`, ""},
		{`{"maxItems":2147483647}`, ""},
		{`{"maxItems":99999999999999999999}`, "schema at $ has an invalid maxItems"},
		{`{"minItems":true}`, "schema at $ has an invalid minItems"},
		{`{"minItems":[]}`, "schema at $ has an invalid minItems"},
		{`{"minItems":"1","maxItems":0}`, "schema at $ has an invalid minItems"},
		{`{"minItems":2,"maxItems":1,"minLength":"x","maxLength":0}`,
			"schema at $ has minItems greater than maxItems\nschema at $ has an invalid minLength"},

		{`{"minimum":1E2,"maximum":-0.5e-3}`, ""},
		{`{"minimum":1e400}`, "schema at $ has an invalid minimum"},
		{`{"maximum":{}}`, "schema at $ has an invalid maximum"},
		{`{"maximum":[1]}`, "schema at $ has an invalid maximum"},

		{`{"enum":[1e-400,"a",null,true,{"a":[1.5]}]}`, ""},
		{`{"enum":[1e400]}`, "schema at $ has an invalid enum"},
		{`{"enum":[{"a":[2,-1e999]}]}`, "schema at $ has an invalid enum"},
		{`{"enum":{"a":1}}`, "schema at $ has an invalid enum"},

		{`{"required":[null,"a"]}`, ""},
		{`{"required":[]}`, ""},
		{`{"required":["a",1]}`, "schema at $ has invalid required fields"},
		{`{"required":[[]]}`, "schema at $ has invalid required fields"},

		{`{"pattern":"(?P<n>a)(?<m>b)\\p{Greek}\\d{1,1000}"}`, ""},
		{`{"pattern":"a{1001}"}`, "schema at $ has an invalid pattern"},
		{`{"pattern":"((a{100}){100}){100}"}`, "schema at $ has an invalid pattern"},
		{`{"pattern":"[z-a]"}`, "schema at $ has an invalid pattern"},
		{`{"pattern":"\\8"}`, "schema at $ has an invalid pattern"},
		{`{"pattern":1}`, "schema at $ has an invalid pattern"},

		{`{"uniqueItems":1}`, "schema at $ has an invalid uniqueItems"},
		{`{"uniqueItems":"true"}`, "schema at $ has an invalid uniqueItems"},
		{`{"format":["uuid"]}`, "schema at $ has an invalid format"},

		{`{"additionalProperties":true,"items":{},"propertyNames":{}}`, ""},
		{`{"additionalProperties":1}`, "schema at $ has invalid additionalProperties"},
		{`{"additionalProperties":[]}`, "schema at $ has invalid additionalProperties"},
		{`{"properties":{"a":null,"b":[],"c":true}}`,
			"schema at $.a is invalid\nschema at $.b is invalid\nschema at $.c is invalid"},
		{`{"anyOf":[null,{},"x"]}`, "schema at $ has an invalid anyOf\nschema at $ has an invalid anyOf"},
		{`{"anyOf":{}}`, "schema at $ has an invalid anyOf"},
		{`{"items":"x","propertyNames":[]}`, "schema at $ has invalid items\nschema at $ has invalid propertyNames"},

		{`{"zeta":1,"alpha":2,"type":null,"enum":null,"minimum":"1","format":"x","pattern":"(",` +
			`"minLength":5,"maxLength":4,"uniqueItems":0,"required":"a",` +
			`"anyOf":[{"$ref":"#"},{"items":{"pattern":"["}}],"propertyNames":{"type":1},` +
			`"items":{"properties":{"q":{"maxItems":-1}}},"additionalProperties":{"oneOf":[]},` +
			`"properties":{"b":{"type":"x","properties":{"c":{"minItems":"1"}}},"a":{"format":2}}}`,
			"schema at $ uses the unsupported keyword alpha\n" +
				"schema at $ uses the unsupported keyword zeta\n" +
				"schema at $ has an invalid enum\n" +
				"schema at $ has an invalid type\n" +
				"schema at $ has invalid required fields\n" +
				"schema at $ has minLength greater than maxLength\n" +
				"schema at $ has an invalid pattern\n" +
				"schema at $ has an invalid minimum\n" +
				"schema at $ has an invalid uniqueItems\n" +
				"schema at $ uses the unsupported format x\n" +
				"schema at $.a has an invalid format\n" +
				"schema at $.b has an invalid type\n" +
				"schema at $.b.c has an invalid minItems\n" +
				"schema at $.* uses the unsupported keyword oneOf\n" +
				"schema at $[].q has an invalid maxItems\n" +
				"schema at ${} has an invalid type\n" +
				"schema at $ uses the unsupported keyword $ref\n" +
				"schema at $[] has an invalid pattern"},
	} {
		err := jsonschema.CheckSchema(json.RawMessage(tt.schema))
		if (tt.want == "") != (err == nil) || err != nil && err.Error() != tt.want {
			t.Errorf("%s: CheckSchema() = %v, want %q", tt.schema, err, tt.want)
		}
	}
}

// nestedSchema nests depth schemas through properties, items, anyOf and additionalProperties in turn.
func nestedSchema(depth int) json.RawMessage {
	schema := `{"type":"string","pattern":"^[a-z]{1,8}$"}`
	for i := 0; i < depth; i++ {
		switch i % 4 {
		case 0:
			schema = `{"type":"object","properties":{"a":` + schema + `,"b":{"type":"integer"}},"required":["a"]}`
		case 1:
			schema = `{"type":"array","minItems":1,"items":` + schema + `}`
		case 2:
			schema = `{"anyOf":[{"type":"null"},` + schema + `]}`
		case 3:
			schema = `{"type":"object","additionalProperties":` + schema + `}`
		}
	}
	return json.RawMessage(schema)
}

// Allocations grow in proportion to the depth of a schema: each further level costs the same. They are
// counted, not timed, so the test holds on any machine.
func TestCheckSchemaAllocationsGrowLinearly(t *testing.T) {
	allocs := func(depth int) float64 {
		schema := nestedSchema(depth)
		if err := jsonschema.CheckSchema(schema); err != nil {
			t.Fatal(err)
		}
		return testing.AllocsPerRun(5, func() { _ = jsonschema.CheckSchema(schema) })
	}
	small, medium, large := allocs(100), allocs(200), allocs(400)
	// The slack covers buffers that grow by doubling, which add a few allocations per doubling of depth.
	if lower, upper := medium-small, large-medium; upper > 2*lower+16 {
		t.Errorf("levels 200 to 400 cost %v allocations, more than twice the %v of levels 100 to 200", upper, lower)
	}
}
