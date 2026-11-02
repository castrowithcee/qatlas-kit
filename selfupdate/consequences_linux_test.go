package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountProcessesOfUserByName(t *testing.T) {
	root := t.TempDir()
	for pid, comm := range map[string]string{"10": "sample\n", "11": "sample\n", "12": "other\n", "13": "a-very-long-pro\n", "self": "sample\n"} {
		if err := os.MkdirAll(filepath.Join(root, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pid, "comm"), []byte(comm), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	uid := os.Getuid()
	if got := countProcesses(root, "sample", 11, uid); got != 1 {
		t.Fatalf("other processes: %d", got)
	}
	if got := countProcesses(root, "a-very-long-program", 0, uid); got != 1 {
		t.Fatalf("truncated name: %d", got)
	}
	if got := countProcesses(root, "sample", 0, uid+1); got != 0 {
		t.Fatalf("foreign user: %d", got)
	}
	if got := countProcesses(filepath.Join(root, "missing"), "sample", 0, uid); got != -1 {
		t.Fatalf("unreadable proc: %d", got)
	}
	if got := countProcesses(root, "", 0, uid); got != -1 {
		t.Fatalf("empty name: %d", got)
	}
}
