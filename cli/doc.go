// Package cli maps errors to the exit codes and stderr diagnostics of a command-line program and checks
// the confirmation of commands that ask or change data.
//
// A Failure carries a validated code, a message and a next step for a person, optionally another one for
// an agent. WriteError prints "<program>: <code>: <message>" and the next step of the route, adds an
// optional detail as one JSON line, shows an external service's answer as a block of its own that is
// marked untrusted, bounded and redacted, and appends usage text only for syntax errors. Every text passes
// the application's redaction hook before it is cut or escaped, and control characters are made visible.
// Nothing is written for an invalid diagnostic. ExitCode maps any error to success, a runtime failure or a
// usage problem; an unrepresentable value and an unknown or repeated field are usage problems.
//
// ConfirmRequest never reads standard input and never waits for an answer nobody can give: in agent mode
// or without a terminal, a needed question becomes a failure that names the flag to repeat the request
// with. A data-changing request needs its own confirmation in addition to the answer to an ordinary
// question.
package cli
