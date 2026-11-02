package selfupdate

import (
	"runtime"
	"testing"
)

func TestConsequencesDescribeTarget(t *testing.T) {
	c := &Client{ProgramName: "sample"}
	got := c.Consequences(Target{Entry: Entry{Version: "v1.0.0-rc.1", Prerelease: true, Older: true}})
	if got.Version != "v1.0.0-rc.1" || !got.Prerelease || !got.Older || got.RenamesRunning != (runtime.GOOS == "windows") {
		t.Fatalf("consequences: %+v", got)
	}
	if runtime.GOOS != "linux" && got.OtherProcesses != -1 {
		t.Fatalf("process count off Linux: %d", got.OtherProcesses)
	}
}
