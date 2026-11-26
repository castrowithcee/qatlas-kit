// Package fileretry repeats a file operation that fails only because another process holds the file for a
// moment.
//
// # Always enforced
//
// Do repeats an operation only after an error that Options.Transient reports and only for the delays in
// force, DefaultDelays unless Options.Delays is set; any other error is returned at once.
//
// # Options
//
//   - Options.Transient: reports the errors that waiting cures. Default: no failure is retried.
//   - Options.Delays: the waits between the attempts. Default: DefaultDelays, 2.775 s in all.
//   - Options.Sleep: how a wait passes, for tests. Default: time.Sleep.
package fileretry
