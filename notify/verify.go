package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// signaturePrefix names the algorithm in front of the hex digest.
const signaturePrefix = "sha256="

// DefaultMaxAge is how far the timestamp of a webhook may lie from the receiver's clock when Verify is
// given no other bound.
const DefaultMaxAge = 5 * time.Minute

// Errors of Verify, to be matched with errors.Is.
var (
	ErrSignature = errors.New("the webhook signature does not match")
	ErrStale     = errors.New("the webhook timestamp is outside the accepted window")
)

// Sign returns the signature of a webhook: "sha256=" and the hex HMAC-SHA256, keyed with key, of the
// timestamp, a dot and the body.
func Sign(key []byte, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks a received webhook against the key both sides share: the signature must match the
// timestamp and the body, and the timestamp must lie within maxAge of now in either direction, so an
// old request cannot be replayed. maxAge zero is DefaultMaxAge. The comparison runs in constant time.
func Verify(key []byte, timestamp, signature string, body []byte, now time.Time, maxAge time.Duration) error {
	if len(key) == 0 {
		return ErrSignature
	}
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	digest, ok := strings.CutPrefix(signature, signaturePrefix)
	got, err := hex.DecodeString(digest)
	if !ok || err != nil {
		return ErrSignature
	}
	want, _ := hex.DecodeString(strings.TrimPrefix(Sign(key, timestamp, body), signaturePrefix))
	if !hmac.Equal(got, want) {
		return ErrSignature
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrStale
	}
	if age := now.Sub(time.Unix(seconds, 0)); age > maxAge || age < -maxAge {
		return ErrStale
	}
	return nil
}
