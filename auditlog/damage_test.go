package auditlog

import (
	"bytes"
	"os"
	"testing"
)

const tornLine = `{"seq":3,"time":"2026-03-01T10:00:00Z","act`

// tearLastLine appends a line cut off by a crash, without a newline, to the file of day d.
func tearLastLine(t *testing.T, log *testLog, d int) {
	t.Helper()
	f, err := os.OpenFile(dayPath(log, d), os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(tornLine); err != nil {
		t.Fatal(err)
	}
}

func TestRefusedTornLineStopsAppend(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{})
	mustAppend(t, log, "example.a")
	tearLastLine(t, log, 1)
	before, _ := os.ReadFile(dayPath(log, 1))
	if err := log.Append(testEntry{Action: "example.b"}); err == nil {
		t.Fatal("Append succeeded after a torn line")
	}
	log.setNow(day(2))
	if err := log.Append(testEntry{Action: "example.b"}); err == nil {
		t.Fatal("Append on the next day succeeded after a torn line")
	}
	if after, _ := os.ReadFile(dayPath(log, 1)); !bytes.Equal(before, after) {
		t.Fatal("a refused Append changed the day file")
	}
	wantBroken(t, verify(t, log.Log, nil), "2026-03-01", ProblemUnparsable, 2)
}

func TestRefusedTornLineStopsRetention(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{RetentionDays: 2})
	mustAppend(t, log, "example.a")
	tearLastLine(t, log, 1)
	log.setNow(day(9))
	if err := log.Append(testEntry{Action: "example.b"}); err == nil {
		t.Fatal("Append succeeded removing a file that ends with a torn line")
	}
	if _, err := os.Stat(dayPath(log, 1)); err != nil {
		t.Fatalf("the refused Append removed the day file: %v", err)
	}
}

func TestDocumentedTornLineIsBridged(t *testing.T) {
	key := testKey(t, "example label")
	log := newTestLog(t, day(1), Options[testEntry]{TornLine: TornLineDocument})
	signed := log.with(log.WithKey(key))
	mustAppend(t, signed, "example.a")
	mustAppend(t, signed, "example.b")
	tearLastLine(t, log, 1)
	mustAppend(t, signed, "example.c")

	lines := readLines(t, dayPath(log, 1))
	marker := parseLine[testEntry](lines[3])
	if len(lines) != 5 || string(lines[2]) != tornLine || !marker.ok || marker.rec.Kind != KindDamage ||
		marker.rec.Damage.LineHash != hashHex([]byte(tornLine)) || marker.rec.Seq != 3 || marker.env.PrevHash != hashHex(lines[1]) {
		t.Fatalf("lines = %q, want the torn line ended and documented by a marker chained from the line before", lines)
	}
	report := verify(t, log.Log, key)
	if report.Broken || report.Days[0].Damaged != 1 || report.Days[0].Changed != 0 || report.Days[0].Entries != 3 {
		t.Fatalf("report = %+v, want one documented damaged line", report)
	}
	days, err := log.VerifyLines(key, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := days[0].Lines[2]; got.Parsed || got.Chain != ChainDamaged {
		t.Fatalf("damaged line = %+v, want it documented", got)
	}

	// A log that refuses torn lines never accepts the marker.
	strict, err := New(Options[testEntry]{Dir: log.Dir(), LockName: testLockName, Validate: validateTestEntry})
	if err != nil {
		t.Fatal(err)
	}
	wantBroken(t, verify(t, strict, key), "2026-03-01", ProblemUnparsable, 3)
}

func TestDocumentedTornLineAcrossDays(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{TornLine: TornLineDocument})
	mustAppend(t, log, "example.a")
	tearLastLine(t, log, 1)
	before, _ := os.ReadFile(dayPath(log, 1))
	log.setNow(day(2))
	mustAppend(t, log, "example.b")
	if after, _ := os.ReadFile(dayPath(log, 1)); !bytes.Equal(before, after) {
		t.Fatal("an older day file must stay unchanged")
	}
	report := verify(t, log.Log, nil)
	if report.Broken || report.Days[0].Damaged != 1 {
		t.Fatalf("report = %+v, want the damaged line documented by the first line of the next day", report)
	}
}

func TestDocumentedTornLineRemovedByRetention(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{TornLine: TornLineDocument, RetentionDays: 2})
	mustAppend(t, log, "example.a")
	tearLastLine(t, log, 1)
	log.setNow(day(9))
	mustAppend(t, log, "example.b")
	lines := readLines(t, dayPath(log, 9))
	cut := parseLine[testEntry](lines[0])
	if len(lines) != 2 || !cut.ok || cut.rec.Kind != KindCut || cut.rec.Cut.ThroughSeq != 1 {
		t.Fatalf("lines = %q, want a cut through the last line that parsed", lines)
	}
	if report := verify(t, log.Log, nil); report.Broken {
		t.Fatalf("report = %+v, want an intact chain", report)
	}
}

func TestVerifyRefusesDamageNoMarkerDocuments(t *testing.T) {
	setup := func(t *testing.T) (*testLog, [][]byte) {
		log := newTestLog(t, day(1), Options[testEntry]{TornLine: TornLineDocument})
		mustAppend(t, log, "example.a")
		tearLastLine(t, log, 1)
		mustAppend(t, log, "example.b")
		return log, readLines(t, dayPath(log, 1))
	}
	cases := map[string]func(lines [][]byte) [][]byte{
		"marker names another line": func(lines [][]byte) [][]byte {
			lines[2] = bytes.Replace(lines[2], []byte(hashHex([]byte(tornLine))), []byte(hashHex([]byte("other"))), 1)
			return lines
		},
		"marker does not chain": func(lines [][]byte) [][]byte {
			lines[2] = bytes.Replace(lines[2], []byte(hashHex(lines[0])), []byte(hashHex([]byte("other"))), 1)
			return lines
		},
		"no marker": func(lines [][]byte) [][]byte {
			return [][]byte{lines[0], lines[1]}
		},
		"marker not right after": func(lines [][]byte) [][]byte {
			return [][]byte{lines[0], lines[1], lines[0], lines[2]}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			log, lines := setup(t)
			writeLines(t, dayPath(log, 1), change(lines))
			report := verify(t, log.Log, nil)
			wantBroken(t, report, "2026-03-01", ProblemUnparsable, 2)
			if report.Days[0].Damaged != 0 {
				t.Fatalf("day = %+v, want no documented damage", report.Days[0])
			}
		})
	}
}
