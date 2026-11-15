package jsonschema

import (
	"encoding/json"
	"regexp/syntax"
	"sync"
)

// Checker checks schemas as CheckSchema does and remembers the schema texts and patterns it found valid, so
// a schema text or pattern seen before is not checked again. A schema text is recognized only when it is
// equal byte for byte. Errors are not remembered, so a malformed schema earns the same error from every
// call as from CheckSchema. The memory grows with the number of distinct valid schemas and patterns
// checked.
//
// The zero value is ready to use. A Checker is safe for concurrent use and must not be copied after first
// use.
type Checker struct {
	mu       sync.RWMutex
	schemas  map[string]struct{}
	patterns map[string]struct{}
}

// CheckSchema checks a schema as the package function CheckSchema does.
func (c *Checker) CheckSchema(schema json.RawMessage) error {
	if c.known(&c.schemas, schema) {
		return nil
	}
	if json.Valid(schema) && knownValid(schema, c) {
		c.remember(&c.schemas, string(schema))
		return nil
	}
	err := checkSchemaTree(schema)
	if err == nil {
		c.remember(&c.schemas, string(schema))
	}
	return err
}

// validPattern reports whether syntax.Parse accepts pattern, as checkNode requires. A nil Checker parses
// every time.
func (c *Checker) validPattern(pattern []byte) bool {
	if c != nil && c.known(&c.patterns, pattern) {
		return true
	}
	s := string(pattern)
	if _, err := syntax.Parse(s, syntax.Perl); err != nil {
		return false
	}
	if c != nil {
		c.remember(&c.patterns, s)
	}
	return true
}

func (c *Checker) known(set *map[string]struct{}, text []byte) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := (*set)[string(text)]
	return ok
}

func (c *Checker) remember(set *map[string]struct{}, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if *set == nil {
		*set = map[string]struct{}{}
	}
	(*set)[text] = struct{}{}
}
