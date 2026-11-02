package approval

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/vault"
)

const (
	testPassphrase       = "example passphrase"
	testFile             = "approvals.age"
	testFingerprintLabel = "example approval v1\x00"
	testMACLabel         = "example approvals v1"
	testPrompt           = "vault passphrase: "
)

type testState struct {
	Endpoint string   `json:"endpoint"`
	Effects  []string `json:"effects"`
	Secret   string   `json:"secret_id"`
}

func vaultOptions(dir string) vault.Options {
	return vault.Options{
		Dir: dir,
		Files: vault.Files{Key: "key.age", Recipient: "recipient", Document: "secrets.age", Pending: "pending",
			Plain: "secrets.json", Sealed: []string{testFile}},
		Schemas:    []int{1},
		WorkFactor: 4,
	}
}

// encryptedVault returns an unlocked encrypted vault and its options.
func encryptedVault(t *testing.T) (*vault.Vault, vault.Options) {
	t.Helper()
	opts := vaultOptions(filepath.Join(t.TempDir(), "vault"))
	v, err := vault.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Create(testPassphrase, []byte(`{"schema":1}`)); err != nil {
		t.Fatal(err)
	}
	return v, opts
}

func newStore(t *testing.T, v *vault.Vault) *Store {
	t.Helper()
	s, err := New(Options{Vault: v, File: testFile, FingerprintLabel: testFingerprintLabel, MACLabel: testMACLabel,
		LockName: "approvals.lock"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// person simulates a terminal with a person who types passphrase; reads counts the prompts.
func person(t *testing.T, terminal bool, passphrase string) (reads *int) {
	t.Helper()
	count := 0
	oldInteractive, oldRead := interactive, readPassphrase
	interactive = func() bool { return terminal }
	readPassphrase = func(string) (string, error) {
		count++
		return passphrase, nil
	}
	t.Cleanup(func() { interactive, readPassphrase = oldInteractive, oldRead })
	return &count
}

func admin(t *testing.T, v *vault.Vault) Admin {
	t.Helper()
	person(t, true, testPassphrase)
	a, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	return a
}

func fingerprint(t *testing.T, state any) string {
	t.Helper()
	fp, err := Fingerprint(testFingerprintLabel, state)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func approve(t *testing.T, s *Store, a Admin, name string, state any) {
	t.Helper()
	if err := s.Approve(a, Item{Name: name, State: state, Shown: fingerprint(t, state)}); err != nil {
		t.Fatalf("Approve(%s) error = %v", name, err)
	}
}

var exampleState = testState{Endpoint: "https://api.example.test", Effects: []string{"read"}, Secret: "id-1"}

func TestFingerprint(t *testing.T) {
	a := fingerprint(t, map[string]any{"b": 1, "a": []string{"x"}})
	b := fingerprint(t, map[string]any{"a": []string{"x"}, "b": 1})
	if a != b || len(a) != 64 {
		t.Fatalf("Fingerprint() = %q and %q, want one hex SHA-256", a, b)
	}
	if raw := fingerprint(t, json.RawMessage(`{ "a": ["x"], "b": 1 }`)); raw != a {
		t.Fatalf("Fingerprint() of raw JSON = %q, want its compact form %q", raw, a)
	}
	other, err := Fingerprint("another label", map[string]any{"a": []string{"x"}, "b": 1})
	if err != nil || other == a {
		t.Fatalf("Fingerprint() with another label = %q, %v, want another fingerprint", other, err)
	}
	if _, err := Fingerprint("", exampleState); err == nil {
		t.Fatal("Fingerprint() with an empty label succeeded")
	}
	if _, err := Fingerprint(testFingerprintLabel, make(chan int)); err == nil {
		t.Fatal("Fingerprint() of a value without JSON form succeeded")
	}
}

func TestAuthorizeRefusesAgent(t *testing.T) {
	v, _ := encryptedVault(t)
	reads := person(t, true, testPassphrase)
	if _, err := Authorize(v, AuthorizeOptions{Agent: true, Prompt: testPrompt}); !errors.Is(err, ErrAgent) {
		t.Fatalf("Authorize() for an agent error = %v, want ErrAgent", err)
	}
	if *reads != 0 {
		t.Fatal("Authorize() asked an agent for the passphrase")
	}
}

func TestAuthorizeRefusesWithoutTerminal(t *testing.T) {
	v, _ := encryptedVault(t)
	reads := person(t, false, testPassphrase)
	dialog := func(string) (string, error) { *reads++; return testPassphrase, nil }
	for _, opts := range []AuthorizeOptions{{Prompt: testPrompt}, {Prompt: testPrompt, Dialog: dialog}} {
		if _, err := Authorize(v, opts); !errors.Is(err, vault.ErrNoTerminal) {
			t.Fatalf("Authorize() without a terminal error = %v, want vault.ErrNoTerminal", err)
		}
	}
	if *reads != 0 {
		t.Fatal("Authorize() asked for the passphrase without a terminal")
	}
}

func TestAuthorizeRefusesWrongPassphrase(t *testing.T) {
	unlocked, opts := encryptedVault(t)
	locked, err := vault.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	person(t, true, "wrong passphrase")
	for _, v := range []*vault.Vault{unlocked, locked} {
		if _, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt}); !errors.Is(err, vault.ErrWrongPassphrase) {
			t.Fatalf("Authorize() with a wrong passphrase error = %v, want vault.ErrWrongPassphrase", err)
		}
	}
	if state, _ := locked.State(); state != vault.StateLocked {
		t.Fatalf("state after a wrong passphrase = %s, want locked", state)
	}
	person(t, true, "")
	if _, err := Authorize(unlocked, AuthorizeOptions{Prompt: testPrompt}); !errors.Is(err, vault.ErrEmptyPassphrase) {
		t.Fatalf("Authorize() with an empty passphrase error = %v", err)
	}
}

func TestAuthorizeRefusesUnencryptedVault(t *testing.T) {
	v, err := vault.New(vaultOptions(filepath.Join(t.TempDir(), "vault")))
	if err != nil {
		t.Fatal(err)
	}
	reads := person(t, true, testPassphrase)
	if _, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt}); !errors.Is(err, vault.ErrAbsent) {
		t.Fatalf("Authorize() of an absent vault error = %v", err)
	}
	if err := v.CreateUnencrypted([]byte(`{"schema":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Authorize(v, AuthorizeOptions{Prompt: testPrompt}); !errors.Is(err, vault.ErrNotEncrypted) {
		t.Fatalf("Authorize() of an unencrypted vault error = %v", err)
	}
	if *reads != 0 {
		t.Fatal("Authorize() asked for the passphrase of a vault without approvals")
	}
}

func TestAuthorizeUnlocksLockedVaultAndUsesDialog(t *testing.T) {
	_, opts := encryptedVault(t)
	locked, err := vault.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	reads := person(t, true, "not asked")
	var prompts []string
	dialog := func(prompt string) (string, error) { prompts = append(prompts, prompt); return testPassphrase, nil }
	a, err := Authorize(locked, AuthorizeOptions{Prompt: testPrompt, Dialog: dialog})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if *reads != 0 || len(prompts) != 1 || prompts[0] != testPrompt {
		t.Fatalf("passphrase read %d times from the terminal and by the dialog with %q", *reads, prompts)
	}
	if state, _ := locked.State(); state != vault.StateUnlocked {
		t.Fatalf("state after Authorize() = %s, want unlocked", state)
	}
	approve(t, newStore(t, locked), a, "example", exampleState)

	failing := func(string) (string, error) { return "", errors.New("dialog closed") }
	if _, err := Authorize(locked, AuthorizeOptions{Prompt: testPrompt, Dialog: failing}); err == nil {
		t.Fatal("Authorize() with a failing dialog succeeded")
	}
	if _, err := Authorize(locked, AuthorizeOptions{}); err == nil {
		t.Fatal("Authorize() without a prompt succeeded")
	}
}

func TestApproveCheckAndCompare(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	if err := s.Check("example", exampleState); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Check() before any approval error = %v, want ErrNotApproved", err)
	}
	d, err := s.Compare("example", exampleState)
	if err != nil || d == nil || !d.New || len(d.Changes) != 3 || d.Fingerprint != fingerprint(t, exampleState) {
		t.Fatalf("Compare() of a new state = %+v, %v", d, err)
	}

	approve(t, s, admin(t, v), "example", exampleState)
	if err := s.Check("example", exampleState); err != nil {
		t.Fatalf("Check() of the approved state error = %v", err)
	}
	if d, err := s.Compare("example", exampleState); d != nil || err != nil {
		t.Fatalf("Compare() of the approved state = %+v, %v, want nil", d, err)
	}

	changed := exampleState
	changed.Effects = []string{"read", "write"}
	if err := s.Check("example", changed); !errors.Is(err, ErrDeviates) {
		t.Fatalf("Check() of a changed state error = %v, want ErrDeviates", err)
	}
	if err := s.Check("other", exampleState); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Check() under another name error = %v, want ErrNotApproved", err)
	}
	d, err = s.Compare("example", changed)
	if err != nil || d == nil || d.New || d.Approved.IsZero() {
		t.Fatalf("Compare() of a changed state = %+v, %v", d, err)
	}
	want := []Change{{Path: "effects[1]", After: `"write"`}}
	if fmt.Sprint(d.Changes) != fmt.Sprint(want) {
		t.Fatalf("Compare().Changes = %+v, want %+v", d.Changes, want)
	}

	// A second Store value, as in a process that holds the vault open, checks against the same approvals.
	key, err := v.Identity()
	if err != nil {
		t.Fatal(err)
	}
	open, err := vault.OpenWithIdentity(vaultOptions(v.Dir()), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := newStore(t, open).Check("example", exampleState); err != nil {
		t.Fatalf("Check() in another process error = %v", err)
	}
}

func TestApproveRefusesStateChangedSinceShown(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a := admin(t, v)
	shown := fingerprint(t, exampleState)
	changed := exampleState
	changed.Endpoint = "https://other.example.test"
	err := s.Approve(a, Item{Name: "first", State: exampleState, Shown: shown},
		Item{Name: "example", State: changed, Shown: shown})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("Approve() of a changed state error = %v, want ErrChanged", err)
	}
	if err := s.Approve(a, Item{Name: "example", State: exampleState}); !errors.Is(err, ErrChanged) {
		t.Fatalf("Approve() without the shown fingerprint error = %v, want ErrChanged", err)
	}
	if approvals, err := s.Approvals(); err != nil || len(approvals) != 0 {
		t.Fatalf("Approvals() after refused approvals = %v, %v, want none", approvals, err)
	}
	if err := s.Approve(a, Item{Name: "example", State: exampleState, Shown: shown},
		Item{Name: "example", State: exampleState, Shown: shown}); err == nil {
		t.Fatal("Approve() of a name listed twice succeeded")
	}
}

func TestChangesRefusedWithoutAdmin(t *testing.T) {
	v, _ := encryptedVault(t)
	other, _ := encryptedVault(t)
	s := newStore(t, v)
	item := Item{Name: "example", State: exampleState, Shown: fingerprint(t, exampleState)}
	record := map[string]Approval{"example": {Fingerprint: item.Shown, State: json.RawMessage(
		`{"endpoint":"https://api.example.test","effects":["read"],"secret_id":"id-1"}`), Approved: time.Now()}}
	for name, a := range map[string]Admin{"zero": {}, "other vault": admin(t, other)} {
		if err := s.Approve(a, item); !errors.Is(err, ErrNotAdmin) {
			t.Fatalf("Approve() with the %s admin error = %v, want ErrNotAdmin", name, err)
		}
		if _, err := s.Revoke(a, "example"); !errors.Is(err, ErrNotAdmin) {
			t.Fatalf("Revoke() with the %s admin error = %v, want ErrNotAdmin", name, err)
		}
		if err := s.Adopt(a, record); !errors.Is(err, ErrNotAdmin) {
			t.Fatalf("Adopt() with the %s admin error = %v, want ErrNotAdmin", name, err)
		}
	}
	if err := s.Check("example", exampleState); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Check() after refused changes error = %v, want ErrNotApproved", err)
	}
}

func TestRevoke(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a := admin(t, v)
	approve(t, s, a, "example", exampleState)
	approve(t, s, a, "kept", exampleState)
	if n, err := s.Revoke(a, "example", "unknown"); n != 1 || err != nil {
		t.Fatalf("Revoke() = %d, %v, want 1", n, err)
	}
	if err := s.Check("example", exampleState); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Check() of a revoked approval error = %v", err)
	}
	if err := s.Check("kept", exampleState); err != nil {
		t.Fatalf("Check() of a kept approval error = %v", err)
	}
}

// writeFile writes the approvals file the way anyone who can encrypt to the vault recipient can.
func writeFile(t *testing.T, v *vault.Vault, file approvalsFile) {
	t.Helper()
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.WriteSealed(testFile, data); err != nil {
		t.Fatal(err)
	}
}

func signed(t *testing.T, v *vault.Vault, label string, schema int, body string) approvalsFile {
	t.Helper()
	key, err := v.Identity()
	if err != nil {
		t.Fatal(err)
	}
	mac, err := vault.MAC(key, label, macData(schema, []byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	return approvalsFile{Schema: schema, Approvals: json.RawMessage(body), MAC: mac}
}

func TestForgedOrDamagedApprovalsRefused(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	state := `{"endpoint":"https://api.example.test","effects":["read"],"secret_id":"id-1"}`
	body := fmt.Sprintf(`{"example":{"fingerprint":%q,"state":%s,"approved":"2030-01-02T03:04:05Z"}}`,
		fingerprint(t, json.RawMessage(state)), state)
	wrongFingerprint := `{"example":{"fingerprint":"00","state":{},"approved":"2030-01-02T03:04:05Z"}}`
	cases := []struct {
		name string
		file approvalsFile
		want error
	}{
		{"no check value", approvalsFile{Schema: fileSchema, Approvals: json.RawMessage(body)}, vault.ErrMACMismatch},
		{"foreign check value", approvalsFile{Schema: fileSchema, Approvals: json.RawMessage(body),
			MAC: strings.Repeat("0", 64)}, vault.ErrMACMismatch},
		{"another label", signed(t, v, "another label", fileSchema, body), vault.ErrMACMismatch},
		{"unknown schema", signed(t, v, testMACLabel, 2, body), vault.ErrUnsupportedSchema},
		{"fingerprint of another state", signed(t, v, testMACLabel, fileSchema, wrongFingerprint), vault.ErrDamaged},
		{"unknown member", signed(t, v, testMACLabel, fileSchema,
			`{"example":{"fingerprint":"00","state":{},"approved":"2030-01-02T03:04:05Z","extra":1}}`), vault.ErrDamaged},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			writeFile(t, v, c.file)
			if _, err := s.Approvals(); !errors.Is(err, c.want) {
				t.Fatalf("Approvals() error = %v, want %v", err, c.want)
			}
			if err := s.Check("example", json.RawMessage(state)); !errors.Is(err, c.want) {
				t.Fatalf("Check() error = %v, want %v", err, c.want)
			}
		})
	}
	if err := v.WriteSealed(testFile, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approvals(); !errors.Is(err, vault.ErrDamaged) {
		t.Fatalf("Approvals() of a file that does not decode error = %v", err)
	}
	writeFile(t, v, signed(t, v, testMACLabel, fileSchema, body))
	if err := s.Check("example", json.RawMessage(state)); err != nil {
		t.Fatalf("Check() of a file written with the vault key error = %v", err)
	}
}

func TestNoApprovalsWithoutUnlockedEncryptedVault(t *testing.T) {
	unlocked, opts := encryptedVault(t)
	approve(t, newStore(t, unlocked), admin(t, unlocked), "example", exampleState)
	locked, err := vault.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := newStore(t, locked).Check("example", exampleState); !errors.Is(err, vault.ErrLocked) {
		t.Fatalf("Check() of a locked vault error = %v, want vault.ErrLocked", err)
	}
	plain, err := vault.New(vaultOptions(filepath.Join(t.TempDir(), "vault")))
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.CreateUnencrypted([]byte(`{"schema":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := newStore(t, plain).Check("example", exampleState); !errors.Is(err, vault.ErrNotEncrypted) {
		t.Fatalf("Check() of an unencrypted vault error = %v, want vault.ErrNotEncrypted", err)
	}
}

func TestAdoptRefusesInconsistentApprovals(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a := admin(t, v)
	state := json.RawMessage(`{"endpoint":"https://api.example.test"}`)
	good := Approval{Fingerprint: fingerprint(t, state), State: state, Approved: time.Now()}
	for name, bad := range map[string]Approval{
		"another fingerprint": {Fingerprint: fingerprint(t, exampleState), State: state, Approved: time.Now()},
		"no time":             {Fingerprint: good.Fingerprint, State: state},
		"no JSON":             {Fingerprint: good.Fingerprint, State: json.RawMessage(`{`), Approved: time.Now()},
	} {
		if err := s.Adopt(a, map[string]Approval{"good": good, "bad": bad}); err == nil {
			t.Fatalf("Adopt() of an approval with %s succeeded", name)
		}
	}
	if approvals, err := s.Approvals(); err != nil || len(approvals) != 0 {
		t.Fatalf("Approvals() after refused adoptions = %v, %v, want none", approvals, err)
	}
}

func TestConcurrentChangesKeepEveryApproval(t *testing.T) {
	v, opts := encryptedVault(t)
	key, err := v.Identity()
	if err != nil {
		t.Fatal(err)
	}
	second, err := vault.OpenWithIdentity(opts, key)
	if err != nil {
		t.Fatal(err)
	}
	stores := []*Store{newStore(t, v), newStore(t, second)}
	admins := []Admin{admin(t, v), admin(t, second)}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state := testState{Endpoint: fmt.Sprintf("https://%d.example.test", i)}
			fp, err := Fingerprint(testFingerprintLabel, state)
			if err == nil {
				err = stores[i%2].Approve(admins[i%2], Item{Name: fmt.Sprint(i), State: state, Shown: fp})
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if approvals, err := stores[0].Approvals(); err != nil || len(approvals) != 8 {
		t.Fatalf("Approvals() after concurrent approvals = %d, %v, want 8", len(approvals), err)
	}
}

func TestNewRefusesIncompleteOptions(t *testing.T) {
	v, _ := encryptedVault(t)
	full := Options{Vault: v, File: testFile, FingerprintLabel: testFingerprintLabel, MACLabel: testMACLabel,
		LockName: "approvals.lock"}
	for name, change := range map[string]func(*Options){
		"vault":             func(o *Options) { o.Vault = nil },
		"file":              func(o *Options) { o.File = "" },
		"fingerprint label": func(o *Options) { o.FingerprintLabel = "" },
		"check value label": func(o *Options) { o.MACLabel = "" },
		"lock name":         func(o *Options) { o.LockName = "" },
		"lock path":         func(o *Options) { o.LockName = filepath.Join("..", "approvals.lock") },
	} {
		opts := full
		change(&opts)
		if _, err := New(opts); err == nil {
			t.Fatalf("New() without a valid %s succeeded", name)
		}
	}
}

func TestDiff(t *testing.T) {
	before := []byte(`{"name":"example","list":["a","b"],"tools":null,"empty":{},"key.with.dots":1,"n":1.50}`)
	after := []byte(`{"name":"exa\u001b[31mmple\u202e","list":["a"],"tools":[],"empty":{},"key.with.dots":2,"n":1.50}`)
	got, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{Path: `["key.with.dots"]`, Before: "1", After: "2"},
		{Path: "list[1]", Before: `"b"`},
		{Path: "name", Before: `"example"`, After: `"exa\u001b[31mmple\u202e"`},
		{Path: "tools", Before: "null", After: "[]"},
	}
	if fmt.Sprintf("%q", got) != fmt.Sprintf("%q", want) {
		t.Fatalf("Diff() = %q\nwant %q", got, want)
	}
	all, err := Diff(nil, []byte("\"\U000e0041\""))
	if err != nil || len(all) != 1 || all[0].Path != "" || all[0].After != `"\udb40\udc41"` {
		t.Fatalf("Diff() of a new root value = %q, %v", all, err)
	}
	for _, bad := range [][]byte{[]byte(`{`), []byte(`{} {}`)} {
		if _, err := Diff(bad, nil); err == nil {
			t.Fatalf("Diff(%s) succeeded", bad)
		}
	}
}
