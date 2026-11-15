package approval

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/vault"
)

func channel(reads *int, passphrase string) func(string) (string, error) {
	return func(string) (string, error) { *reads++; return passphrase, nil }
}

func TestAuthorizePersonChannelRefusals(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "seed", exampleState)
	before := sealedBytes(t, v)
	reads := person(t, false, testPassphrase)
	cases := []struct {
		name string
		opts AuthorizeOptions
		want error
	}{
		{"agent with channel", AuthorizeOptions{Agent: true, Prompt: testPrompt, PersonChannel: channel(reads, testPassphrase)}, ErrAgent},
		{"no terminal, no channel", AuthorizeOptions{Prompt: testPrompt, Dialog: channel(reads, testPassphrase)}, vault.ErrNoTerminal},
		{"wrong passphrase", AuthorizeOptions{Prompt: testPrompt, PersonChannel: channel(reads, "wrong passphrase")}, vault.ErrWrongPassphrase},
		{"empty passphrase", AuthorizeOptions{Prompt: testPrompt, PersonChannel: channel(reads, "")}, vault.ErrEmptyPassphrase},
	}
	for _, c := range cases {
		a, err := Authorize(v, c.opts)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: error = %v, want %v", c.name, err, c.want)
		}
		if a != (Admin{}) {
			t.Fatalf("%s: returned a non-zero Admin", c.name)
		}
	}
	if *reads != 2 { // only the wrong and the empty passphrase were read
		t.Fatalf("passphrase reads = %d, want 2", *reads)
	}
	if !bytes.Equal(before, sealedBytes(t, v)) {
		t.Fatal("a refused Authorize wrote the approvals file")
	}
}

func TestAuthorizeAgentWithTerminalStillRefused(t *testing.T) {
	v, _ := encryptedVault(t)
	reads := person(t, true, testPassphrase)
	if _, err := Authorize(v, AuthorizeOptions{Agent: true, Prompt: testPrompt}); !errors.Is(err, ErrAgent) {
		t.Fatalf("error = %v, want ErrAgent", err)
	}
	if *reads != 0 {
		t.Fatal("asked an agent for the passphrase")
	}
}

func TestAuthorizeRefusesDialogWithPersonChannel(t *testing.T) {
	v, _ := encryptedVault(t)
	reads := person(t, true, testPassphrase)
	f := channel(reads, testPassphrase)
	if _, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt, Dialog: f, PersonChannel: f}); err == nil {
		t.Fatal("Authorize() with Dialog and PersonChannel succeeded")
	}
	if *reads != 0 {
		t.Fatal("Authorize() read a passphrase with both set")
	}
}

func TestAuthorizeRefusesUnencryptedVaultWithPersonChannel(t *testing.T) {
	v, err := vault.New(vaultOptions(filepath.Join(t.TempDir(), "vault")))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.CreateUnencrypted([]byte(`{"schema":1}`)); err != nil {
		t.Fatal(err)
	}
	reads := person(t, false, testPassphrase)
	if _, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt, PersonChannel: channel(reads, testPassphrase)}); err == nil {
		t.Fatal("Authorize() of a vault without approvals succeeded")
	}
	if *reads != 0 {
		t.Fatal("asked for the passphrase of a vault without approvals")
	}
}

func TestAuthorizePersonChannelWithoutTerminal(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	reads := person(t, false, testPassphrase)
	a, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt, PersonChannel: channel(reads, testPassphrase)})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	for way, write := range writers(t, s) {
		if err := write(a); err != nil {
			t.Fatalf("%s with a PersonChannel Admin error = %v", way, err)
		}
	}
	// Valid works the same through the channel.
	clock := &fakeClock{at: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	old := now
	now = clock.now
	t.Cleanup(func() { now = old })
	l, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt, Valid: time.Minute, PersonChannel: channel(reads, testPassphrase)})
	if err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(time.Minute)
	for way, write := range writers(t, s) {
		if err := write(l); !errors.Is(err, ErrAdminExpired) {
			t.Fatalf("%s with an expired channel Admin error = %v, want ErrAdminExpired", way, err)
		}
	}
}
