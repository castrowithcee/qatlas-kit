// Package cli maps errors to the exit codes and stderr diagnostics of a command-line program and checks
// the confirmation of commands that ask or change data.
//
// A Failure carries a validated code, a message and a next step for a person, optionally another one for
// an agent. WriteError prints "<program>: <code>: <message>" and the next step of the route, adds an
// optional detail as one JSON line, shows an external service's answer as a block of its own, and appends
// usage text only for syntax errors. ExitCode maps any error to success, a runtime failure or a usage
// problem.
//
// # Always enforced
//
// Every text passes the redaction hook of ErrorOptions before it is cut or escaped, and control characters
// are made visible. Nothing is written for an invalid diagnostic. An unrepresentable value and an unknown
// or repeated field are usage problems. An external service's answer is always marked untrusted and is
// redacted before it is bounded: at most 8 entries, 800 bytes per message and 200 per other field, and the
// block says "truncated" when something was cut.
//
// ConfirmRequest never reads standard input and never waits for an answer nobody can give: in agent mode
// or without a terminal, a needed question becomes a failure that names the flag to repeat the request
// with. A data-changing request needs its own confirmation in addition to the answer to an ordinary
// question.
//
// # Options
//
//   - ErrorOptions.Usage: usage text written after a syntax error. Default: none is written.
//   - ErrorOptions.Route: the route of the diagnostic, human or agent. Default: RouteHuman.
//   - ErrorOptions.Redact: replaces known secrets in a text. Default: texts are used as they are.
//   - ErrorOptions.ExternalJSON: writes an external answer as one JSON line. Default: a text block.
//   - Failure.AgentNextStep: another next step for the agent route. Default: NextStep.
//   - Failure.Detail: one JSON line after the next step. Default: none.
//   - Failure.External: an external service's answer. Default: none.
//   - Confirmation.RequiresConfirm: the request changes data and needs the Confirm flag besides Yes.
//     Default: only an ordinary question is asked.
package cli
