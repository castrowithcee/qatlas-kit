package vault

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// preparedFiles returns files that do not decrypt, each with the cause it must report. good is a valid
// file of the vault, which the damaged variants are derived from.
func preparedFiles(t *testing.T, v *Vault, good []byte) map[DecryptCause][]byte {
	t.Helper()
	recipient := foreignIdentity(t).Recipient()

	header := bytes.Clone(good)
	mac := bytes.Index(header, []byte("\n--- "))
	if mac < 0 {
		t.Fatal("no header MAC line")
	}
	header[mac+len("\n--- ")+5] ^= 0x01 // inside the MAC value

	payload := bytes.Clone(good)
	payload[len(payload)-1] ^= 0x01

	return map[DecryptCause][]byte{
		CauseWrongKey: encryptFor(t, recipient, []byte("example content")),
		CauseNotAge:   []byte("example-first-line: plain text\nsecond line\n"),
		CauseDamaged:  header,
		"payload":     payload,
	}
}

// wantCause fails unless err is a decrypt DocumentError with cause that wraps ErrDamaged and leaks neither
// the file's first line nor any library wording anywhere along its Unwrap chain.
func wantCause(t *testing.T, name string, err error, cause DecryptCause, path string) {
	t.Helper()
	var de *DocumentError
	if !errors.As(err, &de) || de.Kind != DocumentDecrypt {
		t.Fatalf("%s: error = %v, want a decrypt *DocumentError", name, err)
	}
	if de.Cause != cause || de.Path != path {
		t.Errorf("%s: Cause, Path = %q, %q, want %q, %q", name, de.Cause, de.Path, cause, path)
	}
	if !errors.Is(err, ErrDamaged) {
		t.Errorf("%s: errors.Is(err, ErrDamaged) = false", name)
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		text := e.Error()
		for _, banned := range []string{"example-first-line", "unexpected intro", "parsing age header", "bad header MAC",
			"no identity matched", "did not match any", "failed to read", "incorrect identity", "MAC"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s: error %q contains %q", name, text, banned)
			}
		}
	}
}

func TestDecryptCauseOfDocument(t *testing.T) {
	v, locked := created(t, map[string]string{"k": "example-1"})
	good := readBytes(t, v.documentPath())
	for want, data := range preparedFiles(t, v, good) {
		if want == "payload" {
			want = CauseDamaged
		}
		overwrite(t, v, v.documentPath(), data)
		_, err := locked.Unlock(testPassphrase)
		wantCause(t, "Unlock "+string(want), err, want, v.documentPath())
		_, err = OpenWithIdentity(v.opts, v.identity.key)
		wantCause(t, "OpenWithIdentity "+string(want), err, want, v.documentPath())
	}
}

func TestDecryptCauseOfSealedFile(t *testing.T) {
	v, _ := created(t, map[string]string{"k": "example-1"})
	if err := v.WriteSealed(sealedName, []byte("example settings")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(v.opts.Dir, sealedName)
	good := readBytes(t, path)
	for want, data := range preparedFiles(t, v, good) {
		if want == "payload" {
			want = CauseDamaged
		}
		overwrite(t, v, path, data)
		_, err := v.ReadSealed(sealedName)
		wantCause(t, "ReadSealed "+string(want), err, want, path)
		_, err = ReadSealedWithIdentity(v.opts, v.identity.key, sealedName)
		wantCause(t, "ReadSealedWithIdentity "+string(want), err, want, path)
	}
}

func TestDecryptCauseOfPendingEntry(t *testing.T) {
	v, locked := created(t, map[string]string{"k": "example-1"})
	name := addPending(t, v, testEntry{Name: "n", Value: "example-2"})
	path := filepath.Join(v.pendingPath(), name)
	good := readBytes(t, path)
	for want, data := range preparedFiles(t, v, good) {
		if want == "payload" {
			want = CauseDamaged
		}
		overwrite(t, v, path, data)
		_, err := locked.Unlock(testPassphrase)
		wantCause(t, "Unlock "+string(want), err, want, path)
	}
}

func TestDecryptCauseTextsAreFixed(t *testing.T) {
	seen := map[string]DecryptCause{}
	for _, c := range []DecryptCause{CauseWrongKey, CauseNotAge, CauseDamaged, ""} {
		text := (&DocumentError{Path: "p", Kind: DocumentDecrypt, Cause: c}).Error()
		if other, ok := seen[text]; ok && !(c == "" && other == CauseDamaged) {
			t.Errorf("causes %q and %q share the text %q", other, c, text)
		}
		seen[text] = c
	}
}
