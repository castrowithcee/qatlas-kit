package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"time"
)

const (
	idBytes = 32 // 256 bit, above the 128 bit minimum
	idLen   = 43 // length of idBytes in unpadded base64

	strictCookie = "__Host-session"
	looseCookie  = "session"

	defaultIdle     = 30 * time.Minute
	defaultAbsolute = 8 * time.Hour
)

// SessionOptions configure CookieSessions. The zero value is a safe default.
type SessionOptions struct {
	// Store keeps the sessions; nil keeps them in memory.
	Store SessionStore
	// Idle ends a session that was not used for this long; 0 means 30 minutes.
	Idle time.Duration
	// Absolute ends a session this long after it was issued, however active; 0 means 8 hours.
	Absolute time.Duration
	// Audit receives the requests refused for a missing or wrong CSRF token; nil keeps no protocol.
	Audit LoginAudit
}

// CookieSessions implements Sessions with a random ID in a cookie and the session state on the server. It
// also refuses an unsafe request without the CSRF token of its session.
//
// The cookie is always HttpOnly, SameSite=Strict, Path=/ and without Domain. It carries the __Host- prefix
// and Secure except when the Server binds to loopback only, which it serves over plain HTTP: browsers
// differ on both there. Which form applies is decided by the Server that handles the request, never by
// a request header, and a request that reaches CookieSessions outside a Server gets the strict form.
type CookieSessions struct {
	store     SessionStore
	idle, abs time.Duration
	now       func() time.Time
	audit     LoginAudit
	loginPath string // set by NewPages
}

// NewCookieSessions returns CookieSessions for Options.Sessions. Call Issue after a successful sign-in
// and End on sign-out.
func NewCookieSessions(opts SessionOptions) *CookieSessions {
	c := &CookieSessions{store: opts.Store, idle: opts.Idle, abs: opts.Absolute, now: time.Now, audit: opts.Audit}
	if c.store == nil {
		c.store = NewMemorySessionStore()
	}
	if c.idle <= 0 {
		c.idle = defaultIdle
	}
	if c.abs <= 0 {
		c.abs = defaultAbsolute
	}
	return c
}

// Session is the session of a request as the route handler sees it. It carries no ID.
type Session struct {
	// Created is when the session was issued.
	Created time.Time
	key     string
	csrf    string
}

type sessionCtxKey struct{}

// SessionFrom returns the session that CookieSessions attached to r.
func SessionFrom(r *http.Request) (Session, bool) {
	s, ok := r.Context().Value(sessionCtxKey{}).(Session)
	return s, ok
}

// Session implements Sessions. It never creates a session: an unknown, expired or missing ID is refused
// and a stale cookie is cleared. Each use renews the idle period, up to the absolute limit.
func (c *CookieSessions) Session(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	strict := strictFor(r)
	id, ok := c.cookieID(r, strict)
	if !ok {
		return c.deny(w, r)
	}
	key := keyOf(id)
	rec, found, err := c.store.Get(key)
	if err != nil {
		Error(w, http.StatusInternalServerError)
		return nil, false
	}
	now := c.now()
	if !found || !now.Before(rec.Expires) {
		if found {
			_ = c.store.Delete(key)
		}
		c.clear(w, strict)
		return c.deny(w, r)
	}
	rec.Expires = c.deadline(rec.Created, now)
	ok, err = c.store.Touch(key, rec.Expires)
	if err != nil {
		Error(w, http.StatusInternalServerError)
		return nil, false
	}
	if !ok { // ended between Get and Touch
		c.clear(w, strict)
		return c.deny(w, r)
	}
	csrf := csrfOf("session", id)
	if unsafeMethod(r) && !csrfMatches(r, csrf) {
		c.refuseCSRF(w, r)
		return nil, false
	}
	ctx := context.WithValue(r.Context(), sessionCtxKey{}, Session{Created: rec.Created, key: key, csrf: csrf})
	return r.WithContext(ctx), true
}

// deny refuses r. A browser navigation goes to the sign-in page if there is one; the server answers 401
// otherwise and for every other method.
func (c *CookieSessions) deny(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if c.loginPath != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		_ = Redirect(w, r, c.loginPath, http.StatusSeeOther)
	}
	return nil, false
}

// Issue starts a new session with a new ID and sets its cookie. Call it after a successful sign-in. A
// session the request brought along ends, so an ID known before the sign-in is worthless afterwards.
func (c *CookieSessions) Issue(w http.ResponseWriter, r *http.Request) error {
	strict := strictFor(r)
	if old, ok := c.cookieID(r, strict); ok {
		if err := c.store.Delete(keyOf(old)); err != nil {
			return err
		}
	}
	var b [idBytes]byte
	_, _ = rand.Read(b[:]) // never fails since Go 1.24; it crashes the program instead
	id := base64.RawURLEncoding.EncodeToString(b[:])
	now := c.now()
	if err := c.store.Put(keyOf(id), SessionRecord{Created: now, Expires: c.deadline(now, now)}); err != nil {
		return err
	}
	http.SetCookie(w, cookie(id, strict))
	return nil
}

// End ends the session of the request on the server and deletes its cookie. It is safe without a session.
func (c *CookieSessions) End(w http.ResponseWriter, r *http.Request) error {
	strict := strictFor(r)
	c.clear(w, strict)
	if id, ok := c.cookieID(r, strict); ok {
		return c.store.Delete(keyOf(id))
	}
	return nil
}

func (c *CookieSessions) deadline(created, now time.Time) time.Time {
	return minTime(now.Add(c.idle), created.Add(c.abs))
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func (c *CookieSessions) cookieID(r *http.Request, strict bool) (string, bool) {
	ck, err := r.Cookie(cookieName(strict))
	// The exact length rejects junk before it reaches the store.
	if err != nil || len(ck.Value) != idLen {
		return "", false
	}
	return ck.Value, true
}

func (c *CookieSessions) clear(w http.ResponseWriter, strict bool) {
	ck := cookie("", strict)
	ck.MaxAge = -1
	http.SetCookie(w, ck)
}

func keyOf(id string) string {
	h := sha256.Sum256([]byte(id))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// strictFor is false only for a Server that binds to loopback alone, which serves plain HTTP. Anything
// else, including no Server in the context, is strict.
func strictFor(r *http.Request) bool {
	s, _ := r.Context().Value(serverKey{}).(*Server)
	return s == nil || s.network
}

func cookieName(strict bool) string {
	if strict {
		return strictCookie
	}
	return looseCookie
}

func cookie(value string, strict bool) *http.Cookie {
	return namedCookie(cookieName(strict), value, strict)
}

func namedCookie(name, value string, strict bool) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: strict,
	}
}
