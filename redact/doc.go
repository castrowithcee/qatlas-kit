// Package redact removes known secret values from text before it is shown. Whoever resolves a secret
// registers its value with a Redactor; every diagnostic, error, result and library log passes through it
// afterwards.
//
// # Always enforced
//
// A Redactor replaces each registered value with Marker in a single left-to-right pass, the longest value
// first, so a secret that contains another one is replaced as a whole and no replacement matches inside a
// marker. Each registered value is also covered in its Go-quoted and JSON-escaped forms, so a message that
// prints it with %q or a response that echoes it inside JSON does not bring it back. Empty values and
// values shorter than four bytes are ignored, because replacing them would mangle unrelated output without
// protecting anything.
//
// The package protects only the values that were registered and recognises no secret by its shape. Writer
// redacts each write on its own, so a secret split across two writes is not covered.
//
// # Options
//
// The package has no options.
package redact
