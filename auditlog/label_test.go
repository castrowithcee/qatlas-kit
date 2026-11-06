package auditlog

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"
)

// A log that changed its label: lines under previousLabel carry no key ID, lines under currentLabel carry
// currentKeyID.
const (
	previousLabel = "example log v1"
	currentLabel  = "example log v2"
	currentKeyID  = 2
)

func relabeledKey(t *testing.T) *Key {
	t.Helper()
	key, err := DeriveKeys(testSecret, KeyLabel{Label: currentLabel, ID: currentKeyID}, KeyLabel{Label: previousLabel})
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// labelMAC is the check value of unsigned under label, computed from the definition rather than through Key.
func labelMAC(t *testing.T, label string, unsigned []byte) string {
	t.Helper()
	key, err := hkdf.Key(sha256.New, testSecret, nil, label, sha256.Size)
	if err != nil {
		t.Fatal(err)
	}
	h := hmac.New(sha256.New, key)
	h.Write(unsigned)
	return hex.EncodeToString(h.Sum(nil))
}

// signWith fills the empty check value of unsigned with its check value under key, whatever key ID the
// line carries.
func signWith(key, unsigned []byte) []byte {
	out := append([]byte(nil), unsigned[:len(unsigned)-len(`""}`)]...)
	out = append(append(out, '"'), macWith(key, unsigned)...)
	return append(out, '"', '}')
}

func TestDeriveKeysRefusesBadLabels(t *testing.T) {
	cur := KeyLabel{Label: currentLabel, ID: currentKeyID}
	prev := KeyLabel{Label: previousLabel}
	for name, labels := range map[string][]KeyLabel{
		"no current label":              {{ID: currentKeyID}, prev},
		"no previous label":             {cur, {ID: 1}},
		"previous label is the current": {cur, {Label: currentLabel}},
		"previous label twice":          {cur, prev, {Label: previousLabel, ID: 1}},
		"key ID of the current twice":   {cur, {Label: previousLabel, ID: currentKeyID}},
		"previous key ID twice":         {cur, prev, {Label: "example log v0"}},
	} {
		if key, err := DeriveKeys(testSecret, labels[0], labels[1:]...); err == nil || key != nil {
			t.Errorf("%s: DeriveKeys succeeded", name)
		}
	}
	if _, err := DeriveKeys(nil, cur, prev); err == nil {
		t.Error("DeriveKeys without a secret succeeded")
	}
}

func TestDeriveKeysDerivesEveryLabel(t *testing.T) {
	plain := testKey(t, previousLabel)
	if plain.id != 0 || plain.previous != nil {
		t.Fatal("DeriveKey must give the current label under key ID zero and no previous label")
	}
	key := relabeledKey(t)
	unsigned := []byte(`{"seq":1,"mac":""}`)
	if string(macWith(key.key, unsigned)) != labelMAC(t, currentLabel, unsigned) || key.id != currentKeyID ||
		string(macWith(key.previous[0], unsigned)) != labelMAC(t, previousLabel, unsigned) || len(key.previous) != 1 {
		t.Fatal("each label must be derived as DeriveKey derives it, under its own key ID")
	}
	previous := key.previous[0]
	key.Clear()
	if key.usable() || key.previous != nil || !bytes.Equal(previous, make([]byte, sha256.Size)) {
		t.Fatal("Clear must overwrite the keys under the previous labels as well")
	}
}

// relabeledLog returns a log whose day 1 was written before the change of label, signed under
// previousLabel without a key ID and with an unsigned line between, and whose day 2 was written after
// it, signed under currentLabel with currentKeyID, again with an unsigned line between. before writes as
// the log did before the change, after as it does since.
func relabeledLog(t *testing.T) (before, after *testLog) {
	t.Helper()
	log := newTestLog(t, day(1), Options[testEntry]{})
	before = log.with(log.WithKey(testKey(t, previousLabel)))
	mustAppend(t, before, "example.a")
	mustAppend(t, log, "example.unsigned")
	mustAppend(t, before, "example.b")
	next := newTestLog(t, day(2), Options[testEntry]{Dir: log.Dir(), KeyID: currentKeyID})
	after = next.with(next.WithKey(relabeledKey(t)))
	mustAppend(t, after, "example.c")
	mustAppend(t, next, "example.unsigned")
	mustAppend(t, after, "example.d")
	return before, after
}

func TestLinesUnderPreviousThenCurrentLabelVerify(t *testing.T) {
	before, after := relabeledLog(t)
	for _, line := range readLines(t, dayPath(before, 1)) {
		if bytes.Contains(line, []byte("mac_key")) {
			t.Fatalf("line %s written before the change carries a key ID", line)
		}
	}
	for i, line := range readLines(t, dayPath(after, 2)) {
		at := bytes.LastIndex(line, macField)
		if i == 1 {
			if bytes.Contains(line, []byte("mac_key")) || !bytes.HasSuffix(line, emptyMACSuffix) {
				t.Fatalf("unsigned line = %s, want no key ID", line)
			}
			continue
		}
		unsigned := append(append([]byte(nil), line[:at]...), emptyMACSuffix...)
		want := `","mac_key":2,"mac":"` + labelMAC(t, currentLabel, unsigned) + `"}`
		if !bytes.HasSuffix(line, []byte(want)) {
			t.Fatalf("line = %s, want it to end with %s", line, want)
		}
	}

	report := verify(t, after.Log, relabeledKey(t))
	if report.Broken || !report.Checked || len(report.Days) != 2 || report.Days[0].Changed+report.Days[1].Changed != 0 {
		t.Fatalf("report = %+v, want an intact, checked chain", report)
	}
	days, err := after.VerifyLines(relabeledKey(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range days {
		for i, line := range d.Lines {
			want := MACValid
			if i == 1 {
				want = MACNone
			}
			if line.MAC != want || line.Chain != ChainIntact {
				t.Fatalf("%s line %d = %+v, want MAC state %d", d.Date, i+1, line, want)
			}
		}
	}
	unchecked := verify(t, after.Log, nil)
	if unchecked.Broken || unchecked.Days[0].Unchecked != 2 || unchecked.Days[1].Unchecked != 2 {
		t.Fatalf("report without a checker = %+v, want an intact chain", unchecked)
	}

	// A key without the current label checks no line that names it, and neither does one under a foreign label.
	wantBroken(t, verify(t, after.Log, testKey(t, previousLabel)), "2026-03-02", ProblemMAC, 1)
	foreign, err := DeriveKeys(testSecret, KeyLabel{Label: "other log v2", ID: currentKeyID}, KeyLabel{Label: previousLabel})
	if err != nil {
		t.Fatal(err)
	}
	wantBroken(t, verify(t, after.Log, foreign), "2026-03-02", ProblemMAC, 1)

	// The current key ID comes from the options, not from the key.
	wantBroken(t, verify(t, before.Log, nil), "2026-03-02", ProblemMAC, 1)
}

func TestLineUnderPreviousLabelAfterCurrentBreaksTheChain(t *testing.T) {
	before, after := relabeledLog(t)
	before.setNow(day(3))
	mustAppend(t, before, "example.back")
	for name, checker := range map[string]Checker{"with a key": relabeledKey(t), "without a key": nil} {
		report := verify(t, after.Log, checker)
		wantBroken(t, report, "2026-03-03", ProblemMAC, 1)
		if report.Days[2].Changed != 1 || report.Days[0].Changed+report.Days[1].Changed != 0 {
			t.Fatalf("%s: report = %+v, want only the line back under the previous label changed", name, report)
		}
		days, err := after.VerifyLines(checker, "2026-03-03", "")
		if err != nil || days[0].Lines[0].MAC != MACInvalid {
			t.Fatalf("%s: days = %+v (%v), want the line back under the previous label invalid", name, days, err)
		}
	}
}

func TestPreviousLabelNeverSigns(t *testing.T) {
	key := relabeledKey(t)
	unsigned := func(keyID uint) []byte {
		line, err := encodeLine(header{seq: 1, time: day(1), prevHash: genesisHash, keyID: keyID}, []byte(`"action":"example.a"`))
		if err != nil {
			t.Fatal(err)
		}
		return line
	}
	for _, keyID := range []uint{0, 7} {
		if _, err := key.sign(unsigned(keyID)); err == nil {
			t.Errorf("sign of a line with key ID %d succeeded", keyID)
		}
	}
	if _, err := key.sign(unsigned(currentKeyID)); err != nil {
		t.Fatalf("sign of a line with the current key ID: %v", err)
	}
	if _, err := testKey(t, previousLabel).sign(unsigned(currentKeyID)); err == nil {
		t.Error("a key under key ID zero signed a line with another key ID")
	}

	// A log whose key ID is not the one the key signs under writes nothing.
	for name, pair := range map[string]struct {
		keyID uint
		key   *Key
	}{
		"current key, log without key ID": {0, key},
		"plain key, log with key ID":      {currentKeyID, testKey(t, currentLabel)},
	} {
		log := newTestLog(t, day(1), Options[testEntry]{KeyID: pair.keyID})
		if err := log.WithKey(pair.key).Append(testEntry{Action: "example.a"}); err == nil {
			t.Errorf("%s: Append succeeded", name)
		}
		if files, _ := dayFiles(log.Dir()); len(files) != 0 {
			t.Errorf("%s: a refused Append wrote %v", name, files)
		}
	}
}

func TestCheckRefusesUnknownAndMalformedKeyIDs(t *testing.T) {
	key := relabeledKey(t)
	line := func(keyID string) []byte {
		raw, err := encodeLine(header{seq: 1, time: day(1), prevHash: genesisHash}, []byte(`"action":"example.a"`))
		if err != nil {
			t.Fatal(err)
		}
		if keyID != "" {
			raw = bytes.Replace(raw, emptyMACSuffix, []byte(`,"mac_key":`+keyID+string(emptyMACSuffix)), 1)
		}
		return signWith(key.key, raw)
	}
	valid, err := key.Check([][]byte{line("2"), line("7"), line(""), line("02"), line("0"), line("18446744073709551616")})
	if err != nil {
		t.Fatal(err)
	}
	if want := []bool{true, false, false, false, false, false}; !slices.Equal(valid, want) {
		t.Fatalf("Check = %v, want %v", valid, want)
	}
	for _, keyID := range []string{"02", "0", "18446744073709551616"} {
		if wellFormed(line(keyID)) {
			t.Errorf("a line with key ID %s is well-formed", keyID)
		}
	}

	// Without a key, a malformed key ID breaks the chain as a malformed check value does.
	_, after := relabeledLog(t)
	path := dayPath(after, 2)
	lines := readLines(t, path)
	last := len(lines) - 1
	lines[last] = bytes.Replace(lines[last], []byte(`"mac_key":2`), []byte(`"mac_key":0`), 1)
	writeLines(t, path, lines)
	wantBroken(t, verify(t, after.Log, nil), "2026-03-02", ProblemMAC, len(lines))
}
