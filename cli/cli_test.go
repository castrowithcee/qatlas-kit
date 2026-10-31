package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/output"
)

func TestExitCode(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Fatalf("nil: got %d", got)
	}
	if got := ExitCode(errors.New("failed")); got != 1 {
		t.Fatalf("runtime: got %d", got)
	}
	err, makeErr := NewFailure(output.CodeUsage, "bad input", "Correct the input.", true)
	if makeErr != nil {
		t.Fatal(makeErr)
	}
	if got := ExitCode(fmt.Errorf("wrapped: %w", err)); got != 2 {
		t.Fatalf("usage: got %d", got)
	}
	unsupported := &output.UnsupportedValueError{Format: output.FormatCompact, Field: "nested", Type: "map[string]interface {}"}
	if got := ExitCode(fmt.Errorf("wrapped: %w", unsupported)); got != ExitUsage {
		t.Fatalf("unsupported value: got %d", got)
	}
}

func TestWriteError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "well formed refusal",
			err: &Failure{
				Code: output.CodeConfirmationRequired, Message: "confirmation missing",
				NextStep: "Repeat with --yes.", Usage: true,
			},
			want: "tool: confirmation-required: confirmation missing\nRepeat with --yes.\n",
		},
		{
			name: "syntax with detail",
			err: &Failure{
				Code: output.CodeUsage, Message: "unknown flag", NextStep: "Check the flag.",
				Detail: struct{ Available []string }{[]string{"--output"}},
				Usage:  true, Syntax: true,
			},
			want: "tool: usage: unknown flag\nCheck the flag.\n{\"Available\":[\"--output\"]}\nUsage: tool [flags]\n",
		},
		{
			name: "runtime",
			err:  errors.New("connection failed"),
			want: "tool: runtime: connection failed\nCheck the cause and try again.\n",
		},
		{
			name: "unsupported output value",
			err: &output.UnsupportedValueError{
				Format: output.FormatCompact, Field: "nested", Type: "map[string]interface {}",
			},
			want: "tool: usage: field \"nested\" cannot be rendered as compact: value of type map[string]interface {} is not representable\nChoose a compatible format or select representable fields.\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteError(&out, "tool", tc.err, "Usage: tool [flags]\n"); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteErrorEscapesControls(t *testing.T) {
	failure := &Failure{
		Code: output.CodeRuntime, Message: "failed\n\x1b[31m",
		NextStep: "Retry\x00later.", Detail: map[string]string{"source": "bad\x1bdata"},
		Syntax: true,
	}
	var out bytes.Buffer
	if err := WriteError(&out, "tool\x1b[2J", failure, "Usage: tool\x1b[31m\n"); err != nil {
		t.Fatal(err)
	}
	want := "tool\\x1b[2J: runtime: failed\\n\\x1b[31m\nRetry\\x00later.\n" +
		"{\"source\":\"bad\\u001bdata\"}\nUsage: tool\\x1b[31m\n"
	if got := out.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.ContainsRune(out.String(), '\x1b') {
		t.Fatal("diagnostic contains an escape byte")
	}
}

func TestWriteErrorRejectsInvalidCodeBeforeOutput(t *testing.T) {
	var out bytes.Buffer
	err := WriteError(&out, "tool", &Failure{Code: "invalid_code", Message: "bad", NextStep: "retry"}, "")
	if err == nil || out.Len() != 0 {
		t.Fatalf("invalid code: err %v, output %q", err, out.String())
	}
}

func TestFailureCodeValidation(t *testing.T) {
	for _, code := range []string{"Usage", "bad_code", "bad--code", ""} {
		if _, err := NewFailure(output.Code(code), "message", "next", true); err == nil {
			t.Fatalf("accepted invalid code %q", code)
		}
	}
	if _, err := NewFailure("not-found", "missing", "Try another name.", true); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmationBeforeEffect(t *testing.T) {
	called := false
	action := func() error {
		called = true
		return nil
	}
	if err := ConfirmAction(Confirmation{Agent: true, Question: "Replace data?"}, action); err == nil {
		t.Fatal("agent proceeded without --yes")
	}
	if called {
		t.Fatal("effect ran")
	}
	if err := ConfirmAction(Confirmation{Agent: true, Yes: true, RequiresConfirm: true, Question: "Replace data?"}, action); err == nil {
		t.Fatal("--yes bypassed --confirm")
	}
	if called {
		t.Fatal("effect ran")
	}
	if err := ConfirmAction(Confirmation{Agent: true, Yes: true, Confirm: true, RequiresConfirm: true, Question: "Replace data?"}, action); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("effect did not run after both gates")
	}
}

func TestConfirmationDoesNotUseStdinWithoutTerminal(t *testing.T) {
	opened := false
	err := confirmWith(Confirmation{Question: "Proceed?"}, func() bool { return false }, func() (terminalPair, error) {
		opened = true
		return terminalPair{in: strings.NewReader("yes\n"), out: io.Discard, close: func() {}}, nil
	})
	if err == nil || opened {
		t.Fatalf("noninteractive question: err %v, opened %v", err, opened)
	}
}

func TestTerminalAnswers(t *testing.T) {
	for _, tc := range []struct {
		input string
		yes   bool
	}{
		{"y\n", true},
		{" YES \n", true},
		{"\n", false},
		{"no\n", false},
		{"yes", false},
	} {
		var prompt bytes.Buffer
		err := confirmWith(Confirmation{Question: "Delete one item?"}, func() bool { return true }, func() (terminalPair, error) {
			return terminalPair{in: strings.NewReader(tc.input), out: &prompt, close: func() {}}, nil
		})
		if (err == nil) != tc.yes {
			t.Fatalf("%q: err %v", tc.input, err)
		}
		if got := prompt.String(); got != "Delete one item? [y/N] " {
			t.Fatalf("prompt %q", got)
		}
	}
}
