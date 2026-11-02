package auditlog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The files under testdata/compat were written by the logger this package derives from, with the
// invented secret testSecret and the label compatLabel: "genesis" from the first line on, "cut" after a
// retention cut. They hold signed and unsigned lines, a day without a file, escaped text and size
// counters. compatEntry declares their entry members the way that logger does.
const compatLabel = "example audit log v1"

type compatClient struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type compatEntry struct {
	Path             string        `json:"path,omitempty"`
	Client           *compatClient `json:"client,omitempty"`
	Operation        string        `json:"operation,omitempty"`
	Version          int           `json:"version,omitempty"`
	Connection       string        `json:"connection,omitempty"`
	ConnectionID     string        `json:"connection_id,omitempty"`
	FormerConnection string        `json:"former_connection,omitempty"`
	Effect           string        `json:"effect,omitempty"`
	Result           string        `json:"result,omitempty"`
	DurationMS       int64         `json:"duration_ms,omitempty"`
	ReqBytes         *int64        `json:"req_bytes,omitempty"`
	RespBytes        *int64        `json:"resp_bytes,omitempty"`
	Token            string        `json:"token,omitempty"`
	Session          string        `json:"session,omitempty"`
}

// compatLog copies the fixture variant into a fresh directory and opens it with the lock file name of the
// original logger.
func compatLog(t *testing.T, variant string, now time.Time) *Log[compatEntry] {
	t.Helper()
	src := filepath.Join("testdata", "compat", variant)
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	files, err := dayFiles(src)
	if err != nil || len(files) == 0 {
		t.Fatalf("fixture %s: %v (%v)", variant, files, err)
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, fileMode); err != nil {
			t.Fatal(err)
		}
	}
	log, err := New(Options[compatEntry]{Dir: dir, LockName: ".lock", Validate: func(compatEntry) error { return nil },
		Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return log.WithKey(testKey(t, compatLabel))
}

func TestCompatibleLogsVerify(t *testing.T) {
	for variant, want := range map[string][]DayReport{
		"genesis": {
			{Date: "2026-01-01", Entries: 2, Unverified: 1},
			{Date: "2026-01-02", Entries: 2},
			{Date: "2026-01-04", Entries: 2, Unverified: 1},
		},
		"cut": {
			{Date: "2026-01-02", Entries: 2},
			{Date: "2026-01-04", Entries: 2, Unverified: 1},
		},
	} {
		t.Run(variant, func(t *testing.T) {
			log := compatLog(t, variant, time.Time{})
			report, err := log.Verify(testKey(t, compatLabel))
			if err != nil {
				t.Fatal(err)
			}
			if report.Broken || !report.Checked || len(report.Days) != len(want) {
				t.Fatalf("report = %+v, want an intact, checked chain", report)
			}
			for i, d := range report.Days {
				if d != want[i] {
					t.Errorf("day = %+v, want %+v", d, want[i])
				}
			}
			wrong, err := log.Verify(testKey(t, "other label"))
			if err != nil || !wrong.Broken || wrong.Days[0].Problem != ProblemMAC {
				t.Fatalf("report with another label = %+v (%v), want mismatching check values", wrong, err)
			}
		})
	}
}

func TestCompatibleLinesAreWrittenByteForByte(t *testing.T) {
	key := testKey(t, compatLabel)
	for _, variant := range []string{"genesis", "cut"} {
		src := filepath.Join("testdata", "compat", variant)
		files, err := dayFiles(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range files {
			for _, raw := range readLines(t, filepath.Join(src, name)) {
				p := parseLine[compatEntry](raw)
				if !p.ok {
					t.Fatalf("%s/%s: line does not parse: %s", variant, name, raw)
				}
				var members []byte
				if p.rec.Kind == "" {
					if members, err = entryMembers(p.rec.Entry); err != nil {
						t.Fatal(err)
					}
				}
				h := header{seq: p.rec.Seq, time: p.rec.Time, kind: p.rec.Kind, cut: p.rec.Cut, damage: p.rec.Damage, prevHash: p.env.PrevHash}
				log := &Log[compatEntry]{}
				if p.env.MAC != "" {
					log = log.WithKey(key)
				}
				got, err := log.encode(h, members)
				if err != nil || !bytes.Equal(got, raw) {
					t.Fatalf("%s/%s: encoded\n%s (%v)\nwant\n%s", variant, name, got, err, raw)
				}
			}
		}
	}
}

func TestCompatibleLogsContinue(t *testing.T) {
	for variant, lastSeq := range map[string]uint64{"genesis": 6, "cut": 7} {
		t.Run(variant, func(t *testing.T) {
			log := compatLog(t, variant, time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC))
			before := readLines(t, filepath.Join(log.Dir(), "2026-01-04.jsonl"))
			entry := compatEntry{Path: "cli", Operation: "example.items.list", Connection: "example-b", Effect: "read", Result: "success"}
			if err := log.Append(entry); err != nil {
				t.Fatal(err)
			}
			next := parseLine[compatEntry](readLines(t, filepath.Join(log.Dir(), "2026-01-05.jsonl"))[0])
			if !next.ok || next.rec.Seq != lastSeq+1 || next.env.PrevHash != hashHex(before[len(before)-1]) || next.rec.Entry != entry {
				t.Fatalf("next line = %+v, want sequence %d chained from the last existing line", next, lastSeq+1)
			}
			if report, err := log.Verify(testKey(t, compatLabel)); err != nil || report.Broken {
				t.Fatalf("report = %+v (%v), want an intact chain", report, err)
			}

			// A retention cut over the existing files keeps the chain verifiable as well.
			later, err := New(Options[compatEntry]{Dir: log.Dir(), LockName: ".lock", Validate: func(compatEntry) error { return nil },
				RetentionDays: 2, Now: func() time.Time { return time.Date(2026, 1, 7, 8, 0, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatal(err)
			}
			if err := later.WithKey(testKey(t, compatLabel)).Append(entry); err != nil {
				t.Fatal(err)
			}
			report, err := later.Verify(testKey(t, compatLabel))
			if err != nil || report.Broken || len(report.Days) != 2 || report.Days[0].Date != "2026-01-05" {
				t.Fatalf("report = %+v (%v), want an intact chain from 2026-01-05", report, err)
			}
			page, err := later.Read(Query[compatEntry]{Limit: 10})
			if err != nil || len(page.Records) != 2 || page.Records[0].Entry != entry {
				t.Fatalf("records = %+v (%v), want the two new entries", page.Records, err)
			}
		})
	}
}

func TestCompatibleLogsRead(t *testing.T) {
	log := compatLog(t, "genesis", time.Time{})
	page, err := log.Read(Query[compatEntry]{Limit: 10, Match: func(r Record[compatEntry]) bool { return r.Entry.Path == "mcp" }})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("records = %+v (%v), want the one mcp entry", page.Records, err)
	}
	r := page.Records[0]
	if r.Seq != 3 || r.Entry.Client == nil || r.Entry.Client.Version != "1.0 <beta> & more" || *r.Entry.RespBytes != 900 ||
		r.Time != time.Date(2026, 1, 2, 23, 59, 59, 123456789, time.UTC) {
		t.Fatalf("record = %+v, want the decoded mcp entry", r)
	}
}
