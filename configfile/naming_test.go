package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testNaming = BackupNaming{
	Prefix:          "settings-",
	FirstHandPrefix: "settings-first-hand-",
	HandMarker:      "-hand",
	Suffix:          ".yml",
	SeqWidth:        3,
	CopyMarker:      "-copy-",
}

var numberCodec = Codec[int]{
	Validate: func(n int) error {
		if n < 0 {
			return errors.New("negative value")
		}
		return nil
	},
	Encode: func(n int) ([]byte, error) { return []byte(strconv.Itoa(n)), nil },
	Decode: func(data []byte) (int, error) { return strconv.Atoi(string(data)) },
}

func namedStore(t *testing.T, dir string, codec Codec[int], opts ...Option) *Store[int] {
	t.Helper()
	store, err := NewStore(filepath.Join(dir, "settings.yml"), codec, opts...)
	if err != nil {
		t.Fatal(err)
	}
	tick := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store.now = func() time.Time { tick = tick.Add(time.Second); return tick }
	return store
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func writeFixture(t *testing.T, store *Store[int], name, body string) {
	t.Helper()
	if err := os.MkdirAll(store.backupDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.backupDir(), name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultNamingKeepsNamesTempPrefixAndHandDetection(t *testing.T) {
	dir := t.TempDir()
	store := namedStore(t, dir, numberCodec)
	store.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	if err := os.WriteFile(store.Path(), []byte("001"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSaveNumber(t, store, 2)
	mustSaveNumber(t, store, 3)
	src := filepath.Join(dir, "export")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	copied, err := store.BackupCopy(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"export-copy-20260102T030405Z-000000.bak", "settings.yml-20260102T030405Z-000001.bak", "settings.yml-first-hand-20260102T030405Z-000000.bak"}
	if got := listDir(t, store.backupDir()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("backup names = %v; want %v", got, want)
	}
	if filepath.Base(copied) != want[0] {
		t.Fatalf("copy = %s", copied)
	}
	var seen string
	hook := func() error {
		for _, name := range listDir(t, dir) {
			if strings.HasSuffix(name, ".tmp") {
				seen = name
			}
		}
		return nil
	}
	if err := replaceWithTemp(store.Path(), []byte("9"), "", false, hook, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(seen, ".settings.yml-") {
		t.Fatalf("temporary file = %q", seen)
	}
}

func seedScheme(t *testing.T, store *Store[int]) {
	t.Helper()
	writeFixture(t, store, "settings-20260101T000001Z-000.yml", "1")
	writeFixture(t, store, "settings-20260101T000002Z-000-hand.yml", "02")
	writeFixture(t, store, "settings-first-hand-20260101T000000Z-000.yml", "00")
}

func TestOwnNamingListsReadsRestoresAndUndoes(t *testing.T) {
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(testNaming))
	seedScheme(t, store)
	backups, err := store.Backups()
	if err != nil || len(backups) != 3 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	if !backups[0].Hand || backups[0].FirstHand || backups[1].Hand || !backups[2].FirstHand ||
		backups[0].Name != "settings-20260101T000002Z-000-hand.yml" {
		t.Fatalf("order or kinds wrong: %+v", backups)
	}
	if data, err := store.ReadBackup(backups[1].Name); err != nil || string(data) != "1" {
		t.Fatalf("read = %q, %v", data, err)
	}
	mustSaveNumber(t, store, 7)
	_, rev, err := store.LoadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.RestoreBackup("settings-first-hand-20260101T000000Z-000.yml", rev)
	if err != nil {
		t.Fatal(err)
	}
	if got := readNumberFile(t, store.Path()); string(got) != "00" {
		t.Fatalf("restored = %q", got)
	}
	if !strings.HasPrefix(restored.Previous, "settings-") || !strings.HasSuffix(restored.Previous, ".yml") {
		t.Fatalf("previous = %q", restored.Previous)
	}
	if err := store.UndoRestore(restored); err != nil {
		t.Fatal(err)
	}
	if got := readNumberFile(t, store.Path()); string(got) != "7" {
		t.Fatalf("undone = %q", got)
	}
}

func TestOwnNamingWritesNewBackupsInSameScheme(t *testing.T) {
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(testNaming))
	if err := os.WriteFile(store.Path(), []byte("001"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSaveNumber(t, store, 2) // hand-edited, first
	if err := os.WriteFile(store.Path(), []byte("003"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSaveNumber(t, store, 4) // hand-edited, later
	mustSaveNumber(t, store, 5) // regular
	names := listDir(t, store.backupDir())
	want := []string{
		"settings-20260102T030407Z-000-hand.yml",
		"settings-20260102T030408Z-000.yml",
		"settings-first-hand-20260102T030406Z-000.yml",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v; want %v", names, want)
	}
	backups, err := store.Backups()
	if err != nil || len(backups) != 3 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
}

func TestOwnNamingRotationKeepsTenAndFirstHand(t *testing.T) {
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(testNaming))
	seedScheme(t, store)
	if err := os.WriteFile(store.Path(), []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 20; i++ {
		mustSaveNumber(t, store, i)
	}
	backups, err := store.Backups()
	if err != nil || len(backups) != 11 {
		t.Fatalf("backups = %d, %v; want ten plus first hand", len(backups), err)
	}
	firsts := 0
	for _, b := range backups {
		if b.FirstHand {
			firsts++
		}
	}
	if firsts != 1 || backups[len(backups)-1].Name != "settings-first-hand-20260101T000000Z-000.yml" {
		t.Fatalf("first hand not kept alone: %+v", backups)
	}
}

func TestOwnNamingExistingFirstHandPreventsSecond(t *testing.T) {
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(testNaming))
	writeFixture(t, store, "settings-first-hand-20260101T000000Z-000.yml", "00")
	if err := os.WriteFile(store.Path(), []byte("001"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSaveNumber(t, store, 2)
	backups, err := store.Backups()
	if err != nil || len(backups) != 2 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	for _, b := range backups[:1] {
		if b.FirstHand || !b.Hand {
			t.Fatalf("second hand version = %+v", b)
		}
	}
}

func TestOwnNamingWithoutHandMarkerUsesRegularName(t *testing.T) {
	n := testNaming
	n.HandMarker = ""
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(n))
	writeFixture(t, store, "settings-first-hand-20260101T000000Z-000.yml", "00")
	if err := os.WriteFile(store.Path(), []byte("001"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSaveNumber(t, store, 2)
	if got := listDir(t, store.backupDir()); len(got) != 2 || got[0] != "settings-20260102T030406Z-000.yml" {
		t.Fatalf("names = %v", got)
	}
}

func TestOwnNamingExhaustedSequenceFailsWithoutOverwriting(t *testing.T) {
	n := testNaming
	n.SeqWidth = 1
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(n))
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store.now = func() time.Time { return fixed }
	mustSaveNumber(t, store, 0)
	for i := 1; i <= 10; i++ {
		mustSaveNumber(t, store, i)
	}
	before := map[string]string{}
	for _, name := range listDir(t, store.backupDir()) {
		before[name] = string(readNumberFile(t, filepath.Join(store.backupDir(), name)))
	}
	if len(before) != 10 {
		t.Fatalf("backups = %d; want 10", len(before))
	}
	_, rev, err := store.LoadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveIfUnchanged(99, rev); err == nil {
		t.Fatal("save succeeded with an exhausted sequence")
	}
	if got := string(readNumberFile(t, store.Path())); got != "10" {
		t.Fatalf("file changed to %q", got)
	}
	for name, body := range before {
		if got := string(readNumberFile(t, filepath.Join(store.backupDir(), name))); got != body {
			t.Fatalf("%s overwritten", name)
		}
	}
}

func TestOwnNamingIgnoresNonMatchingNames(t *testing.T) {
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(testNaming))
	writeFixture(t, store, "settings-20260101T000001Z-000.yml", "ok")
	foreign := []string{
		"settings-20260101T000001Z-0000.yml",         // wrong width
		"settings-20260101T000001Z-00.yml",           // wrong width
		"settings-20260101T00000Z-000.yml",           // malformed stamp
		"settings-20260101T000061Z-000.yml",          // non-canonical stamp
		"settings-20260102T000000+0000-000.yml",      // other layout
		"other-20260101T000001Z-001.yml",             // foreign prefix
		"settings-20260101T000001Z-001.bak",          // foreign suffix
		"settings-20260101T000001Z-1x1.yml",          // not digits
		"settings-20260101T000001Z-+01.yml",          // signed
		"settings.yml-copy-20260101T000001Z-000.yml", // copy
		"settings-20260101T000001Z-001-hand-x.yml",
	}
	for _, name := range foreign {
		writeFixture(t, store, name, "foreign")
	}
	target := filepath.Join(store.backupDir(), "settings-20260101T000001Z-000.yml")
	if err := os.Symlink(target, filepath.Join(store.backupDir(), "settings-20260101T000009Z-000.yml")); err != nil {
		t.Log("symlinks unavailable:", err)
	}
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 || backups[0].Name != "settings-20260101T000001Z-000.yml" {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	for _, name := range append(foreign, "settings-20260101T000009Z-000.yml", "../settings.yml") {
		if _, err := store.ReadBackup(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("ReadBackup(%q) = %v; want not found", name, err)
		}
	}
}

func TestOwnNamingCopyStaysOutsideBackups(t *testing.T) {
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(testNaming))
	mustSaveNumber(t, store, 1)
	copied, err := store.BackupCopy(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(copied), "settings.yml-copy-") || !strings.HasSuffix(copied, "-000.yml") {
		t.Fatalf("copy = %s", copied)
	}
	if backups, err := store.Backups(); err != nil || len(backups) != 0 {
		t.Fatalf("copy listed: %v, %v", backups, err)
	}
	// A source name that makes the copy name look like a backup is refused without writing.
	if _, err := store.BackupCopy(filepath.Join(filepath.Dir(store.Path()), "settings-20260101T000001Z-000")); err == nil {
		t.Fatal("copy parsing as a backup accepted")
	}
}

func TestCopySuffixNamesCopyPrivatelyOutsideBackupsAndRotation(t *testing.T) {
	n := testNaming
	n.CopySuffix = ".bak"
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(n))
	mustSaveNumber(t, store, 1)
	copied, err := store.BackupCopy(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if name := filepath.Base(copied); !strings.HasPrefix(name, "settings.yml-copy-") || !strings.HasSuffix(name, "-000.bak") {
		t.Fatalf("copy = %s", copied)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(copied); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("copy mode = %v, %v; want 600", info, err)
		}
	}
	for i := 2; i < 30; i++ {
		mustSaveNumber(t, store, i)
	}
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("copy rotated: %v", err)
	}
	backups, err := store.Backups()
	if err != nil || len(backups) != 10 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	for _, b := range backups {
		if b.Name == filepath.Base(copied) || !strings.HasSuffix(b.Name, ".yml") {
			t.Fatalf("copy or foreign name listed: %s", b.Name)
		}
	}
}

func TestCopySuffixUnsetKeepsSuffixAndDefaultNames(t *testing.T) {
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(testNaming))
	mustSaveNumber(t, store, 1)
	copied, err := store.BackupCopy(store.Path())
	if err != nil || !strings.HasSuffix(copied, "-000.yml") {
		t.Fatalf("copy = %s, %v", copied, err)
	}
	def := namedStore(t, t.TempDir(), numberCodec)
	mustSaveNumber(t, def, 1)
	copied, err = def.BackupCopy(def.Path())
	if err != nil || !strings.HasSuffix(copied, "-000000.bak") || !strings.Contains(copied, "settings.yml-copy-") {
		t.Fatalf("copy = %s, %v", copied, err)
	}
}

func TestCopySuffixExhaustedSequenceFailsWithoutOverwriting(t *testing.T) {
	n := testNaming
	n.SeqWidth, n.CopySuffix = 1, ".bak"
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(n))
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store.now = func() time.Time { return fixed }
	mustSaveNumber(t, store, 1)
	for i := 0; i < 10; i++ {
		if _, err := store.BackupCopy(store.Path()); err != nil {
			t.Fatal(err)
		}
	}
	before := listDir(t, store.backupDir())
	if err := os.WriteFile(store.Path(), []byte("2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BackupCopy(store.Path()); err == nil {
		t.Fatal("copy succeeded with an exhausted sequence")
	}
	for _, name := range before {
		if got := string(readNumberFile(t, filepath.Join(store.backupDir(), name))); got != "1" {
			t.Fatalf("%s overwritten with %q", name, got)
		}
	}
}

func TestInvalidOptionsAreRejectedWithoutCreatingFiles(t *testing.T) {
	mutate := func(f func(*BackupNaming)) BackupNaming { n := testNaming; f(&n); return n }
	cases := map[string]Option{
		"separator":             WithBackupNaming(mutate(func(n *BackupNaming) { n.Prefix = "a/b-" })),
		"backslash":             WithBackupNaming(mutate(func(n *BackupNaming) { n.Suffix = `.y\ml` })),
		"dotdot":                WithBackupNaming(mutate(func(n *BackupNaming) { n.Suffix = "..yml" })),
		"nul":                   WithBackupNaming(mutate(func(n *BackupNaming) { n.HandMarker = "-h\x00" })),
		"empty prefix":          WithBackupNaming(mutate(func(n *BackupNaming) { n.Prefix = "" })),
		"empty first hand":      WithBackupNaming(mutate(func(n *BackupNaming) { n.FirstHandPrefix = "" })),
		"empty suffix":          WithBackupNaming(mutate(func(n *BackupNaming) { n.Suffix = "" })),
		"empty copy":            WithBackupNaming(mutate(func(n *BackupNaming) { n.CopyMarker = "" })),
		"copy suffix separator": WithBackupNaming(mutate(func(n *BackupNaming) { n.CopySuffix = "a/b" })),
		"copy suffix dotdot":    WithBackupNaming(mutate(func(n *BackupNaming) { n.CopySuffix = "..bak" })),
		"copy suffix nul":       WithBackupNaming(mutate(func(n *BackupNaming) { n.CopySuffix = ".b\x00" })),
		"copy suffix parses": WithBackupNaming(mutate(func(n *BackupNaming) {
			n.CopyMarker, n.CopySuffix, n.Prefix, n.FirstHandPrefix = "-", ".yml", "settings.yml-", "x-"
		})),
		"copy suffix differs and parses": WithBackupNaming(mutate(func(n *BackupNaming) {
			n.CopyMarker, n.CopySuffix, n.Prefix, n.FirstHandPrefix = "-", "-hand.yml", "settings.yml-", "x-"
		})),
		"width zero":      WithBackupNaming(mutate(func(n *BackupNaming) { n.SeqWidth = 0 })),
		"width too large": WithBackupNaming(mutate(func(n *BackupNaming) { n.SeqWidth = 10 })),
		"equal prefixes":  WithBackupNaming(mutate(func(n *BackupNaming) { n.FirstHandPrefix = n.Prefix })),
		"copy parses":     WithBackupNaming(mutate(func(n *BackupNaming) { n.CopyMarker = "-"; n.Prefix = "settings.yml-"; n.FirstHandPrefix = "x-" })),
		"temp empty":      WithTempPrefix(""),
		"temp separator":  WithTempPrefix("a/b"),
		"temp dotdot":     WithTempPrefix("a..b"),
		"temp star":       WithTempPrefix("a*"),
		"temp nul":        WithTempPrefix("a\x00"),
	}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "conf")
			if _, err := NewStore(filepath.Join(dir, "settings.yml"), numberCodec, opt); err == nil {
				t.Fatal("invalid option accepted")
			}
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("directory created: %v", err)
			}
		})
	}
}

func TestTempPrefixIsUsedAndLeavesNoResidue(t *testing.T) {
	dir := t.TempDir()
	store := namedStore(t, dir, numberCodec, WithTempPrefix("tmp-"))
	var seen string
	hook := func() error {
		for _, name := range listDir(t, dir) {
			if strings.HasSuffix(name, ".tmp") {
				seen = name
			}
		}
		return errors.New("stop")
	}
	if err := replaceWithTemp(store.Path(), []byte("1"), store.tempName, false, hook, nil); err == nil {
		t.Fatal("expected failure")
	}
	if !strings.HasPrefix(seen, "tmp-") {
		t.Fatalf("temporary file = %q", seen)
	}
	mustSaveNumber(t, store, 1)
	mustSaveNumber(t, store, 2)
	for _, name := range listDir(t, dir) {
		if strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temporary file left: %s", name)
		}
	}
	// A failing write through the store leaves no residue either.
	failing := namedStore(t, dir, Codec[int]{
		Validate: numberCodec.Validate, Encode: numberCodec.Encode,
		Decode: func(data []byte) (int, error) { return 0, errors.New("never decodes") },
	}, WithTempPrefix("tmp-"))
	if _, err := failing.SaveIfUnchanged(3, RevisionAbsent); err == nil {
		t.Fatal("expected failure")
	}
	for _, name := range listDir(t, dir) {
		if strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temporary file left: %s", name)
		}
	}
}

func TestCanonicalizeHookDecidesHandDetection(t *testing.T) {
	// Files written under an old layout: "n=<number>". The codec writes "<number>".
	legacy := func(data []byte) ([]byte, error) {
		text := strings.TrimPrefix(string(data), "n=")
		n, err := strconv.Atoi(text)
		if err != nil {
			return nil, err
		}
		return []byte("n=" + strconv.Itoa(n)), nil
	}
	codec := numberCodec
	codec.Decode = func(data []byte) (int, error) {
		return strconv.Atoi(strings.TrimPrefix(string(data), "n="))
	}
	run := func(c Codec[int]) []Backup {
		store := namedStore(t, t.TempDir(), c, WithBackupNaming(testNaming))
		if err := os.WriteFile(store.Path(), []byte("n=1"), 0o600); err != nil {
			t.Fatal(err)
		}
		mustSaveNumber(t, store, 2)
		backups, err := store.Backups()
		if err != nil || len(backups) != 1 {
			t.Fatalf("backups = %v, %v", backups, err)
		}
		return backups
	}
	if b := run(codec); !b[0].FirstHand {
		t.Fatal("without a hook the old layout must count as hand-edited")
	}
	withHook := codec
	withHook.Canonicalize = legacy
	if b := run(withHook); b[0].FirstHand || b[0].Hand {
		t.Fatalf("old layout counted as hand-edited: %+v", b[0])
	}
	failing := codec
	failing.Canonicalize = func([]byte) ([]byte, error) { return nil, errors.New("unreadable") }
	if b := run(failing); !b[0].FirstHand {
		t.Fatal("hook error must count as hand-edited")
	}
	edited := codec
	edited.Canonicalize = legacy
	store := namedStore(t, t.TempDir(), edited, WithBackupNaming(testNaming))
	if err := os.WriteFile(store.Path(), []byte("n=001"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSaveNumber(t, store, 2)
	if b, _ := store.Backups(); len(b) != 1 || !b[0].FirstHand {
		t.Fatalf("hand-edited number not detected: %+v", b)
	}
}

func TestOwnNamingKeepsLockAndRevisionCheck(t *testing.T) {
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(testNaming), WithTempPrefix("tmp-"))
	rev := mustSaveNumber(t, store, 1)
	mustSaveNumber(t, store, 2)
	if _, err := store.SaveIfUnchanged(3, rev); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if _, err := os.Stat(store.Path() + ".lock"); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
}
