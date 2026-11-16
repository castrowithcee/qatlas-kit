package approval

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/castrowithcee/qatlas-kit/vault"
)

// ErrNoDelegation reports a delegated proof that is missing, incomplete, or made for another vault.
var ErrNoDelegation = errors.New("approving needs a complete delegated proof for this vault")

// ErrDelegationExpired reports a delegated proof whose time has run out.
var ErrDelegationExpired = errors.New("the delegated proof has expired")

// ErrNotCovered reports a state that the delegated proof does not cover.
var ErrNotCovered = errors.New("the delegated proof does not cover this state")

// DelegatedProof lets an application approve, in place of an Admin, on a proof that a person gave earlier,
// such as a token that person created with an Admin. Store.ApproveDelegated and Store.AdoptDelegated take
// it. The kit checks the vault binding, the expiry and the result of Covers before it writes. It cannot
// check where the proof comes from or whether it is genuine: the application guarantees that it was
// established by a person and not by an agent, and that it is presented only by whom it was given to.
// The zero value proves nothing, and so does a proof that lacks a field.
type DelegatedProof struct {
	// Vault is the vault the proof holds for. It must be the vault of the store.
	Vault *vault.Vault
	// Expires is when the proof ends. Exactly one of Expires and Unlimited must be set.
	Expires time.Time
	// Unlimited states explicitly that the proof has no end.
	Unlimited bool
	// Covers tells whether the proof covers approving the state under name, as the canonical JSON the
	// approval would hold. It is called for every item before anything is written. false refuses with
	// ErrNotCovered; an error aborts and is passed on.
	Covers func(name string, state json.RawMessage) (bool, error)
}

// coverage is the check of a delegated proof's Covers; nil covers everything, as an Admin does.
type coverage func(name string, state json.RawMessage) (bool, error)

func (c coverage) check(name string, state json.RawMessage) error {
	if c == nil {
		return nil
	}
	ok, err := c(name, state)
	if err != nil {
		return fmt.Errorf("approval: %s: %w", name, err)
	}
	if !ok {
		return fmt.Errorf("%s: %w", name, ErrNotCovered)
	}
	return nil
}

// delegated refuses a proof that is incomplete, for another vault or expired.
func (s *Store) delegated(p DelegatedProof) (coverage, error) {
	if p.Vault == nil || p.Vault != s.opts.Vault || p.Covers == nil || p.Unlimited == !p.Expires.IsZero() {
		return nil, ErrNoDelegation
	}
	if !p.Unlimited && !now().Before(p.Expires) {
		return nil, ErrDelegationExpired
	}
	return p.Covers, nil
}

// ApproveDelegated is Approve on a delegated proof instead of an Admin. Every other rule of Approve holds.
func (s *Store) ApproveDelegated(proof DelegatedProof, items ...Item) error {
	covers, err := s.delegated(proof)
	if err != nil {
		return err
	}
	return s.approve(covers, items)
}

// AdoptDelegated is Adopt on a delegated proof instead of an Admin. Every other rule of Adopt holds.
func (s *Store) AdoptDelegated(proof DelegatedProof, approvals map[string]Approval) error {
	covers, err := s.delegated(proof)
	if err != nil {
		return err
	}
	return s.adopt(covers, approvals)
}
