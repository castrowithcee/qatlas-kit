package vault

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The vaults in testdata/existing were written by an existing application that predates this package,
// with invented values and its own file names and label, at the default work factor. Opening them with
// those names and that label proves the format needs no migration.
const (
	existingPassphrase    = "example passphrase"
	existingSettingsLabel = "example vault settings v1"
)

type existingEntry struct {
	ID           string            `json:"id"`
	CredentialID string            `json:"credential_id,omitempty"`
	Name         string            `json:"name"`
	Roles        map[string]string `json:"roles"`
	Created      time.Time         `json:"created"`
	Modified     time.Time         `json:"modified"`
}

type existingDocument struct {
	Schema    int                      `json:"schema"`
	Entries   map[string]existingEntry `json:"entries,omitempty"`
	Approvals json.RawMessage          `json:"approvals,omitempty"`
}

type existingPending struct {
	Name     string            `json:"name"`
	Roles    map[string]string `json:"roles"`
	Modified time.Time         `json:"modified"`
}

type existingSettings struct {
	Schema         int    `json:"schema"`
	UpdateHandover string `json:"update_handover"`
	IdleTimeoutNS  int64  `json:"idle_timeout_ns"`
	AdminTimeoutNS int64  `json:"admin_timeout_ns"`
	RetentionDays  int    `json:"retention_days"`
	MaxUnlockNS    int64  `json:"max_unlock_ns"`
	MAC            string `json:"mac"`
}

// mergeExisting merges pending entries the way the existing application does: by entry name.
func mergeExisting(document []byte, pending []Pending) ([]byte, []string, error) {
	var doc existingDocument
	if err := json.Unmarshal(document, &doc); err != nil {
		return nil, nil, err
	}
	if doc.Entries == nil {
		doc.Entries = map[string]existingEntry{}
	}
	var done []string
	for _, p := range pending {
		var payload existingPending
		if err := json.Unmarshal(p.Payload, &payload); err != nil {
			return nil, nil, err
		}
		entry := existingEntry{ID: "00000000000000000000000000000001", Name: payload.Name, Roles: map[string]string{},
			Created: payload.Modified}
		for id, e := range doc.Entries {
			if e.Name == payload.Name {
				entry = doc.Entries[id]
			}
		}
		for role, value := range payload.Roles {
			entry.Roles[role] = value
		}
		entry.Modified = payload.Modified
		doc.Entries[entry.ID] = entry
		done = append(done, p.Name)
	}
	merged, err := json.MarshalIndent(doc, "", "  ")
	return merged, done, err
}

func existingOptions(dir string) Options {
	return Options{
		Dir: dir,
		Files: Files{Key: "key.age", Recipient: "recipient", Document: "secrets.age", Pending: "pending",
			Plain: "secrets.json", Sealed: []string{"settings.age", "tokens.age"}},
		Schemas: []int{1},
		Merge:   mergeExisting,
	}
}

// copyExisting copies a vault from testdata into a private temporary directory, since Git keeps no modes.
func copyExisting(t *testing.T, name string) *Vault {
	t.Helper()
	opts := existingOptions(filepath.Join(t.TempDir(), "vault"))
	v := newVault(t, opts)
	source := filepath.Join("testdata", "existing", name)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return v.write(filepath.Join(opts.Dir, rel), data)
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func entryRoles(t *testing.T, document []byte) map[string]map[string]string {
	t.Helper()
	var doc existingDocument
	if err := json.Unmarshal(document, &doc); err != nil {
		t.Fatal(err)
	}
	roles := map[string]map[string]string{}
	for _, e := range doc.Entries {
		roles[e.Name] = e.Roles
	}
	return roles
}

func TestExistingEncryptedVaultOpens(t *testing.T) {
	v := copyExisting(t, "encrypted")
	if st, err := v.Status(); err != nil || st.State != StateLocked || st.Pending != 1 {
		t.Fatalf("Status() = %+v, %v, want locked with one pending entry", st, err)
	}
	if merged, err := v.Unlock(existingPassphrase); err != nil || merged != 1 {
		t.Fatalf("Unlock() = %d, %v, want the pending entry merged", merged, err)
	}
	doc, err := v.Document()
	if err != nil {
		t.Fatal(err)
	}
	roles := entryRoles(t, doc)
	if roles["example-reader"]["login"] != "example-login-1" || roles["example-writer"]["login"] != "example-login-2" {
		t.Fatalf("document = %s", doc)
	}

	data, err := v.ReadSealed("settings.age")
	if err != nil {
		t.Fatalf("ReadSealed() error = %v", err)
	}
	var settings existingSettings
	if err := json.Unmarshal(data, &settings); err != nil || settings.Schema != 2 {
		t.Fatalf("settings = %s, %v", data, err)
	}
	signed := fmt.Sprintf("%d\n%s\n%d\n%d\n%d\n%d", settings.Schema, settings.UpdateHandover, settings.IdleTimeoutNS,
		settings.AdminTimeoutNS, settings.RetentionDays, settings.MaxUnlockNS)
	id, _ := v.Identity()
	if err := VerifyMAC(id, existingSettingsLabel, []byte(signed), settings.MAC); err != nil {
		t.Fatalf("VerifyMAC() of the existing settings error = %v", err)
	}
	if time.Duration(settings.IdleTimeoutNS) != 2*time.Hour || settings.RetentionDays != 30 {
		t.Fatalf("settings = %+v", settings)
	}

	// The vault keeps working after a passphrase change, with the default work factor of a new key.
	if err := v.ChangePassphrase(existingPassphrase, "example new passphrase"); err != nil {
		t.Fatal(err)
	}
	reopened := newVault(t, v.opts)
	if _, err := reopened.Unlock("example new passphrase"); err != nil {
		t.Fatalf("Unlock() after the passphrase change error = %v", err)
	}
}

func TestExistingUnencryptedVaultOpens(t *testing.T) {
	v := copyExisting(t, "unencrypted")
	wantState(t, v, StateUnencrypted)
	doc, err := v.Document()
	if err != nil {
		t.Fatal(err)
	}
	if entryRoles(t, doc)["example-reader"]["login"] != "example-login-3" {
		t.Fatalf("document = %s", doc)
	}
	opts := v.opts
	opts.WorkFactor = 4
	v = newVault(t, opts)
	if err := v.Encrypt(existingPassphrase); err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	locked := newVault(t, opts)
	if _, err := locked.Unlock(existingPassphrase); err != nil {
		t.Fatal(err)
	}
	if again, _ := locked.Document(); string(again) != string(doc) {
		t.Fatalf("document after Encrypt = %s, want %s", again, doc)
	}
}
