package auditlog

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testSecret = []byte("example secret, not a real key")

func testKey(t *testing.T, label string) *Key {
	t.Helper()
	key, err := DeriveKey(testSecret, label)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestDeriveKey(t *testing.T) {
	if _, err := DeriveKey(nil, "example label"); err == nil {
		t.Error("DeriveKey without a secret succeeded")
	}
	if _, err := DeriveKey(testSecret, ""); err == nil {
		t.Error("DeriveKey without a label succeeded")
	}
	a, b, other := testKey(t, "example label"), testKey(t, "example label"), testKey(t, "other label")
	if !bytes.Equal(a.key, b.key) || bytes.Equal(a.key, other.key) || len(a.key) != sha256.Size {
		t.Fatal("the key must depend on secret and label only, and be 32 bytes long")
	}
	want, err := hkdf.Key(sha256.New, testSecret, nil, "example label", sha256.Size)
	if err != nil || !bytes.Equal(a.key, want) {
		t.Fatal("the key must be HKDF-SHA256 of the secret with the label as context and no salt")
	}
	a.Clear()
	if a.usable() {
		t.Fatal("a cleared key must not be usable")
	}
	if _, err := a.Check([][]byte{[]byte("x")}); err == nil {
		t.Fatal("a cleared key must not check")
	}
	if (*Key)(nil).usable() || (&Key{}).usable() {
		t.Fatal("a nil or zero key must not be usable")
	}
}

func TestSignedLinesFollowTheDefinition(t *testing.T) {
	key := testKey(t, "example label")
	log := newTestLog(t, day(1), Options[testEntry]{})
	signed := log.with(log.WithKey(key))
	if !signed.Signs() || log.Signs() {
		t.Fatal("only the log with the key signs")
	}
	mustAppend(t, signed, "example.a")
	line := readLines(t, dayPath(log, 1))[0]
	at := bytes.LastIndex(line, []byte(`,"mac":"`))
	unsigned := append(append([]byte(nil), line[:at]...), `,"mac":""}`...)
	h := hmac.New(sha256.New, key.key)
	h.Write(unsigned)
	want := `,"mac":"` + hex.EncodeToString(h.Sum(nil)) + `"}`
	if string(line[at:]) != want {
		t.Fatalf("line = %s, want it to end with %s", line, want)
	}
	if bytes.Contains(line, key.key) || bytes.Contains(line, []byte(hex.EncodeToString(key.key))) || bytes.Contains(line, testSecret) {
		t.Fatal("a signed line must not hold key material")
	}
	report := verify(t, log.Log, key)
	if report.Broken || report.Days[0].Signatures(report.Checked) != SignaturesValid {
		t.Fatalf("report = %+v, want valid signatures", report)
	}
}

// signedDays returns a log of three days whose every line is signed with key.
func signedDays(t *testing.T, key *Key) *testLog {
	t.Helper()
	log := newTestLog(t, day(1), Options[testEntry]{RetentionDays: 2})
	signed := log.with(log.WithKey(key))
	mustAppend(t, signed, "example.a")
	signed.setNow(day(2))
	mustAppend(t, signed, "example.b")
	signed.setNow(day(4))
	mustAppend(t, signed, "example.c")
	return signed
}

func TestVerifyRefusesAWrongKey(t *testing.T) {
	log := signedDays(t, testKey(t, "example label"))
	report := verify(t, log.Log, testKey(t, "other label"))
	wantBroken(t, report, "2026-03-02", ProblemMAC, 1)
	for _, d := range report.Days {
		if d.Changed == 0 || d.Signatures(report.Checked) != SignaturesInvalid {
			t.Fatalf("day %+v, want every signed line changed", d)
		}
	}
}

func TestVerifyRefusesAChangedCheckValue(t *testing.T) {
	key := testKey(t, "example label")
	cases := map[string]func(mac []byte) []byte{
		"other value": func(mac []byte) []byte { return bytes.Repeat([]byte("0"), len(mac)) },
		"upper case":  func(mac []byte) []byte { return bytes.ToUpper(mac) },
		"too short":   func(mac []byte) []byte { return mac[:len(mac)-2] },
		"not hex":     func(mac []byte) []byte { return append(mac[:len(mac)-1:len(mac)-1], 'g') },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			log := signedDays(t, key)
			path := dayPath(log, 4)
			lines := readLines(t, path)
			last := lines[len(lines)-1]
			at := bytes.LastIndex(last, []byte(`,"mac":"`)) + len(`,"mac":"`)
			mac := bytes.ToLower(last[at : len(last)-2])
			changed := append(append(append([]byte(nil), last[:at]...), change(mac)...), `"}`...)
			lines[len(lines)-1] = changed
			writeLines(t, path, lines)
			// The newest line has no successor, so only its check value tells.
			report := verify(t, log.Log, key)
			wantBroken(t, report, "2026-03-04", ProblemMAC, len(lines))
			if report.Days[1].Changed != 1 {
				t.Fatalf("day = %+v, want one changed line", report.Days[1])
			}
			unchecked := verify(t, log.Log, nil)
			if name != "other value" && !unchecked.Broken {
				t.Fatalf("report = %+v, want a malformed check value to break the chain without a key too", unchecked)
			}
		})
	}
}

func TestVerifyDetectsAChangedSignedLineWithoutAKey(t *testing.T) {
	key := testKey(t, "example label")
	log := signedDays(t, key)
	path := dayPath(log, 2)
	lines := readLines(t, path)
	lines[0] = bytes.Replace(lines[0], []byte("example.b"), []byte("example.x"), 1)
	writeLines(t, path, lines)
	report := verify(t, log.Log, nil)
	wantBroken(t, report, "2026-03-04", ProblemChain, 1)
	if report.Days[0].Unchecked != 1 {
		t.Fatalf("day = %+v, want an unchecked line", report.Days[0])
	}
	wantBroken(t, verify(t, log.Log, key), "2026-03-02", ProblemMAC, 1)
}

func TestAnUnsignedLineAppendedByHandIsOnlyUnverified(t *testing.T) {
	key := testKey(t, "example label")
	log := signedDays(t, key)
	path := dayPath(log, 4)
	lines := readLines(t, path)
	forged, err := encodeLine(header{seq: 5, time: day(4), prevHash: hashHex(lines[len(lines)-1])}, []byte(`"action":"example.forged"`))
	if err != nil {
		t.Fatal(err)
	}
	writeLines(t, path, append(lines, forged))
	report := verify(t, log.Log, key)
	if report.Broken || report.Days[1].Unverified != 1 || report.Days[1].Signatures(report.Checked) != SignaturesNotChecked {
		t.Fatalf("report = %+v, want the forged line counted as unverified", report)
	}
}

type failingChecker struct {
	answers []bool
	err     error
}

func (c failingChecker) Check([][]byte) ([]bool, error) { return c.answers, c.err }

func TestVerifyFailsClosed(t *testing.T) {
	log := signedDays(t, testKey(t, "example label"))
	for name, checker := range map[string]Checker{
		"error":        failingChecker{err: errors.New("no answer")},
		"too few":      failingChecker{answers: []bool{true}},
		"too many":     failingChecker{answers: []bool{true, true, true, true, true}},
		"absent key":   &Key{},
		"cleared key":  func() *Key { k := testKey(t, "example label"); k.Clear(); return k }(),
		"nil key type": (*Key)(nil),
	} {
		if _, err := log.Verify(checker); err == nil {
			t.Errorf("%s: Verify succeeded", name)
		}
		if _, err := log.VerifyLines(checker, "", ""); err == nil {
			t.Errorf("%s: VerifyLines succeeded", name)
		}
	}
}

func TestSignedLogHoldsNoKeyMaterial(t *testing.T) {
	key := testKey(t, "example label")
	log := signedDays(t, key)
	err := filepath.WalkDir(log.Dir(), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, key.key) || strings.Contains(string(data), hex.EncodeToString(key.key)) || bytes.Contains(data, testSecret) {
			t.Errorf("%s holds key material", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
