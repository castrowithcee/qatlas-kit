package jsonschema_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/jsonschema"
)

type testCase struct{ name, value, want string }

func check(t *testing.T, schema string, tests []testCase) {
	t.Helper()
	for _, tt := range tests {
		err := jsonschema.ValidateJSON(json.RawMessage(schema), json.RawMessage(tt.value))
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("%s: ValidateJSON() = %v, want no error", tt.name, err)
		case tt.want != "" && (err == nil || err.Error() != tt.want):
			t.Errorf("%s: ValidateJSON() = %v, want %q", tt.name, err, tt.want)
		}
	}
}

func decode(t *testing.T, raw string) any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestInputMustBeOneJSONValue(t *testing.T) {
	check(t, `{"type":"object"}`, []testCase{
		{"one object", `{}`, ""},
		{"not JSON", `{`, "input must be valid JSON"},
		{"empty", ``, "input must be valid JSON"},
		{"two values", `{} {}`, "input must contain exactly one JSON value"},
		{"trailing garbage", `{}x`, "input must contain exactly one JSON value"},
	})
}

func TestTypesEnumsAndMembers(t *testing.T) {
	check(t, `{"type":"object","properties":{"id":{"type":"integer"},"kind":{"enum":["a","b",1]},`+
		`"flag":{"type":"boolean"},"none":{"type":"null"},"n":{"type":"number"}},`+
		`"required":["id"],"additionalProperties":false}`, []testCase{
		{"valid", `{"id":7,"kind":"a","flag":true,"none":null,"n":1.5}`, ""},
		{"wrong root type", `[]`, "$ must be object"},
		{"fraction is no integer", `{"id":1.5}`, "$.id must be integer"},
		{"string is no integer", `{"id":"7"}`, "$.id must be integer"},
		{"string is no number", `{"id":7,"n":"1"}`, "$.n must be number"},
		{"string is no boolean", `{"id":7,"flag":"true"}`, "$.flag must be boolean"},
		{"value is not null", `{"id":7,"none":0}`, "$.none must be null"},
		{"value outside the enum", `{"id":7,"kind":"c"}`, "$.kind is not an allowed value"},
		{"enum number is no string", `{"id":7,"kind":"1"}`, "$.kind is not an allowed value"},
		{"missing required", `{}`, "$.id is required"},
		// An unknown member is often the misspelling of a missing required one, so it is named first.
		{"unknown before missing", `{"ID":7}`, "$.ID is not allowed"},
	})
}

func TestErrorsAreDeterministic(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"string"}},"additionalProperties":false}`)
	arguments := json.RawMessage(`{"b":7,"a":"wrong"}`)
	const want = "$.a must be integer"
	for i := 0; i < 100; i++ {
		if err := jsonschema.ValidateJSON(schema, arguments); err == nil || err.Error() != want {
			t.Fatalf("run %d error = %v, want %q", i+1, err, want)
		}
	}
}

func TestBoundsAndPatterns(t *testing.T) {
	check(t, `{"type":"object","properties":{`+
		`"size":{"type":"integer","minimum":1,"maximum":100},`+
		`"date":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},`+
		`"day":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$","x-form":"YYYY-MM-DD"},`+
		`"code":{"type":"string","minLength":2,"maxLength":3},`+
		`"tags":{"type":"array","minItems":1,"maxItems":3,"items":{"type":"string"}},`+
		`"pair":{"type":"array","minItems":2,"items":{"type":"integer"}},`+
		`"a":{"type":"array","items":{"type":"object","properties":{"b":{"type":"array","maxItems":1}}}},`+
		`"free":{"type":"array"}},`+
		`"additionalProperties":false}`, []testCase{
		{"inside the bounds", `{"size":100,"date":"2026-05-14"}`, ""},
		{"at the lower bound", `{"size":1}`, ""},
		{"above the maximum", `{"size":101}`, "$.size must be at most 100"},
		{"below the minimum", `{"size":0}`, "$.size must be at least 1"},
		{"pattern mismatch", `{"date":"14.05.2026"}`, "$.date does not have the required form"},
		{"empty string", `{"date":""}`, "$.date does not have the required form"},
		{"pattern mismatch with a form", `{"day":"14.05.2026"}`, "$.day does not have the required form YYYY-MM-DD"},
		{"characters, not bytes", `{"code":"äöü"}`, ""},
		{"too short", `{"code":"x"}`, "$.code is shorter than 2 characters"},
		{"too long", `{"code":"wxyz"}`, "$.code is longer than 3 characters"},
		{"at the lower item bound", `{"tags":["x"],"pair":[1,2]}`, ""},
		{"at the upper item bound", `{"tags":["x","y","z"]}`, ""},
		{"wrong item type", `{"tags":["x",1]}`, "$.tags[1] must be string"},
		{"empty array with one required item", `{"tags":[]}`, "$.tags must not be empty"},
		{"above the item maximum", `{"tags":["w","x","y","z"]}`, "$.tags must have at most 3 items"},
		{"below an item minimum above one", `{"pair":[1]}`, "$.pair must have at least 2 items"},
		{"nested item bound", `{"a":[{"b":[1,2]}]}`, "$.a[0].b must have at most 1 items"},
		{"array without bounds", `{"free":[],"a":[{"b":[]}]}`, ""},
	})
}

func TestObjectBoundsAndUniqueItems(t *testing.T) {
	schema := `{"type":"object","properties":{` +
		`"fields":{"type":"object","minProperties":1,"maxProperties":2},` +
		`"pairs":{"type":"object","minProperties":2},` +
		`"ids":{"type":"array","uniqueItems":true}},"additionalProperties":false}`
	check(t, schema, []testCase{
		{"valid", `{"fields":{"a":1},"pairs":{"a":1,"b":2},"ids":[1,"1",2,-1,{"a":1},{"a":2}]}`, ""},
		{"empty object", `{"fields":{}}`, "$.fields must not be empty"},
		{"too few members", `{"pairs":{"a":1}}`, "$.pairs must have at least 2 members"},
		{"too many members", `{"fields":{"a":1,"b":2,"c":3}}`, "$.fields must have at most 2 members"},
		{"repeated scalar", `{"ids":[1,2,3,1]}`, "$.ids[3] repeats $.ids[0]"},
		{"equal numbers", `{"ids":[1,1.0]}`, "$.ids[1] repeats $.ids[0]"},
		{"equal exponent number", `{"ids":["x",10,1e1]}`, "$.ids[2] repeats $.ids[1]"},
		{"equal fraction and exponent", `{"ids":[0.1e1,10e-1]}`, "$.ids[1] repeats $.ids[0]"},
		{"signed exponent", `{"ids":[100,1E+2]}`, "$.ids[1] repeats $.ids[0]"},
		{"negative zero", `{"ids":[0,-0.0]}`, "$.ids[1] repeats $.ids[0]"},
		{"nested equal numbers", `{"ids":[{"n":[1]},{"n":[1.0]}]}`, "$.ids[1] repeats $.ids[0]"},
		{"repeated object", `{"ids":[{"a":[1],"b":2},"x",{"b":2,"a":[1]}]}`, "$.ids[2] repeats $.ids[0]"},
	})

	// A lenient check does not refuse data for member counts or repeated items.
	value := decode(t, `{"fields":{},"pairs":{"a":1},"ids":[1,1]}`)
	if err := jsonschema.Validate(json.RawMessage(schema), value, jsonschema.Lenient); err != nil {
		t.Errorf("Lenient: Validate() = %v", err)
	}
	for _, mode := range []jsonschema.Mode{jsonschema.Strict, jsonschema.Mode(99)} {
		if err := jsonschema.Validate(json.RawMessage(schema), value, mode); err == nil {
			t.Errorf("mode %d: Validate() accepted an empty object", mode)
		}
	}
}

// Comparing numbers for uniqueness must cost in proportion to their spelling, not their value.
func TestUniqueItemsWithHugeExponents(t *testing.T) {
	var b bytes.Buffer
	b.WriteString(`{"ids":[`)
	for i := 0; i < 5000; i++ {
		b.WriteString(`1e999999,`)
	}
	b.WriteString(`2]}`)
	err := jsonschema.ValidateJSON(json.RawMessage(`{"properties":{"ids":{"uniqueItems":true}}}`), b.Bytes())
	if err == nil || err.Error() != "$.ids[1] repeats $.ids[0]" {
		t.Fatalf("ValidateJSON() = %v", err)
	}
	distinct := `{"ids":[1e999999,1e999998,-1e999999,1e-999999]}`
	if err := jsonschema.ValidateJSON(json.RawMessage(`{"properties":{"ids":{"uniqueItems":true}}}`),
		json.RawMessage(distinct)); err != nil {
		t.Fatalf("distinct numbers: ValidateJSON() = %v", err)
	}
}

func TestUnionsAndAdditionalPropertySchemas(t *testing.T) {
	scalar := `{"anyOf":[{"type":"string","maxLength":4},{"type":"number"},{"type":"boolean"}]}`
	schema := `{"type":"object","properties":{` +
		`"rows":{"type":"array","items":{"type":"object","properties":{"email":` + scalar + `}}},` +
		`"pair":{"anyOf":[{"type":"string"},{"type":"array","items":{"type":"string"}}]},` +
		`"either":{"anyOf":[{"type":"string"},{"type":"null"},{"type":"array"}]},` +
		`"fields":{"type":"object","additionalProperties":` + scalar + `},` +
		`"operations":{"type":"array"},"description":{"type":"string"},"note":{"type":"string"}},` +
		`"anyOf":[{"required":["operations"]},{"required":["description"]},{"required":["note"]}],` +
		`"additionalProperties":false}`
	check(t, schema, []testCase{
		{"valid", `{"description":"d","rows":[{"email":"abc"}],"fields":{"a":true},"pair":["x"],"either":null}`, ""},
		{"no type fits", `{"note":"n","rows":[{"email":null}]}`, "$.rows[0].email must be string, number, or boolean"},
		{"two types", `{"note":"n","pair":7}`, "$.pair must be string or array"},
		{"null among types", `{"note":"n","either":{}}`, "$.either must be string, null, or array"},
		{"one type fits", `{"note":"n","rows":[{"email":"abcde"}]}`, "$.rows[0].email is longer than 4 characters"},
		{"required set", `{}`, "$.operations, $.description, or $.note is required"},
		{"additional schema", `{"note":"n","fields":{"a":1,"b":{}}}`, "$.fields.b must be string, number, or boolean"},
		{"additional schema branch", `{"note":"n","fields":{"b":"abcde"}}`, "$.fields.b is longer than 4 characters"},
	})

	// A lenient check holds data neither to unions nor to a schema for the other members.
	value := decode(t, `{"rows":[{"email":null}],"fields":{"b":{}}}`)
	if err := jsonschema.Validate(json.RawMessage(schema), value, jsonschema.Lenient); err != nil {
		t.Errorf("Lenient: Validate() = %v", err)
	}
}

func TestLenientKeepsTheShapeChecks(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer","maximum":9},` +
		`"name":{"type":"string","maxLength":2},"tags":{"type":"array","maxItems":1}},` +
		`"required":["id"],"additionalProperties":false}`)
	for raw, want := range map[string]string{
		`{"id":"7"}`:            "$.id must be integer",
		`{"id":10}`:             "$.id must be at most 9",
		`{"id":1,"name":"abc"}`: "$.name is longer than 2 characters",
		`{"id":1,"tags":[1,2]}`: "$.tags must have at most 1 items",
		`{}`:                    "$.id is required",
		`{"id":1,"other":true}`: "$.other is not allowed",
	} {
		err := jsonschema.Validate(schema, decode(t, raw), jsonschema.Lenient)
		if err == nil || err.Error() != want {
			t.Errorf("%s: Validate() = %v, want %q", raw, err, want)
		}
	}
	// A number that was not decoded with UseNumber matches no numeric type.
	if err := jsonschema.Validate(schema, map[string]any{"id": float64(1)}, jsonschema.Strict); err == nil {
		t.Error("a float64 was accepted as integer")
	}
}

// A malformed checked keyword is an invalid schema, never a check that is silently skipped.
func TestInvalidSchemas(t *testing.T) {
	for schema, want := range map[string]string{
		`[]`:                                     "schema is invalid",
		`{"type":["string","null"]}`:             "schema at $ has an invalid type",
		`{"enum":"a"}`:                           "schema at $ has an invalid enum",
		`{"properties":[]}`:                      "schema at $ has invalid properties",
		`{"properties":{"a":1}}`:                 "schema at $.a is invalid",
		`{"required":"a"}`:                       "schema at $ has invalid required fields",
		`{"additionalProperties":"no"}`:          "schema at $ has invalid additionalProperties",
		`{"minProperties":"1"}`:                  "schema at $ has an invalid minProperties",
		`{"maxProperties":-1}`:                   "schema at $ has an invalid maxProperties",
		`{"properties":{"d":{"minItems":1.5}}}`:  "schema at $.d has an invalid minItems",
		`{"properties":{"b":{"maxLength":"2"}}}`: "schema at $.b has an invalid maxLength",
		`{"properties":{"b":{"pattern":"("}}}`:   "schema at $.b has an invalid pattern",
		`{"properties":{"c":{"minimum":"1"}}}`:   "schema at $.c has an invalid minimum",
		`{"properties":{"c":{"maximum":true}}}`:  "schema at $.c has an invalid maximum",
		`{"properties":{"d":{"items":[]}}}`:      "schema at $.d has invalid items",
		`{"anyOf":[]}`:                           "schema at $ has an invalid anyOf",
	} {
		err := jsonschema.ValidateJSON(json.RawMessage(schema), json.RawMessage(`{"a":{},"b":"x","c":1,"d":[1]}`))
		if err == nil || err.Error() != want {
			t.Errorf("%s: ValidateJSON() = %v, want %q", schema, err, want)
		}
	}
}

// A keyword the package does not check would let a value through that the schema author meant to refuse,
// so it makes the schema invalid wherever it stands, even below a member the value does not have.
func TestUnsupportedKeywordsAreRefused(t *testing.T) {
	for schema, want := range map[string]string{
		`{"oneOf":[{"required":["a"]},{"required":["b"]}]}`:            "schema at $ uses the unsupported keyword oneOf",
		`{"properties":{"absent":{"type":"string","format":"email"}}}`: "schema at $.absent uses the unsupported keyword format",
		`{"properties":{"d":{"items":{"$ref":"#/x"}}}}`:                "schema at $.d[] uses the unsupported keyword $ref",
		`{"additionalProperties":{"not":{"type":"null"}}}`:             "schema at $.* uses the unsupported keyword not",
		`{"anyOf":[{"type":"object"},{"const":1}]}`:                    "schema at $ uses the unsupported keyword const",
		`{"type":"object","exclusiveMinimum":1}`:                       "schema at $ uses the unsupported keyword exclusiveMinimum",
	} {
		for _, mode := range []jsonschema.Mode{jsonschema.Strict, jsonschema.Lenient} {
			err := jsonschema.Validate(json.RawMessage(schema), map[string]any{}, mode)
			if err == nil || err.Error() != want {
				t.Errorf("%s (mode %d): Validate() = %v, want %q", schema, mode, err, want)
			}
		}
	}

	annotated := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:example","$comment":"c",` +
		`"title":"T","description":"D","type":"object","properties":{"a":{"type":"string","default":"x",` +
		`"examples":["x"],"deprecated":true,"readOnly":true,"writeOnly":false,"x-form":"word","x-note":"n"}}}`
	if err := jsonschema.ValidateJSON(json.RawMessage(annotated), json.RawMessage(`{"a":"x"}`)); err != nil {
		t.Errorf("annotations: ValidateJSON() = %v", err)
	}
}
