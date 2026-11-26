package web

import (
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

// ErrSignInRefused is the only answer to a failed sign-in, whether the credentials were wrong or the
// attempt was throttled, so the answer never tells whether a code existed.
var ErrSignInRefused = errors.New("web: sign-in refused")

// LoginEventKind names what a LoginEvent reports.
type LoginEventKind string

const (
	LoginSucceeded LoginEventKind = "login"
	LoginFailed    LoginEventKind = "login-failed"
	// LoginThrottled is an attempt refused by the throttle without checking the credentials.
	LoginThrottled LoginEventKind = "login-throttled"
	LogoutDone     LoginEventKind = "logout"
	// CSRFRefused is a request refused for a missing or wrong CSRF token, on the sign-in page or in a session.
	CSRFRefused LoginEventKind = "csrf-refused"
)

// LoginEvent is one entry for the protocol. It holds no credential and no session ID.
type LoginEvent struct {
	Kind LoginEventKind
	Time time.Time
	// Remote is the host of the peer address. Behind a proxy it is the proxy.
	Remote string
}

// LoginAudit receives sign-ins, sign-outs and failed attempts, for example to write them to auditlog.
// Record must not block for long and must be safe for concurrent use.
type LoginAudit interface {
	Record(LoginEvent)
}

// AuthOptions configure an Authenticator.
type AuthOptions struct {
	// Sessions issues and ends the sessions. Required.
	Sessions *CookieSessions
	// Method checks the credentials. Required.
	Method LoginMethod
	// Audit receives the events; nil keeps no protocol.
	Audit LoginAudit
	// Window is how long a failed attempt counts; 0 means 10 minutes.
	Window time.Duration
	// PerAddress is the failed attempts one peer address may make per Window; 0 means 5.
	PerAddress int
	// Global is the failed attempts from all addresses together per Window; 0 means 20. On loopback every
	// client has the same address, and behind a proxy all clients share the proxy's, so this limit is the
	// one that holds there. It also bounds the counters kept per address: only an attempt that passes it
	// creates one.
	Global int
}

// Authenticator turns a successful LoginMethod check into a session, throttles failed attempts and reports
// to the protocol. It is the core that the sign-in and sign-out handlers call.
type Authenticator struct {
	sessions *CookieSessions
	method   LoginMethod
	audit    LoginAudit
	now      func() time.Time

	global  *ratelimit.Counter
	perAddr addrCounters
}

// addrCounters is the part of ratelimit.CounterRegistry that Authenticator uses.
type addrCounters interface {
	For(key string) *ratelimit.Counter
}

// NewAuthenticator returns an Authenticator or an error for a missing Sessions or Method.
func NewAuthenticator(opts AuthOptions) (*Authenticator, error) {
	if opts.Sessions == nil || opts.Method == nil {
		return nil, errors.New("web: an Authenticator needs Sessions and a Method")
	}
	win, perAddr, global := opts.Window, opts.PerAddress, opts.Global
	if win <= 0 {
		win = 10 * time.Minute
	}
	if perAddr <= 0 {
		perAddr = 5
	}
	if global <= 0 {
		global = 20
	}
	return &Authenticator{
		sessions: opts.Sessions, method: opts.Method, audit: opts.Audit, now: time.Now,
		global:  ratelimit.NewCounter(global, ratelimit.Sliding(win), time.Now),
		perAddr: ratelimit.NewCounterRegistry(perAddr, ratelimit.Sliding(win)),
	}, nil
}

// SignIn checks the credentials in r and, if they are valid, issues a new session and sets its cookie. It
// returns ErrSignInRefused for wrong and for throttled attempts alike; any other error is a failure of the
// method or the store. Every failed attempt, including wrong ones, uses up budget until the window passes,
// a successful one costs nothing.
func (a *Authenticator) SignIn(w http.ResponseWriter, r *http.Request) error {
	remote := remoteHost(r)
	// The global counter goes first, so addresses past its limit never get a counter of their own.
	if byAll, err := a.global.Reserve(1); err == nil {
		if byAddr, err := a.perAddr.For(remote).Reserve(1); err == nil {
			return a.check(w, r, remote, byAddr, byAll)
		}
		byAll.Release()
	}
	a.record(LoginThrottled, remote)
	return ErrSignInRefused
}

func (a *Authenticator) check(w http.ResponseWriter, r *http.Request, remote string, rs ...*ratelimit.Reservation) error {
	refund := func() {
		for _, x := range rs {
			x.Release()
		}
	}
	valid, err := a.method.Verify(r)
	if err != nil {
		refund()
		return err
	}
	if !valid {
		a.record(LoginFailed, remote)
		return ErrSignInRefused
	}
	refund()
	if err := a.sessions.Issue(w, r); err != nil {
		return err
	}
	a.record(LoginSucceeded, remote)
	return nil
}

// SignOut ends the session of r on the server, deletes its cookie and reports the sign-out. It is safe
// without a session, which is not reported.
func (a *Authenticator) SignOut(w http.ResponseWriter, r *http.Request) error {
	_, had := a.sessions.cookieID(r, strictFor(r))
	if err := a.sessions.End(w, r); err != nil {
		return err
	}
	if had {
		a.record(LogoutDone, remoteHost(r))
	}
	return nil
}

func (a *Authenticator) record(kind LoginEventKind, remote string) {
	if a.audit != nil {
		a.audit.Record(LoginEvent{Kind: kind, Time: a.now(), Remote: remote})
	}
}

func remoteHost(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}
