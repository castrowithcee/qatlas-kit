package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// NoRedirectClient returns an HTTP client that bounds every request by timeout and never follows a
// redirect, so a credential in a request can only reach the origin the caller configured. The redirect
// answer itself is returned to the caller. rt is the round tripper to use; nil is Go's default transport.
func NoRedirectClient(timeout time.Duration, rt http.RoundTripper) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     rt,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// RetryAfter reads how long a server asks a client to wait from the Retry-After header in seconds. Zero
// means the header is absent, not a positive whole number of seconds, or otherwise unusable. The wait is
// not capped; callers that bound it do so themselves.
func RetryAfter(h http.Header) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After")))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
