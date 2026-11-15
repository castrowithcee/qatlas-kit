package jsonschema

import (
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
)

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// A checker remembers only what it found valid: a malformed schema earns the error of CheckSchema on every
// call, also after a valid schema with the same pattern or a text that differs in one byte.
func TestCheckerRepeatsErrors(t *testing.T) {
	var checker Checker
	for _, schema := range []string{
		`{"pattern":"("}`,
		`{"pattern":"^a$"}`,
		`{"pattern":"^a$","type":"x"}`,
		`{"properties":{"a":{"pattern":"^a$"},"b":{"pattern":"("}}}`,
		`{"type":"string"}`,
		`{"type":"string"} x`,
		`{"type":"string" }`,
		`{"type":"strinG"}`,
		`{"minLength":2,"maxLength":1}`,
		`{"minLength":1,"maxLength":1}`,
		`{"minLength":1,"maxLength":1,"minLength":2}`,
	} {
		want := errorText(CheckSchema(json.RawMessage(schema)))
		for call := 1; call <= 2; call++ {
			if got := errorText(checker.CheckSchema(json.RawMessage(schema))); got != want {
				t.Errorf("%s call %d: Checker.CheckSchema() = %q, want %q", schema, call, got, want)
			}
		}
	}
}

func TestCheckerConcurrentCalls(t *testing.T) {
	schemas := corpus()[:400]
	for i := range 100 {
		kind := []string{"string", "bogus"}[i%2]
		schemas = append(schemas, json.RawMessage(fmt.Sprintf(`{"pattern":"^[a-z]{%d}$","type":"%s"}`, i%10, kind)))
	}
	want := make([]string, len(schemas))
	for i, schema := range schemas {
		want[i] = errorText(CheckSchema(schema))
	}
	var checker Checker
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for i := range schemas {
				// Each worker starts elsewhere, so they meet on schemas and patterns.
				i = (i + worker*len(schemas)/8) % len(schemas)
				if got := errorText(checker.CheckSchema(schemas[i])); got != want[i] {
					t.Errorf("%s: Checker.CheckSchema() = %q, want %q", schemas[i], got, want[i])
				}
			}
		})
	}
	wg.Wait()
}

// TestCheckerAllocationsPerSchema counts the allocations of a new checker over the corpus, so the bound
// holds on any machine.
func TestCheckerAllocationsPerSchema(t *testing.T) {
	schemas := corpus()
	allocs := testing.AllocsPerRun(2, func() {
		var checker Checker
		for _, schema := range schemas {
			_ = checker.CheckSchema(schema)
		}
	})
	if perSchema := allocs / float64(len(schemas)); perSchema > 20 {
		t.Errorf("a new Checker costs %.1f allocations per corpus schema, want at most 20", perSchema)
	}

	// Once a checker knows the patterns, every schema stays within the bound, not only their mean. A
	// trailing space gives each schema a new text with the same patterns.
	var warm Checker
	for _, schema := range schemas {
		_ = warm.CheckSchema(append(slices.Clone(schema), ' '))
	}
	var stats runtime.MemStats
	for _, schema := range schemas {
		runtime.ReadMemStats(&stats)
		before := stats.Mallocs
		_ = warm.CheckSchema(schema)
		runtime.ReadMemStats(&stats)
		if n := stats.Mallocs - before; n > 20 {
			t.Fatalf("a Checker that knows the patterns costs %d allocations for %s, want at most 20", n, schema)
		}
	}
}
