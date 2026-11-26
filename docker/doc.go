// Package docker reads containers from a Docker Engine over its Unix socket: the container list, the
// details of one container, the Compose projects the containers belong to, the containers that use a path,
// one reading of a container's CPU and memory use, a container's logs as a stream of lines, and the
// actions start, stop and restart, with a wait until containers are healthy.
//
// The application names the socket in Options.
//
// On the first call a Client asks the Engine for its API version and uses the lower of that and
// MaxAPIVersion; the outcome of a success is kept in the Client, a failure is not.
//
// Failures are an *Error with an output.Code: an unreachable socket is output.CodeUnreachable, a refused
// socket output.CodePermission, an expired deadline output.CodeTimeout, a cancelled context
// output.CodeRuntime wrapping context.Canceled, an unknown container output.CodeNotFound, an oversized or
// invalid answer output.CodeRuntime, and a malformed container reference output.CodeInvalidRequest. On
// Windows New returns an *Error that wraps errors.ErrUnsupported.
//
// Logs returns a LogStream of lines with the Engine's timestamp and their source (stdout or stderr). Only
// the wait for the answer's headers is bounded by Options.Timeout; a following stream then ends with the
// context passed to Logs or with Close.
//
// Start, Stop and Restart return nil once the Engine accepted the action; starting or stopping a container
// that is already in that state is not an error. A request may run for Options.Timeout plus the grace.
// WaitHealthy polls Inspect until each container runs and, if it has a health check, is healthy, or until
// WaitOptions.Deadline. It reports one WaitResult per reference, in order; a container that failed is a
// result with a WaitReason, not an error. An exited or missing container is final at once, a failing health
// check is watched until the deadline.
//
// # Always enforced
//
// The package opens no TCP or TLS connection, uses no proxy and reads neither environment variables nor the
// contexts of other tools. The socket must be an absolute path. A request is bounded by Options.Timeout and
// a redirect is an error. A container reference must be a valid ID or name of at most 255 characters. Every
// answer except a log stream is read only up to a limit, and an oversized one is an output.CodeRuntime error. A list or
// a wait covers at most MaxContainers containers, and more is an error rather than a silent cut.
//
// A server below MinAPIVersion, or one that reports no valid version, is refused. An *Error message never
// carries an answer body, a header or a value of the container. Environment variables of a container are
// returned by name only; the values are dropped while the answer is decoded.
//
// The multiplexed framing of a log stream is checked strictly: an unknown source, a non-zero reserved byte,
// an oversized frame, a stream that ends inside a frame and a line without a valid timestamp are
// output.CodeRuntime errors, not a silent end. A line is cut at the line limit at a character boundary and
// marked Truncated, and invalid UTF-8 is replaced by U+FFFD. The delivery rate is enforced by backpressure:
// Next waits instead of dropping lines. Logs refuses negative limits, a limit above MaxLogLineBytes, a
// time before 1970 and an Until before Since. Logs adds no end of its own to a followed stream.
//
// The grace period of Stop and Restart is at most MaxGrace and is rounded up to whole seconds; a negative or
// larger one is output.CodeInvalidRequest. WaitHealthy refuses an empty or repeated reference list and a
// negative deadline or interval, raises a shorter interval to MinWaitInterval and checks the deadline
// between polls, with one last poll at the deadline.
//
// Access to the Docker socket is practically root access to the host. The application decides who may use
// a Client. Names, labels, images and paths in the answers, and log text, are text that images and users
// control; a caller masks them and their control characters before it shows them.
//
// # Options
//
//   - Options.Timeout: bounds every request. Default: DefaultTimeout, 30 seconds.
//   - LogOptions.Since: limits the stream to lines at or after a time; to follow only new lines, set it to
//     time.Now with Follow. Default: from the start.
//   - LogOptions.Until: limits the stream to lines at or before a time; with Follow the stream ends only
//     when the Engine ends it. Default: no end.
//   - LogOptions.Tail: limits the stream to the last n lines. Default: all lines.
//   - LogOptions.Follow: keeps the stream open and delivers new lines until the context ends or the stream
//     is closed. Default: the stream ends with the log.
//   - LogOptions.MaxLineBytes: bounds the text of one line. Default: DefaultLogLineBytes, 16 KiB.
//   - LogOptions.MaxLinesPerSecond: bounds the delivery rate. Default: DefaultLogLinesPerSecond, 1000.
//   - The grace argument of Stop and Restart: how long the Engine waits for the container to stop. Default:
//     zero selects the Engine's default.
//   - WaitOptions.Deadline: bounds the whole wait. Default: DefaultWaitDeadline, 60 seconds.
//   - WaitOptions.Interval: the pause between two polls. Default: DefaultWaitInterval, 1 second.
package docker
