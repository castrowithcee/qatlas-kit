package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"filippo.io/age"
)

// Work factors of the scrypt wrapping of the vault key, as the base-2 logarithm of the scrypt cost.
const (
	// DefaultWorkFactor is used when Options.WorkFactor is zero.
	DefaultWorkFactor = 18
	// MaxWorkFactor is the highest work factor age decrypts without raising its own limit.
	MaxWorkFactor = 22
)

// State is what a vault currently is.
type State string

// The states a vault can be in.
const (
	// StateAbsent means the vault has not been created.
	StateAbsent State = "absent"
	// StateUnencrypted means the vault holds its document as plain JSON.
	StateUnencrypted State = "unencrypted"
	// StateLocked means the vault is encrypted and this value does not hold its private key.
	StateLocked State = "locked"
	// StateUnlocked means this value holds the private key and the decrypted document.
	StateUnlocked State = "unlocked"
)

var (
	// ErrAbsent reports an operation that needs a vault which has not been created.
	ErrAbsent = errors.New("the vault does not exist")
	// ErrExists reports an attempt to create a vault that exists already.
	ErrExists = errors.New("the vault exists already")
	// ErrLocked reports an operation that needs the private key, which this value does not hold.
	ErrLocked = errors.New("the vault is locked")
	// ErrEncrypted reports an attempt to encrypt a vault that is encrypted already.
	ErrEncrypted = errors.New("the vault is encrypted already")
	// ErrNotEncrypted reports an operation that needs an encrypted vault.
	ErrNotEncrypted = errors.New("the vault is not encrypted")
	// ErrUnencryptedDisabled reports an unencrypted operation while Files.Plain is empty.
	ErrUnencryptedDisabled = errors.New("the unencrypted vault mode is not enabled")
	// ErrEmptyPassphrase reports a new passphrase of "".
	ErrEmptyPassphrase = errors.New("a passphrase must not be empty")
	// ErrWrongPassphrase reports a passphrase that does not unlock the vault key.
	ErrWrongPassphrase = errors.New("the passphrase does not unlock the vault")
	// ErrKeyMismatch reports a private key that does not belong to the recipient of the vault.
	ErrKeyMismatch = errors.New("the vault key and the vault recipient do not match")
	// ErrDamaged reports a vault file that cannot be decrypted or decoded: it is damaged or was not
	// written for this vault.
	ErrDamaged = errors.New("a vault file is damaged or was not written for this vault")
	// ErrUnsupportedSchema reports a document whose schema version the application does not accept.
	ErrUnsupportedSchema = errors.New("the vault document has an unsupported schema version")
	// ErrPendingUnmerged reports pending entries the vault cannot merge, because no MergeFunc is set or
	// because the operation needs every entry merged.
	ErrPendingUnmerged = errors.New("the vault has pending entries that were not merged")
)

// Files names the files of a vault inside its directory. Every name is a single path element; the
// application chooses them.
type Files struct {
	// Key holds the private key, wrapped with the passphrase.
	Key string
	// Recipient holds the public recipient as age text.
	Recipient string
	// Document holds the document, encrypted to the recipient.
	Document string
	// Pending is the directory of entries added without the passphrase.
	Pending string
	// Plain holds the document of an unencrypted vault. Empty disables the unencrypted mode.
	Plain string
	// Sealed are further files encrypted to the recipient, read and written with ReadSealed and
	// WriteSealed and removed by Decrypt.
	Sealed []string
}

// Pending is one pending entry handed to a MergeFunc.
type Pending struct {
	// Name is the file name in the pending directory.
	Name string
	// Payload is the decrypted entry. It is cleared after the MergeFunc returns.
	Payload []byte
}

// MergeFunc merges pending entries into document, which is nil while the vault has no document file. It
// returns the merged document and the names of the entries it merged; those files are removed once the
// merged document is written. An entry it leaves out stays pending.
type MergeFunc func(document []byte, pending []Pending) (merged []byte, done []string, err error)

// Options configures a vault. The zero value of every optional field is the safe choice.
type Options struct {
	// Dir is the vault directory itself.
	Dir string
	// Files names the vault files. Every name except Plain and Sealed is required.
	Files Files
	// Schemas are the schema versions of the document the application accepts. The document is a JSON
	// object whose integer member "schema" is one of them.
	Schemas []int
	// Merge merges pending entries on unlock. Without it, a vault with pending entries does not unlock.
	Merge MergeFunc
	// WorkFactor is the scrypt work factor of a newly wrapped key. Zero is DefaultWorkFactor. Values below
	// it are accepted only in test binaries, values above MaxWorkFactor never.
	WorkFactor int
}

// Status describes a vault without any secret.
type Status struct {
	State State
	Dir   string
	// Pending is the number of pending entries.
	Pending int
}

// Vault is one vault directory. A Vault is safe for concurrent use.
type Vault struct {
	opts Options

	// rename replaces a file atomically; a test replaces it to simulate an interrupted write.
	rename func(oldpath, newpath string) error

	mu       sync.Mutex
	unlocked bool
	identity *identity
	doc      []byte
}

// New checks opts and returns the vault it describes. The vault does not have to exist.
func New(opts Options) (*Vault, error) {
	if err := opts.check(); err != nil {
		return nil, err
	}
	opts.Files.Sealed = append([]string(nil), opts.Files.Sealed...)
	opts.Schemas = append([]int(nil), opts.Schemas...)
	if opts.WorkFactor == 0 {
		opts.WorkFactor = DefaultWorkFactor
	}
	return &Vault{opts: opts, rename: os.Rename}, nil
}

func (o Options) check() error {
	if o.Dir == "" {
		return errors.New("vault: no directory")
	}
	names := []string{o.Files.Key, o.Files.Recipient, o.Files.Document, o.Files.Pending}
	if o.Files.Plain != "" {
		names = append(names, o.Files.Plain)
	}
	names = append(names, o.Files.Sealed...)
	seen := map[string]bool{}
	for _, name := range names {
		if !isFileName(name) {
			return fmt.Errorf("vault: %q is not a single file name", name)
		}
		folded := strings.ToLower(name)
		if seen[folded] {
			return fmt.Errorf("vault: the file name %q is used twice", name)
		}
		seen[folded] = true
	}
	if len(o.Schemas) == 0 {
		return errors.New("vault: no document schema version")
	}
	for _, schema := range o.Schemas {
		if schema <= 0 {
			return fmt.Errorf("vault: schema version %d is not positive", schema)
		}
	}
	switch {
	case o.WorkFactor < 0 || o.WorkFactor > MaxWorkFactor:
		return fmt.Errorf("vault: work factor %d is outside 1 to %d", o.WorkFactor, MaxWorkFactor)
	case o.WorkFactor > 0 && o.WorkFactor < DefaultWorkFactor && !testing.Testing():
		return fmt.Errorf("vault: work factor %d is below %d", o.WorkFactor, DefaultWorkFactor)
	}
	return nil
}

func isFileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\:`) &&
		filepath.Base(name) == name
}

// Dir returns the vault directory.
func (v *Vault) Dir() string { return v.opts.Dir }

func (v *Vault) path(name string) string { return filepath.Join(v.opts.Dir, name) }
func (v *Vault) keyPath() string         { return v.path(v.opts.Files.Key) }
func (v *Vault) recipientPath() string   { return v.path(v.opts.Files.Recipient) }
func (v *Vault) documentPath() string    { return v.path(v.opts.Files.Document) }
func (v *Vault) pendingPath() string     { return v.path(v.opts.Files.Pending) }
func (v *Vault) plainPath() string       { return v.path(v.opts.Files.Plain) }

// State reports what the vault currently is. It reads no secret.
func (v *Vault) State() (State, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.state()
}

func (v *Vault) state() (State, error) {
	if v.unlocked {
		return StateUnlocked, nil
	}
	if v.opts.Files.Plain != "" {
		// An interrupted Encrypt leaves the plain document beside a new key; the plain one still counts.
		if exists, err := fileExists(v.plainPath()); err != nil {
			return "", err
		} else if exists {
			return StateUnencrypted, nil
		}
	}
	if exists, err := fileExists(v.keyPath()); err != nil {
		return "", err
	} else if exists {
		return StateLocked, nil
	}
	return StateAbsent, nil
}

// Status reports the state and the number of pending entries without a passphrase.
func (v *Vault) Status() (Status, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	state, err := v.state()
	if err != nil {
		return Status{}, err
	}
	names, err := v.pendingFiles()
	if err != nil {
		return Status{}, err
	}
	return Status{State: state, Dir: v.opts.Dir, Pending: len(names)}, nil
}

// Forget drops the key and document this value holds, so the vault is locked again. It touches no file.
func (v *Vault) Forget() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.forget()
}

func (v *Vault) forget() {
	clear(v.doc)
	v.unlocked, v.identity, v.doc = false, nil, nil
}

// Identity returns the private key of the unlocked vault. It is as secret as the document.
func (v *Vault) Identity() (*age.X25519Identity, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	return v.identity.key, nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

func isNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }
