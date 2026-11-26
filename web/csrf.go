package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

const (
	// CSRFField is the name of the form field that carries the CSRF token.
	CSRFField = "csrf_token"
	// CSRFHeader is the request header that may carry the token instead, for requests a script sends.
	CSRFHeader = "X-CSRF-Token"

	strictLoginCookie = "__Host-login-csrf"
	looseLoginCookie  = "login-csrf"
)

// CSRFToken returns the CSRF token of the session of r, for a form of a route that is not public:
// <input type="hidden" name="{{csrf field}}" value="...">. It is empty without a session. The token is
// bound to the session and changes with every sign-in. Never put it in a URL.
func CSRFToken(r *http.Request) string {
	s, _ := SessionFrom(r)
	return s.csrf
}

// csrfOf derives the token from a secret ID, so no state is kept for it and a store never holds it. The
// purpose separates the tokens of sessions from those of the sign-in page.
func csrfOf(purpose, id string) string {
	h := sha256.Sum256([]byte("csrf\x00" + purpose + "\x00" + id))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// csrfMatches compares the token of the request with want in constant time. A missing token never matches.
func csrfMatches(r *http.Request, want string) bool {
	got := r.Header.Get(CSRFHeader)
	if got == "" {
		got = r.PostFormValue(CSRFField)
	}
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func unsafeMethod(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// refuseCSRF answers a request whose token is missing or wrong with 403 and reports it to the protocol.
// Neither the token nor an ID reaches the log or the entry.
func (c *CookieSessions) refuseCSRF(w http.ResponseWriter, r *http.Request) {
	if c.audit != nil {
		c.audit.Record(LoginEvent{Kind: CSRFRefused, Time: c.now(), Remote: remoteHost(r)})
	}
	if s, _ := r.Context().Value(serverKey{}).(*Server); s != nil {
		s.refuse(w, r, http.StatusForbidden, "missing or wrong CSRF token")
		return
	}
	Error(w, http.StatusForbidden)
}

// newLoginSecret returns the random value of the cookie that binds the sign-in page to its form. There is
// no session before the sign-in, so the page's own cookie takes its place.
func newLoginSecret() string {
	var b [idBytes]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func loginCookieName(strict bool) string {
	if strict {
		return strictLoginCookie
	}
	return looseLoginCookie
}
