package httpx

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

// Class is the stable outcome of a failed request. Callers branch on it instead of on message text and map
// it to their own public error codes.
type Class string

// The classes a failed answer maps to. ClassNotFound is a resource the remote side does not hold or does
// not show to this credential: APIs that hide what a credential may not see answer both the same way, so
// the class never claims which of the two it is.
const (
	ClassAuth            Class = "auth"
	ClassPermission      Class = "permission"
	ClassNotFound        Class = "not-found"
	ClassRateLimited     Class = "rate-limited"
	ClassUnreachable     Class = "unreachable"
	ClassTimeout         Class = "timeout"
	ClassInvalidResponse Class = "invalid-response"
	ClassRemoteError     Class = "remote-error"
)

// Error is a failed request normalized to a class. Its message never carries the answer body, a header or
// a credential.
//
// Original holds what the remote side itself answered. It is never part of Error(): a caller publishes it
// only redacted, bounded and marked as untrusted.
type Error struct {
	Class    Class
	Op       string
	Message  string
	Original *Original
}

func (e *Error) Error() string { return e.Op + ": " + e.Message }

// StatusTexts are the API-specific parts of the status messages. Subject names the remote side in the
// messages for 429, 503, 504, a redirect, and any other status. Auth, Permission, and NotFound are the
// complete messages for 401, 403, and 404, because their wording differs from API to API.
type StatusTexts struct {
	Subject    string
	Auth       string
	Permission string
	NotFound   string
}

// ClassifyStatus maps the HTTP status of a failed request to a class and message. The answer body is never
// read into the message. It covers 401, 403, 404, 429, 503, 504, every 3xx, and any other status. Holding a
// rate limiter for a 429 is up to the caller, which has the response headers; see RetryAfter.
func ClassifyStatus(op string, status int, texts StatusTexts) *Error {
	subject := texts.Subject
	switch {
	case status == http.StatusUnauthorized:
		return &Error{Class: ClassAuth, Op: op, Message: texts.Auth}
	case status == http.StatusForbidden:
		return &Error{Class: ClassPermission, Op: op, Message: texts.Permission}
	case status == http.StatusNotFound:
		return &Error{Class: ClassNotFound, Op: op, Message: texts.NotFound}
	case status == http.StatusTooManyRequests:
		return &Error{Class: ClassRateLimited, Op: op, Message: subject + " rate-limited the operation"}
	case status == http.StatusServiceUnavailable:
		return &Error{Class: ClassUnreachable, Op: op, Message: subject + " is unavailable or in maintenance"}
	case status == http.StatusGatewayTimeout:
		return &Error{Class: ClassTimeout, Op: op, Message: subject + " did not answer in time"}
	case status >= 300 && status < 400:
		return &Error{Class: ClassRemoteError, Op: op,
			Message: subject + " answered with a redirect, which is not followed for this request"}
	}
	return &Error{Class: ClassRemoteError, Op: op,
		Message: subject + " rejected the operation (HTTP " + strconv.Itoa(status) + ")"}
}

// ReadJSON reads an answer body of at most limit bytes and decodes it into out. A body that cannot be read
// or exceeds the limit, and a body that is not valid JSON for out, yield an *Error of ClassInvalidResponse
// whose message names subject. It returns nil on success.
func ReadJSON(op, subject string, r io.Reader, limit int64, out any) error {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(data)) > limit {
		return &Error{Class: ClassInvalidResponse, Op: op,
			Message: "the " + subject + " response could not be read within the size limit"}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &Error{Class: ClassInvalidResponse, Op: op, Message: subject + " returned an invalid response"}
	}
	return nil
}
