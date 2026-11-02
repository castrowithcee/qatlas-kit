// Package output renders ordered results in the table, JSON, compact and TOON formats and defines the
// error codes that applications share.
//
// Encode is the single path from a result to its bytes. Before anything is written it drops null members,
// empty lists and empty objects at every depth (see DropEmpty) and refuses a value that the chosen text
// format cannot represent with an UnsupportedValueError, a usage problem. Project restricts a result to
// the selected fields and reports an unknown or repeated field with a ProjectionError that names the
// available fields. The table format writes column names in upper case and makes control characters
// visible; JSON keeps the declared field order and writes a missing collection cell as null.
//
// AllCodes returns the shared error codes followed by the codes an application adds for its own domain.
package output
