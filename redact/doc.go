// Package redact removes known secret values from text before it is shown. Whoever resolves a secret
// registers its value with a Redactor; every diagnostic, error, result and library log passes through it
// afterwards. Each registered value is also covered in its Go-quoted and JSON-escaped forms, so a message
// that prints it with %q or a response that echoes it inside JSON does not bring it back.
package redact
