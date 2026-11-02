package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/output"
)

const (
	ExitSuccess = 0
	ExitRuntime = 1
	ExitUsage   = 2
)

// Route is the reader a diagnostic addresses.
type Route int

const (
	// RouteHuman addresses a person at a terminal: a next step names a command or a key.
	RouteHuman Route = iota
	// RouteAgent addresses an agent: where only a person can act, the next step tells it to ask the user.
	RouteAgent
)

// Failure is a diagnostic with a stable code and an actionable next step for each route.
type Failure struct {
	Code     output.Code
	Message  string
	NextStep string
	// AgentNextStep replaces NextStep on RouteAgent when it is set.
	AgentNextStep string
	// Detail is written as one JSON line after the next step.
	Detail any
	// External is what an external service answered; it is shown apart from the application's own text.
	External *ExternalAnswer
	Usage    bool
	Syntax   bool
	Cause    error
}

func (e *Failure) Error() string { return e.Message }
func (e *Failure) Unwrap() error { return e.Cause }

// NewFailure checks the public code before an application can emit it.
func NewFailure(code output.Code, message, nextStep string, usage bool) (*Failure, error) {
	if !output.ValidCode(code) {
		return nil, fmt.Errorf("invalid error code %q", code)
	}
	if message == "" || nextStep == "" {
		return nil, fmt.Errorf("error message and next step are required")
	}
	return &Failure{Code: code, Message: message, NextStep: nextStep, Usage: usage}, nil
}

// ExitCode maps a diagnostic to the public process exit contract.
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	return failureOf(err).exitCode()
}

func (e *Failure) exitCode() int {
	if e.Usage {
		return ExitUsage
	}
	return ExitRuntime
}

// ErrorOptions are the application's choices for one diagnostic.
type ErrorOptions struct {
	// Usage is written only after a syntax error.
	Usage string
	Route Route
	// Redact replaces the known secrets in a text. It runs on every text before it is cut or escaped.
	Redact func(string) string
	// ExternalJSON writes an external answer as one JSON line instead of a text block.
	ExternalJSON bool
}

func (o ErrorOptions) redact(s string) string {
	if o.Redact == nil {
		return s
	}
	return o.Redact(s)
}

// WriteError writes the stable diagnostic to stderr. Only syntax errors carry usage text. Nothing is
// written when the diagnostic itself is invalid.
func WriteError(w io.Writer, program string, err error, opts ErrorOptions) error {
	if err == nil {
		return nil
	}
	failure := failureOf(err)
	if program == "" {
		return fmt.Errorf("program name is required")
	}
	if !output.ValidCode(failure.Code) {
		return fmt.Errorf("invalid error code %q", failure.Code)
	}
	step := failure.NextStep
	if opts.Route == RouteAgent && failure.AgentNextStep != "" {
		step = failure.AgentNextStep
	}
	if step == "" {
		return fmt.Errorf("next step is required")
	}
	name := escapeControl(opts.redact(program))
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s: %s: %s\n%s\n", name, failure.Code,
		escapeControl(opts.redact(failure.Message)), escapeControl(opts.redact(step)))
	if failure.Detail != nil {
		line, err := jsonLine(failure.Detail, opts.redact)
		if err != nil {
			return err
		}
		b.Write(line)
	}
	if answer := failure.External.clean(opts.redact); answer != nil {
		if opts.ExternalJSON {
			line, err := jsonLine(answer, func(s string) string { return s })
			if err != nil {
				return err
			}
			b.Write(line)
		} else {
			b.WriteString(answer.block(name))
		}
	}
	if failure.Syntax && opts.Usage != "" {
		b.WriteString(escapeUsage(opts.redact(opts.Usage)))
	}
	_, err = w.Write(b.Bytes())
	return err
}

func failureOf(err error) *Failure {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure
	}
	var unsupported *output.UnsupportedValueError
	if errors.As(err, &unsupported) {
		return &Failure{
			Code: output.CodeUsage, Message: unsupported.Error(),
			NextStep: "Choose a compatible format or select representable fields.", Usage: true,
		}
	}
	var projection *output.ProjectionError
	if errors.As(err, &projection) {
		return &Failure{
			Code: output.CodeUsage, Message: projection.Error(),
			NextStep: "Select each field once from the available fields.", Usage: true,
		}
	}
	return &Failure{Code: output.CodeRuntime, Message: err.Error(), NextStep: "Check the cause and try again."}
}

// jsonLine encodes v as one line. Strings are redacted after encoding, so a value of any type is covered,
// and the declared member order survives.
func jsonLine(v any, redact func(string) string) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if raw, err = redactJSON(raw, redact); err != nil {
		return nil, err
	}
	return append(visibleJSON(raw), '\n'), nil
}

// redactJSON rewrites every string of a JSON document, member names included, without reordering it.
func redactJSON(raw []byte, redact func(string) string) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	type scope struct {
		object bool
		tokens int
	}
	var (
		b     bytes.Buffer
		stack []scope
	)
	for {
		token, err := dec.Token()
		if err == io.EOF {
			return b.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		if delim, ok := token.(json.Delim); ok && (delim == '}' || delim == ']') {
			stack = stack[:len(stack)-1]
			b.WriteRune(rune(delim))
			continue
		}
		if len(stack) > 0 {
			top := &stack[len(stack)-1]
			switch {
			case top.object && top.tokens%2 == 1:
				b.WriteByte(':')
			case top.tokens > 0:
				b.WriteByte(',')
			}
			top.tokens++
		}
		switch value := token.(type) {
		case json.Delim:
			b.WriteRune(rune(value))
			stack = append(stack, scope{object: value == '{'})
		case string:
			encoded, err := json.Marshal(redact(value))
			if err != nil {
				return nil, err
			}
			b.Write(encoded)
		case json.Number:
			b.WriteString(value.String())
		case bool:
			b.WriteString(strconv.FormatBool(value))
		case nil:
			b.WriteString("null")
		}
	}
}

// visibleJSON escapes the controls and invisible characters that encoding/json leaves raw, such as DEL, C1
// controls, and bidirectional overrides. Outside its strings a compact JSON document holds only printable
// ASCII, so every such rune is inside a string, where an escape keeps its value.
func visibleJSON(raw []byte) []byte {
	var b bytes.Buffer
	for len(raw) > 0 {
		r, size := utf8.DecodeRune(raw)
		switch {
		case !unicode.IsControl(r) && unicode.IsPrint(r):
			b.Write(raw[:size])
		case r > 0xffff:
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, r1, r2)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
		raw = raw[size:]
	}
	return b.Bytes()
}

// escapeControl makes untrusted text visible without letting it move the cursor or add diagnostic lines.
func escapeControl(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			quoted := strconv.QuoteRune(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func escapeUsage(usage string) string {
	lines := strings.Split(usage, "\n")
	for i := range lines {
		lines[i] = escapeControl(lines[i])
	}
	return strings.Join(lines, "\n")
}

const (
	AgentFlagHelp   = "agent mode: compact output by default, no progress, and no questions; use --yes for actions that ask"
	YesFlagHelp     = "answer this command's ordinary confirmation question with yes"
	ConfirmFlagHelp = "confirm this one data-changing request; required separately from --yes"
)
