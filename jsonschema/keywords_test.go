package jsonschema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/jsonschema"
)

// validateBoth runs Validate in Strict and Lenient mode, since both check these keywords alike.
func validateBoth(t *testing.T, schema, value string) (strict, lenient error) {
	t.Helper()
	strict = jsonschema.Validate(json.RawMessage(schema), decode(t, value), jsonschema.Strict)
	lenient = jsonschema.Validate(json.RawMessage(schema), decode(t, value), jsonschema.Lenient)
	return strict, lenient
}

func TestConst(t *testing.T) {
	const notAllowed = "$.v is not the allowed value"
	for _, tt := range []struct{ name, schema, value, want string }{
		{"string equal", `{"const":"latest"}`, `"latest"`, ""},
		{"string other", `{"const":"latest"}`, `"stable"`, "$ is not the allowed value"},
		{"string is no number", `{"const":"1"}`, `1`, "$ is not the allowed value"},
		{"boolean equal", `{"type":"boolean","const":true}`, `true`, ""},
		{"boolean other", `{"type":"boolean","const":true}`, `false`, "$ is not the allowed value"},
		{"null equal", `{"const":null}`, `null`, ""},
		{"null refuses a value", `{"const":null}`, `0`, "$ is not the allowed value"},
		{"null const refuses an object", `{"const":null}`, `{}`, "$ is not the allowed value"},
		{"object equal", `{"const":{"a":[1,{"b":null}],"c":"x"}}`, `{"c":"x","a":[1,{"b":null}]}`, ""},
		{"object other", `{"const":{"a":[1,{"b":null}]}}`, `{"a":[2,{"b":null}]}`, "$ is not the allowed value"},
		{"object extra member", `{"const":{"a":1}}`, `{"a":1,"b":2}`, "$ is not the allowed value"},
		{"array equal", `{"const":[1,"x",null]}`, `[1,"x",null]`, ""},
		{"array other order", `{"const":[1,"x",null]}`, `["x",1,null]`, "$ is not the allowed value"},
		{"number equal", `{"const":2.5}`, `2.5`, ""},
		{"number other", `{"const":2.5}`, `2.6`, "$ is not the allowed value"},
		{"const beside enum, both hold", `{"const":"a","enum":["a","b"]}`, `"a"`, ""},
		{"const beside enum, enum fails", `{"const":"c","enum":["a","b"]}`, `"c"`, "$ is not an allowed value"},
		{"const beside enum, const fails", `{"const":"a","enum":["a","b"]}`, `"b"`, "$ is not the allowed value"},
	} {
		strict, lenient := validateBoth(t, tt.schema, tt.value)
		for mode, err := range map[string]error{"strict": strict, "lenient": lenient} {
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("%s (%s): Validate() = %v, want no error", tt.name, mode, err)
			case tt.want != "" && (err == nil || err.Error() != tt.want):
				t.Errorf("%s (%s): Validate() = %v, want %q", tt.name, mode, err, tt.want)
			}
		}
	}

	// A member's path leads to the message, and a const in an anyOf branch works in Strict mode.
	schema := `{"type":"object","properties":{"v":{"const":"latest"}}}`
	check(t, schema, []testCase{{"member", `{"v":"old"}`, notAllowed}})
	check(t, `{"anyOf":[{"const":"latest"},{"type":"integer","minimum":1}]}`, []testCase{
		{"const branch", `"latest"`, ""},
		{"integer branch", `3`, ""},
		// anyOf reports the error of its last branch.
		{"no branch", `"old"`, "$ must be integer"},
	})
}

func TestExclusiveBounds(t *testing.T) {
	schema := `{"type":"number","exclusiveMinimum":0,"exclusiveMaximum":1}`
	for _, tt := range []struct{ value, want string }{
		{`0`, "$ must be greater than 0"},
		{`-3`, "$ must be greater than 0"},
		{`1`, "$ must be less than 1"},
		{`7`, "$ must be less than 1"},
		{`0.5`, ""},
		{`0.0000001`, ""},
		{`0.9999999`, ""},
	} {
		strict, lenient := validateBoth(t, schema, tt.value)
		for mode, err := range map[string]error{"strict": strict, "lenient": lenient} {
			if tt.want == "" && err != nil || tt.want != "" && (err == nil || err.Error() != tt.want) {
				t.Errorf("%s (%s): Validate() = %v, want %q", tt.value, mode, err, tt.want)
			}
		}
	}
	// A bound does not apply to a value that is no number, as with minimum.
	check(t, `{"exclusiveMinimum":0}`, []testCase{{"string", `"x"`, ""}})
}

func TestMinimumAndExclusiveMinimumBothApply(t *testing.T) {
	check(t, `{"minimum":2,"exclusiveMinimum":0}`, []testCase{
		{"fails minimum only", `1`, "$ must be at least 2"},
		{"passes both", `2`, ""},
	})
	check(t, `{"minimum":0,"exclusiveMinimum":2}`, []testCase{
		{"fails exclusiveMinimum only", `2`, "$ must be greater than 2"},
		{"passes both", `3`, ""},
	})
	check(t, `{"maximum":5,"exclusiveMaximum":3}`, []testCase{
		{"fails exclusiveMaximum only", `3`, "$ must be less than 3"},
		{"passes both", `2`, ""},
	})
	check(t, `{"maximum":3,"exclusiveMaximum":5}`, []testCase{
		{"fails maximum only", `4`, "$ must be at most 3"},
	})
	// Contradicting bounds are no invalid schema, as with minimum above maximum.
	if err := jsonschema.CheckSchema(json.RawMessage(`{"exclusiveMinimum":5,"exclusiveMaximum":1}`)); err != nil {
		t.Errorf("CheckSchema() = %v", err)
	}
}

func TestInvalidConstAndExclusiveBounds(t *testing.T) {
	for schema, want := range map[string]string{
		`{"const":1e400}`:                                          "schema at $ has an invalid const",
		`{"const":{"a":[-1e999]}}`:                                 "schema at $ has an invalid const",
		`{"const":1,"const":1e400}`:                                "schema at $ has an invalid const",
		`{"exclusiveMinimum":true}`:                                "schema at $ has an invalid exclusiveMinimum",
		`{"exclusiveMinimum":"0"}`:                                 "schema at $ has an invalid exclusiveMinimum",
		`{"exclusiveMinimum":null}`:                                "schema at $ has an invalid exclusiveMinimum",
		`{"exclusiveMinimum":1e400}`:                               "schema at $ has an invalid exclusiveMinimum",
		`{"exclusiveMaximum":false}`:                               "schema at $ has an invalid exclusiveMaximum",
		`{"exclusiveMaximum":"1"}`:                                 "schema at $ has an invalid exclusiveMaximum",
		`{"exclusiveMaximum":null}`:                                "schema at $ has an invalid exclusiveMaximum",
		`{"exclusiveMaximum":[1]}`:                                 "schema at $ has an invalid exclusiveMaximum",
		`{"properties":{"a":{"const":1e400}}}`:                     "schema at $.a has an invalid const",
		`{"items":{"exclusiveMinimum":true}}`:                      "schema at $[] has an invalid exclusiveMinimum",
		`{"anyOf":[{"type":"null"},{"const":[1e400]}]}`:            "schema at $ has an invalid const",
		`{"additionalProperties":{"exclusiveMaximum":"x"}}`:        "schema at $.* has an invalid exclusiveMaximum",
		`{"propertyNames":{"exclusiveMinimum":false}}`:             "schema at ${} has an invalid exclusiveMinimum",
		`{"properties":{"a":{"items":{"const":{"b":1e400}}}}}`:     "schema at $.a[] has an invalid const",
		`{"properties":{"a":{"anyOf":[{"exclusiveMaximum":{}}]}}}`: "schema at $.a has an invalid exclusiveMaximum",
	} {
		err := jsonschema.CheckSchema(json.RawMessage(schema))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: CheckSchema() = %v, want %q", schema, err, want)
		}
		for _, mode := range []jsonschema.Mode{jsonschema.Strict, jsonschema.Lenient} {
			if err := jsonschema.Validate(json.RawMessage(schema), map[string]any{}, mode); err == nil {
				t.Errorf("%s (mode %d): Validate accepted an invalid schema", schema, mode)
			}
		}
	}
	for _, schema := range []string{
		`{"const":null}`, `{"const":1e-400}`, `{"exclusiveMinimum":-1.5e2,"exclusiveMaximum":0}`,
		`{"properties":{"a":{"items":{"const":{"b":[null]}}}}}`,
	} {
		if err := jsonschema.CheckSchema(json.RawMessage(schema)); err != nil {
			t.Errorf("%s: CheckSchema() = %v", schema, err)
		}
	}
}

// An error names paths and the schema's words, never the rejected value or the const value that value
// could equal.
func TestConstAndExclusiveErrorsNameNoValue(t *testing.T) {
	for _, tt := range []struct{ schema, value string }{
		{`{"const":"sentinel-const-word"}`, `"sentinel-input-word"`},
		{`{"const":{"k":"sentinel-const-word"}}`, `{"k":"sentinel-input-word"}`},
		{`{"const":["sentinel-const-word"]}`, `["sentinel-input-word"]`},
		{`{"const":424242}`, `424243`},
		{`{"exclusiveMinimum":0}`, `-987654`},
		{`{"exclusiveMaximum":0}`, `987654`},
	} {
		strict, lenient := validateBoth(t, tt.schema, tt.value)
		for _, err := range []error{strict, lenient} {
			if err == nil {
				t.Errorf("%s: value %s was accepted", tt.schema, tt.value)
				continue
			}
			for _, secret := range []string{"sentinel", "424242", "424243", "987654"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("%s: error %q names a value", tt.schema, err)
				}
			}
		}
	}
}
