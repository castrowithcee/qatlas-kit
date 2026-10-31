// Package output renders ordered results and defines application-neutral CLI codes.
package output

import (
	"fmt"
	"regexp"
	"strings"
)

// Format is a rendering of a result.
type Format string

// TOON follows specification 4.1; callers choose the format for each command.
const (
	FormatTable   Format = "table"
	FormatJSON    Format = "json"
	FormatCompact Format = "compact"
	FormatTOON    Format = "toon"
)

// ParseFormat validates a format name.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatTable, FormatJSON, FormatCompact, FormatTOON:
		return Format(s), nil
	}
	return "", fmt.Errorf("unknown output format %q, want %s, %s, %s, or %s",
		s, FormatTable, FormatJSON, FormatCompact, FormatTOON)
}

// ResolveFormat applies an explicit output choice before the agent-mode default.
func ResolveFormat(explicit string, agent bool) (Format, error) {
	if explicit != "" {
		return ParseFormat(explicit)
	}
	if agent {
		return FormatCompact, nil
	}
	return FormatTable, nil
}

// Field is one named value of an object. The table, JSON, and compact formats expect scalars: string,
// bool, int64, float64, or nil. MarshalTOON additionally accepts nested objects and arrays.
type Field struct {
	Name  string
	Value any
}

// Object is a single record with a stable field order.
type Object struct {
	Fields []Field
}

// Row holds the values of one collection record, addressed by column name. A missing key renders as an
// empty field and as JSON null.
type Row map[string]any

// Collection is an ordered list of records sharing one column order.
type Collection struct {
	Columns []string
	Rows    []Row
}

// Result is what a command hands to the encoders.
type Result interface{ isResult() }

func (Object) isResult()     {}
func (Collection) isResult() {}

// ProjectionError reports a field in --fields that cannot be used. It is a usage problem.
type ProjectionError struct {
	Field     string
	Available []string
	Duplicate bool
}

func (e *ProjectionError) Error() string {
	if e.Duplicate {
		return fmt.Sprintf("field %q is requested more than once", e.Field)
	}
	return fmt.Sprintf("unknown field %q, available fields are %s", e.Field, strings.Join(e.Available, ", "))
}

// Project restricts a result to the named fields, in the order given. An empty list keeps everything.
// Validation is central so every format receives the same field selection.
func Project(result Result, fields []string) (Result, error) {
	if len(fields) == 0 {
		return result, nil
	}

	// A repeated name would produce a duplicate key in the JSON output.
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if seen[f] {
			return nil, &ProjectionError{Field: f, Duplicate: true}
		}
		seen[f] = true
	}

	switch r := result.(type) {
	case Collection:
		available := r.Columns
		columns := make([]string, 0, len(fields))
		for _, f := range fields {
			if !contains(available, f) {
				return nil, &ProjectionError{Field: f, Available: available}
			}
			columns = append(columns, f)
		}
		return Collection{Columns: columns, Rows: r.Rows}, nil

	case Object:
		available := make([]string, len(r.Fields))
		byName := make(map[string]Field, len(r.Fields))
		for i, f := range r.Fields {
			available[i] = f.Name
			byName[f.Name] = f
		}
		out := make([]Field, 0, len(fields))
		for _, name := range fields {
			f, ok := byName[name]
			if !ok {
				return nil, &ProjectionError{Field: name, Available: available}
			}
			out = append(out, f)
		}
		return Object{Fields: out}, nil
	}
	return result, nil
}

// Code is a stable error class that callers can extend.
type Code string

const (
	CodeUsage                Code = "usage"
	CodeInvalidRequest       Code = "invalid-request"
	CodeConfigMissing        Code = "config-missing"
	CodeConfigInvalid        Code = "config-invalid"
	CodeConfirmationRequired Code = "confirmation-required"
	CodeConfirmationDeclined Code = "confirmation-declined"
	CodeUnreachable          Code = "unreachable"
	CodeAuth                 Code = "auth"
	CodePermission           Code = "permission"
	CodeNotFound             Code = "not-found"
	CodeTimeout              Code = "timeout"
	CodeRateLimited          Code = "rate-limited"
	CodeRuntime              Code = "runtime"
)

var codeName = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*$`)

// ValidCode reports whether a caller-owned error code follows the public syntax.
func ValidCode(code Code) bool { return codeName.MatchString(string(code)) }

// AllCodes returns the generic codes followed by validated caller-owned codes.
// It rejects collisions so each documented code has exactly one meaning.
func AllCodes(extra ...Code) ([]Code, error) {
	codes := []Code{
		CodeUsage, CodeInvalidRequest, CodeConfigMissing, CodeConfigInvalid,
		CodeConfirmationRequired, CodeConfirmationDeclined, CodeUnreachable, CodeAuth, CodePermission,
		CodeNotFound, CodeTimeout, CodeRateLimited, CodeRuntime,
	}
	seen := make(map[Code]bool, len(codes)+len(extra))
	for _, code := range codes {
		seen[code] = true
	}
	for _, code := range extra {
		if !ValidCode(code) {
			return nil, fmt.Errorf("invalid error code %q", code)
		}
		if seen[code] {
			return nil, fmt.Errorf("duplicate error code %q", code)
		}
		seen[code] = true
		codes = append(codes, code)
	}
	return codes, nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
