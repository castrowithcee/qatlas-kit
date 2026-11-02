package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"filippo.io/age"
)

// identity is the vault key pair: the X25519 identity that decrypts the document and the pending
// entries, and its recipient, the content of the recipient file.
type identity struct {
	key       *age.X25519Identity
	recipient *age.X25519Recipient
}

func generateIdentity() (*identity, error) {
	key, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("cannot generate a vault key: %w", err)
	}
	return &identity{key: key, recipient: key.Recipient()}, nil
}

// wrapKey encrypts the private key to passphrase with age's scrypt recipient, the content of the key file.
func wrapKey(id *identity, passphrase string, workFactor int) ([]byte, error) {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	recipient.SetWorkFactor(workFactor)
	secret := []byte(id.key.String())
	defer clear(secret)
	return encryptTo(secret, recipient)
}

// unwrapKey recovers the identity from the key file and checks that it belongs to the recipient file. The
// age library's own messages are never passed on.
func unwrapKey(keyData []byte, passphrase string, recipientText []byte) (*identity, error) {
	if passphrase == "" {
		return nil, ErrWrongPassphrase
	}
	scrypt, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	plain, err := decryptFrom(keyData, scrypt)
	if err != nil {
		if errors.Is(err, age.ErrIncorrectIdentity) {
			return nil, ErrWrongPassphrase
		}
		return nil, fmt.Errorf("cannot decrypt the vault key: %w", ErrDamaged)
	}
	defer clear(plain)
	key, err := age.ParseX25519Identity(string(bytes.TrimSpace(plain)))
	if err != nil {
		return nil, fmt.Errorf("cannot read the vault key: %w", ErrDamaged)
	}
	return bindIdentity(key, recipientText)
}

// bindIdentity pairs key with the recipient file and refuses a key that is not the recipient's.
func bindIdentity(key *age.X25519Identity, recipientText []byte) (*identity, error) {
	recipient, err := parseRecipient(recipientText)
	if err != nil {
		return nil, err
	}
	if key.Recipient().String() != recipient.String() {
		return nil, ErrKeyMismatch
	}
	return &identity{key: key, recipient: recipient}, nil
}

func parseRecipient(text []byte) (*age.X25519Recipient, error) {
	recipient, err := age.ParseX25519Recipient(string(bytes.TrimSpace(text)))
	if err != nil {
		return nil, fmt.Errorf("cannot read the vault recipient: %w", ErrDamaged)
	}
	return recipient, nil
}

func encryptTo(plaintext []byte, recipient age.Recipient) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("cannot encrypt vault data: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("cannot encrypt vault data: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("cannot encrypt vault data: %w", err)
	}
	return buf.Bytes(), nil
}

func decryptFrom(data []byte, id age.Identity) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(data), id)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// checkSchema refuses a document that is not a JSON object with an accepted integer schema version.
func checkSchema(document []byte, schemas []int) error {
	var envelope struct {
		Schema *int `json:"schema"`
	}
	if err := json.Unmarshal(document, &envelope); err != nil || envelope.Schema == nil {
		return fmt.Errorf("the vault document is not a versioned JSON object: %w", ErrDamaged)
	}
	if !slices.Contains(schemas, *envelope.Schema) {
		return fmt.Errorf("%w: %d", ErrUnsupportedSchema, *envelope.Schema)
	}
	return nil
}
