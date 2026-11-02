package approval

import (
	"encoding/json"
	"testing"
	"time"
)

// The fingerprints below were computed by an existing application that predates this package, with its own
// code for the canonical state and the hash and only its label replaced by existingLabel, from invented
// values. Matching them proves that its approvals stay valid when it hands over its label and its state.
const (
	existingLabel       = "example connection approval v1\x00"
	existingFull        = "66a58fcd63de8b2711d4bde1726a7903fcbf547f73c8b4027f3d6e599fa0a107"
	existingMinimal     = "205c4bfb36973560eb750d73f01f98466f7c693a7bbaa947f54e3637b157205b"
	existingFullEntryID = "0123456789abcdef0123456789abcdef"
	existingMinEntryID  = "fedcba9876543210fedcba9876543210"
)

type existingForward struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
}

// existingState is the canonical state of the existing application, fields in its order, lists sorted.
type existingState struct {
	CredentialID string            `json:"credential_id"`
	Credential   string            `json:"credential"`
	Provider     string            `json:"provider"`
	Origin       string            `json:"origin"`
	Effects      []string          `json:"permissions"`
	Targets      []string          `json:"targets"`
	Tools        []string          `json:"tools"`
	Exclude      []string          `json:"exclude,omitempty"`
	Paths        []string          `json:"paths,omitempty"`
	FilesRead    []string          `json:"files_read,omitempty"`
	FilesWrite   []string          `json:"files_write,omitempty"`
	Forward      []existingForward `json:"forward,omitempty"`
	Disabled     bool              `json:"disabled,omitempty"`
	ActiveUntil  string            `json:"active_until,omitempty"`
}

var (
	existingFullState = existingState{
		CredentialID: existingFullEntryID, Credential: "example-cred", Provider: "example-provider",
		Origin: "https://api.example.test/v1", Effects: []string{"read", "write"}, Targets: []string{"team-a", "team-b"},
		Tools: []string{"items.get", "items.list"}, Exclude: []string{"secret-folder"},
		Paths: []string{"/home/example/project"}, FilesRead: []string{"/home/example/in"},
		FilesWrite: []string{"/home/example/out"},
		Forward:    []existingForward{{Name: "example-forward", Fields: []string{"login", "token"}}},
		Disabled:   true, ActiveUntil: "2030-01-02T03:04:05Z",
	}
	// The minimal state has no tools list, which is not the same as an empty one, and empty effects and targets.
	existingMinimalState = existingState{
		CredentialID: existingMinEntryID, Credential: "example-cred", Provider: "example-provider",
		Origin: "https://api.example.test", Effects: []string{}, Targets: []string{},
	}
)

func TestExistingFingerprintsMatch(t *testing.T) {
	for want, state := range map[string]existingState{existingFull: existingFullState, existingMinimal: existingMinimalState} {
		if got, err := Fingerprint(existingLabel, state); err != nil || got != want {
			t.Fatalf("Fingerprint() = %q, %v, want the existing %q", got, err, want)
		}
	}
}

func TestExistingApprovalsStayValid(t *testing.T) {
	v, _ := encryptedVault(t)
	s, err := New(Options{Vault: v, File: testFile, FingerprintLabel: existingLabel, MACLabel: testMACLabel,
		LockName: "approvals.lock"})
	if err != nil {
		t.Fatal(err)
	}
	// The existing application kept its approvals elsewhere: the fingerprint and the state it was given for.
	existing := map[string]Approval{}
	approved := time.Date(2029, 5, 6, 7, 8, 9, 0, time.UTC)
	for name, record := range map[string]struct {
		fingerprint string
		state       existingState
	}{"example-full": {existingFull, existingFullState}, "example-min": {existingMinimal, existingMinimalState}} {
		state, err := json.Marshal(record.state)
		if err != nil {
			t.Fatal(err)
		}
		existing[name] = Approval{Fingerprint: record.fingerprint, State: state, Approved: approved}
	}
	if err := s.Adopt(admin(t, v), existing); err != nil {
		t.Fatalf("Adopt() of the existing approvals error = %v", err)
	}
	if err := s.Check("example-full", existingFullState); err != nil {
		t.Fatalf("Check() of an existing approval error = %v", err)
	}
	if err := s.Check("example-min", existingMinimalState); err != nil {
		t.Fatalf("Check() of an existing approval error = %v", err)
	}
	approvals, err := s.Approvals()
	if err != nil || approvals["example-full"].Fingerprint != existingFull || !approvals["example-full"].Approved.Equal(approved) {
		t.Fatalf("Approvals() = %+v, %v, want the existing fingerprint and time", approvals, err)
	}

	// A replaced credential entry is another state and needs a new approval, as before.
	replaced := existingFullState
	replaced.CredentialID = "00000000000000000000000000000002"
	if err := s.Check("example-full", replaced); err == nil {
		t.Fatal("Check() of a state with another credential entry succeeded")
	}
}
