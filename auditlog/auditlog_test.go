package auditlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The worker mode runs one real Append in a fresh process, so the concurrency test exercises the lock
// across processes and not only across goroutines.
const (
	workerModeEnv = "AUDITLOG_TEST_WORKER"
	workerDirEnv  = "AUDITLOG_TEST_DIR"
	testLockName  = ".lock"
)

func TestMain(m *testing.M) {
	if os.Getenv(workerModeEnv) == "1" {
		log, err := New(Options[testEntry]{Dir: os.Getenv(workerDirEnv), LockName: testLockName, Validate: validateTestEntry})
		if err == nil {
			err = log.Append(testEntry{Action: "example.run", Result: "success"})
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type testEntry struct {
	Action string `json:"action,omitempty"`
	Result string `json:"result,omitempty"`
}

func validateTestEntry(e testEntry) error {
	if e.Action == "" {
		return errors.New("action is required")
	}
	return nil
}

func day(d int) time.Time { return time.Date(2026, 3, d, 10, 0, 0, 0, time.UTC) }

type testLog struct {
	*Log[testEntry]
	clock *time.Time
}

// setNow moves the clock of the log and of every log derived from it with with.
func (tl *testLog) setNow(at time.Time) { *tl.clock = at }

// newTestLog returns a log in a temporary directory whose clock stands at at until it is moved.
func newTestLog(t *testing.T, at time.Time, opts Options[testEntry]) *testLog {
	t.Helper()
	tl := &testLog{clock: &at}
	if opts.Dir == "" {
		opts.Dir = filepath.Join(t.TempDir(), "logs")
	}
	opts.LockName, opts.Validate = testLockName, validateTestEntry
	opts.Now = func() time.Time { return *tl.clock }
	log, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	tl.Log = log
	return tl
}

func (tl *testLog) with(log *Log[testEntry]) *testLog { return &testLog{Log: log, clock: tl.clock} }

func mustAppend(t *testing.T, log interface{ Append(testEntry) error }, action string) {
	t.Helper()
	if err := log.Append(testEntry{Action: action, Result: "success"}); err != nil {
		t.Fatalf("Append(%q) error = %v", action, err)
	}
}

func dayPath(log *testLog, d int) string {
	return filepath.Join(log.Dir(), day(d).Format(DateLayout)+".jsonl")
}

func readLines(t *testing.T, path string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return splitLines(data)
}

func writeLines(t *testing.T, path string, lines [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	for _, line := range lines {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), fileMode); err != nil {
		t.Fatal(err)
	}
}

func verify(t *testing.T, log *Log[testEntry], checker Checker) Report {
	t.Helper()
	report, err := log.Verify(checker)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	return report
}

func TestNewRefusesBadOptions(t *testing.T) {
	good := Options[testEntry]{Dir: t.TempDir(), LockName: testLockName, Validate: validateTestEntry}
	cases := map[string]func(*Options[testEntry]){
		"no directory":         func(o *Options[testEntry]) { o.Dir = "" },
		"no lock name":         func(o *Options[testEntry]) { o.LockName = "" },
		"lock name with path":  func(o *Options[testEntry]) { o.LockName = filepath.Join("sub", "x.lock") },
		"lock name dot dot":    func(o *Options[testEntry]) { o.LockName = ".." },
		"lock is a day file":   func(o *Options[testEntry]) { o.LockName = "2026-03-01.jsonl" },
		"no validation":        func(o *Options[testEntry]) { o.Validate = nil },
		"negative retention":   func(o *Options[testEntry]) { o.RetentionDays = -1 },
		"unknown torn line":    func(o *Options[testEntry]) { o.TornLine = 7 },
		"negative torn handle": func(o *Options[testEntry]) { o.TornLine = -1 },
	}
	for name, mutate := range cases {
		opts := good
		mutate(&opts)
		if _, err := New(opts); err == nil {
			t.Errorf("%s: New succeeded", name)
		}
	}
	log, err := New(good)
	if err != nil {
		t.Fatalf("New(good) error = %v", err)
	}
	if _, err := log.WithRetention(-1); err == nil {
		t.Error("WithRetention(-1) succeeded")
	}
}

func TestAppendChainsAcrossDays(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{})
	mustAppend(t, log, "example.a")
	mustAppend(t, log, "example.b")
	log.setNow(day(3)) // a day without an entry in between
	mustAppend(t, log, "example.c")

	if _, err := os.Stat(dayPath(log, 2)); !os.IsNotExist(err) {
		t.Fatalf("a day without an entry must not get a file, stat error = %v", err)
	}
	report := verify(t, log.Log, nil)
	if report.Broken || len(report.Days) != 2 || report.Days[0].Entries != 2 || report.Days[1].Entries != 1 {
		t.Fatalf("report = %+v, want an intact chain over two days with 2 and 1 entries", report)
	}
	for _, d := range report.Days {
		if d.Unverified != d.Entries || d.Signatures(report.Checked) != SignaturesNotChecked {
			t.Fatalf("day %+v: unsigned entries must count as unverified and not checked", d)
		}
	}
	first := readLines(t, dayPath(log, 1))[0]
	last := readLines(t, dayPath(log, 1))[1]
	next := parseLine[testEntry](readLines(t, dayPath(log, 3))[0])
	if !bytes.Contains(first, []byte(`"prev_hash":"`+genesisHash+`"`)) || !next.ok || next.rec.Seq != 3 ||
		next.env.PrevHash != hashHex(last) || next.rec.Entry.Action != "example.c" {
		t.Fatalf("first line %s, next day %+v: want the genesis hash, then seq 3 chained from the day before", first, next)
	}
}

func TestAppendAcrossDayWithEmptyCurrentFile(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{})
	mustAppend(t, log, "example.a")
	if err := os.WriteFile(dayPath(log, 2), nil, fileMode); err != nil {
		t.Fatal(err)
	}
	log.setNow(day(2))
	mustAppend(t, log, "example.b")
	if report := verify(t, log.Log, nil); report.Broken {
		t.Fatalf("report = %+v, want an intact chain", report)
	}
}

type objectWith string

func (o objectWith) MarshalJSON() ([]byte, error) { return []byte(o), nil }

func TestAppendRefusesInvalidEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	log := newTestLog(t, day(1), Options[testEntry]{Dir: dir})
	if err := log.Append(testEntry{Result: "success"}); err == nil || !strings.Contains(err.Error(), "invalid log entry") {
		t.Errorf("Append of an entry Validate refuses = %v, want an error", err)
	}
	cases := map[string]objectWith{
		"reserved member":         `{"seq":7}`,
		"reserved in other case":  `{"Prev_Hash":"x"}`,
		"reserved mac":            `{"a":1,"MAC":"x"}`,
		"reserved kind":           `{"kind":"retention-cut"}`,
		"duplicate member":        `{"a":1,"a":2}`,
		"duplicate in other case": `{"a":1,"A":2}`,
		"array":                   `[1,2]`,
		"string":                  `"text"`,
		"null":                    `null`,
	}
	for name, raw := range cases {
		other, err := New(Options[objectWith]{Dir: dir, LockName: testLockName, Validate: func(objectWith) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		if err := other.Append(raw); err == nil {
			t.Errorf("%s: Append succeeded", name)
		}
	}
	if files, _ := dayFiles(dir); len(files) != 0 {
		t.Errorf("refused entries wrote files %v", files)
	}
}

func TestLineFormatAndFileMode(t *testing.T) {
	log := newTestLog(t, day(1).Add(1500*time.Millisecond), Options[testEntry]{})
	mustAppend(t, log, "example.a")
	line := readLines(t, dayPath(log, 1))[0]
	want := `{"seq":1,"time":"2026-03-01T10:00:01.5Z","action":"example.a","result":"success","prev_hash":"` + genesisHash + `","mac":""}`
	if string(line) != want {
		t.Fatalf("line = %s, want %s", line, want)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for path, mode := range map[string]os.FileMode{log.Dir(): dirMode, dayPath(log, 1): fileMode} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != mode {
			t.Errorf("mode of %s = %v (%v), want %v", path, info.Mode().Perm(), err, mode)
		}
	}
}

func TestRetentionCutIsNotAGap(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{RetentionDays: 2})
	mustAppend(t, log, "example.a")
	mustAppend(t, log, "example.b")
	log.setNow(day(2))
	mustAppend(t, log, "example.c")
	log.setNow(day(5))
	mustAppend(t, log, "example.d")

	if _, err := os.Stat(dayPath(log, 1)); !os.IsNotExist(err) {
		t.Fatalf("the expired day file must be removed, stat error = %v", err)
	}
	lines := readLines(t, dayPath(log, 5))
	cut := parseLine[testEntry](lines[0])
	if len(lines) != 2 || !cut.ok || cut.rec.Kind != KindCut || cut.rec.Cut.ThroughSeq != 3 || cut.rec.Seq != 4 {
		t.Fatalf("day 5 = %q, want a cut through 3 before the entry", lines)
	}
	if report := verify(t, log.Log, nil); report.Broken {
		t.Fatalf("report = %+v, want a documented cut", report)
	}

	// A second cut removes the day that held the first cut.
	log.setNow(day(9))
	mustAppend(t, log, "example.e")
	if report := verify(t, log.Log, nil); report.Broken || len(report.Days) != 1 || report.Days[0].Entries != 1 {
		t.Fatalf("report = %+v, want one intact day after the second cut", report)
	}
}

func TestZeroRetentionKeepsEveryDayFile(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{})
	mustAppend(t, log, "example.a")
	log.setNow(day(1).AddDate(1, 0, 0))
	mustAppend(t, log, "example.b")
	if files, err := dayFiles(log.Dir()); err != nil || len(files) != 2 {
		t.Fatalf("day files = %v (%v), want both kept", files, err)
	}
	shorter, err := log.WithRetention(30)
	if err != nil {
		t.Fatal(err)
	}
	mustAppend(t, log.with(shorter), "example.c")
	if files, err := dayFiles(log.Dir()); err != nil || len(files) != 1 {
		t.Fatalf("day files = %v (%v), want only the newest", files, err)
	}
	if report := verify(t, log.Log, nil); report.Broken {
		t.Fatalf("report = %+v, want an intact chain", report)
	}
}

func TestRetentionKeepsTheChainWhenItRemovesEveryFile(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{RetentionDays: 1})
	mustAppend(t, log, "example.a")
	log.setNow(day(20))
	mustAppend(t, log, "example.b")
	lines := readLines(t, dayPath(log, 20))
	cut := parseLine[testEntry](lines[0])
	if !cut.ok || cut.rec.Seq != 2 || cut.rec.Cut.ThroughSeq != 1 || cut.env.PrevHash == genesisHash {
		t.Fatalf("cut = %s, want seq 2 chained from the removed line", lines[0])
	}
	if report := verify(t, log.Log, nil); report.Broken {
		t.Fatalf("report = %+v, want an intact chain", report)
	}
}

func TestDayFileThatIsNoRegularFileIsRefused(t *testing.T) {
	log := newTestLog(t, day(2), Options[testEntry]{})
	mustAppend(t, log, "example.a")
	if err := os.Mkdir(dayPath(log, 1), dirMode); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(testEntry{Action: "example.b"}); err == nil {
		t.Error("Append succeeded beside a directory named like a day file")
	}
	if _, err := log.Verify(nil); err == nil {
		t.Error("Verify succeeded beside a directory named like a day file")
	}
	if _, err := log.Read(Query[testEntry]{Limit: 1}); err == nil {
		t.Error("Read succeeded beside a directory named like a day file")
	}
}

func TestAppendConcurrentGoroutines(t *testing.T) {
	log := newTestLog(t, day(1), Options[testEntry]{})
	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- log.Append(testEntry{Action: "example.run"})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	assertSequence(t, log.Log, n)
}

func TestAppendConcurrentProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("starts processes")
	}
	dir := filepath.Join(t.TempDir(), "logs")
	const n = 6
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), workerModeEnv+"=1", workerDirEnv+"="+dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("worker process failed: %w: %s", err, out)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	log, err := New(Options[testEntry]{Dir: dir, LockName: testLockName, Validate: validateTestEntry})
	if err != nil {
		t.Fatal(err)
	}
	assertSequence(t, log, n)
}

// assertSequence checks that the log holds exactly lines 1 to want and that the chain verifies.
func assertSequence(t *testing.T, log *Log[testEntry], want int) {
	t.Helper()
	if report := verify(t, log, nil); report.Broken {
		t.Fatalf("report = %+v, want an intact chain", report)
	}
	files, err := dayFiles(log.Dir())
	if err != nil {
		t.Fatal(err)
	}
	var seqs []uint64
	for _, name := range files {
		for _, line := range readLines(t, filepath.Join(log.Dir(), name)) {
			var e envelope
			if err := json.Unmarshal(line, &e); err != nil {
				t.Fatal(err)
			}
			seqs = append(seqs, e.Seq)
		}
	}
	if len(seqs) != want {
		t.Fatalf("got %d lines, want %d", len(seqs), want)
	}
	for i, seq := range seqs {
		if seq != uint64(i+1) {
			t.Fatalf("line %d has sequence %d", i+1, seq)
		}
	}
}

func TestLastLineFromMatchesSplitLines(t *testing.T) {
	long := strings.Repeat("x", 9000)
	inputs := []string{"", "\n", "\n\n", "a", "a\n", "a\nb", "a\nb\n\n", long, "a\n" + long + "\n", long + "\nb"}
	for _, input := range inputs {
		lines := splitLines([]byte(input))
		var want []byte
		if len(lines) > 0 {
			want = lines[len(lines)-1]
		}
		for _, block := range []int64{1, 3, 4096} {
			got, err := lastLineFrom(strings.NewReader(input), int64(len(input)), block)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("lastLineFrom(%.20q, block %d) = %.20q (%v), want %.20q", input, block, got, err, want)
			}
		}
	}
}
