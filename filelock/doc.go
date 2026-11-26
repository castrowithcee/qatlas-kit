// Package filelock provides an exclusive, blocking, whole-file lock that works the same way across
// processes on Linux, macOS and Windows. Callers lock a dedicated, stable lock file, never a file that is
// replaced by rename, because the lock belongs to the open file and not to the path.
//
// # Always enforced
//
// Lock is exclusive and blocks until it is held; it has no timeout and no shared mode. It is advisory on
// Linux and macOS, so it protects only against processes that take the lock too. It is scoped to the open
// file, so several goroutines of one process that lock separately opened files serialize like separate
// processes. Unlock releases it, and so does the end of the process. The package sets no file mode, creates
// no file and checks nothing about the file it is given.
//
// # Options
//
// The package has no options.
package filelock
