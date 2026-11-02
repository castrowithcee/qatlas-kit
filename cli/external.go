package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/httpx"
)

// Bounds of an external answer, in bytes of text after redaction.
const (
	maxExternalEntries = 8
	maxExternalMessage = 800
	maxExternalField   = 200
)

// ExternalAnswer is what an external service answered about a failure. It is untrusted: WriteError
// redacts, bounds, and marks it as a block of its own, so a reader never takes it for the application's
// text.
type ExternalAnswer struct {
	// Source names who answered, for example "service"; the block is headed "<Source> answer".
	Source string
	httpx.Original
}

// cleanAnswer is an external answer as it may be shown.
type cleanAnswer struct {
	Source        string       `json:"source"`
	Untrusted     bool         `json:"untrusted"`
	Status        int          `json:"status,omitempty"`
	Errors        []cleanEntry `json:"errors,omitempty"`
	Documentation string       `json:"documentation,omitempty"`
	Truncated     bool         `json:"truncated"`
}

type cleanEntry struct {
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message,omitempty"`
}

// clean redacts before it cuts, so a secret split by a bound can never survive as a prefix. It returns nil
// when nothing of the answer is left.
func (a *ExternalAnswer) clean(redact func(string) string) *cleanAnswer {
	if a == nil {
		return nil
	}
	out := &cleanAnswer{Source: redact(a.Source), Untrusted: true}
	if out.Source == "" {
		out.Source = "external service"
	}
	text := func(s string, limit int) string {
		s = strings.TrimSpace(strings.ToValidUTF8(redact(s), "\uFFFD"))
		if len(s) <= limit {
			return s
		}
		out.Truncated = true
		for limit > 0 && !utf8.RuneStart(s[limit]) {
			limit--
		}
		return s[:limit]
	}
	if a.Status > 0 {
		out.Status = a.Status
	}
	out.Documentation = text(a.Documentation, maxExternalField)
	for i, e := range a.Entries {
		if i == maxExternalEntries {
			out.Truncated = true
			break
		}
		entry := cleanEntry{
			Type: text(e.Type, maxExternalField), Code: text(e.Code, maxExternalField),
			Path: text(e.Path, maxExternalField), Message: text(e.Message, maxExternalMessage),
		}
		if entry != (cleanEntry{}) {
			out.Errors = append(out.Errors, entry)
		}
	}
	if out.Status == 0 && len(out.Errors) == 0 && out.Documentation == "" {
		return nil
	}
	return out
}

// block renders the answer for a person, set apart by its header and a margin. program is already escaped.
func (a *cleanAnswer) block(program string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s answer (untrusted, not written by %s)", escapeControl(a.Source), program)
	if a.Status != 0 {
		fmt.Fprintf(&b, ", HTTP %d", a.Status)
	}
	if a.Truncated {
		b.WriteString(", truncated")
	}
	b.WriteString(":\n")
	for _, e := range a.Errors {
		var parts []string
		for _, part := range []string{e.Type, e.Code} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		line := strings.Join(parts, " ")
		if e.Path != "" {
			line += " at " + e.Path
		}
		if e.Message != "" {
			if line != "" {
				line += ": "
			}
			line += e.Message
		}
		b.WriteString("  | " + escapeControl(line) + "\n")
	}
	if a.Documentation != "" {
		b.WriteString("  | documentation: " + escapeControl(a.Documentation) + "\n")
	}
	return b.String()
}
