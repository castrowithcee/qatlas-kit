// Package cli maps errors to the exit codes and stderr diagnostics of a command-line program and checks
// the confirmation of commands that ask or change data.
//
// A Failure carries a validated code, a message and a next step. WriteError prints it with control
// characters made visible, adds an optional detail as one JSON line, and appends usage text only for
// syntax errors. ExitCode maps any error to success, a runtime failure or a usage problem.
//
// ConfirmRequest never reads standard input and never waits for an answer nobody can give: in agent mode
// or without a terminal, a needed question becomes a failure that names the flag to repeat the request
// with. A data-changing request needs its own confirmation in addition to the answer to an ordinary
// question.
package cli
