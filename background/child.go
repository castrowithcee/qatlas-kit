package background

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// Child is the side of a started process: the handover it was given and the report pipe to the starter.
type Child struct {
	handover, report *os.File
	mu               sync.Mutex
	reported         bool
}

// Inherit returns the handover and report the process inherited from Start. It reports false if the
// process was not started by Start, for instance when a person ran the subcommand by hand; it then
// touches nothing.
func Inherit() (*Child, bool) {
	handover, report, ok := inherited()
	if !ok {
		return nil, false
	}
	return &Child{handover: handover, report: report}, true
}

// ReadHandover reads the whole handover and closes the pipe. It fails if the handover is longer than
// limit bytes. The caller owns the returned bytes and should clear them after use; errors never quote them.
func (c *Child) ReadHandover(limit int) ([]byte, error) {
	if limit <= 0 {
		_ = c.handover.Close()
		return nil, errors.New("no handover size limit given")
	}
	data, err := io.ReadAll(io.LimitReader(c.handover, int64(limit)+1))
	_ = c.handover.Close()
	if err != nil {
		clear(data)
		return nil, errors.New("cannot read the handover")
	}
	if len(data) > limit {
		clear(data)
		return nil, fmt.Errorf("the handover is larger than %d bytes", limit)
	}
	return data, nil
}

// Ready reports ReportReady to the starter. Only the first report of a Child counts.
func (c *Child) Ready() { c.send(ReportReady) }

// Running reports ReportRunning: another instance already runs, so this process is about to end.
func (c *Child) Running() { c.send(ReportRunning) }

// Fail reports why the process did not start. The reason must not contain secrets; it is shortened to one
// line.
func (c *Child) Fail(reason string) { c.send(cleanReason(reason)) }

// Close reports a failure if nothing was reported yet and releases both pipes. Defer it right after Inherit
// so the starter never waits for a process that ended silently.
func (c *Child) Close() {
	c.send("the process stopped before it was ready")
	_ = c.handover.Close()
}

func (c *Child) send(word string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reported {
		return
	}
	c.reported = true
	_, _ = fmt.Fprintln(c.report, word)
	_ = c.report.Close()
}
