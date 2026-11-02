package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Fingerprint returns the lowercase hex SHA-256 of label followed by the JSON encoding of state.
//
// The JSON encoding is the canonical form: encoding/json writes struct fields in their declared order and
// map keys sorted, so the same state always yields the same bytes. The application makes the state
// canonical before it hands it over, for instance by sorting lists whose order carries no meaning. A
// json.RawMessage is used in its compact form. label separates these fingerprints from every other hash of
// the same bytes and must not be empty.
func Fingerprint(label string, state any) (string, error) {
	_, fingerprint, err := encode(label, state)
	return fingerprint, err
}

// encode returns the canonical JSON of state and its fingerprint.
func encode(label string, state any) (json.RawMessage, string, error) {
	if label == "" {
		return nil, "", errors.New("approval: a fingerprint label must not be empty")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, "", fmt.Errorf("approval: the state has no JSON form: %w", err)
	}
	h := sha256.New()
	h.Write([]byte(label))
	h.Write(data)
	return data, hex.EncodeToString(h.Sum(nil)), nil
}
