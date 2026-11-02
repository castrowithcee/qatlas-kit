package auditlog

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// macSize is the length of a check value in hex digits: an HMAC-SHA256.
const macSize = 2 * sha256.Size

// macField is how the check value begins in a stored line. It is always the last member, so a line ends
// with macField, the value and `"}`, and an unsigned line ends with emptyMACSuffix.
var (
	macField       = []byte(`,"mac":"`)
	emptyMACSuffix = []byte(`,"mac":""}`)
)

// ErrMalformedMAC reports a line whose check value is not one Append could have written.
var ErrMalformedMAC = errors.New("the check value is malformed")

// Key is the log key. It signs lines and checks the check values of stored lines, and it never hands out
// its bytes. The zero value and a nil Key sign nothing. A Key is safe for concurrent use until Clear.
type Key struct{ key []byte }

// DeriveKey returns the log key for secret: HKDF-SHA256 with secret as the input key material, no salt and
// label as the context, 32 bytes long. The same secret and label always yield the same key. The
// application chooses a label that no other purpose of the same secret uses. DeriveKey does not keep or
// change secret; clearing it stays the caller's job.
func DeriveKey(secret []byte, label string) (*Key, error) {
	switch {
	case len(secret) == 0:
		return nil, errors.New("no secret to derive the log key from")
	case label == "":
		return nil, errors.New("no label to derive the log key with")
	}
	key, err := hkdf.Key(sha256.New, secret, nil, label, sha256.Size)
	if err != nil {
		return nil, errors.New("cannot derive the log key")
	}
	return &Key{key: key}, nil
}

// Clear overwrites the key, after which it signs nothing and checks nothing.
func (k *Key) Clear() {
	if k != nil {
		clear(k.key)
		k.key = nil
	}
}

func (k *Key) usable() bool { return k != nil && len(k.key) > 0 }

// mac is the check value of an unsigned line: the lowercase hex HMAC-SHA256, keyed with the log key, of the
// exact bytes of the line encoded with an empty mac member, that is, ending in `,"mac":""}`, without a
// newline. The prev_hash of the next line is the SHA-256 of the whole stored line, check value included.
func (k *Key) mac(unsigned []byte) []byte {
	h := hmac.New(sha256.New, k.key)
	h.Write(unsigned)
	sum := h.Sum(nil)
	out := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(out, sum)
	return out
}

// sign returns unsigned with its empty mac member filled with its check value.
func (k *Key) sign(unsigned []byte) ([]byte, error) {
	if !bytes.HasSuffix(unsigned, emptyMACSuffix) {
		return nil, errors.New("a line to sign does not end with its empty check value")
	}
	mac := k.mac(unsigned)
	prefix := unsigned[:len(unsigned)-len(`""}`)]
	signed := make([]byte, 0, len(prefix)+len(mac)+3)
	signed = append(signed, prefix...)
	signed = append(signed, '"')
	signed = append(signed, mac...)
	return append(signed, '"', '}'), nil
}

// Check reports for each stored line whether its check value is the one the key gives it, comparing in
// constant time. A line whose check value is malformed does not match.
func (k *Key) Check(lines [][]byte) ([]bool, error) {
	if !k.usable() {
		return nil, errors.New("the log key is not available")
	}
	valid := make([]bool, len(lines))
	for i, line := range lines {
		if unsigned, mac, err := unsignedOf(line); err == nil {
			valid[i] = hmac.Equal(k.mac(unsigned), mac)
		}
	}
	return valid, nil
}

// unsignedOf splits a signed line into the bytes its check value was computed over and the check value. It
// fails with ErrMalformedMAC unless the line ends with a mac member of exactly macSize lowercase hex
// digits, the only form sign writes.
func unsignedOf(line []byte) (unsigned, mac []byte, err error) {
	at := bytes.LastIndex(line, macField)
	if at < 0 {
		return nil, nil, ErrMalformedMAC
	}
	rest := line[at+len(macField):]
	if len(rest) != macSize+2 || rest[macSize] != '"' || rest[macSize+1] != '}' {
		return nil, nil, ErrMalformedMAC
	}
	mac = rest[:macSize]
	for _, c := range mac {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return nil, nil, ErrMalformedMAC
		}
	}
	unsigned = make([]byte, 0, at+len(emptyMACSuffix))
	unsigned = append(unsigned, line[:at]...)
	return append(unsigned, emptyMACSuffix...), mac, nil
}

func wellFormed(raw []byte) bool {
	_, _, err := unsignedOf(raw)
	return err == nil
}

// Checker checks the check values of stored lines and answers one result per line, in order. A *Key is
// one; a process that holds the key can offer another without handing it out. An error means the check
// could not be made at all, which Verify never reports as valid or invalid.
type Checker interface {
	Check(lines [][]byte) ([]bool, error)
}
