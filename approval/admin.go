package approval

import (
	"errors"
	"fmt"
	"time"

	"github.com/castrowithcee/qatlas-kit/vault"
)

// ErrAgent reports an approval asked for by an agent. Only a person approves.
var ErrAgent = errors.New("an agent never approves: a person approves at a terminal")

// ErrNotAdmin reports a change of the approvals without the admin proof of a person for this vault.
var ErrNotAdmin = errors.New("changing the approvals needs the admin proof of a person for this vault")

// ErrAdminExpired reports an admin proof whose validity has run out. A new Authorize is the only way back.
var ErrAdminExpired = errors.New("the admin proof of a person has expired")

// Test seams; a real run keeps the vault's own terminal rules.
var (
	interactive    = vault.Interactive
	readPassphrase = vault.ReadPassphrase
	now            = time.Now
)

// AuthorizeOptions describe how Authorize asks for the passphrase.
type AuthorizeOptions struct {
	// Agent is true while the application runs on behalf of an agent, such as in its agent mode. Authorize
	// then refuses before it asks for anything.
	Agent bool
	// Prompt is the text shown before the passphrase is read. It must not be empty.
	Prompt string
	// Dialog reads the passphrase in a masked dialog of a terminal user interface that runs on this
	// process's terminal. Nil reads it with vault.ReadPassphrase from the controlling terminal. Either way
	// Authorize first checks for a terminal and then verifies the passphrase against the vault.
	Dialog func(prompt string) (string, error)
	// PersonChannel is the one way to authorize without a terminal: the application delivers the passphrase
	// through a channel of its own, such as a form of its web interface, and thereby guarantees that the
	// passphrase comes from a person and not from an agent or a stored secret. Authorize then skips only
	// the terminal check; the passphrase is verified against the vault as always. Nil keeps the terminal
	// rules. Setting it together with Dialog is an error.
	PersonChannel func(prompt string) (string, error)
	// Valid limits how long the Admin is accepted by Approve and Adopt, counted from Authorize. Zero means
	// no limit, as long as the application keeps the Admin. A negative value is an error.
	Valid time.Duration
}

// Admin proves that a person typed the passphrase of one vault, at this process's terminal or through the
// application's PersonChannel. Only Authorize creates one; the zero value proves nothing. The application
// keeps it no longer than the command or the admin session it was asked for.
//
// An Admin with a validity expires; Approve and Adopt then refuse it with ErrAdminExpired. Extend renews
// one that has not expired yet.
type Admin struct {
	vault   *vault.Vault
	expires time.Time // zero: no limit
}

// expired reports whether a limited Admin has run out.
func (a Admin) expired() bool {
	return !a.expires.IsZero() && !now().Before(a.expires)
}

// Extend returns the Admin with a validity of d counted from now. It asks for no passphrase, so it only
// renews a proof that is still valid: a zero Admin and an expired one fail (ErrNotAdmin, ErrAdminExpired),
// and so does an Admin without a limit, which has nothing to extend. d must be positive.
func (a Admin) Extend(d time.Duration) (Admin, error) {
	if a.vault == nil {
		return Admin{}, ErrNotAdmin
	}
	if d <= 0 {
		return Admin{}, errors.New("approval: the validity must be positive")
	}
	if a.expires.IsZero() {
		return Admin{}, errors.New("approval: an admin proof without a limit has nothing to extend")
	}
	if a.expired() {
		return Admin{}, ErrAdminExpired
	}
	a.expires = now().Add(d)
	return a, nil
}

// Authorize asks the person at the terminal for the passphrase of the encrypted vault v and returns the
// proof that it was typed. It refuses with ErrAgent for an agent and with vault.ErrNoTerminal when no
// standard stream is a terminal, both before it asks. A vault that is unlocked already is no proof, so its
// passphrase is verified all the same; a locked vault is unlocked with it, so its approvals can be
// written. A wrong passphrase fails with vault.ErrWrongPassphrase, an unencrypted vault with
// vault.ErrNotEncrypted, since it holds no approvals.
func Authorize(v *vault.Vault, opts AuthorizeOptions) (Admin, error) {
	if v == nil {
		return Admin{}, errors.New("approval: no vault")
	}
	if opts.Prompt == "" {
		return Admin{}, errors.New("approval: no passphrase prompt")
	}
	if opts.Valid < 0 {
		return Admin{}, errors.New("approval: the validity must not be negative")
	}
	if opts.Agent {
		return Admin{}, ErrAgent
	}
	if opts.PersonChannel != nil && opts.Dialog != nil {
		return Admin{}, errors.New("approval: Dialog and PersonChannel are set together")
	}
	if opts.PersonChannel == nil && !interactive() {
		return Admin{}, vault.ErrNoTerminal
	}
	state, err := v.State()
	if err != nil {
		return Admin{}, err
	}
	if err := encrypted(state); err != nil {
		return Admin{}, err
	}
	read := opts.Dialog
	if opts.PersonChannel != nil {
		read = opts.PersonChannel
	} else if read == nil {
		read = readPassphrase
	}
	passphrase, err := read(opts.Prompt)
	if err != nil {
		if errors.Is(err, vault.ErrNoTerminal) {
			return Admin{}, err
		}
		return Admin{}, fmt.Errorf("approval: cannot read the passphrase: %w", err)
	}
	if passphrase == "" {
		return Admin{}, vault.ErrEmptyPassphrase
	}
	if state == vault.StateLocked {
		_, err = v.Unlock(passphrase)
	} else {
		err = v.VerifyPassphrase(passphrase)
	}
	if err != nil {
		return Admin{}, err
	}
	admin := Admin{vault: v}
	if opts.Valid > 0 {
		admin.expires = now().Add(opts.Valid)
	}
	return admin, nil
}

// encrypted reports the error for a vault state that holds no approvals.
func encrypted(state vault.State) error {
	switch state {
	case vault.StateAbsent:
		return vault.ErrAbsent
	case vault.StateUnencrypted:
		return vault.ErrNotEncrypted
	}
	return nil
}
