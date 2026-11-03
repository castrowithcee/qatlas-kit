package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/httpx"
	"github.com/castrowithcee/qatlas-kit/output"
)

// The range of Engine API versions this package speaks. The version used is the lower of the server's
// and MaxAPIVersion; a server below MinAPIVersion is refused.
const (
	MinAPIVersion = "1.41"
	MaxAPIVersion = "1.52"
)

// DefaultTimeout bounds a request when Options.Timeout is zero.
const DefaultTimeout = 30 * time.Second

const (
	subject = "container engine"
	// host is the placeholder authority of every request; the connection always goes to the socket.
	host = "docker"
	// maxRefLength bounds a container ID or name.
	maxRefLength = 255
)

// ErrUnsupportedAPIVersion is the cause of the Error returned when the server speaks a version below
// MinAPIVersion.
var ErrUnsupportedAPIVersion = errors.New("docker: engine API version is not supported")

var refPattern = regexp.MustCompile(`^/?[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

var versionPattern = regexp.MustCompile(`^([0-9]{1,3})\.([0-9]{1,3})$`)

// Options configures a Client.
type Options struct {
	// Socket is the absolute path of the Engine's Unix socket. It is required; nothing is read from the
	// environment or from other tools' configuration.
	Socket string
	// Timeout bounds every request. Zero means DefaultTimeout.
	Timeout time.Duration
}

// Error is a failed operation. Message never carries an answer body, a header or a value of the
// container; Unwrap yields only a cause that carries none either, such as a context error.
type Error struct {
	Code    output.Code
	Message string
	cause   error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the cause, or nil.
func (e *Error) Unwrap() error { return e.cause }

// Client talks to one Engine over its Unix socket. It is safe for concurrent use.
type Client struct {
	http *http.Client
	// stream has no overall timeout, for log streams that stay open; the transport bounds the wait for headers.
	stream    *http.Client
	transport *http.Transport
	// actions has no limit on the wait for headers: a stop answers only after the container's grace period.
	actions         *http.Client
	actionTransport *http.Transport
	timeout         time.Duration
	minWaitInterval time.Duration

	// gate is a semaphore of one, so a caller waiting for another caller's ping can still be cancelled.
	gate       chan struct{}
	negotiated string
}

// New returns a Client for the socket in opts. It does not connect.
func New(opts Options) (*Client, error) {
	if !supported {
		return nil, &Error{Code: output.CodeRuntime,
			Message: "container engine access is not supported on this platform", cause: errors.ErrUnsupported}
	}
	if !filepath.IsAbs(opts.Socket) || strings.ContainsRune(opts.Socket, 0) {
		return nil, &Error{Code: output.CodeConfigInvalid, Message: "the container engine socket must be an absolute path"}
	}
	if opts.Timeout < 0 {
		return nil, &Error{Code: output.CodeConfigInvalid, Message: "the container engine timeout must not be negative"}
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	socket := opts.Socket
	dialer := &net.Dialer{Timeout: timeout}
	newTransport := func(headerTimeout time.Duration) *http.Transport {
		return &http.Transport{
			// No Proxy: the connection goes to the socket and never to an address from the environment.
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "unix", socket)
			},
			DisableCompression:    true,
			ResponseHeaderTimeout: headerTimeout,
		}
	}
	transport := newTransport(timeout)
	actionTransport := newTransport(0)
	return &Client{http: httpx.NoRedirectClient(timeout, transport), stream: httpx.NoRedirectClient(0, transport), transport: transport,
		actions: httpx.NoRedirectClient(0, actionTransport), actionTransport: actionTransport,
		timeout: timeout, minWaitInterval: MinWaitInterval, gate: make(chan struct{}, 1)}, nil
}

// Close releases idle connections. The Client stays usable.
func (c *Client) Close() {
	c.transport.CloseIdleConnections()
	c.actionTransport.CloseIdleConnections()
}

// checkRef validates a container ID or name and returns its escaped path segment.
func checkRef(ref string) (string, error) {
	if len(ref) > maxRefLength || !refPattern.MatchString(ref) {
		return "", &Error{Code: output.CodeInvalidRequest, Message: "the container reference is not a valid ID or name"}
	}
	return url.PathEscape(strings.TrimPrefix(ref, "/")), nil
}

func parseVersion(s string) (major, minor int, ok bool) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}

func versionLess(aMajor, aMinor, bMajor, bMinor int) bool {
	return aMajor < bMajor || aMajor == bMajor && aMinor < bMinor
}

// apiVersion negotiates once per Client; a failure is not cached.
func (c *Client) apiVersion(ctx context.Context) (string, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return "", contextError(ctx.Err())
	}
	if c.negotiated != "" {
		return c.negotiated, nil
	}
	resp, err := c.send(ctx, "http://"+host+"/_ping")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK {
		return "", statusError(resp.StatusCode)
	}
	sMajor, sMinor, ok := parseVersion(resp.Header.Get("Api-Version"))
	if !ok {
		return "", &Error{Code: output.CodeRuntime, Message: "the container engine did not report a valid API version"}
	}
	minMajor, minMinor, _ := parseVersion(MinAPIVersion)
	maxMajor, maxMinor, _ := parseVersion(MaxAPIVersion)
	if versionLess(sMajor, sMinor, minMajor, minMinor) {
		return "", &Error{Code: output.CodeRuntime,
			Message: "the container engine API is older than the supported " + MinAPIVersion,
			cause:   ErrUnsupportedAPIVersion}
	}
	if versionLess(maxMajor, maxMinor, sMajor, sMinor) {
		sMajor, sMinor = maxMajor, maxMinor
	}
	c.negotiated = fmt.Sprintf("%d.%d", sMajor, sMinor)
	return c.negotiated, nil
}

func (c *Client) send(ctx context.Context, target string) (*http.Response, error) {
	return c.request(ctx, c.http, http.MethodGet, target)
}

func (c *Client) request(ctx context.Context, hc *http.Client, method, target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, &Error{Code: output.CodeInvalidRequest, Message: "the request could not be built"}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	return resp, nil
}

// getJSON requests path (already escaped, below the API version) and decodes at most limit bytes of the
// answer into out.
func (c *Client) getJSON(ctx context.Context, op, path string, query url.Values, limit int64, out any) error {
	version, err := c.apiVersion(ctx)
	if err != nil {
		return err
	}
	target := "http://" + host + "/v" + version + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	resp, err := c.send(ctx, target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp.StatusCode)
	}
	if err := httpx.ReadJSON(op, subject, resp.Body, limit, out); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return contextError(cerr)
		}
		var he *httpx.Error
		if errors.As(err, &he) {
			return &Error{Code: output.CodeRuntime, Message: he.Message}
		}
		return &Error{Code: output.CodeRuntime, Message: "the " + subject + " returned an invalid response"}
	}
	return nil
}

func contextError(err error) *Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: output.CodeTimeout, Message: "the " + subject + " did not answer in time", cause: context.DeadlineExceeded}
	}
	return &Error{Code: output.CodeRuntime, Message: "the request was cancelled", cause: context.Canceled}
}

// transportError classifies a failure below HTTP. Its text is never used: it names the socket path.
func transportError(ctx context.Context, err error) *Error {
	if cerr := ctx.Err(); cerr != nil {
		return contextError(cerr)
	}
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return contextError(context.Canceled)
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout():
		return contextError(context.DeadlineExceeded)
	case errors.Is(err, fs.ErrPermission):
		return &Error{Code: output.CodePermission, Message: "access to the container engine socket was denied", cause: fs.ErrPermission}
	}
	return &Error{Code: output.CodeUnreachable, Message: "the " + subject + " could not be reached"}
}

func statusError(status int) *Error {
	failure := httpx.ClassifyStatus("docker", status, httpx.StatusTexts{
		Subject:    "the " + subject,
		Auth:       "the " + subject + " refused the request",
		Permission: "the " + subject + " refused the request",
		NotFound:   "the container was not found",
	})
	return &Error{Code: codeOf(failure.Class), Message: failure.Message}
}

func codeOf(class httpx.Class) output.Code {
	switch class {
	case httpx.ClassAuth:
		return output.CodeAuth
	case httpx.ClassPermission:
		return output.CodePermission
	case httpx.ClassNotFound:
		return output.CodeNotFound
	case httpx.ClassRateLimited:
		return output.CodeRateLimited
	case httpx.ClassUnreachable:
		return output.CodeUnreachable
	case httpx.ClassTimeout:
		return output.CodeTimeout
	}
	return output.CodeRuntime
}
