package auditlog

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
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
	wantBroken(t, verify(t, before.Log, nil), "2026-03-02", ProblemKeyIDChange, 1)
}

func TestLineUnderPreviousLabelAfterCurrentBreaksTheChain(t *testing.T) {
	before, after := relabeledLog(t)
	before.setNow(day(3))
	mustAppend(t, before, "example.back")
	for name, checker := range map[string]Checker{"with a key": relabeledKey(t), "without a key": nil} {
		report := verify(t, after.Log, checker)
		wantBroken(t, report, "2026-03-03", ProblemKeyIDChange, 1)
		if report.Days[2].ProblemSeq != 7 || report.Days[2].Changed != 1 || report.Days[0].Changed+report.Days[1].Changed != 0 {
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

// wantProblem fails unless the first problem of the report is problem at line of day, on the number of
// changed lines given.
func wantProblem(t *testing.T, name string, report Report, day string, problem Problem, line int) {
	t.Helper()
	if !report.Broken {
		t.Fatalf("%s: report = %+v, want a broken chain", name, report)
	}
	for _, d := range report.Days {
		if d.Problem != ProblemNone {
			if d.Date != day || d.Problem != problem || d.ProblemLine != line || d.Changed < 1 {
				t.Fatalf("%s: first problem on %s = %q at line %d (changed %d), want %q at line %d of %s",
					name, d.Date, d.Problem, d.ProblemLine, d.Changed, problem, line, day)
			}
			return
		}
	}
	t.Fatalf("%s: report = %+v names no problem", name, report)
}

func TestKeyIDChangeBackIsItsOwnProblem(t *testing.T) {
	key7, err := DeriveKeys(testSecret, KeyLabel{Label: "example log v3", ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	unknownKeyID := func(before, after *testLog) *testLog {
		next := newTestLog(t, day(3), Options[testEntry]{Dir: after.Dir(), KeyID: 7})
		next.clock = before.clock
		return next.with(next.WithKey(key7))
	}
	cases := map[string]struct {
		write func(before, after *testLog)
		day   string
		seq   uint64
	}{
		"back to the previous label": {func(before, _ *testLog) {
			before.setNow(day(3))
			mustAppend(t, before, "example.back")
		}, "2026-03-03", 7},
		"back after an unsigned entry": {func(before, _ *testLog) {
			before.setNow(day(3))
			mustAppend(t, before.with(before.WithKey(nil)), "example.unsigned")
			mustAppend(t, before, "example.back")
		}, "2026-03-03", 8},
		"back on the day of the change": {func(before, _ *testLog) {
			before.setNow(day(2))
			mustAppend(t, before, "example.back")
		}, "2026-03-02", 7},
		"an unknown key ID": {func(before, after *testLog) {
			before.setNow(day(3))
			mustAppend(t, unknownKeyID(before, after), "example.unknown")
		}, "2026-03-03", 7},
	}
	for name, c := range cases {
		before, after := relabeledLog(t)
		c.write(before, after)
		for who, checker := range map[string]Checker{"with a key": relabeledKey(t), "without a key": nil} {
			report := verify(t, after.Log, checker)
			label := name + ", " + who
			wantProblem(t, label, report, c.day, ProblemKeyIDChange, len(readLines(t, filepath.Join(after.Dir(), c.day+".jsonl"))))
			for _, d := range report.Days {
				if d.Problem != ProblemNone && (d.ProblemSeq != c.seq || d.Changed != 1) {
					t.Fatalf("%s: day = %+v, want seq %d and one changed line", label, d, c.seq)
				}
			}
			days, err := after.VerifyLines(checker, c.day, c.day)
			if err != nil {
				t.Fatal(err)
			}
			lines := days[0].Lines
			if last := lines[len(lines)-1]; last.MAC != MACInvalid {
				t.Fatalf("%s: last line = %+v, want MACInvalid", label, last)
			}
		}
	}

	// A wrong check value on such a line is still the change back, with a key.
	before, after := relabeledLog(t)
	before.setNow(day(3))
	mustAppend(t, before, "example.back")
	path := dayPath(after, 3)
	lines := readLines(t, path)
	lines[0][len(lines[0])-3] ^= 1 // another hex digit
	writeLines(t, path, lines)
	wantProblem(t, "wrong check value", verify(t, after.Log, relabeledKey(t)), "2026-03-03", ProblemKeyIDChange, 1)

	// A malformed key ID stays a problem of the check value.
	_, after = relabeledLog(t)
	path = dayPath(after, 2)
	lines = readLines(t, path)
	lines[2] = bytes.Replace(lines[2], []byte(`"mac_key":2`), []byte(`"mac_key":0`), 1)
	writeLines(t, path, lines)
	for _, checker := range []Checker{relabeledKey(t), nil} {
		wantProblem(t, "malformed key ID", verify(t, after.Log, checker), "2026-03-02", ProblemMAC, 3)
	}

	// A single change from the previous label to the current one stays intact.
	_, after = relabeledLog(t)
	for _, checker := range []Checker{relabeledKey(t), nil} {
		if report := verify(t, after.Log, checker); report.Broken || report.Days[0].Changed+report.Days[1].Changed != 0 {
			t.Fatalf("report = %+v, want an intact chain", report)
		}
	}
}

// signedLog is a log whose day files hold signed lines, with the key that checks them.
type signedLog struct {
	*testLog
	key *Key
}

// signedLogs returns logs with signed lines of every shape: with a key ID, without one, and a retention
// cut marker.
func signedLogs(t *testing.T) map[string]signedLog {
	t.Helper()
	_, current := relabeledLog(t)

	plain := newTestLog(t, day(1), Options[testEntry]{})
	signedPlain := plain.with(plain.WithKey(testKey(t, previousLabel)))
	for _, action := range []string{"example.a", "example.b", "example.c"} {
		mustAppend(t, signedPlain, action)
	}

	cut := newTestLog(t, day(1), Options[testEntry]{RetentionDays: 2, KeyID: currentKeyID})
	signedCut := cut.with(cut.WithKey(relabeledKey(t)))
	mustAppend(t, signedCut, "example.a")
	cut.setNow(day(5))
	mustAppend(t, signedCut, "example.b")
	mustAppend(t, signedCut, "example.c")
	lines := readLines(t, dayPath(cut, 5))
	if marker := parseLine[testEntry](lines[0]); !marker.ok || marker.rec.Kind != KindCut {
		t.Fatalf("day 5 = %q, want a retention cut marker first", lines)
	}
	return map[string]signedLog{
		"key ID":    {current, relabeledKey(t)},
		"no key ID": {signedPlain, testKey(t, previousLabel)},
		"marker":    {signedCut, relabeledKey(t)},
	}
}

// misplacements returns line, which is signed, with its mac_key member moved, doubled or escaped.
func misplacements(line []byte) map[string][]byte {
	stripped := line
	if at := bytes.Index(line, keyIDField); at >= 0 {
		end := at + len(keyIDField)
		for end < len(line) && line[end] >= '0' && line[end] <= '9' {
			end++
		}
		stripped = append(append([]byte(nil), line[:at]...), line[end:]...)
	}
	insert := func(before, member string) []byte {
		return bytes.Replace(stripped, []byte(before), []byte(member+before), 1)
	}
	out := map[string][]byte{
		"doubled": insert(`,"mac":"`, `,"mac_key":2,"mac_key":2`),
		"escaped": insert(`,"mac":"`, `,"mac\u005fkey":2`),
		// encoding/json reads these spellings as mac_key, so they count as one.
		"upper case, before mac":   insert(`,"mac":"`, `,"MAC_KEY":2`),
		"upper case, in place":     bytes.Replace(line, keyIDField, []byte(`,"MAC_KEY":`), 1),
		"mixed case, moved":        insert(`,"time"`, `,"Mac_Key":2`),
		"escaped mixed case":       insert(`,"mac":"`, `,"MAC\u005fKey":2`),
		"kelvin sign":              insert(`,"mac":"`, ",\"mac_\u212Aey\":2"),
		"escaped kelvin sign":      insert(`,"mac":"`, `,"mac_\u212aey":2`),
		"doubled, mixed case":      insert(`,"mac":"`, `,"mac_key":2,"MAC_KEY":2`),
		"doubled, mixed case last": insert(`,"mac":"`, `,"MAC_KEY":2,"mac_key":2`),
		"doubled, moved":           insert(`,"time"`, `,"mac_key":2,"Mac_Key":2`),
	}
	for _, id := range []string{"0", "2", "7"} {
		out["moved, value "+id] = insert(`,"time"`, `,"mac_key":`+id)
	}
	return out
}

func TestMisplacedKeyIDBreaksTheChain(t *testing.T) {
	for name, log := range signedLogs(t) {
		files, err := dayFiles(log.Dir())
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			date := dayFilePattern.FindStringSubmatch(file)[1]
			path := filepath.Join(log.Dir(), file)
			orig := readLines(t, path)
			for i, line := range orig {
				if bytes.HasSuffix(line, emptyMACSuffix) {
					continue // an unsigned line
				}
				for how, changed := range misplacements(line) {
					if bytes.Equal(changed, line) {
						continue
					}
					lines := append([][]byte(nil), orig...)
					lines[i] = changed
					writeLines(t, path, lines)
					for who, checker := range map[string]Checker{"with a key": log.key, "without a key": nil} {
						wantProblem(t, name+" "+date+" line "+string(rune('1'+i))+" "+how+" "+who, verify(t, log.Log, checker), date, ProblemMAC, i+1)
					}
				}
			}
			writeLines(t, path, orig)
		}
	}
}

func TestMisplacedKeyIDGoesBeforeOtherProblems(t *testing.T) {
	// After a line under the current key ID, a moved mac_key naming another is no change back.
	_, after := relabeledLog(t)
	path := dayPath(after, 2)
	lines := readLines(t, path)
	lines[2] = misplacements(lines[2])["moved, value 7"]
	writeLines(t, path, lines)
	for _, checker := range []Checker{relabeledKey(t), nil} {
		wantProblem(t, "after the current key ID", verify(t, after.Log, checker), "2026-03-02", ProblemMAC, 3)
	}

	// A broken chain in the same line is named first.
	_, after = relabeledLog(t)
	path = dayPath(after, 2)
	lines = readLines(t, path)
	lines[2] = misplacements(lines[2])["moved, value 0"]
	lines[2] = bytes.Replace(lines[2], []byte(`"prev_hash":"`), []byte(`"prev_hash":"0`), 1)
	writeLines(t, path, lines)
	for _, checker := range []Checker{relabeledKey(t), nil} {
		wantProblem(t, "broken chain", verify(t, after.Log, checker), "2026-03-02", ProblemChain, 3)
	}
}

func TestKeyMemberOKRejectsAnyBytes(t *testing.T) {
	good := `{"seq":1,"mac_key":2,"mac":"x"}`
	for _, in := range []string{
		"", "{", "[]", "null", "{}", `{"mac":"x"}x`, `{"a":1,"a":`, `{"mac_key":2,"mac_key":2,"mac":"x"}`,
		`{"mac_key":"2","mac":"x"}`, `{"mac_key": 2,"mac":"x"}`, `{"mac_key":2,"seq":1,"mac":"x"}`,
		`{"mac\u005fkey":2,"mac":"x"}`, `{"MAC_KEY":2,"mac":"x"}`, `{"Mac_Key":2,"mac":"x"}`,
		"{\"mac_\u212Aey\":2,\"mac\":\"x\"}", `{"mac_\u212aey":2,"mac":"x"}`, `{"mac_key":2,"MAC_KEY":2,"mac":"x"}`,
		`{"MAC_KEY":2,"mac_key":2,"mac":"x"}`, `{"x":{"mac_key":2},"mac":"x"}x`, "\xff\xfe{", string(bytes.Repeat([]byte("["), 100000)),
		`{"a":` + string(bytes.Repeat([]byte("["), 100000)),
	} {
		if keyMemberOK([]byte(in)) && in != good {
			t.Errorf("keyMemberOK(%.40q) = true", in)
		}
	}
	for _, in := range []string{good, `{"seq":1,"mac":"x"}`, `{"x":{"mac_key":5},"mac":"x"}`} {
		if !keyMemberOK([]byte(in)) {
			t.Errorf("keyMemberOK(%q) = false", in)
		}
	}
}
