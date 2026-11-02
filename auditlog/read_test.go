package auditlog

import (
	"errors"
	"testing"
	"time"
)

func actions(records []Record[testEntry]) []string {
	var out []string
	for _, r := range records {
		out = append(out, r.Entry.Action)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReadPagesNewestFirstAcrossDays(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	var got []string
	q := Query[testEntry]{Limit: 2}
	for range 5 {
		page, err := log.Read(q)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, actions(page.Records)...)
		if page.Next == "" {
			break
		}
		q.After = page.Next
	}
	if want := []string{"example.e", "example.d", "example.c", "example.b", "example.a"}; !equal(got, want) {
		t.Fatalf("pages = %v, want %v", got, want)
	}
}

func TestReadFilters(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	page, err := log.Read(Query[testEntry]{Limit: 10, From: day(2), To: day(4).Add(-time.Hour)})
	if err != nil || !equal(actions(page.Records), []string{"example.c"}) {
		t.Fatalf("time range = %v (%v), want only day 2", actions(page.Records), err)
	}
	match := func(r Record[testEntry]) bool { return r.Entry.Action != "example.d" }
	page, err = log.Read(Query[testEntry]{Limit: 10, From: day(4), Match: match})
	if err != nil || !equal(actions(page.Records), []string{"example.e"}) {
		t.Fatalf("match = %v (%v), want example.e", actions(page.Records), err)
	}
	empty := newTestLog(t, day(1), Options[testEntry]{})
	if page, err := empty.Read(Query[testEntry]{Limit: 1}); err != nil || len(page.Records) != 0 {
		t.Fatalf("missing directory = %+v (%v), want no entries", page, err)
	}
}

func TestReadRefusesBadInput(t *testing.T) {
	log := threeDays(t, Options[testEntry]{})
	if _, err := log.Read(Query[testEntry]{}); err == nil {
		t.Error("Read without a limit succeeded")
	}
	for _, cursor := range []string{"x", "2026-03-01", "2026-03-01/", "2026-3-1/2", "2026-03-01/-1", "../2026-03-01/1"} {
		if _, err := log.Read(Query[testEntry]{Limit: 1, After: cursor}); !errors.Is(err, ErrBadCursor) {
			t.Errorf("cursor %q: error = %v, want ErrBadCursor", cursor, err)
		}
	}
}

func TestReadSkipsMarkersAndLinesThatDoNotParse(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{RetentionDays: 1})
	mustAppend(t, log, "example.a")
	log.setNow(day(5))
	mustAppend(t, log, "example.b")
	tearLastLine(t, log, 5)
	page, err := log.Read(Query[testEntry]{Limit: 10})
	if err != nil || !equal(actions(page.Records), []string{"example.b"}) || page.Records[0].Seq != 3 {
		t.Fatalf("records = %+v (%v), want only the entry", page.Records, err)
	}
}

func TestFileDays(t *testing.T) {
	east := time.FixedZone("east", 2*3600)
	from, to, err := FileDays("2026-03-02", "2026-03-02", east)
	if err != nil || from != "2026-03-01" || to != "2026-03-02" {
		t.Fatalf("FileDays east = %q %q (%v)", from, to, err)
	}
	west := time.FixedZone("west", -5*3600)
	from, to, err = FileDays("2026-03-02", "", west)
	if err != nil || from != "2026-03-02" || to != "" {
		t.Fatalf("FileDays west = %q %q (%v)", from, to, err)
	}
	if _, to, err = FileDays("", "2026-03-02", west); err != nil || to != "2026-03-03" {
		t.Fatalf("FileDays west to = %q (%v)", to, err)
	}
	if from, to, err = FileDays("2026-03-02", "2026-03-03", nil); err != nil || from != "2026-03-02" || to != "2026-03-03" {
		t.Fatalf("FileDays UTC = %q %q (%v)", from, to, err)
	}
	for _, bad := range [][2]string{{"02.03.2026", ""}, {"", "2026-02-30"}} {
		if _, _, err := FileDays(bad[0], bad[1], nil); err == nil {
			t.Errorf("FileDays(%q, %q) succeeded", bad[0], bad[1])
		}
	}
}

func byResult(r Record[testEntry]) (string, bool) {
	return r.Entry.Result, r.Entry.Action != "example.skip"
}

func TestDailyActivity(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{})
	mustAppend(t, log, "example.a")
	if err := log.Append(testEntry{Action: "example.b", Result: "failure"}); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, log, "example.skip")
	log.setNow(day(3).Add(13 * time.Hour)) // 23:00 UTC, the next day further east
	mustAppend(t, log, "example.c")
	log.setNow(day(5))
	mustAppend(t, log, "example.d")
	tearLastLine(t, log, 5)

	days, err := log.VerifyLines(nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := DailyActivity(days, "2026-02-28", "2026-03-05", nil, byResult)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 || got[0].Entries != 0 || got[1].Entries != 2 || got[1].Groups["success"] != 1 ||
		got[1].Groups["failure"] != 1 || got[3].Entries != 1 || got[5].Entries != 1 || !got[5].Altered ||
		got[1].Altered || got[2].Date != "2026-03-02" {
		t.Fatalf("activity = %+v", got)
	}

	east := time.FixedZone("east", 2*3600)
	got, err = DailyActivity(days, "", "", east, byResult)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[0].Date != "2026-03-01" || got[3].Date != "2026-03-04" || got[3].Entries != 1 || got[2].Entries != 0 {
		t.Fatalf("activity east = %+v, want the late entry on the next local day", got)
	}
	if got, err := DailyActivity[testEntry](nil, "", "", nil, byResult); err != nil || got != nil {
		t.Fatalf("no lines = %+v (%v), want nothing", got, err)
	}
	if _, err := DailyActivity(days, "yesterday", "", nil, byResult); err == nil {
		t.Fatal("DailyActivity with a malformed day succeeded")
	}
}

func TestDailyActivityMarksGapsAndDocumentedDamage(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{TornLine: TornLineDocument})
	mustAppend(t, log, "example.a")
	tearLastLine(t, log, 1)
	log.setNow(day(2))
	mustAppend(t, log, "example.b")
	mustAppend(t, log, "example.c")
	path := dayPath(log, 2)
	lines := readLines(t, path)
	// Without sequence 3, day 2 skips from the marker to sequence 4.
	writeLines(t, path, [][]byte{lines[0], lines[2]})
	days, err := log.VerifyLines(nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := DailyActivity(days, "", "", nil, byResult)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Damaged || got[0].Altered || got[0].Gap || !got[1].Gap || got[1].Altered {
		t.Fatalf("activity = %+v, want damage on day 1 and a gap on day 2", got)
	}
}
