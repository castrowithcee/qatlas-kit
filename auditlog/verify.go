package auditlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Problem is the kind of the first problem of a day.
type Problem string

// The problems a DayReport names.
const (
	ProblemNone       Problem = ""
	ProblemUnparsable Problem = "unparsable-line"
	ProblemGenesis    Problem = "bad-genesis"
	ProblemMissing    Problem = "missing-entries"
	ProblemChain      Problem = "chain-broken"
	ProblemMAC        Problem = "check-value-mismatch"
)

// DayReport is one day file's part of a Report.
type DayReport struct {
	Date string
	// Entries counts the entries of the day; a marker is not one.
	Entries int
	// Unverified counts the lines of the day, markers included, whose check value is empty: written without
	// the key, which never breaks the chain by itself.
	Unverified int
	// Unchecked counts the lines of the day that carry a check value nobody checked, because there was no
	// Checker.
	Unchecked int
	// Damaged counts the lines of the day that do not parse but are documented by the damaged-line marker
	// right after them. Such a line does not break the chain.
	Damaged int
	// Changed counts the lines of the day whose check value is malformed or does not match. Each one breaks
	// the chain.
	Changed int
	// Missing counts the sequence numbers skipped right before a line of this day that no retention cut
	// documents.
	Missing uint64
	// Problem is the first problem of the day; ProblemSeq is the sequence number of its line, 0 for a line
	// that does not parse, and ProblemLine its 1-based line number in the day file.
	Problem     Problem
	ProblemSeq  uint64
	ProblemLine int
}

// Signatures is what the check values of a day amount to.
type Signatures int

const (
	// SignaturesNotChecked is a day with a line that was not checked against a key.
	SignaturesNotChecked Signatures = iota
	// SignaturesValid is a day whose every line was checked and matched.
	SignaturesValid
	// SignaturesInvalid is a day with a check value that is malformed or does not match.
	SignaturesInvalid
)

// Signatures summarizes the check values of the day; checked is Report.Checked.
func (d DayReport) Signatures(checked bool) Signatures {
	switch {
	case d.Changed > 0:
		return SignaturesInvalid
	case !checked || d.Unverified > 0 || d.Unchecked > 0:
		return SignaturesNotChecked
	}
	return SignaturesValid
}

// Report is the result of Verify: one row per day file, in date order.
type Report struct {
	Days []DayReport
	// Checked is true when the check values were checked, which takes a Checker.
	Checked bool
	// Broken is true when the chain breaks anywhere that is not a documented retention cut or documented
	// damage.
	Broken bool
}

// MACState is what a check found about the check value of one line.
type MACState int

const (
	// MACNone is a line without a check value.
	MACNone MACState = iota
	// MACUnchecked is a line with a well-formed check value nobody checked.
	MACUnchecked
	// MACValid is a line whose check value matches it.
	MACValid
	// MACInvalid is a line whose check value is malformed or does not match it.
	MACInvalid
)

// ChainState is where, if anywhere, the chain breaks at one line.
type ChainState int

const (
	// ChainIntact is a line the chain runs through, or one only Missing explains.
	ChainIntact ChainState = iota
	// ChainUnparsed is a line that does not parse and that no marker documents.
	ChainUnparsed
	// ChainGenesis is a first line with sequence number 1 that does not chain from the genesis hash.
	ChainGenesis
	// ChainNext is a line the next line does not chain from, although it follows it by sequence number.
	ChainNext
	// ChainPrevious is a line that does not chain from the line before it, where that line is not among the
	// lines returned or the sequence number itself is out of order.
	ChainPrevious
	// ChainDamaged is a line that does not parse, documented by the damaged-line marker after it.
	ChainDamaged
)

// LineResult is what VerifyLines found for one line of a day file.
type LineResult[T any] struct {
	// Record is the line decoded; the zero Record for a line that does not parse.
	Record Record[T]
	Parsed bool
	MAC    MACState
	Chain  ChainState
	// Missing counts the sequence numbers skipped right before this line that no retention cut documents.
	Missing uint64
}

// DayLines holds the LineResult of every line of one day file, in file order.
type DayLines[T any] struct {
	Date  string
	Lines []LineResult[T]
}

// Verify walks every day file in date order and checks the chain: the sequence number of each line follows
// the one before by exactly one, and its prev_hash is the SHA-256 of the exact previous line that parses,
// across day files. The first line is exempt two ways only: its sequence number is 1 and it chains from
// the genesis hash, or its sequence number follows the greatest through_seq of the retention-cut markers
// that are still there. A missing directory is an intact, empty chain.
//
// Every well-formed check value is checked with checker when there is one; a malformed one, or one that
// does not match, breaks the chain, while an empty one counts as unverified. A check that cannot be made
// at all fails Verify. The files are read under the lock, which is released before the first check.
func (l *Log[T]) Verify(checker Checker) (Report, error) {
	report, _, err := l.verify(checker, nil)
	return report, err
}

// VerifyLines walks the chain as Verify does, across every day file, and reports each line of the day
// files from from through to (DateLayout, inclusive; "" leaves that end open). Only the check values of
// those days are handed to checker.
func (l *Log[T]) VerifyLines(checker Checker, from, to string) ([]DayLines[T], error) {
	for _, date := range []string{from, to} {
		if date != "" {
			if _, err := time.Parse(DateLayout, date); err != nil {
				return nil, fmt.Errorf("a day must have the form %s", DateLayout)
			}
		}
	}
	inRange := func(date string) bool { return (from == "" || date >= from) && (to == "" || date <= to) }
	_, lines, err := l.verify(checker, inRange)
	return lines, err
}

type dayFile[T any] struct {
	date   string
	raw    [][]byte
	parsed []parsedLine[T]
}

// verify is the walk of Verify and VerifyLines. inRange is nil for Verify, which checks every check value
// and records no line; otherwise it selects the days whose check values are checked and whose lines are
// recorded.
func (l *Log[T]) verify(checker Checker, inRange func(date string) bool) (Report, []DayLines[T], error) {
	if _, err := os.Stat(l.opts.Dir); err != nil {
		if os.IsNotExist(err) {
			return Report{Checked: checker != nil}, nil, nil
		}
		return Report{}, nil, err
	}
	days, err := l.readDays()
	if err != nil {
		return Report{}, nil, err
	}
	record := func(date string) bool { return inRange != nil && inRange(date) }
	checks := func(date string) bool { return checker != nil && (inRange == nil || inRange(date)) }

	// Retention removes only the oldest contiguous days, so the greatest through_seq of the markers still
	// there matches the first line that is left, wherever that marker sits.
	var cutThroughSeq uint64
	haveCut := false
	for _, d := range days {
		for _, p := range d.parsed {
			if p.ok && p.env.Kind == KindCut && (!haveCut || p.env.Cut.ThroughSeq > cutThroughSeq) {
				cutThroughSeq, haveCut = p.env.Cut.ThroughSeq, true
			}
		}
	}

	var results []bool
	if checker != nil {
		var signed [][]byte
		for _, d := range days {
			if !checks(d.date) {
				continue
			}
			for i, p := range d.parsed {
				if p.ok && p.env.MAC != "" && wellFormed(d.raw[i]) {
					signed = append(signed, d.raw[i])
				}
			}
		}
		if len(signed) > 0 {
			results, err = checker.Check(signed)
			if err != nil {
				return Report{}, nil, fmt.Errorf("cannot check the check values: %w", err)
			}
			if len(results) != len(signed) {
				return Report{}, nil, errors.New("cannot check the check values: the checker answered for the wrong number of lines")
			}
		}
	}

	type finding struct {
		problem Problem
		seq     uint64
	}
	type damagedLine struct {
		day    *DayReport
		number int
		line   *LineResult[T]
	}
	var (
		report        = Report{Checked: checker != nil, Days: make([]DayReport, len(days))}
		out           []DayLines[T]
		previousRaw   []byte
		previousSeq   uint64
		sawFirstEntry bool
		// previousLine is the recorded result of previousRaw, nil when it was not recorded.
		previousLine *LineResult[T]
		// pending is the damaged line that the marker on the next line documents, if that marker holds.
		pending *damagedLine
	)
	setProblem := func(day *DayReport, problem Problem, seq uint64, number int) {
		if day.Problem == ProblemNone {
			day.Problem, day.ProblemSeq, day.ProblemLine = problem, seq, number
		}
		report.Broken = true
	}
	for k, d := range days {
		day := &report.Days[k]
		day.Date = d.date
		var lines []LineResult[T]
		if record(d.date) {
			lines = make([]LineResult[T], 0, len(d.raw))
		}
		for i, raw := range d.raw {
			number := i + 1
			var line *LineResult[T]
			if lines != nil {
				lines = append(lines, LineResult[T]{})
				line = &lines[len(lines)-1]
			}
			p := d.parsed[i]
			if !p.ok {
				if l.opts.TornLine == TornLineDocument && documents(nextLine(days, k, i), raw) {
					pending = &damagedLine{day, number, line}
				} else {
					setProblem(day, ProblemUnparsable, 0, number)
					setChain(line, ChainUnparsed)
				}
				continue
			}
			entry := p.env
			if line != nil {
				line.Record, line.Parsed = p.rec, true
			}
			if entry.Kind == "" {
				day.Entries++
			}
			// The problems of a line are collected first, so a damaged line that its marker fails to document
			// is named before the marker.
			var found []finding
			switch {
			case !sawFirstEntry && entry.Seq == 1:
				if entry.PrevHash != genesisHash {
					found = append(found, finding{ProblemGenesis, entry.Seq})
					setChain(line, ChainGenesis)
				}
			case !sawFirstEntry && haveCut && entry.Seq == cutThroughSeq+1:
				// Documented: the retention cut says the chain continues exactly here.
			case !sawFirstEntry:
				var missing uint64
				if haveCut && entry.Seq > cutThroughSeq+1 {
					missing = entry.Seq - cutThroughSeq - 1
				} else if !haveCut && entry.Seq > 1 {
					missing = entry.Seq - 1
				}
				day.Missing += missing
				found = append(found, finding{ProblemMissing, entry.Seq})
				if line != nil {
					line.Missing = missing
					if missing == 0 {
						line.Chain = ChainPrevious
					}
				}
			case entry.Seq != previousSeq+1 || entry.PrevHash != hashHex(previousRaw):
				if entry.Seq > previousSeq+1 {
					day.Missing += entry.Seq - previousSeq - 1
					if line != nil {
						line.Missing = entry.Seq - previousSeq - 1
					}
				} else if entry.Seq == previousSeq+1 && previousLine != nil {
					setChain(previousLine, ChainNext)
				} else {
					setChain(line, ChainPrevious)
				}
				found = append(found, finding{ProblemChain, entry.Seq})
			}
			state := countMAC(day, checks(d.date), &results, entry.MAC, raw)
			if state == MACInvalid {
				found = append(found, finding{ProblemMAC, entry.Seq})
			}
			if line != nil {
				line.MAC = state
			}
			if pending != nil {
				if len(found) == 0 {
					pending.day.Damaged++
					setChain(pending.line, ChainDamaged)
				} else {
					setProblem(pending.day, ProblemUnparsable, 0, pending.number)
					setChain(pending.line, ChainUnparsed)
				}
				pending = nil
			}
			for _, f := range found {
				setProblem(day, f.problem, f.seq, number)
			}
			sawFirstEntry = true
			previousRaw, previousSeq, previousLine = raw, entry.Seq, line
		}
		if lines != nil {
			out = append(out, DayLines[T]{Date: d.date, Lines: lines})
		}
	}
	return report, out, nil
}

// setChain records state on line unless line is nil or already records a break of its own.
func setChain[T any](line *LineResult[T], state ChainState) {
	if line != nil && line.Chain == ChainIntact {
		line.Chain = state
	}
}

// nextLine returns the parsed line after line i of day k in the walk, which is the first line of the next
// day file that has one when line i is the last of its day; ok is false when there is none.
func nextLine[T any](days []dayFile[T], k, i int) parsedLine[T] {
	if i+1 < len(days[k].raw) {
		return days[k].parsed[i+1]
	}
	for _, d := range days[k+1:] {
		if len(d.raw) > 0 {
			return d.parsed[0]
		}
	}
	return parsedLine[T]{}
}

// documents reports whether next is a damaged-line marker that names the hash of exactly damaged. Whether
// the marker holds as a line is checked where it is walked.
func documents[T any](next parsedLine[T], damaged []byte) bool {
	return next.ok && next.env.Kind == KindDamage && next.env.Damage.LineHash == hashHex(damaged)
}

// countMAC counts the check value of one line into its day and returns its state. results holds the
// answers of the checker for the well-formed check values not counted yet, in order.
func countMAC(day *DayReport, checked bool, results *[]bool, mac string, raw []byte) MACState {
	switch {
	case mac == "":
		day.Unverified++
		return MACNone
	case wellFormed(raw) && !checked:
		day.Unchecked++
		return MACUnchecked
	case wellFormed(raw):
		valid := (*results)[0]
		*results = (*results)[1:]
		if valid {
			return MACValid
		}
	}
	day.Changed++
	return MACInvalid
}

// readDays reads and parses every day file under the lock, which it releases before it returns.
func (l *Log[T]) readDays() ([]dayFile[T], error) {
	unlock, err := l.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	files, err := dayFiles(l.opts.Dir)
	if err != nil {
		return nil, err
	}
	days := make([]dayFile[T], 0, len(files))
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(l.opts.Dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		d := dayFile[T]{date: dayFilePattern.FindStringSubmatch(name)[1], raw: splitLines(data)}
		d.parsed = make([]parsedLine[T], len(d.raw))
		for i, raw := range d.raw {
			d.parsed[i] = parseLine[T](raw)
		}
		days = append(days, d)
	}
	return days, nil
}
