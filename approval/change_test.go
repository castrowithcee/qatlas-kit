package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/castrowithcee/qatlas-kit/vault"
)

var otherState = testState{Endpoint: "https://other.example.test", Effects: []string{"write"}, Secret: "id-2"}

func sealedBytes(t *testing.T, v *vault.Vault) []byte {
	t.Helper()
	data, err := v.ReadSealed(testFile)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRevokeRemovesOnlyNamedWithoutAdmin(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a := admin(t, v)
	approve(t, s, a, "one", exampleState)
	approve(t, s, a, "two", exampleState)
	approve(t, s, a, "three", otherState)
	n, undo, err := s.Revoke("one", "three", "absent")
	if err != nil || n != 2 || undo == nil {
		t.Fatalf("Revoke() = %d, %v, %v", n, undo != nil, err)
	}
	got, err := s.Approvals()
	if err != nil || len(got) != 1 {
		t.Fatalf("Approvals() = %v, %v, want only two", got, err)
	}
	if err := s.Check("two", exampleState); err != nil {
		t.Fatalf("Check(two) error = %v", err)
	}
}

func TestRevokeOfNothingWritesNothing(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "one", exampleState)
	before := sealedBytes(t, v)
	n, undo, err := s.Revoke("absent")
	if n != 0 || undo != nil || err != nil {
		t.Fatalf("Revoke() = %d, %v, %v", n, undo != nil, err)
	}
	if !bytes.Equal(before, sealedBytes(t, v)) {
		t.Fatal("Revoke() of nothing wrote the file")
	}
}

func TestMoveKeepsStateFingerprintAndTime(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "old", exampleState)
	before, _ := s.Approvals()
	if _, err := s.Move("old", "new"); err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	after, err := s.Approvals()
	if err != nil || len(after) != 1 {
		t.Fatalf("Approvals() = %v, %v", after, err)
	}
	moved, was := after["new"], before["old"]
	if moved.Fingerprint != was.Fingerprint || !moved.Approved.Equal(was.Approved) ||
		string(moved.State) != string(was.State) {
		t.Fatalf("moved approval = %+v, want %+v", moved, was)
	}
	if err := s.Check("old", exampleState); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Check(old) error = %v, want ErrNotApproved", err)
	}
	if err := s.Check("new", exampleState); err != nil {
		t.Fatalf("Check(new) of the same state error = %v", err)
	}
	if err := s.Check("new", otherState); !errors.Is(err, ErrDeviates) {
		t.Fatalf("Check(new) of another state error = %v, want ErrDeviates", err)
	}
}

func TestMoveRefusedWithoutWriting(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a := admin(t, v)
	approve(t, s, a, "one", exampleState)
	approve(t, s, a, "two", otherState)
	before := sealedBytes(t, v)
	cases := []struct {
		name     string
		from, to string
		want     error
	}{
		{"occupied target", "one", "two", ErrExists},
		{"missing source", "absent", "free", ErrNotApproved},
		{"empty source", "", "free", nil},
		{"empty target", "one", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			undo, err := s.Move(c.from, c.to)
			if err == nil || undo != nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("Move() = %v, %v, want %v", undo != nil, err, c.want)
			}
			if !bytes.Equal(before, sealedBytes(t, v)) {
				t.Fatal("a refused Move() wrote the file")
			}
		})
	}
}

func TestMoveOntoItselfWritesNothing(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "one", exampleState)
	before := sealedBytes(t, v)
	if undo, err := s.Move("one", "one"); err != nil || undo != nil {
		t.Fatalf("Move() = %v, %v", undo != nil, err)
	}
	if !bytes.Equal(before, sealedBytes(t, v)) {
		t.Fatal("Move() onto itself wrote the file")
	}
}

func TestUndoRestoresPreviousBytes(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "one", exampleState)
	before := sealedBytes(t, v)
	_, undo, err := s.Revoke("one")
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatalf("Undo() of Revoke error = %v", err)
	}
	if !bytes.Equal(before, sealedBytes(t, v)) {
		t.Fatal("Undo() of Revoke did not restore the previous bytes")
	}
	moveUndo, err := s.Move("one", "two")
	if err != nil {
		t.Fatal(err)
	}
	if err := moveUndo(); err != nil {
		t.Fatalf("Undo() of Move error = %v", err)
	}
	if !bytes.Equal(before, sealedBytes(t, v)) {
		t.Fatal("Undo() of Move did not restore the previous bytes")
	}
	if err := s.Check("one", exampleState); err != nil {
		t.Fatalf("Check() after Undo() error = %v", err)
	}
}

func TestUndoTwiceWritesNothing(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "one", exampleState)
	_, undo, err := s.Revoke("one")
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	restored := sealedBytes(t, v)
	if err := undo(); !errors.Is(err, ErrModified) {
		t.Fatalf("second Undo() error = %v, want ErrModified", err)
	}
	if !bytes.Equal(restored, sealedBytes(t, v)) {
		t.Fatal("second Undo() wrote the file")
	}
}

func TestUndoRefusedAfterForeignChange(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a := admin(t, v)
	approve(t, s, a, "one", exampleState)
	approve(t, s, a, "two", exampleState)
	_, undo, err := s.Revoke("one")
	if err != nil {
		t.Fatal(err)
	}
	approve(t, s, a, "three", otherState)
	foreign := sealedBytes(t, v)
	if err := undo(); !errors.Is(err, ErrModified) {
		t.Fatalf("Undo() after a foreign change error = %v, want ErrModified", err)
	}
	if !bytes.Equal(foreign, sealedBytes(t, v)) {
		t.Fatal("a refused Undo() wrote the file")
	}
	if err := s.Check("one", exampleState); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Check(one) error = %v, want ErrNotApproved", err)
	}
}

func TestUndoRefusedAfterReplacedFile(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "one", exampleState)
	_, undo, err := s.Revoke("one")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.WriteSealed(testFile, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	if err := undo(); !errors.Is(err, ErrModified) {
		t.Fatalf("Undo() of a replaced file error = %v, want ErrModified", err)
	}
	if got := sealedBytes(t, v); string(got) != "not json" {
		t.Fatal("a refused Undo() wrote the file")
	}
}

func TestRevokeAndMoveRefuseDamagedFiles(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	state := `{"endpoint":"https://api.example.test","effects":["read"],"secret_id":"id-1"}`
	good := `{"example":{"fingerprint":"` + fingerprint(t, json.RawMessage(state)) + `","state":` + state +
		`,"approved":"2030-01-02T03:04:05Z"}}`
	bad := `{"example":{"fingerprint":"00","state":{},"approved":"2030-01-02T03:04:05Z"}}`
	cases := map[string]approvalsFile{
		"wrong check value":        signed(t, v, "another label", fileSchema, good),
		"inconsistent fingerprint": signed(t, v, testMACLabel, fileSchema, bad),
	}
	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			writeFile(t, v, file)
			before := sealedBytes(t, v)
			if n, undo, err := s.Revoke("example"); err == nil || n != 0 || undo != nil {
				t.Fatalf("Revoke() = %d, %v, %v, want an error", n, undo != nil, err)
			}
			if undo, err := s.Move("example", "other"); err == nil || undo != nil {
				t.Fatalf("Move() = %v, %v, want an error", undo != nil, err)
			}
			if !bytes.Equal(before, sealedBytes(t, v)) {
				t.Fatal("a refused change wrote the file")
			}
		})
	}
	writeFile(t, v, signed(t, v, testMACLabel, fileSchema, good))
	if _, err := s.Move("example", "other"); err != nil {
		t.Fatalf("Move() of a valid file error = %v", err)
	}
}

func TestApproveAndAdoptStillNeedAdmin(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	item := Item{Name: "one", State: exampleState, Shown: fingerprint(t, exampleState)}
	if err := s.Approve(Admin{}, item); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("Approve() error = %v, want ErrNotAdmin", err)
	}
	if err := s.Adopt(Admin{}, nil); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("Adopt() error = %v, want ErrNotAdmin", err)
	}
}
