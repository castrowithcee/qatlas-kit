package auditlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Query selects the entries Read returns. Zero values leave a filter open; Limit must be positive.
type Query[T any] struct {
	// From and To bound the time of an entry, both inclusive.
	From, To time.Time
	// Match keeps only the entries it accepts; nil keeps every entry.
	Match func(Record[T]) bool
	Limit int
	// After is the Next value of the previous Page; empty starts at the newest entry.
	After string
}

// Page is one slice of the matching entries, newest first. Next is non-empty when more entries match after
// the last one returned.
type Page[T any] struct {
	Records []Record[T]
	Next    string
}

// ErrBadCursor reports a Query.After that is not a value Read handed out.
var ErrBadCursor = errors.New("the cursor is not valid")

// cursor names the entry after which a page continues: its day file and its sequence number.
type cursor struct {
	day string
	seq uint64
}

func (c cursor) String() string { return c.day + "/" + strconv.FormatUint(c.seq, 10) }

func parseCursor(text string) (cursor, error) {
	day, seqText, ok := strings.Cut(text, "/")
	seq, err := strconv.ParseUint(seqText, 10, 64)
	if !ok || err != nil || !dayFilePattern.MatchString(day+".jsonl") {
		return cursor{}, ErrBadCursor
	}
	return cursor{day, seq}, nil
}

// Read returns the entries that match q, newest first, at most q.Limit of them. It does not check the
// chain; Verify does. It takes no lock and writes nothing, so a line a concurrent Append has not finished
// is skipped like any other line that does not parse. Markers are never returned. A missing directory
// holds no entries.
func (l *Log[T]) Read(q Query[T]) (Page[T], error) {
	if q.Limit < 1 {
		return Page[T]{}, errors.New("limit must be positive")
	}
	var after cursor
	if q.After != "" {
		var err error
		if after, err = parseCursor(q.After); err != nil {
			return Page[T]{}, err
		}
	}
	files, err := dayFiles(l.opts.Dir)
	if err != nil {
		return Page[T]{}, err
	}
	fromDay, toDay := "", ""
	if !q.From.IsZero() {
		fromDay = q.From.UTC().Format(DateLayout)
	}
	if !q.To.IsZero() {
		toDay = q.To.UTC().Format(DateLayout)
	}

	var page Page[T]
	lastDay := ""
	for i := len(files) - 1; i >= 0; i-- {
		day := dayFilePattern.FindStringSubmatch(files[i])[1]
		switch {
		case toDay != "" && day > toDay, after.day != "" && day > after.day:
			continue
		case fromDay != "" && day < fromDay:
			return page, nil
		}
		data, err := os.ReadFile(filepath.Join(l.opts.Dir, files[i]))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Page[T]{}, fmt.Errorf("read %s: %w", files[i], err)
		}
		lines := splitLines(data)
		for j := len(lines) - 1; j >= 0; j-- {
			p := parseLine[T](lines[j])
			if !p.ok || p.rec.Kind != "" || !q.matches(p.rec) || after.day == day && p.rec.Seq >= after.seq {
				continue
			}
			if len(page.Records) == q.Limit {
				page.Next = cursor{lastDay, page.Records[len(page.Records)-1].Seq}.String()
				return page, nil
			}
			page.Records = append(page.Records, p.rec)
			lastDay = day
		}
	}
	return page, nil
}

func (q Query[T]) matches(r Record[T]) bool {
	switch {
	case !q.From.IsZero() && r.Time.Before(q.From), !q.To.IsZero() && r.Time.After(q.To):
		return false
	case q.Match != nil && !q.Match(r):
		return false
	}
	return true
}

// FileDays returns the range of UTC day files that can hold a line of the days from through to in loc
// (DateLayout, inclusive; "" leaves that end open), for VerifyLines. A nil loc is UTC.
func FileDays(from, to string, loc *time.Location) (string, string, error) {
	loc = orUTC(loc)
	if from != "" {
		day, err := time.ParseInLocation(DateLayout, from, loc)
		if err != nil {
			return "", "", fmt.Errorf("a day must have the form %s", DateLayout)
		}
		from = day.UTC().Format(DateLayout)
	}
	if to != "" {
		day, err := time.ParseInLocation(DateLayout, to, loc)
		if err != nil {
			return "", "", fmt.Errorf("a day must have the form %s", DateLayout)
		}
		to = day.AddDate(0, 0, 1).Add(-time.Nanosecond).UTC().Format(DateLayout)
	}
	return from, to, nil
}

// DayActivity is what the lines of one day in a location hold.
type DayActivity struct {
	// Date is the day in DateLayout.
	Date string
	// Entries counts the entries of the day that the group function counted; Groups counts them by group.
	Entries int
	Groups  map[string]int
	// Altered is true when a line of the day does not parse without a marker documenting it, fails its
	// check value or breaks the chain.
	Altered bool
	// Damaged is true when a line of the day does not parse and a marker documents it.
	Damaged bool
	// Gap is true when sequence numbers are missing right before a line of the day that no retention cut
	// documents.
	Gap bool
}

// DailyActivity aggregates the lines VerifyLines returned per day in loc, for every day from through to
// (DateLayout, inclusive), days without a line included; "" leaves an end open at the first or last day
// that holds a line. group names the group of an entry and whether it counts at all; markers never count.
// A line without a time, and a line that does not parse, belongs to the UTC day of its file. A nil loc is
// UTC.
func DailyActivity[T any](days []DayLines[T], from, to string, loc *time.Location, group func(Record[T]) (string, bool)) ([]DayActivity, error) {
	loc = orUTC(loc)
	var first, last time.Time
	var err error
	if from != "" {
		if first, err = time.ParseInLocation(DateLayout, from, loc); err != nil {
			return nil, fmt.Errorf("a day must have the form %s", DateLayout)
		}
	}
	if to != "" {
		if last, err = time.ParseInLocation(DateLayout, to, loc); err != nil {
			return nil, fmt.Errorf("a day must have the form %s", DateLayout)
		}
	}
	byDay := map[string]*DayActivity{}
	for _, file := range days {
		for _, line := range file.Lines {
			date := file.Date
			if line.Parsed && !line.Record.Time.IsZero() {
				date = line.Record.Time.In(loc).Format(DateLayout)
			}
			if (from != "" && date < from) || (to != "" && date > to) {
				continue
			}
			a := byDay[date]
			if a == nil {
				a = &DayActivity{Date: date}
				byDay[date] = a
			}
			switch {
			case line.Chain == ChainDamaged:
				a.Damaged = true
			case !line.Parsed || line.MAC == MACInvalid || line.Chain != ChainIntact:
				a.Altered = true
			}
			if line.Missing > 0 {
				a.Gap = true
			}
			if !line.Parsed || line.Record.Kind != "" || group == nil {
				continue
			}
			if name, ok := group(line.Record); ok {
				a.Entries++
				if a.Groups == nil {
					a.Groups = map[string]int{}
				}
				a.Groups[name]++
			}
		}
	}
	if from == "" || to == "" {
		if len(byDay) == 0 {
			return nil, nil
		}
		dates := make([]string, 0, len(byDay))
		for date := range byDay {
			dates = append(dates, date)
		}
		slices.Sort(dates)
		if from == "" {
			first, _ = time.ParseInLocation(DateLayout, dates[0], loc)
		}
		if to == "" {
			last, _ = time.ParseInLocation(DateLayout, dates[len(dates)-1], loc)
		}
	}
	var out []DayActivity
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		date := day.Format(DateLayout)
		if a := byDay[date]; a != nil {
			out = append(out, *a)
		} else {
			out = append(out, DayActivity{Date: date})
		}
	}
	return out, nil
}

func orUTC(loc *time.Location) *time.Location {
	if loc == nil {
		return time.UTC
	}
	return loc
}
