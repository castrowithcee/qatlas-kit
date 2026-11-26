package vault

import (
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// plainPair returns two values of one unencrypted vault with a write lock, as two processes hold it.
func plainPair(t *testing.T, values map[string]string) (a, b *Vault) {
	t.Helper()
	opts := lockedOptions(filepath.Join(t.TempDir(), "vault"))
	a = newVault(t, opts)
	if err := a.CreateUnencrypted(encodeDoc(t, values)); err != nil {
		t.Fatalf("CreateUnencrypted() error = %v", err)
	}
	return a, newVault(t, opts)
}

func plainOnDisk(t *testing.T, v *Vault) map[string]string {
	t.Helper()
	return decodeDoc(t, readBytes(t, v.plainPath()))
}

func TestPlainLockedUpdatesLoseNoChange(t *testing.T) {
	a, b := plainPair(t, map[string]string{})
	const rounds = 25
	for range rounds {
		for _, v := range []*Vault{a, b} {
			if _, err := v.Update(increment("count")); err != nil {
				t.Fatalf("Update() error = %v", err)
			}
		}
	}
	var wg sync.WaitGroup
	for _, v := range []*Vault{a, b} {
		wg.Go(func() {
			for range rounds {
				if _, err := v.Update(increment("count")); err != nil {
					t.Errorf("Update() error = %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
	if got := plainOnDisk(t, a)["count"]; got != strconv.Itoa(4*rounds) {
		t.Fatalf("count = %s, want %d: an update was based on a document read before another one", got, 4*rounds)
	}
	wantState(t, a, StateUnencrypted)
	wantPrivate(t, a.plainPath())
}

func TestPlainUpdateComparesAndSwaps(t *testing.T) {
	a, b := plainPair(t, map[string]string{"token": "example-old"})
	if _, err := b.Update(replaceToken("example-old", "example-b", nil)); err != nil {
		t.Fatal(err)
	}
	before := vaultFiles(t, a.Dir())
	var found string
	undo, err := a.Update(replaceToken("example-old", "example-a", &found))
	if err != nil {
		t.Fatalf("Update() that changes nothing error = %v", err)
	}
	if found != "example-b" {
		t.Fatalf("the update saw token %q, want the one on disk", found)
	}
	wantFiles(t, vaultFiles(t, a.Dir()), before)
	if err := undo(); err != nil {
		t.Fatalf("undo() of no change error = %v", err)
	}
	wantFiles(t, vaultFiles(t, a.Dir()), before)
	if _, err := a.Update(replaceToken("example-b", "example-a", nil)); err != nil {
		t.Fatal(err)
	}
	wantValues(t, plainOnDisk(t, a), map[string]string{"token": "example-a"})
}

func TestPlainUpdateRejections(t *testing.T) {
	fixed := func(document []byte, done ...string) UpdateFunc {
		return func([]byte, []Pending) ([]byte, []string, error) { return document, done, nil }
	}
	cases := []struct {
		name   string
		change UpdateFunc
		want   error
	}{
		{"change fails", func([]byte, []Pending) ([]byte, []string, error) { return nil, nil, errExample },
			errExample},
		{"unsupported schema", fixed([]byte(`{"schema":7}`)), ErrUnsupportedSchema},
		{"not an object", fixed([]byte(`[]`)), ErrDamaged},
		{"reported entry", fixed([]byte(`{"schema":1}`), "0123456789abcdef0123456789abcdef.age"), nil},
	}
	for _, c := range cases {
		for _, lock := range []bool{false, true} {
			t.Run(c.name+"/lock "+strconv.FormatBool(lock), func(t *testing.T) {
				opts := testOptions(filepath.Join(t.TempDir(), "vault"))
				if lock {
					opts.Files.Lock = lockName
				}
				v := newVault(t, opts)
				if err := v.CreateUnencrypted(encodeDoc(t, map[string]string{"reader": "example-1"})); err != nil {
					t.Fatal(err)
				}
				before := vaultFiles(t, v.Dir())
				undo, err := v.Update(c.change)
				if err == nil || c.want != nil && !errors.Is(err, c.want) || undo != nil {
					t.Fatalf("Update() = %v, error %v, want it refused with %v", undo != nil, err, c.want)
				}
				wantFiles(t, vaultFiles(t, v.Dir()), before)
			})
		}
	}
}

func TestPlainUpdateUndo(t *testing.T) {
	t.Run("first update after create", func(t *testing.T) {
		for _, lock := range []bool{false, true} {
			opts := testOptions(filepath.Join(t.TempDir(), "vault"))
			if lock {
				opts.Files.Lock = lockName
			}
			v := newVault(t, opts)
			if err := v.CreateUnencrypted(encodeDoc(t, map[string]string{"reader": "example-1"})); err != nil {
				t.Fatal(err)
			}
			before := vaultFiles(t, v.Dir())
			undo, err := v.Update(setAndMerge("reader", "example-2"))
			if err != nil {
				t.Fatal(err)
			}
			wantValues(t, plainOnDisk(t, v), map[string]string{"reader": "example-2"})
			if err := undo(); err != nil {
				t.Fatalf("undo() error = %v", err)
			}
			wantFiles(t, vaultFiles(t, v.Dir()), before)
			wantPrivate(t, v.plainPath())
			wantState(t, v, StateUnencrypted)
		}
	})
	t.Run("another write between", func(t *testing.T) {
		a, b := plainPair(t, map[string]string{"reader": "example-1", "token": "example-old"})
		a.opts.Revert = testRevert
		undo, err := a.Update(setAndMerge("reader", "example-2"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.Update(replaceToken("example-old", "example-b", nil)); err != nil {
			t.Fatal(err)
		}
		if err := undo(); err != nil {
			t.Fatalf("undo() error = %v", err)
		}
		wantValues(t, plainOnDisk(t, a), map[string]string{"reader": "example-1", "token": "example-b"})
		after := vaultFiles(t, a.Dir())
		if err := undo(); err != nil {
			t.Fatalf("a second undo() error = %v", err)
		}
		wantFiles(t, vaultFiles(t, a.Dir()), after)
	})
	t.Run("another write between without revert", func(t *testing.T) {
		a, b := plainPair(t, map[string]string{"reader": "example-1", "token": "example-old"})
		undo, err := a.Update(setAndMerge("reader", "example-2"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.Update(replaceToken("example-old", "example-b", nil)); err != nil {
			t.Fatal(err)
		}
		before := vaultFiles(t, a.Dir())
		if err := undo(); !errors.Is(err, ErrConflict) {
			t.Fatalf("undo() error = %v, want ErrConflict", err)
		}
		wantFiles(t, vaultFiles(t, a.Dir()), before)
	})
}
