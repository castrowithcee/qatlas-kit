package auditlog

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

// macSize is the length of a check value in hex digits: an HMAC-SHA256.
const macSize = 2 * sha256.Size

// macField is how the check value begins in a stored line. It is always the last member, so a line ends
// with macField, the value and `"}`, and an unsigned line ends with emptyMACSuffix. keyIDField begins the
// key ID of a signed line; it is always the member right before mac, so the key ID is read from the bytes
// alone, the same way by Check and by Verify.
var (
	macField       = []byte(`,"mac":"`)
	emptyMACSuffix = []byte(`,"mac":""}`)
	keyIDField     = []byte(`,"mac_key":`)
)

// ErrMalformedMAC reports a line whose check value is not one Append could have written.
var ErrMalformedMAC = errors.New("the check value is malformed")

// KeyLabel is a label a log key is derived with and the key ID that names it in a signed line, as the
// member mac_key right before mac. The application chooses both. ID zero names the label by the absence of
// mac_key, which is how every line signed under a key from DeriveKey reads.
type KeyLabel struct {
	Label string
	ID    uint
}

// Key is the log key. It signs lines and checks the check values of stored lines, and it never hands out
// its bytes. The zero value and a nil Key sign nothing. A Key is safe for concurrent use until Clear.
type Key struct {
	key []byte
	id  uint
	// previous holds the keys under the previous labels by key ID. They only check.
	previous map[uint][]byte
}

// DeriveKey returns the log key for secret: HKDF-SHA256 with secret as the input key material, no salt and
// label as the context, 32 bytes long. The same secret and label always yield the same key. The
// application chooses a label that no other purpose of the same secret uses. DeriveKey does not keep or
// change secret; clearing it stays the caller's job. The key signs under key ID zero, as the one from
// DeriveKeys with KeyLabel{Label: label} and no previous label does.
func DeriveKey(secret []byte, label string) (*Key, error) {
	return DeriveKeys(secret, KeyLabel{Label: label})
}

// DeriveKeys returns the log key for secret under current, derived as DeriveKey derives it, and beside it
// one key under each previous label, derived the same way. Lines are signed under current only and carry
// current.ID. A previous label serves the transition after the application changed its label: it checks
// the lines that carry its key ID and never signs one. Remove the previous labels with the next major
// release of the application. Every label must be non-empty and differ from the others, and so must every
// key ID.
func DeriveKeys(secret []byte, current KeyLabel, previous ...KeyLabel) (*Key, error) {
	if len(secret) == 0 {
		return nil, errors.New("no secret to derive the log key from")
	}
	labels := map[string]bool{}
	ids := map[uint]bool{}
	for i, l := range append([]KeyLabel{current}, previous...) {
		switch {
		case l.Label == "":
			return nil, errors.New("no label to derive the log key with")
		case i > 0 && l.Label == current.Label:
			return nil, errors.New("a previous label of the log key must differ from the current one")
		case labels[l.Label]:
			return nil, errors.New("a label of the log key is given twice")
		case ids[l.ID]:
			return nil, fmt.Errorf("the key ID %d of the log key is given twice", l.ID)
		}
		labels[l.Label], ids[l.ID] = true, true
	}
	k := &Key{id: current.ID}
	var err error
	if k.key, err = hkdf.Key(sha256.New, secret, nil, current.Label, sha256.Size); err != nil {
		return nil, errors.New("cannot derive the log key")
	}
	for _, l := range previous {
		key, err := hkdf.Key(sha256.New, secret, nil, l.Label, sha256.Size)
		if err != nil {
			k.Clear()
			return nil, errors.New("cannot derive the log key")
		}
		if k.previous == nil {
			k.previous = make(map[uint][]byte, len(previous))
		}
		k.previous[l.ID] = key
	}
	return k, nil
}

// Clear overwrites the key and the keys under the previous labels, after which it signs nothing and checks
// nothing.
func (k *Key) Clear() {
	if k != nil {
		clear(k.key)
		k.key = nil
		for _, key := range k.previous {
			clear(key)
		}
		k.previous = nil
	}
}

func (k *Key) usable() bool { return k != nil && len(k.key) > 0 }

// keyFor returns the key that checks lines with key ID id, nil when there is none.
func (k *Key) keyFor(id uint) []byte {
	if id == k.id {
		return k.key
	}
	return k.previous[id]
}

// macWith is the check value of an unsigned line: the lowercase hex HMAC-SHA256, keyed with key, of the
// exact bytes of the line encoded with an empty mac member, that is, ending in `,"mac":""}`, without a
// newline. The prev_hash of the next line is the SHA-256 of the whole stored line, check value included.
func macWith(key, unsigned []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(unsigned)
	sum := h.Sum(nil)
	out := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(out, sum)
	return out
}

// sign returns unsigned with its empty mac member filled with its check value. It refuses a line that
// does not carry the key ID of the current label, so nothing is ever signed under a previous one.
func (k *Key) sign(unsigned []byte) ([]byte, error) {
	if !bytes.HasSuffix(unsigned, emptyMACSuffix) {
		return nil, errors.New("a line to sign does not end with its empty check value")
	}
	if id, err := keyIDOf(unsigned); err != nil || id != k.id {
		return nil, errors.New("a line to sign does not carry the key ID of the current label")
	}
	mac := macWith(k.key, unsigned)
	prefix := unsigned[:len(unsigned)-len(`""}`)]
	signed := make([]byte, 0, len(prefix)+len(mac)+3)
	signed = append(signed, prefix...)
	signed = append(signed, '"')
	signed = append(signed, mac...)
	return append(signed, '"', '}'), nil
}

// Check reports for each stored line whether its check value is the one the key under the label its key
// ID names gives it, comparing in constant time. A line whose check value is malformed, or whose key ID
// names no label of the key, does not match.
func (k *Key) Check(lines [][]byte) ([]bool, error) {
	if !k.usable() {
		return nil, errors.New("the log key is not available")
	}
	valid := make([]bool, len(lines))
	for i, line := range lines {
		if unsigned, mac, id, err := unsignedOf(line); err == nil {
			if key := k.keyFor(id); key != nil {
				valid[i] = hmac.Equal(macWith(key, unsigned), mac)
			}
		}
	}
	return valid, nil
}

// unsignedOf splits a signed line into the bytes its check value was computed over, the check value and
// the key ID. It fails with ErrMalformedMAC unless the line ends with a mac member of exactly macSize
// lowercase hex digits and a key ID that keyIDOf reads, the only form sign writes.
func unsignedOf(line []byte) (unsigned, mac []byte, id uint, err error) {
	at := bytes.LastIndex(line, macField)
	if at < 0 {
		return nil, nil, 0, ErrMalformedMAC
	}
	rest := line[at+len(macField):]
	if len(rest) != macSize+2 || rest[macSize] != '"' || rest[macSize+1] != '}' {
		return nil, nil, 0, ErrMalformedMAC
	}
	mac = rest[:macSize]
	for _, c := range mac {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return nil, nil, 0, ErrMalformedMAC
		}
	}
	unsigned = make([]byte, 0, at+len(emptyMACSuffix))
	unsigned = append(unsigned, line[:at]...)
	unsigned = append(unsigned, emptyMACSuffix...)
	if id, err = keyIDOf(unsigned); err != nil {
		return nil, nil, 0, err
	}
	return unsigned, mac, id, nil
}

func wellFormed(raw []byte) bool {
	_, _, _, err := unsignedOf(raw)
	return err == nil
}

// keyIDOf reads the key ID of a line that ends with emptyMACSuffix: the number of a mac_key member right
// before mac, or zero when no number follows mac_key there. A number there that is zero, has a leading
// zero or does not fit is malformed, since sign never writes one.
func keyIDOf(unsigned []byte) (uint, error) {
	rest := unsigned[:len(unsigned)-len(emptyMACSuffix)]
	start := len(rest)
	for start > 0 && rest[start-1] >= '0' && rest[start-1] <= '9' {
		start--
	}
	if !bytes.HasSuffix(rest[:start], keyIDField) {
		return 0, nil
	}
	digits := rest[start:]
	if len(digits) == 0 || digits[0] == '0' {
		return 0, ErrMalformedMAC
	}
	id, err := strconv.ParseUint(string(digits), 10, strconv.IntSize)
	if err != nil {
		return 0, ErrMalformedMAC
	}
	return uint(id), nil
}

// Checker checks the check values of stored lines and answers one result per line, in order. A *Key is
// one; a process that holds the key can offer another without handing it out. An error means the check
// could not be made at all, which Verify never reports as valid or invalid.
type Checker interface {
	Check(lines [][]byte) ([]bool, error)
}
