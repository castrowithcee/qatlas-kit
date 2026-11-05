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
	if result := replacement.Finish(); result.Outcome != OutcomeLocked || result.Err != nil {
		t.Fatalf("rollback = %+v, want locked", result)
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
