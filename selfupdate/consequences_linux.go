package selfupdate

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// OtherProcesses counts the processes of the current user besides this one whose command name is name,
// read through /proc. A failure is -1, because the count only informs a question.
func OtherProcesses(name string) int {
	return countProcesses("/proc", name, os.Getpid(), os.Getuid())
}

func countProcesses(root, name string, self, uid int) int {
	if name == "" {
		return -1
	}
	// The kernel keeps at most 15 bytes of a command name.
	if len(name) > 15 {
		name = name[:15]
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return -1
	}
	count := 0
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != uid {
			continue
		}
		comm, err := os.ReadFile(root + "/" + entry.Name() + "/comm")
		if err == nil && strings.TrimSuffix(string(comm), "\n") == name {
			count++
		}
	}
	return count
}
