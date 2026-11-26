// Package output renders ordered results in the table, JSON, compact and TOON formats and defines the
// error codes that applications share.
//
// Encode is the single path from a result to its bytes. Project restricts a result to the selected fields.
// JSON keeps the declared field order and writes a missing collection cell as null. AllCodes returns the
// shared error codes followed by the codes an application adds for its own domain.
//
// # Always enforced
//
// Before anything is written, Encode drops null members, empty lists and empty objects at every depth (see
// DropEmpty) and refuses a value that the chosen text format cannot represent with an UnsupportedValueError,
// a usage problem. Project reports an unknown or repeated field with a ProjectionError that names the
// available fields.
//
// Control characters in terminal output are always made visible. The table format writes a newline, carriage
// return and tab as \n, \r and \t and every other control character as \xNN. The compact format writes them
// as \n, \r, \t and \uNNNN, and also escapes the backslash, the delimiter | and, in objects, =. Ordinary
// Unicode is left as it is. The table format writes column names in upper case.
//
// ParseFormat and ResolveFormat refuse an unknown format. AllCodes refuses an invalid or duplicate code.
//
// # Options
//
//   - ResolveFormat's agent argument: selects compact when no format is given. Default: table.
//   - AllCodes' extra codes: error codes for an application's own domain. Default: only the shared codes.
package output
