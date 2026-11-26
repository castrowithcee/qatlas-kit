package projectpath

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func realEnv() searchEnv { return searchEnv{now: time.Now, device: deviceLookup()} }

func run(t *testing.T, root, text string, opt SearchOptions) SearchResult {
	t.Helper()
	res, err := search(context.Background(), root, text, opt, realEnv())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// rel returns the match paths relative to root, with forward slashes.
func rel(t *testing.T, root string, res SearchResult) []string {
	t.Helper()
	base := Canonical(root)
	var out []string
	for _, m := range res.Matches {
		r, err := filepath.Rel(base, m.Path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(r))
	}
	return out
}

func TestSplitSearch(t *testing.T) {
	for _, c := range []struct {
		in, start, text string
		ok              bool
	}{
		{"/opt*n8n", "/opt", "n8n", true},
		{"~/a/b*x*y", "~/a/b", "x*y", true},
		{"/opt/n8n", "", "", false},
		{"/opt*", "", "", false},
		{"*n8n", "", "", false},
		{"", "", "", false},
	} {
		start, text, ok := SplitSearch(c.in)
		if ok != c.ok || (ok && (start != c.start || text != c.text)) {
			t.Errorf("SplitSearch(%q) = %q, %q, %v", c.in, start, text, ok)
		}
	}
}

func TestSearchOrderAndKind(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "stacks/n8n", "a/b/n8n", "n8n-old", "x/my-n8n", "N8N")
	touch(t, root, "n8n.txt", "stacks/other.txt", "stacks/N8nNotes.md")
	res := run(t, root, "n8N", SearchOptions{})
	if res.End != Complete {
		t.Fatalf("End = %v", res.End)
	}
	// Rank first (same name, prefix, part), then depth, then path.
	want := []string{"N8N", "stacks/n8n", "a/b/n8n", "n8n-old", "n8n.txt", "stacks/N8nNotes.md", "x/my-n8n"}
	if got := rel(t, root, res); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	var isDir []bool
	for _, m := range res.Matches {
		isDir = append(isDir, m.Dir)
	}
	if !reflect.DeepEqual(isDir, []bool{true, true, true, true, false, false, true}) {
		t.Errorf("Dir flags %v", isDir)
	}
	dirs := rel(t, root, run(t, root, "n8n", SearchOptions{Kind: OnlyDirectories}))
	if !reflect.DeepEqual(dirs, []string{"N8N", "stacks/n8n", "a/b/n8n", "n8n-old", "x/my-n8n"}) {
		t.Errorf("dirs %v", dirs)
	}
	files := rel(t, root, run(t, root, "n8n", SearchOptions{Kind: OnlyFiles}))
	if !reflect.DeepEqual(files, []string{"n8n.txt", "stacks/N8nNotes.md"}) {
		t.Errorf("files %v", files)
	}
}

func TestSearchErrors(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "f")
	ctx := context.Background()
	for name, c := range map[string]struct {
		start, text string
		kind        Kind
	}{
		"empty text":    {root, "", Both},
		"missing start": {filepath.Join(root, "none"), "x", Both},
		"file start":    {filepath.Join(root, "f"), "x", Both},
		"unknown kind":  {root, "x", Kind(9)},
	} {
		if _, err := Search(ctx, c.start, c.text, SearchOptions{Kind: c.kind}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestSearchEntryLimit(t *testing.T) {
	root := t.TempDir()
	for i := range 20 {
		touch(t, root, "f"+string(rune('a'+i)))
	}
	res := run(t, root, "f", SearchOptions{MaxEntries: 5})
	if res.End != EntryLimit || res.Entries != 5 || len(res.Matches) > 5 {
		t.Fatalf("%+v", res)
	}
	if res := run(t, root, "f", SearchOptions{MaxEntries: 20}); res.End != Complete || res.Entries != 20 {
		t.Fatalf("exact limit: %+v", res)
	}
}

func TestSearchMatchLimit(t *testing.T) {
	root := t.TempDir()
	for i := range 20 {
		touch(t, root, "hit"+string(rune('a'+i)))
	}
	res := run(t, root, "hit", SearchOptions{MaxMatches: 3})
	if res.End != MatchLimit || len(res.Matches) != 3 {
		t.Fatalf("%+v", res)
	}
}

func TestSearchTimeLimit(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "a/b/c")
	touch(t, root, "a/x", "a/b/y", "a/b/c/z")
	var ticks atomic.Int64
	env := realEnv()
	env.now = func() time.Time { return time.Unix(0, 0).Add(time.Duration(ticks.Add(1)) * time.Second) }
	res, err := search(context.Background(), root, "zzz", SearchOptions{Timeout: 3 * time.Second}, env)
	if err != nil || res.End != TimeLimit {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSearchCanceled(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "hit")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Search(ctx, root, "hit", SearchOptions{})
	if err != nil || res.End != Canceled || len(res.Matches) != 0 || res.Entries != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSearchDoesNotFollowLinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mkdirs(t, outside, "inside-link-target")
	touch(t, outside, "inside-link-target/needle")
	if err := os.Symlink(outside, filepath.Join(root, "needle-link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	res := run(t, root, "needle", SearchOptions{})
	if got := rel(t, root, res); !reflect.DeepEqual(got, []string{"needle-link"}) {
		t.Fatalf("got %v", got)
	}
	if m := res.Matches[0]; !m.Link || m.Dir {
		t.Errorf("match %+v", m)
	}
}

func TestSearchSkipsUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced here")
	}
	root := t.TempDir()
	mkdirs(t, root, "closed", "open")
	touch(t, root, "closed/needle", "open/needle")
	closed := filepath.Join(root, "closed")
	if err := os.Chmod(closed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(closed, 0o755) })
	res := run(t, root, "needle", SearchOptions{})
	if got := rel(t, root, res); !reflect.DeepEqual(got, []string{"open/needle"}) || res.Skipped != 1 || res.End != Complete {
		t.Fatalf("%v %+v", got, res)
	}
}

func TestSearchSkipsOtherDevice(t *testing.T) {
	if deviceLookup() == nil {
		t.Skip("no device check on this platform")
	}
	root := t.TempDir()
	mkdirs(t, root, "mount", "same")
	touch(t, root, "mount/needle", "same/needle")
	canon := Canonical(root)
	env := realEnv()
	env.device = func(path string) (uint64, bool) {
		if path == filepath.Join(canon, "mount") {
			return 2, true
		}
		return 1, true
	}
	res, err := search(context.Background(), root, "needle", SearchOptions{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if got := rel(t, root, res); !reflect.DeepEqual(got, []string{"same/needle"}) || res.Skipped != 1 {
		t.Fatalf("%v %+v", got, res)
	}
}

func TestSearchSkipsUnsafeNames(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ne‮edle"), nil, 0o644); err != nil {
		t.Skip("name not allowed here:", err)
	}
	touch(t, root, "needle")
	if got := rel(t, root, run(t, root, "e", SearchOptions{Kind: OnlyFiles})); !reflect.DeepEqual(got, []string{"needle"}) {
		t.Fatalf("got %v", got)
	}
}

func TestSearchSkipsExcludedDirs(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "skipme", "keep")
	touch(t, root, "skipme/needle", "keep/needle")
	env := realEnv()
	env.excluded = []string{filepath.Join(Canonical(root), "skipme")}
	res, err := search(context.Background(), root, "needle", SearchOptions{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if got := rel(t, root, res); !reflect.DeepEqual(got, []string{"keep/needle"}) || res.Skipped != 1 {
		t.Fatalf("%v %+v", got, res)
	}
}

func TestExcludedDirsPerPlatform(t *testing.T) {
	want := []string(nil)
	if runtime.GOOS == "linux" {
		want = []string{"/proc", "/sys", "/dev", "/run"}
	}
	if !reflect.DeepEqual(excludedDirs(), want) {
		t.Fatal(excludedDirs())
	}
}
