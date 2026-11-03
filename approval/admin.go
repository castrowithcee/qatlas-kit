package approval

import (
	"errors"
	"fmt"

	"github.com/castrowithcee/qatlas-kit/vault"
)

// ErrAgent reports an approval asked for by an agent. Only a person approves.
var ErrAgent = errors.New("an agent never approves: a person approves at a terminal")

// ErrNotAdmin reports a change of the approvals without the admin proof of a person for this vault.
var ErrNotAdmin = errors.New("changing the approvals needs the admin proof of a person for this vault")

// Test seams; a real run keeps the vault's own terminal rules.
var (
	interactive    = vault.Interactive
	readPassphrase = vault.ReadPassphrase
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
}

// Admin proves that a person at this process's terminal typed the passphrase of one vault. Only Authorize
// creates one; the zero value proves nothing. The application keeps it no longer than the command or the
// admin session it was asked for.
type Admin struct {
	vault *vault.Vault
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
	if opts.Agent {
		return Admin{}, ErrAgent
	}
	if !interactive() {
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
	if read == nil {
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
	return Admin{vault: v}, nil
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
