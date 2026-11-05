// Package jsonschema checks JSON values against a subset of JSON Schema and names the first violation by
// its path, such as "$.items[2].name is required". Errors name paths and the schema's own words, never a
// rejected value; a path does name the member keys of the value it leads through.
//
// The checked keywords are type (one name), enum, properties, required, additionalProperties (a boolean
// or a schema), minProperties, maxProperties, items (one schema), minItems, maxItems, uniqueItems,
// minLength, maxLength (in characters), pattern (Go regular expression syntax), format (uuid or date-time
// as RFC 3339, on strings only), propertyNames (a schema every member name is checked against as a string,
// in both modes; its errors name the member in braces after the object's path, such as $.labels{name}),
// minimum, maximum and anyOf.
// x-form names the form a pattern checks in words for the error message. The annotations title,
// description, default, examples, deprecated, readOnly, writeOnly, $comment, $schema, $id and every x-
// keyword are accepted and not checked. Any other keyword, anywhere in the schema, makes the schema
// invalid, and so does any other format name, so a constraint is never skipped in silence. A malformed
// checked keyword is an invalid schema as well, wherever it stands.
//
// CheckSchema checks a schema without a value and reports every such error at once. Validate and
// ValidateJSON apply it before they check the value.
//
// Strict mode, the default, holds a value to every checked keyword and suits input. Lenient mode suits
// data a remote side produced; see Mode.
package jsonschema
