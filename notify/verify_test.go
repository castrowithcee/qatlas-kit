package notify

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestVerifyAcceptsTheSignatureOfSign(t *testing.T) {
	key := []byte(invented("hook-key"))
	body := []byte(`{"event":"demo"}`)
	timestamp := strconv.FormatInt(fixedNow.Unix(), 10)
	if err := Verify(key, timestamp, Sign(key, timestamp, body), body, fixedNow.Add(time.Minute), 0); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRefusesWrongOrOldSignatures(t *testing.T) {
	key := []byte(invented("hook-key"))
	body := []byte(`{"event":"demo"}`)
	timestamp := strconv.FormatInt(fixedNow.Unix(), 10)
	signature := Sign(key, timestamp, body)
	cases := map[string]struct {
		key                  []byte
		timestamp, signature string
		body                 []byte
		now                  time.Time
		maxAge               time.Duration
		want                 error
	}{
		"other key":         {[]byte(invented("other-key")), timestamp, signature, body, fixedNow, 0, ErrSignature},
		"empty key":         {nil, timestamp, Sign(nil, timestamp, body), body, fixedNow, 0, ErrSignature},
		"altered body":      {key, timestamp, signature, []byte(`{"event":"other"}`), fixedNow, 0, ErrSignature},
		"altered timestamp": {key, strconv.FormatInt(fixedNow.Unix()+1, 10), signature, body, fixedNow, 0, ErrSignature},
		"missing prefix":    {key, timestamp, signature[len("sha256="):], body, fixedNow, 0, ErrSignature},
		"not hex":           {key, timestamp, "sha256=zz", body, fixedNow, 0, ErrSignature},
		"empty signature":   {key, timestamp, "", body, fixedNow, 0, ErrSignature},
		"truncated":         {key, timestamp, signature[:len(signature)-2], body, fixedNow, 0, ErrSignature},
		"too old":           {key, timestamp, signature, body, fixedNow.Add(DefaultMaxAge + time.Second), 0, ErrStale},
		"from the future":   {key, timestamp, signature, body, fixedNow.Add(-DefaultMaxAge - time.Second), 0, ErrStale},
		"older than bound":  {key, timestamp, signature, body, fixedNow.Add(31 * time.Second), 30 * time.Second, ErrStale},
		"not a number":      {key, "soon", Sign(key, "soon", body), body, fixedNow, 0, ErrStale},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Verify(tc.key, tc.timestamp, tc.signature, tc.body, tc.now, tc.maxAge); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
