//go:build windows

package localipc

import (
	"os"
	"testing"
)

// A pipe or process of another user cannot be set up without a second
// account, so only the user's own channels are tested.
func wantEndedByHolder(t *testing.T, state *os.ProcessState) {
	t.Helper()
	if state.ExitCode() != 1 {
		t.Fatalf("the helper ended with %v; want exit code 1 from TerminateProcess", state)
	}
}
