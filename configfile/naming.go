package configfile

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSeqWidth  = 6
	maxSeqWidth      = 9
	defaultSuffix    = ".bak"
	defaultFirstHand = "first-hand-"
	defaultCopy      = "-copy-"
)

// BackupNaming chooses how a store names its backups, for reading and writing alike. Without
// WithBackupNaming, names are <file>-<stamp>-<six digits>.bak, <file>-first-hand-<stamp>-<six digits>.bak for the
// first hand-edited version, and <source>-copy-<stamp>-<six digits>.bak for BackupCopy (with CopySuffix,
// that suffix replaces .bak on copies only). The stamp is fixed (UTC, 20060102T150405Z).
//
// A selected naming replaces the default entirely: files in another scheme are not backups of this store.
// Rotation, permissions and the sequence rules do not depend on it.
type BackupNaming struct {
	// Prefix starts every regular backup name, for example "settings-". Required.
	Prefix string
	// FirstHandPrefix starts the name of the first hand-edited version in place of Prefix. It must differ
	// from Prefix. Required.
	FirstHandPrefix string
	// HandMarker, when set, is placed between the sequence and Suffix of a later hand-edited version, which
	// rotates like a regular backup. Without it later hand-edited versions use regular names.
	HandMarker string
	// Suffix ends every name, for example ".yml". Required.
	Suffix string
	// SeqWidth is the number of digits of the sequence, 1 to 9. The sequence stops at 10^SeqWidth.
	SeqWidth int
	// CopyMarker sits between the source name and the stamp in a BackupCopy name, for example "-copy-".
	// Required. The copy name must never parse as a backup of this store, whatever the source name.
	CopyMarker string
	// CopySuffix ends a BackupCopy name in place of Suffix, for example ".bak", so a copy of a file that is
	// not a document of the schema does not look like one. Optional: without it, Suffix applies. The copy
	// name must still never parse as a backup of this store.
	CopySuffix string
}

// Option configures NewStore.
type Option func(*options)

type options struct {
	naming     *BackupNaming
	tempPrefix *string
}

// WithBackupNaming selects the backup naming of the store. NewStore rejects an invalid naming.
func WithBackupNaming(n BackupNaming) Option { return func(o *options) { o.naming = &n } }

// WithTempPrefix selects the prefix of the temporary file a write creates beside the target, which is
// removed after a failure. The name is the prefix, random characters and ".tmp". The default is a dot,
// the file name and a dash. NewStore rejects an invalid prefix.
func WithTempPrefix(prefix string) Option { return func(o *options) { o.tempPrefix = &prefix } }

// naming is a validated scheme with the file-dependent defaults resolved.
type naming struct {
	prefix, firstHandPrefix, handMarker, suffix, copyMarker, copySuffix string
	width                                                               int
}

func defaultNaming(base string) naming {
	return naming{
		prefix: base + "-", firstHandPrefix: base + "-" + defaultFirstHand, suffix: defaultSuffix,
		copyMarker: defaultCopy, copySuffix: defaultSuffix, width: defaultSeqWidth,
	}
}

func (n naming) limit() int {
	limit := 1
	for i := 0; i < n.width; i++ {
		limit *= 10
	}
	return limit
}

func validNamePart(part string) error {
	if part == "" {
		return errors.New("is empty")
	}
	if strings.Contains(part, "..") {
		return errors.New("contains ..")
	}
	for _, r := range part {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			return fmt.Errorf("contains %q", r)
		}
	}
	return nil
}

func newNaming(n BackupNaming) (naming, error) {
	for _, part := range []struct{ name, value string }{
		{"Prefix", n.Prefix}, {"FirstHandPrefix", n.FirstHandPrefix}, {"Suffix", n.Suffix}, {"CopyMarker", n.CopyMarker},
	} {
		if err := validNamePart(part.value); err != nil {
			return naming{}, fmt.Errorf("invalid backup naming: %s %w", part.name, err)
		}
	}
	if n.HandMarker != "" {
		if err := validNamePart(n.HandMarker); err != nil {
			return naming{}, fmt.Errorf("invalid backup naming: HandMarker %w", err)
		}
	}
	if n.SeqWidth < 1 || n.SeqWidth > maxSeqWidth {
		return naming{}, fmt.Errorf("invalid backup naming: SeqWidth must be 1 to %d", maxSeqWidth)
	}
	if n.Prefix == n.FirstHandPrefix {
		return naming{}, errors.New("invalid backup naming: Prefix and FirstHandPrefix are equal")
	}
	copySuffix := n.Suffix
	if n.CopySuffix != "" {
		if err := validNamePart(n.CopySuffix); err != nil {
			return naming{}, fmt.Errorf("invalid backup naming: CopySuffix %w", err)
		}
		copySuffix = n.CopySuffix
	}
	return naming{
		prefix: n.Prefix, firstHandPrefix: n.FirstHandPrefix, handMarker: n.HandMarker, suffix: n.Suffix,
		copyMarker: n.CopyMarker, copySuffix: copySuffix, width: n.SeqWidth,
	}, nil
}

// copyName returns the file name BackupCopy gives a copy of the file called src.
func (n naming) copyName(src, stamp string, seq int) string {
	return fmt.Sprintf("%s%s%s-%0*d%s", src, n.copyMarker, stamp, n.width, seq, n.copySuffix)
}

// checkCopy rejects a copy name that would count as a backup of the store.
func (n naming) checkCopy(src string) error {
	name := n.copyName(src, time.Unix(0, 0).UTC().Format(stampLayout), 0)
	if _, ok := n.parse(name); ok {
		return errors.New("copy name would parse as a backup")
	}
	return nil
}

// sample returns the name of a backup of each kind for the stamp and sequence 0.
func (n naming) sample(firstHand, hand bool) string {
	prefix := n.prefix
	marker := ""
	if firstHand {
		prefix = n.firstHandPrefix
	}
	if hand {
		marker = n.handMarker
	}
	return n.name(prefix, time.Unix(0, 0).UTC().Format(stampLayout), 0, marker)
}

func (n naming) name(prefix, stamp string, seq int, marker string) string {
	return fmt.Sprintf("%s%s-%0*d%s%s", prefix, stamp, n.width, seq, marker, n.suffix)
}

// validate checks the scheme as a whole: every kind of name parses to exactly its own kind, and a copy
// of the store's own file never parses.
func (n naming) validate(base string) error {
	kinds := []struct{ firstHand, hand bool }{{false, false}, {true, false}}
	if n.handMarker != "" {
		kinds = append(kinds, struct{ firstHand, hand bool }{false, true})
	}
	for _, kind := range kinds {
		got, ok := n.parse(n.sample(kind.firstHand, kind.hand))
		if !ok || got.FirstHand != kind.firstHand || got.Hand != kind.hand {
			return errors.New("invalid backup naming: names of different kinds are not distinguishable")
		}
	}
	if err := n.checkCopy(base); err != nil {
		return fmt.Errorf("invalid backup naming: %w", err)
	}
	return nil
}

// parse recognizes a backup name of this scheme. A name that fits more than one kind is not a backup.
func (n naming) parse(name string) (Backup, bool) {
	var found []Backup
	try := func(prefix, marker string, firstHand, hand bool) {
		if b, ok := n.parseForm(name, prefix, marker); ok {
			b.FirstHand, b.Hand = firstHand, hand
			found = append(found, b)
		}
	}
	try(n.firstHandPrefix, "", true, false)
	try(n.prefix, "", false, false)
	if n.handMarker != "" {
		try(n.prefix, n.handMarker, false, true)
	}
	if len(found) != 1 {
		return Backup{}, false
	}
	return found[0], true
}

func (n naming) parseForm(name, prefix, marker string) (Backup, bool) {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, marker+n.suffix) ||
		len(name) < len(prefix)+len(marker)+len(n.suffix) {
		return Backup{}, false
	}
	part := name[len(prefix) : len(name)-len(marker)-len(n.suffix)]
	stamp, seqText, ok := strings.Cut(part, "-")
	if !ok || len(seqText) != n.width {
		return Backup{}, false
	}
	when, err := time.Parse(stampLayout, stamp)
	if err != nil || when.UTC().Format(stampLayout) != stamp {
		return Backup{}, false
	}
	seq, err := atoiCanonical(seqText, n.width)
	if err != nil {
		return Backup{}, false
	}
	return Backup{Name: name, Time: when, seq: seq}, true
}

func validTempPrefix(prefix string) error {
	if err := validNamePart(prefix); err != nil {
		return fmt.Errorf("invalid temporary file prefix: %w", err)
	}
	return nil
}

// atoiCanonical parses a sequence that is exactly width digits with no sign.
func atoiCanonical(text string, width int) (int, error) {
	n, err := strconv.Atoi(text)
	if err != nil || fmt.Sprintf("%0*d", width, n) != text {
		return 0, errors.New("sequence is not canonical")
	}
	return n, nil
}
