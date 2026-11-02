package vaultproc

import (
	"encoding/json"
	"errors"
	"time"

	"filippo.io/age"
)

// MaxState bounds the state a vault process reads from its handover.
const MaxState = 16 << 20

// State is what a vault process holds, as it is handed to the process at its start and to its successor.
// Identity and Document are as secret as the vault itself.
type State struct {
	// Identity is the vault's own key; it answers every client's challenge.
	Identity *age.X25519Identity
	// Document is the application's decrypted document, as vault.Snapshot holds it.
	Document []byte
	// UnlockedAt is when the vault was unlocked. Zero means now. A successor keeps its predecessor's.
	UnlockedAt time.Time
	// LocksAt is the latest time the process locks at unless a request touches it first. It is zero at the
	// first start; a successor must have its predecessor's, so a handover never extends an unlock.
	LocksAt time.Time
	// MaxUnlock is the longest the vault stays unlocked from UnlockedAt; zero means no limit. A successor
	// takes the shorter of its predecessor's and its own Config.MaxUnlock.
	MaxUnlock time.Duration
}

type wireState struct {
	V          int           `json:"v"`
	Identity   []byte        `json:"identity"`
	Document   []byte        `json:"document,omitempty"`
	UnlockedAt time.Time     `json:"unlocked_at"`
	LocksAt    *time.Time    `json:"locks_at,omitempty"`
	MaxUnlock  time.Duration `json:"max_unlock,omitempty"`
}

// encodeState returns the handover bytes of s. The caller clears them.
func encodeState(s State) ([]byte, error) {
	if s.Identity == nil {
		return nil, errors.New("the vault process state has no key")
	}
	w := wireState{V: ProtocolVersion, Identity: []byte(s.Identity.String()), Document: s.Document,
		UnlockedAt: s.UnlockedAt, MaxUnlock: s.MaxUnlock}
	defer clear(w.Identity)
	if !s.LocksAt.IsZero() {
		at := s.LocksAt
		w.LocksAt = &at
	}
	data, err := json.Marshal(w)
	if err != nil {
		return nil, errors.New("cannot encode the vault process state")
	}
	return data, nil
}

// decodeState reads handover bytes. Its errors never quote them. The caller clears the Document.
func decodeState(data []byte) (State, error) {
	var w wireState
	err := json.Unmarshal(data, &w)
	defer clear(w.Identity)
	if err != nil || w.V != ProtocolVersion || len(w.Identity) == 0 || w.MaxUnlock < 0 {
		clear(w.Document)
		return State{}, errors.New("the vault process state cannot be read")
	}
	id, err := age.ParseX25519Identity(string(w.Identity))
	if err != nil {
		clear(w.Document)
		return State{}, errors.New("the vault key handed over cannot be read")
	}
	s := State{Identity: id, Document: w.Document, UnlockedAt: w.UnlockedAt, MaxUnlock: w.MaxUnlock}
	if w.LocksAt != nil {
		s.LocksAt = *w.LocksAt
	}
	return s, nil
}
