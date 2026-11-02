package auditlog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/filelock"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// DateLayout is how a day is written, in the name of a day file and in a range of days.
const DateLayout = "2006-01-02"

// dayFilePattern matches the name of a day file and captures its date.
var dayFilePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.jsonl$`)

// genesisHash is the prev_hash of the very first entry of a log: the SHA-256 of zero bytes.
var genesisHash = hashHex(nil)

// The kinds of marker line (see Record.Kind). An entry has the empty kind.
const (
	// KindCut marks a retention-cut marker, which carries only Cut beside the chain fields.
	KindCut = "retention-cut"
	// KindDamage marks a damaged-line marker, which carries only Damage beside the chain fields.
	KindDamage = "damaged-line"
)

// Cut documents a retention deletion: the sequence number of the last line the deletion removed.
type Cut struct {
	ThroughSeq uint64 `json:"through_seq"`
}

// Damage documents a line that does not parse, by the SHA-256 of its exact bytes without the newline. It
// never holds the bytes themselves.
type Damage struct {
	LineHash string `json:"line_hash"`
}

// TornLine is what Append and Verify do with a line that does not parse, such as one torn by a crash while
// it was written.
type TornLine int

const (
	// TornLineRefuse makes Append fail while the newest line does not parse, and Verify report every such
	// line as a break.
	TornLineRefuse TornLine = iota
	// TornLineDocument makes Append write a KindDamage marker right after a newest line that does not
	// parse, chained from the last line before it that does, and Verify accept such a line only when that
	// marker follows it and holds as a line itself, chain and check value included.
	TornLineDocument
)

// reservedKeys are the members of a line that the log owns; an entry may not carry any of them.
var reservedKeys = []string{"seq", "time", "kind", "cut", "damage", "prev_hash", "mac"}

// Options configures a Log. Dir, LockName and Validate are required.
type Options[T any] struct {
	// Dir is the directory that holds the day files.
	Dir string
	// LockName is the file name of the lock file inside Dir.
	LockName string
	// Validate checks an entry before anything is written. Append refuses an entry it rejects.
	Validate func(T) error
	// RetentionDays is how many days of day files Append keeps; zero never removes a day file.
	RetentionDays int
	// TornLine selects how a line that does not parse is handled; the zero value refuses it.
	TornLine TornLine
	// Now is the clock of Append; nil means time.Now.
	Now func() time.Time
}

// Log appends entries of type T to the day files of one directory and checks their chain. T is encoded
// with encoding/json and must encode as a JSON object without a reserved member. A Log is safe for
// concurrent use; Append serializes on the lock file across goroutines and processes.
type Log[T any] struct {
	opts Options[T]
	key  *Key
}

// New returns a log with opts. It touches no file: the directory and its files are created on the first
// Append.
func New[T any](opts Options[T]) (*Log[T], error) {
	switch {
	case opts.Dir == "":
		return nil, errors.New("no log directory")
	case opts.LockName == "" || filepath.Base(opts.LockName) != opts.LockName || opts.LockName == "." ||
		opts.LockName == ".." || dayFilePattern.MatchString(opts.LockName):
		return nil, errors.New("the lock file name must be a plain file name that is not a day file")
	case opts.Validate == nil:
		return nil, errors.New("no entry validation")
	case opts.RetentionDays < 0:
		return nil, errors.New("retention must not be negative")
	case opts.TornLine != TornLineRefuse && opts.TornLine != TornLineDocument:
		return nil, errors.New("unknown torn line handling")
	}
	return &Log[T]{opts: opts}, nil
}

// WithKey returns a log like l that signs every line it writes with key. A nil key signs nothing. The key
// stays the caller's, who clears it once no log uses it any more.
func (l *Log[T]) WithKey(key *Key) *Log[T] {
	out := *l
	out.key = key
	return &out
}

// WithRetention returns a log like l that keeps days days of day files; zero never removes a day file.
func (l *Log[T]) WithRetention(days int) (*Log[T], error) {
	if days < 0 {
		return nil, errors.New("retention must not be negative")
	}
	out := *l
	out.opts.RetentionDays = days
	return &out, nil
}

// Signs reports whether l signs the lines it writes.
func (l *Log[T]) Signs() bool { return l.key.usable() }

// Dir is the directory of the day files.
func (l *Log[T]) Dir() string { return l.opts.Dir }

func (l *Log[T]) clock() time.Time {
	if l.opts.Now != nil {
		return l.opts.Now().UTC()
	}
	return time.Now().UTC()
}

// Append writes entry as the next line of the chain, into the day file of the current UTC date, across day
// files and processes. Under the same lock, retention runs first: day files older than the retention are
// removed, and if a line was removed, a KindCut marker is written right before the entry. A newest line
// that does not parse is handled as Options.TornLine says.
//
// A returned error means the entry might not be durable. The caller reports it as a warning; it never
// changes the result of the action the entry records.
func (l *Log[T]) Append(entry T) error {
	if err := l.opts.Validate(entry); err != nil {
		return fmt.Errorf("invalid log entry: %w", err)
	}
	members, err := entryMembers(entry)
	if err != nil {
		return err
	}
	dir := l.opts.Dir
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	unlock, err := l.lock()
	if err != nil {
		return err
	}
	defer unlock()

	now := l.clock()
	todayPath := filepath.Join(dir, now.Format(DateLayout)+".jsonl")

	removed, err := l.applyRetention(now)
	if err != nil {
		return err
	}
	files, err := dayFiles(dir)
	if err != nil {
		return err
	}
	end, err := l.lastLine(files)
	if err != nil {
		return err
	}
	if end.damaged != nil && l.opts.TornLine == TornLineRefuse {
		return fmt.Errorf("parse %s: the last line is not a valid log line", end.damagedPath)
	}

	// A surviving file is always newer than what retention removed, so it wins; removed is the fallback when
	// retention deleted every file, so the chain continues instead of restarting at the genesis hash.
	prevHash, seq := genesisHash, uint64(1)
	switch {
	case end.raw != nil:
		prevHash, seq = hashHex(end.raw), end.seq+1
	case removed.raw != nil:
		prevHash, seq = hashHex(removed.raw), removed.seq+1
	}

	var lines [][]byte
	next := func(h header, members []byte) error {
		h.seq, h.time, h.prevHash = seq, now, prevHash
		raw, err := l.encode(h, members)
		if err != nil {
			return err
		}
		lines = append(lines, raw)
		prevHash, seq = hashHex(raw), seq+1
		return nil
	}
	if end.damaged != nil {
		if end.unterminated && end.damagedPath == todayPath {
			// appendLines writes an empty line as a lone newline, which ends the damaged line.
			lines = append(lines, nil)
		}
		if err := next(header{kind: KindDamage, damage: &Damage{LineHash: hashHex(end.damaged)}}, nil); err != nil {
			return err
		}
	}
	if removed.raw != nil {
		if err := next(header{kind: KindCut, cut: &Cut{ThroughSeq: removed.seq}}, nil); err != nil {
			return err
		}
	}
	if err := next(header{}, members); err != nil {
		return err
	}
	return appendLines(todayPath, lines)
}

// entryMembers returns the members of entry's JSON object without its braces. It refuses anything but a
// single object, a reserved member in any letter case, since encoding/json matches names that way, and a
// member that occurs twice.
func entryMembers[T any](entry T) ([]byte, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("encode log entry: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("a log entry must encode as a JSON object")
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("encode log entry: %w", err)
		}
		name, _ := tok.(string)
		for _, reserved := range reservedKeys {
			if strings.EqualFold(name, reserved) {
				return nil, fmt.Errorf("a log entry must not have the member %q", reserved)
			}
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("a log entry has the member %q twice", name)
		}
		seen[strings.ToLower(name)] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("encode log entry: %w", err)
		}
	}
	return data[1 : len(data)-1], nil
}

// header holds the members of a line that the log owns.
type header struct {
	seq      uint64
	time     time.Time
	kind     string
	cut      *Cut
	damage   *Damage
	prevHash string
}

// encodeLine returns a line with an empty check value. The members come in the order seq, time, kind, the
// entry's members, cut, damage, prev_hash and mac, so mac is always last.
func encodeLine(h header, members []byte) ([]byte, error) {
	at, err := json.Marshal(h.time)
	if err != nil {
		return nil, fmt.Errorf("encode log line: %w", err)
	}
	var b bytes.Buffer
	b.WriteString(`{"seq":`)
	b.WriteString(strconv.FormatUint(h.seq, 10))
	b.WriteString(`,"time":`)
	b.Write(at)
	if h.kind != "" {
		b.WriteString(`,"kind":"` + h.kind + `"`)
	}
	if len(members) > 0 {
		b.WriteByte(',')
		b.Write(members)
	}
	if h.cut != nil {
		b.WriteString(`,"cut":{"through_seq":` + strconv.FormatUint(h.cut.ThroughSeq, 10) + `}`)
	}
	if h.damage != nil {
		b.WriteString(`,"damage":{"line_hash":"` + h.damage.LineHash + `"}`)
	}
	b.WriteString(`,"prev_hash":"` + h.prevHash + `"`)
	b.Write(emptyMACSuffix)
	return b.Bytes(), nil
}

// encode is the line stored for h and members: signed when l has a key.
func (l *Log[T]) encode(h header, members []byte) ([]byte, error) {
	raw, err := encodeLine(h, members)
	if err != nil || !l.key.usable() {
		return raw, err
	}
	return l.key.sign(raw)
}

// Record is one parsed line of a log.
type Record[T any] struct {
	Seq  uint64
	Time time.Time
	// Kind is empty for an entry, KindCut or KindDamage for a marker.
	Kind   string
	Cut    *Cut
	Damage *Damage
	// Entry is the line decoded as T; the zero T for a marker.
	Entry T
}

// envelope is how the members the log owns are read back.
type envelope struct {
	Seq      uint64    `json:"seq"`
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"`
	Cut      *Cut      `json:"cut"`
	Damage   *Damage   `json:"damage"`
	PrevHash string    `json:"prev_hash"`
	MAC      string    `json:"mac"`
}

// parsedLine is a line decoded both as the members the log owns and as T.
type parsedLine[T any] struct {
	env envelope
	rec Record[T]
	ok  bool
}

// parseLine decodes raw. A line parses only when it decodes as T as well and its kind is the empty kind
// without a marker member or a known marker kind with exactly its own member.
func parseLine[T any](raw []byte) parsedLine[T] {
	var p parsedLine[T]
	if json.Unmarshal(raw, &p.env) != nil || json.Unmarshal(raw, &p.rec.Entry) != nil {
		return parsedLine[T]{}
	}
	switch e := p.env; {
	case e.Kind == "" && e.Cut == nil && e.Damage == nil:
	case e.Kind == KindCut && e.Cut != nil && e.Damage == nil:
		var zero T
		p.rec.Entry = zero
	case e.Kind == KindDamage && e.Damage != nil && e.Cut == nil:
		var zero T
		p.rec.Entry = zero
	default:
		return parsedLine[T]{}
	}
	p.rec.Seq, p.rec.Time, p.rec.Kind, p.rec.Cut, p.rec.Damage = p.env.Seq, p.env.Time, p.env.Kind, p.env.Cut, p.env.Damage
	p.ok = true
	return p
}

// removedEnd is the last line that parses among the files retention removed, raw nil when there is none.
type removedEnd struct {
	seq uint64
	raw []byte
}

// applyRetention removes every day file older than the retention relative to now. Retention only removes
// a contiguous run of the oldest days, so the line a cut documents is the last line that parses among
// exactly the files it removes.
func (l *Log[T]) applyRetention(now time.Time) (removedEnd, error) {
	if l.opts.RetentionDays == 0 {
		return removedEnd{}, nil
	}
	files, err := dayFiles(l.opts.Dir)
	if err != nil {
		return removedEnd{}, err
	}
	cutoff := now.AddDate(0, 0, -l.opts.RetentionDays).Format(DateLayout)
	var expired []string
	for _, name := range files {
		if dayFilePattern.FindStringSubmatch(name)[1] < cutoff {
			expired = append(expired, name)
		}
	}
	if len(expired) == 0 {
		return removedEnd{}, nil
	}
	end, err := l.lastLine(expired)
	if err != nil {
		return removedEnd{}, err
	}
	if end.damaged != nil && l.opts.TornLine == TornLineRefuse {
		return removedEnd{}, fmt.Errorf("parse %s: the last line is not a valid log line", end.damagedPath)
	}
	for _, name := range expired {
		if err := os.Remove(filepath.Join(l.opts.Dir, name)); err != nil && !os.IsNotExist(err) {
			return removedEnd{}, fmt.Errorf("remove expired log %s: %w", name, err)
		}
	}
	return removedEnd{seq: end.seq, raw: end.raw}, nil
}

// dayFiles returns the day file names of dir in date order. A directory that does not exist holds none.
// An entry that has the name of a day file but is no regular file is refused rather than skipped.
func dayFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !dayFilePattern.MatchString(entry.Name()) {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", filepath.Join(dir, entry.Name()))
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// chainEnd is where the chain of a set of day files continues.
type chainEnd struct {
	// seq and raw are the last line that parses, raw nil when there is none.
	seq uint64
	raw []byte
	// damaged is the last line of the newest file holding one when that line does not parse; damagedPath is
	// its file, and unterminated is true when no newline follows it.
	damaged      []byte
	damagedPath  string
	unterminated bool
}

// lastLine returns where the chain of files continues: their last line that parses, newest file first.
// When the very last line does not parse, it is returned as damaged and the search goes on before it. Only
// that case reads a whole file; otherwise lastLine reads only the tail of the file it stops at.
func (l *Log[T]) lastLine(files []string) (chainEnd, error) {
	var end chainEnd
	for i := len(files) - 1; i >= 0; i-- {
		path := filepath.Join(l.opts.Dir, files[i])
		raw, err := lastLineOf(path)
		if err != nil {
			return chainEnd{}, err
		}
		if raw == nil {
			continue
		}
		if p := parseLine[T](raw); p.ok {
			end.seq, end.raw = p.env.Seq, raw
			return end, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return chainEnd{}, fmt.Errorf("read %s: %w", path, err)
		}
		lines := splitLines(data)
		if end.damaged == nil {
			end.damaged, end.damagedPath = lines[len(lines)-1], path
			end.unterminated = !bytes.HasSuffix(data, []byte("\n"))
		}
		for j := len(lines) - 2; j >= 0; j-- {
			if p := parseLine[T](lines[j]); p.ok {
				end.seq, end.raw = p.env.Seq, lines[j]
				return end, nil
			}
		}
	}
	return end, nil
}

// lastLineBlockSize is the size of the blocks lastLineOf reads backward from the end of a day file.
const lastLineBlockSize = 4096

// lastLineOf returns the last line of path, or nil when the file does not exist or holds no line.
func lastLineOf(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	raw, err := lastLineFrom(f, info.Size(), lastLineBlockSize)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return raw, nil
}

// lastLineFrom returns the last line of the size bytes in r, exactly as the last element of splitLines
// would, or nil when there is none. It reads blocks backward from the end until the line start is found,
// so its cost does not grow with the file.
func lastLineFrom(r io.ReaderAt, size, blockSize int64) ([]byte, error) {
	var tail []byte
	for pos := size; pos > 0; {
		start := max(pos-blockSize, 0)
		block := make([]byte, pos-start)
		if _, err := r.ReadAt(block, start); err != nil {
			return nil, err
		}
		tail = append(block, tail...)
		pos = start
		trimmed := bytes.TrimRight(tail, "\n")
		if len(trimmed) == 0 {
			continue
		}
		if i := bytes.LastIndexByte(trimmed, '\n'); i >= 0 {
			return trimmed[i+1:], nil
		}
	}
	trimmed := bytes.TrimRight(tail, "\n")
	if len(trimmed) == 0 {
		return nil, nil
	}
	return trimmed, nil
}

// splitLines returns the lines of data without their newline. Trailing newlines end the data; an empty
// line before them is a line, which does not parse.
func splitLines(data []byte) [][]byte {
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		return nil
	}
	return bytes.Split(data, []byte("\n"))
}

// appendLines appends each line and a newline to path, creating it private if needed and forcing that mode
// either way. It syncs before returning. A crash while it writes can still leave the last line torn.
func appendLines(path string, lines [][]byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if err := f.Chmod(fileMode); err != nil {
		return fmt.Errorf("set the permissions of %s: %w", path, err)
	}
	var buf bytes.Buffer
	for _, line := range lines {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// hashHex is the hex SHA-256 of the exact bytes of one line without its newline, as written or read, never
// of a re-encoding.
func hashHex(line []byte) string {
	sum := sha256.Sum256(line)
	return hex.EncodeToString(sum[:])
}

// lock opens, creating if needed, and exclusively locks the lock file. The directory must exist.
func (l *Log[T]) lock() (func(), error) {
	path := filepath.Join(l.opts.Dir, l.opts.LockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}
