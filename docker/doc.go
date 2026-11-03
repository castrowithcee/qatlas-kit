// Package docker reads containers from a Docker Engine over its Unix socket: the container list, the
// details of one container, the Compose projects the containers belong to, the containers that use a path,
// and one reading of a container's CPU and memory use.
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
// Environment variables of a container are returned by name only; the values are dropped while the answer
// is decoded.
//
// Access to the Docker socket is practically root access to the host. The application decides who may use
// a Client. Names, labels, images and paths in the answers are text that images and users control; a caller
// masks them before it shows them.
package docker
