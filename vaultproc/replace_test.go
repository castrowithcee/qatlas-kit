//go:build linux || darwin || windows

package vaultproc

import (
	"context"
	"errors"
	"testing"

	"github.com/castrowithcee/qatlas-kit/selfupdate"
)

func TestReplacementLocksOnRollback(t *testing.T) {
	r := serve(t, testConfig(t), State{Identity: newIdentity(t), Document: []byte(testDocument)})
	replacement := NewReplacement(r.client)
	if err := replacement.BeforeReplace(context.Background(), selfupdate.ReleaseFiles{Rollback: true}); err != nil {
		t.Fatal(err)
	}
	replacement.AfterReplace(context.Background(), selfupdate.ReleaseFiles{Rollback: true})
	if result := replacement.Finish(); result.Outcome != OutcomeLocked || result.Err != nil || result.Reason != HandoverRollback {
		t.Fatalf("rollback = %+v, want locked with reason %q", result, HandoverRollback)
	}
	awaitStopped(t, r)
}

func TestReplacementWithoutProcess(t *testing.T) {
	client := &Client{Endpoint: testEndpoint(t), Recipient: newIdentity(t).Recipient().String(), Version: 1, ProofLabel: testLabel}
	replacement := NewReplacement(client)
	if err := replacement.BeforeReplace(context.Background(), selfupdate.ReleaseFiles{}); err != nil {
		t.Fatal(err)
	}
	replacement.AfterReplace(context.Background(), selfupdate.ReleaseFiles{})
	if result := replacement.Finish(); result.Outcome != OutcomeNone || result.Err != nil {
		t.Fatalf("no process = %+v", result)
	}
}

func TestHandoverWithoutConfigLocks(t *testing.T) {
	r := serve(t, testConfig(t), State{Identity: newIdentity(t), Document: []byte(testDocument)})
	h, err := r.client.PrepareHandover(context.Background(), selfupdate.ReleaseFiles{
		Checksums: "/example/checksums.txt", Signature: "/example/checksums.txt.sig", Archive: "/example/tool.tar.gz",
		ArchiveName: "tool.tar.gz"})
	if h != nil || !errors.Is(err, ErrHandover) {
		t.Fatalf("PrepareHandover = %v, %v; want ErrHandover", h, err)
	}
	awaitStopped(t, r)
}

// TestReplacementSettlesWithAReason uses no client: a settle that asked the process to lock would panic.
func TestReplacementSettlesWithAReason(t *testing.T) {
	ctx := context.Background()
	for name, test := range map[string]struct {
		err  error
		want ReplacementResult
	}{
		"bad request": {err: &HandoverError{Reason: HandoverBadRequest},
			want: ReplacementResult{Outcome: OutcomeLocked, Reason: HandoverBadRequest}},
		"locked": {err: &HandoverError{Reason: HandoverLocked},
			want: ReplacementResult{Outcome: OutcomeLocked, Reason: HandoverLocked}},
		"too large": {err: &HandoverError{Reason: HandoverTooLarge},
			want: ReplacementResult{Outcome: OutcomeLocked, Reason: HandoverTooLarge}},
		"busy": {err: &HandoverError{Reason: HandoverBusy},
			want: ReplacementResult{Outcome: OutcomeLocked, Reason: HandoverBusy}},
		"declined":    {want: ReplacementResult{Outcome: OutcomeLocked, Reason: HandoverDeclined}},
		"not running": {err: ErrNotRunning},
	} {
		t.Run(name, func(t *testing.T) {
			r := &Replacement{}
			r.settle(ctx, nil, test.err)
			got := r.Finish()
			if test.err != nil && test.want.Outcome == OutcomeLocked {
				if !errors.Is(got.Err, ErrHandover) || got.Err != test.err {
					t.Fatalf("Err = %v, want %v", got.Err, test.err)
				}
				got.Err = nil
			}
			if got != test.want {
				t.Fatalf("result = %+v, want %+v", got, test.want)
			}
		})
	}
}
