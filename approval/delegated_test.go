package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var proofTime = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

func fixedClock(t *testing.T) {
	t.Helper()
	old := now
	now = func() time.Time { return proofTime }
	t.Cleanup(func() { now = old })
}

func coversAll(string, json.RawMessage) (bool, error) { return true, nil }

// delegatedWays runs both ways to add approvals with the same proof.
func delegatedWays(t *testing.T, s *Store) map[string]func(DelegatedProof) error {
	t.Helper()
	data, _ := json.Marshal(exampleState)
	return map[string]func(DelegatedProof) error{
		"ApproveDelegated": func(p DelegatedProof) error {
			return s.ApproveDelegated(p, Item{Name: "new", State: exampleState, Shown: fingerprint(t, exampleState)})
		},
		"AdoptDelegated": func(p DelegatedProof) error {
			return s.AdoptDelegated(p, map[string]Approval{"new": {
				Fingerprint: fingerprint(t, exampleState), State: data, Approved: time.Now()}})
		},
	}
}

func TestDelegatedProofRefusedWithoutWriting(t *testing.T) {
	fixedClock(t)
	v, _ := encryptedVault(t)
	foreign, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "seed", exampleState)
	boom := errors.New("covering failed")
	cases := []struct {
		name  string
		proof DelegatedProof
		check func(error) bool
	}{
		{"zero", DelegatedProof{}, func(e error) bool { return errors.Is(e, ErrNoDelegation) }},
		{"expired", DelegatedProof{Vault: v, Expires: proofTime, Covers: coversAll},
			func(e error) bool { return errors.Is(e, ErrDelegationExpired) }},
		{"no expiry", DelegatedProof{Vault: v, Covers: coversAll},
			func(e error) bool { return errors.Is(e, ErrNoDelegation) }},
		{"both expiry and unlimited", DelegatedProof{Vault: v, Unlimited: true, Expires: proofTime.Add(time.Hour), Covers: coversAll},
			func(e error) bool { return errors.Is(e, ErrNoDelegation) }},
		{"foreign vault", DelegatedProof{Vault: foreign, Unlimited: true, Covers: coversAll},
			func(e error) bool { return errors.Is(e, ErrNoDelegation) }},
		{"no vault", DelegatedProof{Unlimited: true, Covers: coversAll},
			func(e error) bool { return errors.Is(e, ErrNoDelegation) }},
		{"no covers", DelegatedProof{Vault: v, Unlimited: true},
			func(e error) bool { return errors.Is(e, ErrNoDelegation) }},
		{"does not cover", DelegatedProof{Vault: v, Unlimited: true,
			Covers: func(string, json.RawMessage) (bool, error) { return false, nil }},
			func(e error) bool { return errors.Is(e, ErrNotCovered) }},
		{"covering fails", DelegatedProof{Vault: v, Unlimited: true,
			Covers: func(string, json.RawMessage) (bool, error) { return false, boom }},
			func(e error) bool { return errors.Is(e, boom) && !errors.Is(e, ErrNotCovered) }},
		{"covering fails but says true", DelegatedProof{Vault: v, Unlimited: true,
			Covers: func(string, json.RawMessage) (bool, error) { return true, boom }},
			func(e error) bool { return errors.Is(e, boom) }},
	}
	for way, write := range delegatedWays(t, s) {
		for _, c := range cases {
			before := sealedBytes(t, v)
			if err := write(c.proof); err == nil || !c.check(err) {
				t.Fatalf("%s with %s proof error = %v", way, c.name, err)
			}
			if !bytes.Equal(before, sealedBytes(t, v)) {
				t.Fatalf("%s with %s proof wrote the approvals file", way, c.name)
			}
		}
	}
}

func TestDelegatedProofSucceedsAndCoversSeesState(t *testing.T) {
	fixedClock(t)
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	state, _ := json.Marshal(exampleState)
	for way, write := range delegatedWays(t, s) {
		var gotName string
		var gotState json.RawMessage
		covers := func(name string, st json.RawMessage) (bool, error) {
			gotName, gotState = name, st
			return true, nil
		}
		for _, p := range []DelegatedProof{
			{Vault: v, Expires: proofTime.Add(time.Second), Covers: covers},
			{Vault: v, Unlimited: true, Covers: covers},
		} {
			gotName, gotState = "", nil
			if err := write(p); err != nil {
				t.Fatalf("%s error = %v", way, err)
			}
			if gotName != "new" || !bytes.Equal(gotState, state) {
				t.Fatalf("%s: Covers saw %q %s, want %q %s", way, gotName, gotState, "new", state)
			}
		}
		if err := s.Check("new", exampleState); err != nil {
			t.Fatalf("%s: Check() error = %v", way, err)
		}
	}
}

func TestDelegatedKeepsOtherRulesOfApprove(t *testing.T) {
	fixedClock(t)
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	p := DelegatedProof{Vault: v, Unlimited: true, Covers: coversAll}
	if err := s.ApproveDelegated(p, Item{Name: "one", State: exampleState, Shown: "stale"}); !errors.Is(err, ErrChanged) {
		t.Fatalf("ApproveDelegated() with a stale fingerprint error = %v, want ErrChanged", err)
	}
}
