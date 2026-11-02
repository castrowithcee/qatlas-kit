// Package jsonschema checks JSON values against a subset of JSON Schema and names the first violation by
// its path, such as "$.items[2].name is required". Errors name paths and the schema's own words, never a
// rejected value; a path does name the member keys of the value it leads through.
//
// The checked keywords are type (one name), enum, properties, required, additionalProperties (a boolean
// or a schema), minProperties, maxProperties, items (one schema), minItems, maxItems, uniqueItems,
// minLength, maxLength (in characters), pattern (Go regular expression syntax), minimum, maximum and anyOf.
// x-form names the form a pattern checks in words for the error message. The annotations title,
// description, default, examples, deprecated, readOnly, writeOnly, $comment, $schema, $id and every x-
// keyword are accepted and not checked. Any other keyword, anywhere in the schema, makes the schema
// invalid, so a constraint is never skipped in silence. A malformed checked keyword that applies to the
// value is reported as an invalid schema as well.
//
// Strict mode, the default, holds a value to every checked keyword and suits input. Lenient mode suits
// data a remote side produced; see Mode.
package jsonschema
