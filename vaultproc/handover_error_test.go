package vaultproc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

const handoverPrefix = "the vault process did not hand the vault over and locked itself: "

// TestHandoverReasonsAtTheClient says how every HandoverReason reaches a client today. A reason without an answer
// code of its own arrives as another reason or as another error.
func TestHandoverReasonsAtTheClient(t *testing.T) {
	for _, test := range []struct {
		name    string
		code    string
		reason  HandoverReason // the reason the client reads; empty when the answer is no ErrHandover
		text    string
		other   error // the error the client returns instead
		covered HandoverReason
	}{
		{name: "bad request", code: codeBadRequest, other: ErrBadRequest, covered: HandoverBadRequest},
		{name: "unsupported", code: codeNoHandover, reason: HandoverUnsupported,
			text: "this platform or process does not hand over", covered: HandoverUnsupported},
		{name: "rollback", code: codeRollback, reason: HandoverRollback, text: "a rollback always locks", covered: HandoverRollback},
		{name: "not verified", code: codeUnverified, reason: HandoverNotVerified,
			text: "it could not verify the release", covered: HandoverNotVerified},
		// The process answers a file over the limit with the code of the check that read it.
		{name: "too large at the release", code: codeUnverified, reason: HandoverNotVerified,
			text: "it could not verify the release", covered: HandoverTooLarge},
		{name: "program unchanged", code: codeUnchanged, reason: HandoverProgramUnchanged,
			text: "its program was not replaced", covered: HandoverProgramUnchanged},
		{name: "program mismatch", code: codeMismatch, reason: HandoverProgramMismatch,
			text: "the installed program is not the one the release holds", covered: HandoverProgramMismatch},
		{name: "no successor", code: codeHandoverFailed, reason: HandoverNoSuccessor,
			text: "no successor took the vault over", covered: HandoverNoSuccessor},
		// A process that is locking answers as one that is not running.
		{name: "locked", code: codeLocked, other: ErrNotRunning, covered: HandoverLocked},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := answerError(test.code)
			if test.other != nil {
				if err != test.other || errors.Is(err, ErrHandover) {
					t.Fatalf("answerError(%q) = %v, want %v", test.code, err, test.other)
				}
				return
			}
			var he *HandoverError
			if !errors.As(err, &he) || he.Reason != test.reason || !errors.Is(err, ErrHandover) {
				t.Fatalf("answerError(%q) = %v, want ErrHandover with reason %q", test.code, err, test.reason)
			}
			if err.Error() != handoverPrefix+test.text {
				t.Fatalf("text = %q", err.Error())
			}
		})
	}
	// The user's setting that asks for a lock is no error: PrepareHandover returns no handover and no
	// error, and Config.OnHandover is told HandoverDeclined. The two client-only reasons are tested below.
}

func TestHandoverReasonsAreAllCovered(t *testing.T) {
	// Keep this list equal to the constants of HandoverReason.
	all := []HandoverReason{HandoverBadRequest, HandoverUnsupported, HandoverRollback, HandoverNotVerified,
		HandoverTooLarge, HandoverDeclined, HandoverProgramUnchanged, HandoverProgramMismatch, HandoverNoSuccessor,
		HandoverLocked, HandoverAnswerUnreadable, HandoverNotReplaced}
	seen := map[HandoverReason]bool{}
	for _, reason := range all {
		if reason == "" || seen[reason] {
			t.Fatalf("reason %q is empty or listed twice", reason)
		}
		seen[reason] = true
	}
}

// pipeHandover returns a Handover whose peer answers the commit with resp's bytes.
func pipeHandover(t *testing.T, answer string) *Handover {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	go func() {
		buf := make([]byte, 4096)
		if _, err := server.Read(buf); err == nil {
			_, _ = server.Write([]byte(answer + "\n"))
		}
	}()
	c := &Client{Version: 1, ProofLabel: testLabel}
	return &Handover{client: c, conn: client}
}

func TestCommitAnswerUnreadable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for name, answer := range map[string]string{
		"no result":  `{"v":1,"app":1}`,
		"no process": `{"v":1,"app":1,"result":{"pid":0}}`,
		"bad result": `{"v":1,"app":1,"result":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			pid, err := pipeHandover(t, answer).Commit(ctx)
			var he *HandoverError
			if pid != 0 || !errors.As(err, &he) || he.Reason != HandoverAnswerUnreadable || !errors.Is(err, ErrHandover) {
				t.Fatalf("Commit = %d, %v; want ErrHandover with reason %q", pid, err, HandoverAnswerUnreadable)
			}
			if err.Error() != handoverPrefix+"the answer cannot be read" {
				t.Fatalf("text = %q", err.Error())
			}
		})
	}
}

func TestFinishProgramNotReplaced(t *testing.T) {
	_, conn := net.Pipe()
	r := &Replacement{handover: &Handover{conn: conn}}
	result := r.Finish()
	var he *HandoverError
	if result.Outcome != OutcomeLocked || !errors.As(result.Err, &he) || he.Reason != HandoverNotReplaced ||
		!errors.Is(result.Err, ErrHandover) {
		t.Fatalf("Finish = %+v; want locked with ErrHandover and reason %q", result, HandoverNotReplaced)
	}
	if result.Err.Error() != handoverPrefix+"the update did not replace the program" {
		t.Fatalf("text = %q", result.Err.Error())
	}
}
