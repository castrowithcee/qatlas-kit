package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/vault"
)

// fakeClock is a settable clock for the expiry tests.
type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time { return c.at }

func limitedAdmin(t *testing.T, v *vault.Vault, valid time.Duration) (Admin, *fakeClock) {
	t.Helper()
	person(t, true, testPassphrase)
	clock := &fakeClock{at: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	old := now
	now = clock.now
	t.Cleanup(func() { now = old })
	a, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt, Valid: valid})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	return a, clock
}

// writers runs both ways to add approvals with the same Admin.
func writers(t *testing.T, s *Store) map[string]func(Admin) error {
	t.Helper()
	data, _ := json.Marshal(exampleState)
	return map[string]func(Admin) error{
		"Approve": func(a Admin) error {
			return s.Approve(a, Item{Name: "new", State: exampleState, Shown: fingerprint(t, exampleState)})
		},
		"Adopt": func(a Admin) error {
			return s.Adopt(a, map[string]Approval{"new": {
				Fingerprint: fingerprint(t, exampleState), State: data, Approved: time.Now()}})
		},
	}
}

func TestAdminExpiryRefusedWithoutWriting(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "seed", exampleState)
	foreignVault, _ := encryptedVault(t)
	expired, clock := limitedAdmin(t, v, time.Minute)
	clock.at = clock.at.Add(time.Minute) // the limit itself is already expired
	cases := []struct {
		name  string
		admin Admin
		want  error
	}{
		{"expired", expired, ErrAdminExpired},
		{"zero", Admin{}, ErrNotAdmin},
		{"foreign vault", admin(t, foreignVault), ErrNotAdmin},
	}
	for way, write := range writers(t, s) {
		for _, c := range cases {
			before := sealedBytes(t, v)
			if err := write(c.admin); !errors.Is(err, c.want) {
				t.Fatalf("%s with %s Admin error = %v, want %v", way, c.name, err, c.want)
			}
			if !bytes.Equal(before, sealedBytes(t, v)) {
				t.Fatalf("%s with %s Admin wrote the approvals file", way, c.name)
			}
		}
	}
}

func TestAdminWithinValidityAndAfterExtend(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	for way, write := range writers(t, s) {
		a, clock := limitedAdmin(t, v, time.Minute)
		clock.at = clock.at.Add(59 * time.Second)
		if err := write(a); err != nil {
			t.Fatalf("%s within the validity error = %v", way, err)
		}
		extended, err := a.Extend(time.Hour)
		if err != nil {
			t.Fatalf("Extend() error = %v", err)
		}
		clock.at = clock.at.Add(30 * time.Minute) // past the first limit, within the new one
		if err := write(extended); err != nil {
			t.Fatalf("%s after Extend error = %v", way, err)
		}
		if err := write(a); !errors.Is(err, ErrAdminExpired) {
			t.Fatalf("%s with the unextended copy error = %v, want ErrAdminExpired", way, err)
		}
		clock.at = clock.at.Add(time.Hour)
		if err := write(extended); !errors.Is(err, ErrAdminExpired) {
			t.Fatalf("%s after the extended limit error = %v, want ErrAdminExpired", way, err)
		}
	}
}

func TestAdminWithoutValidityNeverExpires(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a, clock := limitedAdmin(t, v, 0)
	clock.at = clock.at.Add(1000 * time.Hour)
	for way, write := range writers(t, s) {
		if err := write(a); err != nil {
			t.Fatalf("%s with an Admin without validity error = %v", way, err)
		}
	}
	if _, err := a.Extend(time.Hour); err == nil {
		t.Fatal("Extend() of an Admin without a limit succeeded")
	}
}

func TestExtendRefusesExpiredZeroAndBadDuration(t *testing.T) {
	v, _ := encryptedVault(t)
	a, clock := limitedAdmin(t, v, time.Minute)
	if _, err := (Admin{}).Extend(time.Hour); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("Extend() of a zero Admin error = %v, want ErrNotAdmin", err)
	}
	for _, d := range []time.Duration{0, -time.Hour} {
		if _, err := a.Extend(d); err == nil {
			t.Fatalf("Extend(%v) succeeded", d)
		}
	}
	clock.at = clock.at.Add(time.Minute)
	if _, err := a.Extend(time.Hour); !errors.Is(err, ErrAdminExpired) {
		t.Fatalf("Extend() of an expired Admin error = %v, want ErrAdminExpired", err)
	}
}

func TestAuthorizeRefusesNegativeValidity(t *testing.T) {
	v, _ := encryptedVault(t)
	reads := person(t, true, testPassphrase)
	if _, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt, Valid: -time.Second}); err == nil {
		t.Fatal("Authorize() with a negative validity succeeded")
	}
	if *reads != 0 {
		t.Fatal("Authorize() asked for the passphrase with a negative validity")
	}
}

// Admin stays comparable, so existing callers that compare it keep compiling.
var _ = Admin{} == Admin{}
