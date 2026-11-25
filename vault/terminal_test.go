package vault

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A process whose standard streams are all redirected never waits for a prompt. The deadline makes the
// test fail instead of hanging should the rule be missing.
func TestReadPassphraseRefusesANonInteractiveProcess(t *testing.T) {
	original := interactive
	interactive = func() bool { return false }
	t.Cleanup(func() { interactive = original })

	done := make(chan error, 1)
	go func() {
		_, err := ReadPassphrase("example prompt: ")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNoTerminal) {
			t.Fatalf("ReadPassphrase() error = %v, want ErrNoTerminal", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadPassphrase() waited for input in a process that is not interactive")
	}
	if Interactive() {
		t.Fatal("Interactive() = true, want false")
	}
}

func TestReadConfirmRefusesANonInteractiveProcess(t *testing.T) {
	original := interactive
	interactive = func() bool { return false }
	t.Cleanup(func() { interactive = original })

	done := make(chan error, 1)
	go func() {
		_, err := ReadConfirm("example prompt [y/N] ")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNoTerminal) {
			t.Fatalf("ReadConfirm() error = %v, want ErrNoTerminal", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadConfirm() waited for input in a process that is not interactive")
	}
}

func TestReadConfirmAnswers(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr error
	}{
		{"y", "y\n", true, nil},
		{"YES", "YES\n", true, nil},
		{"padded yes", "  yes \r\n", true, nil},
		{"empty line", "\n", false, nil},
		{"n", "n\n", false, nil},
		{"yess", "yess\n", false, nil},
		{"overlong line", strings.Repeat(" ", confirmLimit) + "y\n", false, nil},
		{"overlong line of yes", "y" + strings.Repeat(" ", confirmLimit) + "\n", false, nil},
		{"no line break", "y", true, nil},
		{"no line break, no", "no", false, nil},
		{"end of input", "", false, ErrNoTerminal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := readConfirm(strings.NewReader(tt.input), &out, "Continue? [y/N] ")
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("readConfirm(%q) = %v, %v; want %v, %v", tt.input, got, err, tt.want, tt.wantErr)
			}
			if out.String() != "Continue? [y/N] " {
				t.Fatalf("prompt = %q", out.String())
			}
		})
	}
}

func TestReadConfirmMasksControlCharacters(t *testing.T) {
	var out bytes.Buffer
	prompt := "Delete \x1b[2Jname\r\x07?\nAgain ‮? [y/N] "
	if _, err := readConfirm(strings.NewReader("n\n"), &out, prompt); err != nil {
		t.Fatal(err)
	}
	want := "Delete \\x1b[2Jname\\r\\a?\nAgain \\u202e? [y/N] "
	if out.String() != want {
		t.Fatalf("prompt = %q, want %q", out.String(), want)
	}
}

func TestReadHiddenRefusesANonInteractiveProcess(t *testing.T) {
	original := interactive
	interactive = func() bool { return false }
	t.Cleanup(func() { interactive = original })

	done := make(chan error, 1)
	go func() {
		_, err := ReadHidden(context.Background(), "example prompt: ")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNoTerminal) {
			t.Fatalf("ReadHidden() error = %v, want ErrNoTerminal", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadHidden() waited for input in a process that is not interactive")
	}
}
