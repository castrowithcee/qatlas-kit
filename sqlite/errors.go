package sqlite

import (
	"errors"
	"io/fs"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/castrowithcee/qatlas-kit/output"
)

// Error is a failed database operation. Message names the operation and carries the cause's text; Unwrap
// yields the cause.
type Error struct {
	Code    output.Code
	Message string
	cause   error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the cause, or nil.
func (e *Error) Unwrap() error { return e.cause }

func newError(code output.Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}

// classify maps a driver or file system failure to a code.
func classify(err error) output.Code {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
			return output.CodeTimeout
		case sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB:
			return output.CodeConfigInvalid
		case sqlite3.SQLITE_PERM, sqlite3.SQLITE_READONLY, sqlite3.SQLITE_AUTH:
			return output.CodePermission
		}
	}
	switch {
	case errors.Is(err, fs.ErrPermission):
		return output.CodePermission
	case errors.Is(err, fs.ErrNotExist):
		return output.CodeNotFound
	}
	return output.CodeRuntime
}

// wrap turns a cause into an *Error with the code classify finds. An *Error passes through unchanged.
func wrap(action string, cause error) *Error {
	var e *Error
	if errors.As(cause, &e) {
		return e
	}
	return newError(classify(cause), action+": "+cause.Error(), cause)
}
