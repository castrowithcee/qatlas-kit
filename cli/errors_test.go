package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/httpx"
	"github.com/castrowithcee/qatlas-kit/output"
)

const sampleSecret = "s3cr3t-T0KEN"

func redactSample(s string) string { return strings.ReplaceAll(s, sampleSecret, "[redacted]") }

func TestWriteErrorRedactsEveryText(t *testing.T) {
	failure := &Failure{
		Code: output.CodeAuth, Message: "token " + sampleSecret + " refused",
		NextStep: "Replace " + sampleSecret + ".", Syntax: true,
		Detail: map[string]any{sampleSecret: []any{"x" + sampleSecret}},
		External: &ExternalAnswer{Source: "service", Original: httpx.Original{
			Status: 401, Documentation: "https://example.invalid/" + sampleSecret,
			Entries: []httpx.OriginalEntry{{Code: sampleSecret, Message: "bad " + sampleSecret}},
		}},
	}
	for _, externalJSON := range []bool{false, true} {
		var out bytes.Buffer
		err := WriteError(&out, "tool", failure, ErrorOptions{
			Usage: "Usage: tool --token " + sampleSecret + "\n", Redact: redactSample, ExternalJSON: externalJSON,
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), sampleSecret) {
			t.Fatalf("secret reached the diagnostic:\n%s", out.String())
		}
		if got := strings.Count(out.String(), "[redacted]"); got != 8 {
			t.Errorf("external JSON %v: %d markers in\n%s", externalJSON, got, out.String())
		}
	}
}

func TestWriteErrorRedactsBeforeCut(t *testing.T) {
	message := strings.Repeat("a", maxExternalMessage-4) + sampleSecret
	var out bytes.Buffer
	err := WriteError(&out, "tool", &Failure{
		Code: output.CodeRuntime, Message: "failed", NextStep: "Retry.",
		External: &ExternalAnswer{Original: httpx.Original{Entries: []httpx.OriginalEntry{{Message: message}}}},
	}, ErrorOptions{Redact: redactSample})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), sampleSecret[:4]) {
		t.Fatalf("a prefix of the secret survived the cut:\n%s", out.String())
	}
}

func TestWriteErrorNextStepByRoute(t *testing.T) {
	both := &Failure{
		Code: output.CodeVaultLocked, Message: "the vault is locked",
		NextStep: "Run 'tool vault unlock'.", AgentNextStep: "An agent cannot unlock the vault; ask the user.",
	}
	humanOnly := &Failure{Code: output.CodeNotFound, Message: "no such item", NextStep: "List the items."}
	for _, tc := range []struct {
		failure *Failure
		route   Route
		want    string
	}{
		{both, RouteHuman, "Run 'tool vault unlock'."},
		{both, RouteAgent, "An agent cannot unlock the vault; ask the user."},
		{humanOnly, RouteAgent, "List the items."},
	} {
		var out bytes.Buffer
		if err := WriteError(&out, "tool", tc.failure, ErrorOptions{Route: tc.route}); err != nil {
			t.Fatal(err)
		}
		if lines := strings.Split(out.String(), "\n"); lines[1] != tc.want {
			t.Errorf("route %d: next step %q, want %q", tc.route, lines[1], tc.want)
		}
	}
}

func TestWriteErrorRejectsInvalidDiagnosticBeforeOutput(t *testing.T) {
	for name, failure := range map[string]*Failure{
		"no next step":        {Code: output.CodeRuntime, Message: "failed"},
		"only an agent step":  {Code: output.CodeRuntime, Message: "failed", AgentNextStep: "Ask the user."},
		"unencodable detail":  {Code: output.CodeRuntime, Message: "failed", NextStep: "Retry.", Detail: func() {}},
		"invalid code":        {Code: "Bad", Message: "failed", NextStep: "Retry."},
		"invalid code syntax": {Code: "bad--code", Message: "failed", NextStep: "Retry."},
	} {
		var out bytes.Buffer
		if err := WriteError(&out, "tool", failure, ErrorOptions{}); err == nil || out.Len() != 0 {
			t.Errorf("%s: err %v, output %q", name, err, out.String())
		}
	}
	var out bytes.Buffer
	if err := WriteError(&out, "", &Failure{Code: output.CodeRuntime, Message: "m", NextStep: "n"}, ErrorOptions{}); err == nil || out.Len() != 0 {
		t.Errorf("empty program: err %v, output %q", err, out.String())
	}
}

func TestWriteErrorExternalBlock(t *testing.T) {
	entries := make([]httpx.OriginalEntry, maxExternalEntries+1)
	for i := range entries {
		entries[i] = httpx.OriginalEntry{Code: fmt.Sprintf("E%d", i)}
	}
	entries[0] = httpx.OriginalEntry{Type: "error", Code: "E0", Path: "/items/0", Message: "line\n\x1b[2Jfake: runtime: ok\u202e"}
	failure := &Failure{
		Code: output.CodeRuntime, Message: "the service refused the request", NextStep: "Check the request.",
		External: &ExternalAnswer{Source: "service", Original: httpx.Original{
			Status: 422, Entries: entries, Documentation: " https://example.invalid/doc ",
		}},
	}
	var out bytes.Buffer
	if err := WriteError(&out, "tool", failure, ErrorOptions{}); err != nil {
		t.Fatal(err)
	}
	want := "tool: runtime: the service refused the request\nCheck the request.\n" +
		"service answer (untrusted, not written by tool), HTTP 422, truncated:\n" +
		"  | error E0 at /items/0: line\\n\\x1b[2Jfake: runtime: ok\\u202e\n"
	for i := 1; i < maxExternalEntries; i++ {
		want += fmt.Sprintf("  | E%d\n", i)
	}
	want += "  | documentation: https://example.invalid/doc\n"
	if got := out.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestWriteErrorExternalJSON(t *testing.T) {
	failure := &Failure{
		Code: output.CodeRuntime, Message: "refused", NextStep: "Retry later.",
		Detail: struct{ B, A string }{"b", "a"},
		External: &ExternalAnswer{Original: httpx.Original{
			Entries: []httpx.OriginalEntry{{Message: strings.Repeat("é", maxExternalMessage)}},
		}},
	}
	var out bytes.Buffer
	if err := WriteError(&out, "tool", failure, ErrorOptions{ExternalJSON: true}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 4 || lines[2] != `{"B":"b","A":"a"}` {
		t.Fatalf("lines %q", lines)
	}
	var answer struct {
		Source    string
		Untrusted bool
		Truncated bool
		Errors    []struct{ Message string }
	}
	if err := json.Unmarshal([]byte(lines[3]), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Source != "external service" || !answer.Untrusted || !answer.Truncated ||
		len(answer.Errors[0].Message) != maxExternalMessage {
		t.Fatalf("answer %+v", answer)
	}
}

func TestWriteErrorOmitsEmptyExternalAnswer(t *testing.T) {
	var out bytes.Buffer
	failure := &Failure{
		Code: output.CodeRuntime, Message: "failed", NextStep: "Retry.",
		External: &ExternalAnswer{Source: "service", Original: httpx.Original{
			Entries: []httpx.OriginalEntry{{Message: "  "}},
		}},
	}
	if err := WriteError(&out, "tool", failure, ErrorOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "untrusted") {
		t.Fatalf("empty answer shown:\n%s", out.String())
	}
}

func TestDetailLineKeepsControlsVisible(t *testing.T) {
	failure := &Failure{
		Code: output.CodeRuntime, Message: "failed", NextStep: "Retry.",
		Detail: map[string]string{"name": "a\x7fb\u009bc\u202ed\U000e0041"},
	}
	var out bytes.Buffer
	if err := WriteError(&out, "tool", failure, ErrorOptions{}); err != nil {
		t.Fatal(err)
	}
	line := strings.Split(out.String(), "\n")[2]
	if want := `{"name":"a\u007fb\u009bc\u202ed\udb40\udc41"}`; line != want {
		t.Fatalf("detail %q, want %q", line, want)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(line), &decoded); err != nil || decoded["name"] != "a\x7fb\u009bc\u202ed\U000e0041" {
		t.Fatalf("detail does not decode to its value: %v %q", err, decoded)
	}
}

func TestProjectionErrorIsUsage(t *testing.T) {
	_, err := output.Project(output.Object{Fields: []output.Field{{Name: "name", Value: "x"}}}, []string{"size"})
	if err == nil {
		t.Fatal("unknown field accepted")
	}
	if got := ExitCode(fmt.Errorf("wrapped: %w", err)); got != ExitUsage {
		t.Errorf("exit code %d", got)
	}
	var out bytes.Buffer
	if err := WriteError(&out, "tool", err, ErrorOptions{Usage: "Usage: tool\n"}); err != nil {
		t.Fatal(err)
	}
	want := "tool: usage: unknown field \"size\", available fields are name\nSelect each field once from the available fields.\n"
	if got := out.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
