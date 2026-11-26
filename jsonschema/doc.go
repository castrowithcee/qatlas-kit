// Package jsonschema checks JSON values against a subset of JSON Schema and names the first violation by
// its path, such as "$.items[2].name is required".
//
// CheckSchema checks a schema without a value and reports every error at once. Validate and ValidateJSON
// apply it before they check the value.
//
// A Checker applies the same check and remembers across calls what it found valid. It suits an
// application that checks many schemas at start, some of them repeated; see Checker for its memory and
// concurrency.
//
// # Always enforced
//
// Errors name paths and the schema's own words, never a rejected value; a path does name the member keys of
// the value it leads through.
//
// The checked keywords are type (one name), enum, properties, required, additionalProperties (a boolean
// or a schema), minProperties, maxProperties, items (one schema), minItems, maxItems, uniqueItems,
// minLength, maxLength (in characters), pattern (Go regular expression syntax), format (uuid or date-time
// as RFC 3339, on strings only), propertyNames (a schema every member name is checked against as a string,
// in both modes; its errors name the member in braces after the object's path, such as $.labels{name}),
// minimum, maximum, exclusiveMinimum, exclusiveMaximum (the number form of Draft 2020-12 only, so the
// boolean form of Draft 4 is an invalid schema), const and anyOf. Every keyword applies on its own, so
// minimum and exclusiveMinimum may stand together and both are checked. const takes any JSON value and
// compares it as enum does; a literal null const is a valid schema that requires null, while null is
// malformed for every other checked keyword.
//
// x-form names the form a pattern checks in words for the error message. The annotations title,
// description, default, examples, deprecated, readOnly, writeOnly, $comment, $schema, $id and every x-
// keyword are accepted and not checked. Any other keyword, anywhere in the schema, makes the schema
// invalid, and so does any other format name, so a constraint is never skipped in silence. A malformed
// checked keyword is an invalid schema as well, wherever it stands.
//
// ValidateJSON requires exactly one JSON value and names a field the schema does not allow before a missing
// required one.
//
// # Options
//
//   - Validate's mode: Strict holds a value to every checked keyword and suits input; Lenient suits data a
//     remote side produced and does not hold it to minProperties, maxProperties, uniqueItems, anyOf or a
//     schema under additionalProperties; see Mode. Default: Strict, the zero value and the mode of
//     ValidateJSON, also for any value other than Lenient.
//   - Checker: remembers valid schemas and patterns across calls. Default: the package functions remember
//     nothing.
package jsonschema
