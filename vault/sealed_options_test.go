package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSealedWithoutRecipientFile(t *testing.T) {
	v, _ := created(t, nil)
	if err := v.WriteSealed(sealedName, []byte("example settings")); err != nil {
		t.Fatal(err)
	}
	key := v.identity.key
	opts := v.opts
	sealed := filepath.Join(opts.Dir, sealedName)
	if err := os.Remove(v.recipientPath()); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadSealedWithIdentity(opts, key, sealedName); !errors.Is(err, ErrAbsent) {
		t.Errorf("without the option error = %v, want ErrAbsent", err)
	}
	got, err := ReadSealedWithIdentityOptions(opts, key, sealedName, AllowMissingRecipient())
	if err != nil || string(got) != "example settings" {
		t.Fatalf("with the option = %q, %v", got, err)
	}

	// The key is still needed.
	_, err = ReadSealedWithIdentityOptions(opts, foreignIdentity(t), sealedName, AllowMissingRecipient())
	wantCause(t, "wrong key", err, CauseWrongKey, sealed)

	// A damaged file is still damaged.
	good := readBytes(t, sealed)
	good[len(good)-1] ^= 0x01
	overwrite(t, v, sealed, good)
	_, err = ReadSealedWithIdentityOptions(opts, key, sealedName, AllowMissingRecipient())
	wantCause(t, "damaged", err, CauseDamaged, sealed)

	// A missing sealed file is still a missing file.
	if err := os.Remove(sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSealedWithIdentityOptions(opts, key, sealedName, AllowMissingRecipient()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing sealed file error = %v, want ErrNotExist", err)
	}
}

func TestAllowMissingRecipientKeepsTheBindingOfAnExistingFile(t *testing.T) {
	v, _ := created(t, nil)
	if err := v.WriteSealed(sealedName, []byte("example settings")); err != nil {
		t.Fatal(err)
	}
	other := foreignIdentity(t)
	overwrite(t, v, v.recipientPath(), []byte(other.Recipient().String()+"\n"))
	_, err := ReadSealedWithIdentityOptions(v.opts, v.identity.key, sealedName, AllowMissingRecipient())
	if !errors.Is(err, ErrKeyMismatch) {
		t.Errorf("mismatching recipient file error = %v, want ErrKeyMismatch", err)
	}
	overwrite(t, v, v.recipientPath(), []byte("not a recipient\n"))
	_, err = ReadSealedWithIdentityOptions(v.opts, v.identity.key, sealedName, AllowMissingRecipient())
	if !errors.Is(err, ErrDamaged) {
		t.Errorf("damaged recipient file error = %v, want ErrDamaged", err)
	}
}

// An application that means to decrypt a vault without a document file as an empty one writes the empty
// document itself; Decrypt alone refuses.
func TestDecryptWithoutDocumentFile(t *testing.T) {
	v, locked := created(t, map[string]string{"k": "example-1"})
	if err := os.Remove(v.documentPath()); err != nil {
		t.Fatal(err)
	}
	if err := locked.Decrypt(testPassphrase); !errors.Is(err, ErrDamaged) {
		t.Fatalf("Decrypt() without a document file error = %v, want ErrDamaged", err)
	}
	if _, err := os.Stat(v.keyPath()); err != nil {
		t.Fatalf("a refused Decrypt removed the key file: %v", err)
	}

	if _, err := locked.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	doc, err := locked.Document()
	if err != nil || doc != nil {
		t.Fatalf("Document() = %q, %v, want nil", doc, err)
	}
	if err := locked.Write(encodeDoc(t, map[string]string{})); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := locked.Decrypt(testPassphrase); err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	plain, err := locked.Document()
	if err != nil || len(decodeDoc(t, plain)) != 0 {
		t.Fatalf("Document() after Decrypt = %q, %v", plain, err)
	}
	wantState(t, locked, StateUnencrypted)
}
