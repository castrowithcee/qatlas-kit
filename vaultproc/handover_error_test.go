package vaultproc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

const handoverPrefix = "the vault process did not hand the vault over and locked itself: "

// TestHandoverReasonsAtTheClient says how every answer code of a handover reaches a client: as its own
// reason, equal to the reason the server reports for that code.
func TestHandoverReasonsAtTheClient(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   string
		reason HandoverReason
		text   string
		also   []error // the sentinels the error matches besides ErrHandover
	}{
		{name: "bad request", code: codeHandoverBadRequest, reason: HandoverBadRequest,
			text: "the handover request cannot be read", also: []error{ErrBadRequest}},
		{name: "unsupported", code: codeNoHandover, reason: HandoverUnsupported,
			text: "this platform or process does not hand over"},
		{name: "rollback", code: codeRollback, reason: HandoverRollback, text: "a rollback always locks"},
		{name: "not verified", code: codeUnverified, reason: HandoverNotVerified,
			text: "it could not verify the release"},
		{name: "too large", code: codeHandoverTooLarge, reason: HandoverTooLarge,
			text: "a release file or the installed program is over the size limit"},
		{name: "program unchanged", code: codeUnchanged, reason: HandoverProgramUnchanged,
			text: "its program was not replaced"},
		{name: "program mismatch", code: codeMismatch, reason: HandoverProgramMismatch,
			text: "the installed program is not the one the release holds"},
		{name: "no successor", code: codeHandoverFailed, reason: HandoverNoSuccessor,
			text: "no successor took the vault over"},
		{name: "locked", code: codeHandoverLocked, reason: HandoverLocked,
			text: "it was already locking", also: []error{ErrNotRunning}},
		{name: "busy", code: codeHandoverBusy, reason: HandoverBusy, text: "another handover is under way"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !isKitCode(test.code) {
				t.Fatalf("%q is not reserved", test.code)
			}
			if got := handoverReason(test.code); got != test.reason {
				t.Fatalf("the server reports %q for %q, want %q", got, test.code, test.reason)
			}
			err := answerError(test.code)
			var he *HandoverError
			if !errors.As(err, &he) || he.Reason != test.reason || !errors.Is(err, ErrHandover) {
				t.Fatalf("answerError(%q) = %v, want ErrHandover with reason %q", test.code, err, test.reason)
			}
			if he.Reason != handoverReason(test.code) {
				t.Fatalf("the client reads %q, the server reports %q", he.Reason, handoverReason(test.code))
			}
			if err.Error() != handoverPrefix+test.text {
				t.Fatalf("text = %q", err.Error())
			}
			// Only the two reasons that had a sentinel before they had a code match it.
			for _, sentinel := range []error{ErrBadRequest, ErrNotRunning} {
				want := false
				for _, also := range test.also {
					want = want || also == sentinel
				}
				if errors.Is(err, sentinel) != want {
					t.Fatalf("errors.Is(%v, %v) = %v, want %v", err, sentinel, !want, want)
				}
			}
		})
	}
	// The general codes keep their own errors: a request that cannot be read before it is known to be a
	// handover, and a process that is locking when it is challenged.
	if err := answerError(codeBadRequest); err != ErrBadRequest || errors.Is(err, ErrHandover) {
		t.Fatalf("answerError(bad-request) = %v", err)
	}
	if err := answerError(codeLocked); err != ErrNotRunning || errors.Is(err, ErrHandover) {
		t.Fatalf("answerError(locked) = %v", err)
	}
	// The user's setting that asks for a lock is no error: PrepareHandover returns no handover and no
	// error, and Config.OnHandover is told HandoverDeclined. The two client-only reasons are tested below.
}

func TestHandoverReasonsAreAllCovered(t *testing.T) {
	// Keep this list equal to the constants of HandoverReason.
	all := []HandoverReason{HandoverBadRequest, HandoverUnsupported, HandoverRollback, HandoverNotVerified,
		HandoverTooLarge, HandoverDeclined, HandoverProgramUnchanged, HandoverProgramMismatch, HandoverNoSuccessor,
		HandoverLocked, HandoverBusy, HandoverAnswerUnreadable, HandoverNotReplaced}
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

func TestPrepareAnswerUnreadable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for name, answer := range map[string]string{
		"bad result": `{"v":1,"app":1,"result":"x"}`,
		"bad type":   `{"v":1,"app":1,"result":{"hand_over":"yes"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := pipeHandover(t, answer)
			got, err := h.client.prepareOn(ctx, h.conn, []byte(`{}`))
			var he *HandoverError
			if got != nil || !errors.As(err, &he) || he.Reason != HandoverAnswerUnreadable || !errors.Is(err, ErrHandover) {
				t.Fatalf("prepare = %v, %v; want ErrHandover with reason %q", got, err, HandoverAnswerUnreadable)
			}
			if err.Error() != handoverPrefix+"the answer cannot be read" {
				t.Fatalf("text = %q", err.Error())
			}
		})
	}
}

func TestPrepareDeclinedIsNoError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := pipeHandover(t, `{"v":1,"app":1,"result":{}}`)
	if got, err := h.client.prepareOn(ctx, h.conn, []byte(`{}`)); got != nil || err != nil {
		t.Fatalf("prepare = %v, %v; want no handover and no error", got, err)
	}
}

// serveHandoverOn runs serveHandover on one end of a pipe and returns the answer the other end reads.
func serveHandoverOn(t *testing.T, server *Server, body string) response {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer b.Close()
		server.serveHandover(b, json.RawMessage(body))
	}()
	var resp response
	if err := readMessage(a, &resp); err != nil {
		t.Fatal(err)
	}
	<-done
	return resp
}

func TestHandoverRefusedAtAStoppedServer(t *testing.T) {
	cfg := testConfig(t)
	log := attachHooks(&cfg)
	server, err := NewServer(cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)}, false)
	if err != nil {
		t.Fatal(err)
	}
	server.stop(LockCommand)
	resp := serveHandoverOn(t, server, `{"phase":"prepare","release":{}}`)
	if resp.Error != codeHandoverLocked {
		t.Fatalf("answer = %q, want %q", resp.Error, codeHandoverLocked)
	}
	err = answerError(resp.Error)
	var he *HandoverError
	if !errors.As(err, &he) || he.Reason != HandoverLocked || !errors.Is(err, ErrHandover) || !errors.Is(err, ErrNotRunning) {
		t.Fatalf("client error = %v, want ErrHandover with reason %q", err, HandoverLocked)
	}
	if len(log.handovers) != 0 {
		t.Fatal("OnHandover ran for a process that was locking")
	}
	if server.lock != (LockEvent{Reason: LockCommand}) {
		t.Fatalf("the first lock reason changed: %+v", server.lock)
	}
}

func TestHandoverRequestThatCannotBeRead(t *testing.T) {
	for name, body := range map[string]string{
		"wrong phase":     `{"phase":"commit","release":{}}`,
		"no release":      `{"phase":"prepare"}`,
		"not a request":   `"x"`,
		"unknown phase":   `{"phase":"other","release":{}}`,
		"no phase at all": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t)
			log := attachHooks(&cfg)
			server, err := NewServer(cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)}, false)
			if err != nil {
				t.Fatal(err)
			}
			resp := serveHandoverOn(t, server, body)
			if resp.Error != codeHandoverBadRequest {
				t.Fatalf("answer = %q, want %q", resp.Error, codeHandoverBadRequest)
			}
			if !server.stopping() || server.lock != (LockEvent{Reason: LockHandoverFailed, Handover: HandoverBadRequest}) {
				t.Fatalf("lock = %+v, stopping %v; want the handover to lock", server.lock, server.stopping())
			}
			if result := <-log.handovers; result != (HandoverResult{Reason: HandoverBadRequest}) {
				t.Fatalf("OnHandover = %+v", result)
			}
		})
	}
}
