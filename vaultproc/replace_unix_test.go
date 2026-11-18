//go:build linux || darwin

package vaultproc

import (
	"context"
	"errors"
	"testing"

	"filippo.io/age"
)

func TestReplacementResultOfRefusedHandover(t *testing.T) {
	h := newHandover(t, func(h *handoverSetup) { h.cfg.Handover.HandsOver = func(*age.X25519Identity) bool { return false } })
	replacement := NewReplacement(h.client)
	if err := replacement.BeforeReplace(context.Background(), h.files); err != nil {
		t.Fatal(err)
	}
	replacement.AfterReplace(context.Background(), h.files)
	if result := replacement.Finish(); result != (ReplacementResult{Outcome: OutcomeLocked, Reason: HandoverDeclined}) {
		t.Fatalf("declined = %+v", result)
	}
	awaitStopped(t, h.running)
}

func TestReplacementResultOfTooLargeArchive(t *testing.T) {
	h := newHandover(t, func(h *handoverSetup) { h.cfg.Handover.MaxFileSize = 16 })
	replacement := NewReplacement(h.client)
	if err := replacement.BeforeReplace(context.Background(), h.files); err != nil {
		t.Fatal(err)
	}
	result := replacement.Finish()
	var he *HandoverError
	if result.Outcome != OutcomeLocked || result.Reason != HandoverTooLarge || !errors.As(result.Err, &he) || he.Reason != HandoverTooLarge {
		t.Fatalf("too large = %+v", result)
	}
	awaitStopped(t, h.running)
}
