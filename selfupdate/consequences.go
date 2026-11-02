package selfupdate

import "runtime"

// Consequences is the data an application words its question before an installation from.
type Consequences struct {
	Version    string
	Prerelease bool
	Older      bool
	// RenamesRunning reports that the running executable is moved aside to a ".old" file, which
	// CleanupLeftovers removes later, because the platform cannot replace a running program in place.
	RenamesRunning bool
	// OtherProcesses is the number of other processes of the current user that run the program, or -1 when
	// it cannot be counted.
	OtherProcesses int
}

// Consequences describes what installing t does on this platform now.
func (c *Client) Consequences(t Target) Consequences {
	return Consequences{t.Version, t.Prerelease, t.Older, runtime.GOOS == "windows", OtherProcesses(c.ProgramName)}
}
