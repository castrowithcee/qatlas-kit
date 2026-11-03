package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/filelock"
	"github.com/castrowithcee/qatlas-kit/vault"
)

var (
	// ErrNotApproved reports a name the vault holds no approval for.
	ErrNotApproved = errors.New("no state is approved under this name")
	// ErrDeviates reports a current state that differs from the approved one.
	ErrDeviates = errors.New("the state differs from the approved state")
	// ErrChanged reports a state that changed after it was shown to the person; nothing was approved.
	ErrChanged = errors.New("the state changed since it was shown")
)

// fileSchema is the version of the approvals file.
const fileSchema = 1

// Options configure a Store. Every field is required.
type Options struct {
	// Vault is the encrypted vault that holds the approvals.
	Vault *vault.Vault
	// File is the sealed file of the vault that holds them, one of its vault.Files.Sealed.
	File string
	// FingerprintLabel is the label of Fingerprint for every state of this store.
	FingerprintLabel string
	// MACLabel is the label the check value of the approvals file is keyed with (see vault.MAC). It is the
	// application's own and differs from every other label it derives a key with.
	MACLabel string
	// LockName is the file in the vault directory that every change of the approvals locks, so changes of
	// several processes follow one another. It holds no data and is not one of the vault's files.
	LockName string
}

// Approval is one approved state.
type Approval struct {
	// Fingerprint is the Fingerprint of State under the store's label.
	Fingerprint string `json:"fingerprint"`
	// State is the canonical JSON of the state as it was approved, for showing what changed since.
	State json.RawMessage `json:"state"`
	// Approved is when a person approved it, in UTC.
	Approved time.Time `json:"approved"`
}

// Item is one state a person approves.
type Item struct {
	Name  string
	State any
	// Shown is the fingerprint of the state as it was shown to the person, see Deviation.Fingerprint.
	Shown string
}

// Deviation is a current state that is not approved as it is.
type Deviation struct {
	Name string
	// Fingerprint is the fingerprint of the current state; hand it to Item.Shown to approve this state.
	Fingerprint string
	// New is true when nothing is approved under Name; Changes then lists the whole current state.
	New bool
	// Approved is when the approved state was approved, zero for a new one.
	Approved time.Time
	Changes  []Change
}

// Store keeps the approvals of an application in a sealed file of its vault. A Store is safe for
// concurrent use as far as its vault is; it reads the file anew for every call.
type Store struct {
	opts Options
}

// approvalsFile is the JSON form of the approvals file. MAC covers the schema and the exact bytes of
// Approvals.
type approvalsFile struct {
	Schema    int             `json:"schema"`
	Approvals json.RawMessage `json:"approvals"`
	MAC       string          `json:"mac"`
}

// New checks opts and returns the store they describe.
func New(opts Options) (*Store, error) {
	switch {
	case opts.Vault == nil:
		return nil, errors.New("approval: no vault")
	case opts.File == "":
		return nil, errors.New("approval: no approvals file")
	case opts.FingerprintLabel == "":
		return nil, errors.New("approval: no fingerprint label")
	case opts.MACLabel == "":
		return nil, errors.New("approval: no check value label")
	case opts.LockName == "" || opts.LockName != filepath.Base(opts.LockName) || opts.LockName == "." ||
		opts.LockName == ".." || strings.ContainsAny(opts.LockName, `/\:`):
		return nil, fmt.Errorf("approval: %q is not a single file name", opts.LockName)
	}
	return &Store{opts: opts}, nil
}

// Approvals returns every approval by name. A vault without an approvals file holds none. It fails with
// vault.ErrLocked, vault.ErrNotEncrypted or vault.ErrAbsent where there are no approvals to read, with
// vault.ErrMACMismatch for a file that was not written with the vault key, and with vault.ErrDamaged for
// one that does not decode or whose fingerprints do not match their states.
func (s *Store) Approvals() (map[string]Approval, error) {
	state, err := s.opts.Vault.State()
	if err != nil {
		return nil, err
	}
	if err := encrypted(state); err != nil {
		return nil, err
	}
	data, err := s.opts.Vault.ReadSealed(s.opts.File)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Approval{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer clear(data)
	var file approvalsFile
	if err := strictDecode(data, &file); err != nil {
		return nil, fmt.Errorf("approval: the approvals file does not decode: %w", vault.ErrDamaged)
	}
	if file.Schema != fileSchema {
		return nil, fmt.Errorf("approval: approvals file schema %d: %w", file.Schema, vault.ErrUnsupportedSchema)
	}
	key, err := s.opts.Vault.Identity()
	if err != nil {
		return nil, err
	}
	if err := vault.VerifyMAC(key, s.opts.MACLabel, macData(file.Schema, file.Approvals), file.MAC); err != nil {
		return nil, fmt.Errorf("approval: the approvals file: %w", err)
	}
	approvals := map[string]Approval{}
	if err := strictDecode(file.Approvals, &approvals); err != nil {
		return nil, fmt.Errorf("approval: the approvals do not decode: %w", vault.ErrDamaged)
	}
	for name, a := range approvals {
		if err := s.consistent(name, a); err != nil {
			return nil, err
		}
	}
	return approvals, nil
}

// Check reports whether state is approved under name exactly as it is: nil when it is, ErrNotApproved
// when nothing is approved under name, and ErrDeviates when the approved state differs.
func (s *Store) Check(name string, state any) error {
	_, fingerprint, err := encode(s.opts.FingerprintLabel, state)
	if err != nil {
		return err
	}
	approvals, err := s.Approvals()
	if err != nil {
		return err
	}
	approved, ok := approvals[name]
	switch {
	case !ok:
		return fmt.Errorf("%s: %w", name, ErrNotApproved)
	case approved.Fingerprint != fingerprint:
		return fmt.Errorf("%s: %w", name, ErrDeviates)
	}
	return nil
}

// Compare returns how state differs from what is approved under name, or nil when it is approved as it
// is.
func (s *Store) Compare(name string, state any) (*Deviation, error) {
	current, fingerprint, err := encode(s.opts.FingerprintLabel, state)
	if err != nil {
		return nil, err
	}
	approvals, err := s.Approvals()
	if err != nil {
		return nil, err
	}
	approved, ok := approvals[name]
	if ok && approved.Fingerprint == fingerprint {
		return nil, nil
	}
	changes, err := Diff(approved.State, current)
	if err != nil {
		return nil, err
	}
	return &Deviation{Name: name, Fingerprint: fingerprint, New: !ok, Approved: approved.Approved,
		Changes: changes}, nil
}

// Approve approves every item as its state is now. Every item must carry the fingerprint of the state
// that was shown; a state that differs from it fails with ErrChanged, and then nothing is approved.
func (s *Store) Approve(admin Admin, items ...Item) error {
	if err := s.authorized(admin); err != nil {
		return err
	}
	now := time.Now().UTC()
	next := map[string]Approval{}
	for _, item := range items {
		if item.Name == "" {
			return errors.New("approval: an item has no name")
		}
		if _, twice := next[item.Name]; twice {
			return fmt.Errorf("approval: %s is listed twice", item.Name)
		}
		state, fingerprint, err := encode(s.opts.FingerprintLabel, item.State)
		if err != nil {
			return err
		}
		if item.Shown != fingerprint {
			return fmt.Errorf("%s: %w", item.Name, ErrChanged)
		}
		next[item.Name] = Approval{Fingerprint: fingerprint, State: state, Approved: now}
	}
	return s.update(func(approvals map[string]Approval) bool {
		maps.Copy(approvals, next)
		return len(next) > 0
	})
}

// Revoke removes the approvals of names and reports how many it removed.
func (s *Store) Revoke(admin Admin, names ...string) (int, error) {
	if err := s.authorized(admin); err != nil {
		return 0, err
	}
	removed := 0
	err := s.update(func(approvals map[string]Approval) bool {
		for _, name := range names {
			if _, ok := approvals[name]; ok {
				delete(approvals, name)
				removed++
			}
		}
		return removed > 0
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// Adopt takes over approvals a person gave before, such as those an application kept elsewhere, and
// replaces any held under the same names. Each must carry the fingerprint of its state under the store's
// label and the time it was approved; one that does not is refused, and then nothing is adopted. An
// approval keeps holding exactly when its fingerprint matches, so adopting needs no new approval.
func (s *Store) Adopt(admin Admin, approvals map[string]Approval) error {
	if err := s.authorized(admin); err != nil {
		return err
	}
	next := make(map[string]Approval, len(approvals))
	for name, a := range approvals {
		if name == "" {
			return errors.New("approval: an approval has no name")
		}
		if a.Approved.IsZero() {
			return fmt.Errorf("approval: %s has no approval time", name)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, a.State); err != nil {
			return fmt.Errorf("approval: the state of %s is not JSON: %w", name, err)
		}
		a.State = compact.Bytes()
		a.Approved = a.Approved.UTC()
		if err := s.consistent(name, a); err != nil {
			return err
		}
		next[name] = a
	}
	return s.update(func(current map[string]Approval) bool {
		maps.Copy(current, next)
		return len(next) > 0
	})
}

// authorized refuses a change without the admin proof for this store's vault.
func (s *Store) authorized(admin Admin) error {
	if admin.vault == nil || admin.vault != s.opts.Vault {
		return ErrNotAdmin
	}
	return nil
}

// consistent refuses an approval whose fingerprint is not that of its state.
func (s *Store) consistent(name string, a Approval) error {
	_, fingerprint, err := encode(s.opts.FingerprintLabel, a.State)
	if err != nil || fingerprint != a.Fingerprint {
		return fmt.Errorf("approval: the fingerprint of %s does not match its state: %w", name, vault.ErrDamaged)
	}
	return nil
}

// update applies change to the approvals and, when it reports a change, writes them with a new check
// value.
func (s *Store) update(change func(map[string]Approval) bool) error {
	state, err := s.opts.Vault.State()
	if err != nil {
		return err
	}
	if err := encrypted(state); err != nil {
		return err
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	approvals, err := s.Approvals()
	if err != nil {
		return err
	}
	if !change(approvals) {
		return nil
	}
	body, err := json.Marshal(approvals)
	if err != nil {
		return fmt.Errorf("approval: cannot encode the approvals: %w", err)
	}
	key, err := s.opts.Vault.Identity()
	if err != nil {
		return err
	}
	mac, err := vault.MAC(key, s.opts.MACLabel, macData(fileSchema, body))
	if err != nil {
		return err
	}
	data, err := json.Marshal(approvalsFile{Schema: fileSchema, Approvals: body, MAC: mac})
	if err != nil {
		return fmt.Errorf("approval: cannot encode the approvals: %w", err)
	}
	return s.opts.Vault.WriteSealed(s.opts.File, data)
}

// lock opens, creating if needed, and exclusively locks the lock file.
func (s *Store) lock() (func(), error) {
	path := filepath.Join(s.opts.Vault.Dir(), s.opts.LockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("approval: cannot open the lock %s: %w", path, err)
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("approval: cannot lock %s: %w", path, err)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}

// macData is what the check value of the approvals file covers.
func macData(schema int, approvals []byte) []byte {
	return append(fmt.Appendf(nil, "%d\n", schema), approvals...)
}

func strictDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}
