// Package docker reads containers from a Docker Engine over its Unix socket: the container list, the
// details of one container, the Compose projects the containers belong to, the containers that use a path,
// one reading of a container's CPU and memory use, a container's logs as a stream of lines, and the
// actions start, stop and restart, with a wait until containers are healthy.
//
// The application names the socket in Options. The package opens no TCP or TLS connection and reads neither
// environment variables nor the contexts of other tools. A request is bounded by Options.Timeout, and a
// redirect is an error.
//
// On the first call a Client asks the Engine for its API version and uses the lower of that and
// MaxAPIVersion. A server below MinAPIVersion, or one that reports no valid version, is refused; the
// outcome of a success is kept in the Client, a failure is not.
//
// Failures are an *Error with an output.Code: an unreachable socket is output.CodeUnreachable, a refused
// socket output.CodePermission, an expired deadline output.CodeTimeout, a cancelled context
// output.CodeRuntime wrapping context.Canceled, an unknown container output.CodeNotFound, an oversized or
// invalid answer output.CodeRuntime, and a malformed container reference output.CodeInvalidRequest. The
// message never carries an answer body, a header or a value of the container. On Windows New returns an
// *Error that wraps errors.ErrUnsupported.
//
// Logs returns a LogStream of lines with the Engine's timestamp and their source (stdout or stderr). The
// multiplexed framing is checked strictly: an unknown source, a non-zero reserved byte, an oversized frame,
// a stream that ends inside a frame and a line without a valid timestamp are output.CodeRuntime errors, not
// a silent end. A line is cut at LogOptions.MaxLineBytes at a character boundary and marked Truncated, and
// invalid UTF-8 is replaced by U+FFFD. MaxLinesPerSecond is enforced by backpressure: Next waits instead of
// dropping lines. Only the wait for the answer's headers is bounded by Options.Timeout; a following stream
// then ends with the context passed to Logs or with Close. To follow only new lines, set Follow with Since
// set to time.Now. Since and Until select a time range; Until before Since is refused. With Follow and Until
// the stream ends only when the Engine ends it, Logs adds no end of its own. Log text is untrusted; a
// caller masks control characters before it shows it.
//
// Start, Stop and Restart return nil once the Engine accepted the action; starting or stopping a container
// that is already in that state is not an error. The grace period of Stop and Restart is rounded up to whole
// seconds, zero selects the Engine's default, and a request may run for Options.Timeout plus the grace.
// WaitHealthy polls Inspect until each container runs and, if it has a health check, is healthy, or until
// WaitOptions.Deadline. It reports one WaitResult per reference, in order; a container that failed is a
// result with a WaitReason, not an error. An exited or missing container is final at once, a failing health
// check is watched until the deadline.
//
// Environment variables of a container are returned by name only; the values are dropped while the answer
// is decoded.
//
// Access to the Docker socket is practically root access to the host. The application decides who may use
// a Client. Names, labels, images and paths in the answers are text that images and users control; a caller
// masks them before it shows them.
package docker
