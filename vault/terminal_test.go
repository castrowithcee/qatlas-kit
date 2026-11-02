package vault

import (
	"errors"
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
