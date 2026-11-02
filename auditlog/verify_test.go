package auditlog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// threeDays returns a log with two entries on day 1, one on day 2 and two on day 4.
func threeDays(t *testing.T, opts Options[testEntry]) *testLog {
	t.Helper()
	log := newTestLog(t, day(1), opts)
	mustAppend(t, log, "example.a")
	mustAppend(t, log, "example.b")
	log.setNow(day(2))
	mustAppend(t, log, "example.c")
	log.setNow(day(4))
	mustAppend(t, log, "example.d")
	mustAppend(t, log, "example.e")
	return log
}

func wantBroken(t *testing.T, report Report, day string, problem Problem, line int) {
	t.Helper()
	if !report.Broken {
		t.Fatalf("report = %+v, want a broken chain", report)
	}
	for _, d := range report.Days {
		if d.Problem != ProblemNone {
			if d.Date != day || d.Problem != problem || d.ProblemLine != line {
				t.Fatalf("first problem on %s = %q at line %d, want %q on %s at line %d", d.Date, d.Problem, d.ProblemLine, problem, day, line)
			}
			return
		}
	}
	t.Fatalf("report = %+v names no problem", report)
}

func TestVerifyIntactChain(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	report := verify(t, log.Log, nil)
	if report.Broken || report.Checked || len(report.Days) != 3 {
		t.Fatalf("report = %+v, want three intact days", report)
	}
}

func TestVerifyMissingDirIsAnEmptyChain(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{})
	report := verify(t, log.Log, &Key{key: []byte("k")})
	if report.Broken || len(report.Days) != 0 || !report.Checked {
		t.Fatalf("report = %+v, want an intact, empty, checked chain", report)
	}
}

func TestVerifyDetectsAChangedEntry(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	path := dayPath(log, 1)
	lines := readLines(t, path)
	lines[1] = bytes.Replace(lines[1], []byte("example.b"), []byte("example.x"), 1)
	writeLines(t, path, lines)
	wantBroken(t, verify(t, log.Log, nil), "2026-03-02", ProblemChain, 1)
}

func TestVerifyDetectsADeletedEntry(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	path := dayPath(log, 4)
	lines := readLines(t, path)
	// Removing the first line of day 4 leaves the next one without the line it chains from.
	writeLines(t, path, lines[1:])
	report := verify(t, log.Log, nil)
	wantBroken(t, report, "2026-03-04", ProblemChain, 1)
	if report.Days[2].Missing != 1 {
		t.Fatalf("missing = %d, want 1", report.Days[2].Missing)
	}
}

func TestVerifyDetectsAnInsertedEntry(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	path := dayPath(log, 1)
	lines := readLines(t, path)
	// A forged line that chains correctly from the one before it still breaks the link to the next one.
	forged, err := encodeLine(header{seq: 2, time: day(1), prevHash: hashHex(lines[0])}, []byte(`"action":"example.forged"`))
	if err != nil {
		t.Fatal(err)
	}
	writeLines(t, path, [][]byte{lines[0], forged, lines[1]})
	wantBroken(t, verify(t, log.Log, nil), "2026-03-01", ProblemChain, 3)
}

func TestVerifyDetectsSwappedEntries(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	path := dayPath(log, 4)
	lines := readLines(t, path)
	writeLines(t, path, [][]byte{lines[1], lines[0]})
	wantBroken(t, verify(t, log.Log, nil), "2026-03-04", ProblemChain, 1)
}

func TestVerifyDetectsAMissingMiddleDay(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	if err := os.Remove(dayPath(log, 2)); err != nil {
		t.Fatal(err)
	}
	report := verify(t, log.Log, nil)
	wantBroken(t, report, "2026-03-04", ProblemChain, 1)
	if report.Days[1].Missing != 1 {
		t.Fatalf("missing = %d, want 1", report.Days[1].Missing)
	}
}

func TestVerifyDetectsTheOldestDayRemovedWithoutACut(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	if err := os.Remove(dayPath(log, 1)); err != nil {
		t.Fatal(err)
	}
	report := verify(t, log.Log, nil)
	wantBroken(t, report, "2026-03-02", ProblemMissing, 1)
	if report.Days[0].Missing != 2 || report.Days[0].ProblemSeq != 3 {
		t.Fatalf("day = %+v, want two missing before sequence 3", report.Days[0])
	}
}

func TestVerifyDetectsAWrongGenesisHash(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	path := dayPath(log, 1)
	lines := readLines(t, path)
	lines[0] = bytes.Replace(lines[0], []byte(genesisHash), []byte(hashHex([]byte("x"))), 1)
	writeLines(t, path, lines)
	wantBroken(t, verify(t, log.Log, nil), "2026-03-01", ProblemGenesis, 1)
}

func TestVerifyDetectsAGapAfterADocumentedCut(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{RetentionDays: 2})
	mustAppend(t, log, "example.a")
	log.setNow(day(5))
	mustAppend(t, log, "example.b")
	mustAppend(t, log, "example.c")
	path := dayPath(log, 5)
	lines := readLines(t, path)
	// Without the line right after the cut, the next one does not continue from it.
	writeLines(t, path, [][]byte{lines[0], lines[2]})
	wantBroken(t, verify(t, log.Log, nil), "2026-03-05", ProblemChain, 2)

	// Without the cut itself the first line left is not where any cut says the chain continues.
	writeLines(t, path, lines[1:])
	wantBroken(t, verify(t, log.Log, nil), "2026-03-05", ProblemMissing, 1)
}

func TestVerifyRejectsLinesThatDoNotParse(t *testing.T) {
	cases := map[string][]byte{
		"not json":            []byte("not json"),
		"empty line":          nil,
		"unknown kind":        []byte(`{"seq":3,"kind":"other","prev_hash":"","mac":""}`),
		"cut without cut":     []byte(`{"seq":3,"kind":"retention-cut","prev_hash":"","mac":""}`),
		"entry with cut":      []byte(`{"seq":3,"cut":{"through_seq":1},"prev_hash":"","mac":""}`),
		"cut with damage":     []byte(`{"seq":3,"kind":"retention-cut","cut":{"through_seq":1},"damage":{"line_hash":""},"prev_hash":"","mac":""}`),
		"entry of wrong type": []byte(`{"seq":3,"action":7,"prev_hash":"","mac":""}`),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			log := threeDays(t, Options[testEntry]{})
			path := dayPath(log, 2)
			writeLines(t, path, append([][]byte{bad}, readLines(t, path)...))
			wantBroken(t, verify(t, log.Log, nil), "2026-03-02", ProblemUnparsable, 1)
		})
	}
}

func TestVerifyLinesReportsEveryLineOfTheRange(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	key := testKey(t, "example audit log")
	signed := log.with(log.WithKey(key))
	signed.setNow(day(4))
	mustAppend(t, signed, "example.f")

	days, err := log.VerifyLines(key, "2026-03-02", "2026-03-04")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].Date != "2026-03-02" || len(days[0].Lines) != 1 || len(days[1].Lines) != 3 {
		t.Fatalf("days = %+v, want day 2 with one line and day 4 with three", days)
	}
	for _, line := range days[1].Lines[:2] {
		if !line.Parsed || line.Chain != ChainIntact || line.MAC != MACNone {
			t.Fatalf("line = %+v, want an intact unsigned line", line)
		}
	}
	if last := days[1].Lines[2]; last.MAC != MACValid || last.Record.Entry.Action != "example.f" || last.Record.Seq != 6 {
		t.Fatalf("last line = %+v, want the signed entry", last)
	}
	for _, bad := range [][2]string{{"2026-3-1", ""}, {"", "yesterday"}} {
		if _, err := log.VerifyLines(nil, bad[0], bad[1]); err == nil {
			t.Errorf("VerifyLines(%q, %q) succeeded", bad[0], bad[1])
		}
	}
}

func TestVerifyLinesMarksWhereTheChainBreaks(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	path := dayPath(log, 4)
	lines := readLines(t, path)
	lines[0] = bytes.Replace(lines[0], []byte("example.d"), []byte("example.x"), 1)
	writeLines(t, path, append(lines, []byte("torn")))

	days, err := log.VerifyLines(nil, "2026-03-04", "")
	if err != nil {
		t.Fatal(err)
	}
	got := days[0].Lines
	if got[0].Chain != ChainNext || got[1].Chain != ChainIntact || got[2].Chain != ChainUnparsed || got[2].Parsed {
		t.Fatalf("lines = %+v, want the changed line, its successor and an unparsed line", got)
	}

	// When the line before is outside the range, the break is recorded on the first line of the range.
	days, err = log.VerifyLines(nil, "2026-03-02", "2026-03-02")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Lines[0].Chain != ChainIntact {
		t.Fatalf("days = %+v, want day 2 intact", days)
	}
	if err := os.Remove(dayPath(log, 2)); err != nil {
		t.Fatal(err)
	}
	days, err = log.VerifyLines(nil, "2026-03-04", "")
	if err != nil {
		t.Fatal(err)
	}
	if days[0].Lines[0].Missing != 1 {
		t.Fatalf("first line = %+v, want one missing line before it", days[0].Lines[0])
	}
	if _, err := os.Stat(filepath.Join(log.Dir(), testLockName)); err != nil {
		t.Fatalf("the lock file must exist after a check: %v", err)
	}
}
