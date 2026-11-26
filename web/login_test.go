package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

type recordingAudit struct {
	mu     sync.Mutex
	events []LoginEvent
}

func (a *recordingAudit) Record(e LoginEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
}

func (a *recordingAudit) kinds() []LoginEventKind {
	var ks []LoginEventKind
	for _, e := range a.events {
		ks = append(ks, e.Kind)
	}
	return ks
}

const testAddr = "192.0.2.1"

// authFixture has a clock for the audit time and for the counters of testAddr and of all addresses.
type authFixture struct {
	auth  *Authenticator
	code  *OnceCode
	audit *recordingAudit
	clock *testClock
}

func newAuth(t *testing.T, opts AuthOptions) *authFixture {
	t.Helper()
	f := &authFixture{code: NewOnceCode(OnceCodeOptions{}), audit: &recordingAudit{},
		clock: &testClock{t: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}}
	opts.Sessions, opts.Method, opts.Audit = NewCookieSessions(SessionOptions{}), f.code, f.audit
	a, err := NewAuthenticator(opts)
	if err != nil {
		t.Fatal(err)
	}
	a.now = f.clock.now
	a.global = ratelimit.NewCounter(max(opts.Global, 20), ratelimit.Sliding(10*time.Minute), f.clock.now)
	a.perAddr.(*ratelimit.CounterRegistry).Replace(testAddr, ratelimit.NewCounter(max(opts.PerAddress, 5), ratelimit.Sliding(10*time.Minute), f.clock.now))
	f.auth = a
	return f
}

func (f *authFixture) signIn(code, addr string, cookies ...*http.Cookie) (*httptest.ResponseRecorder, error) {
	r := codeRequest(code)
	r.RemoteAddr = addr + ":4000"
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	return w, f.auth.SignIn(w, r)
}

func TestNewAuthenticatorNeedsSessionsAndMethod(t *testing.T) {
	if _, err := NewAuthenticator(AuthOptions{Method: NewOnceCode(OnceCodeOptions{})}); err == nil {
		t.Error("no Sessions accepted")
	}
	if _, err := NewAuthenticator(AuthOptions{Sessions: NewCookieSessions(SessionOptions{})}); err == nil {
		t.Error("no Method accepted")
	}
}

func TestSignInIssuesNewSession(t *testing.T) {
	f := newAuth(t, AuthOptions{})
	first, err := f.signIn(f.code.Generate(), testAddr)
	if err != nil {
		t.Fatal(err)
	}
	old := setCookie(t, first)
	rec, err := f.signIn(f.code.Generate(), testAddr, old)
	if err != nil {
		t.Fatal(err)
	}
	c := setCookie(t, rec)
	if len(c.Value) != idLen || c.Value == old.Value {
		t.Errorf("cookie value %q", c.Value)
	}
	if _, ok, _ := f.auth.sessions.store.Get(keyOf(old.Value)); ok {
		t.Error("the old session survives the sign-in")
	}
	if _, ok, _ := f.auth.sessions.store.Get(keyOf(c.Value)); !ok {
		t.Error("no session behind the cookie")
	}
}

func TestSignInWrongAndThrottledLookAlike(t *testing.T) {
	f := newAuth(t, AuthOptions{PerAddress: 2})
	f.auth.perAddr.(*ratelimit.CounterRegistry).Replace(testAddr, ratelimit.NewCounter(2, ratelimit.Sliding(10*time.Minute), f.clock.now))
	code := f.code.Generate()

	rec1, wrong := f.signIn("2222-2222", testAddr)
	rec2, _ := f.signIn("3333-3333", testAddr)
	// Now throttled: even the right code gets the same answer, and no cookie.
	rec3, throttled := f.signIn(code, testAddr)
	if !errors.Is(wrong, ErrSignInRefused) || throttled == nil || wrong.Error() != throttled.Error() || !errors.Is(throttled, ErrSignInRefused) {
		t.Fatalf("answers differ: %v / %v", wrong, throttled)
	}
	for _, rec := range []*httptest.ResponseRecorder{rec1, rec2, rec3} {
		if len(rec.Result().Cookies()) != 0 || rec.Body.Len() != 0 {
			t.Error("a refused attempt wrote a cookie or body")
		}
	}
	if got := f.audit.kinds(); fmt.Sprint(got) != "[login-failed login-failed login-throttled]" {
		t.Errorf("events %v", got)
	}

	// The window passes, the code was never consumed, the budget is back.
	f.clock.t = f.clock.t.Add(10*time.Minute + time.Second)
	if _, err := f.signIn(code, testAddr); err != nil {
		t.Errorf("after the window: %v", err)
	}
}

func TestGlobalLimitHoldsAcrossAddresses(t *testing.T) {
	f := newAuth(t, AuthOptions{})
	f.auth.global = ratelimit.NewCounter(3, ratelimit.Sliding(10*time.Minute), f.clock.now)
	code := f.code.Generate()
	for i := range 3 {
		if _, err := f.signIn("2222-2222", fmt.Sprintf("198.51.100.%d", i)); !errors.Is(err, ErrSignInRefused) {
			t.Fatal(err)
		}
	}
	if _, err := f.signIn(code, "198.51.100.99"); !errors.Is(err, ErrSignInRefused) {
		t.Errorf("a new address got past the global limit: %v", err)
	}
	if got := f.audit.kinds(); got[len(got)-1] != LoginThrottled {
		t.Errorf("events %v", got)
	}
	// A throttled attempt from one address does not use the budget of the others.
	f.clock.t = f.clock.t.Add(10*time.Minute + time.Second)
	if _, err := f.signIn(code, "198.51.100.99"); err != nil {
		t.Errorf("after the window: %v", err)
	}
}

func TestSuccessfulSignInCostsNothing(t *testing.T) {
	f := newAuth(t, AuthOptions{PerAddress: 2, Global: 2})
	f.auth.global = ratelimit.NewCounter(2, ratelimit.Sliding(10*time.Minute), f.clock.now)
	f.auth.perAddr.(*ratelimit.CounterRegistry).Replace(testAddr, ratelimit.NewCounter(2, ratelimit.Sliding(10*time.Minute), f.clock.now))
	for range 6 {
		if _, err := f.signIn(f.code.Generate(), testAddr); err != nil {
			t.Fatal(err)
		}
	}
}

type failingMethod struct{}

func (failingMethod) Verify(*http.Request) (bool, error) { return false, errors.New("method down") }

func TestMethodErrorIsNotAWrongAttempt(t *testing.T) {
	f := newAuth(t, AuthOptions{})
	f.auth.method = failingMethod{}
	_, err := f.signIn("2222-2222", testAddr)
	if err == nil || errors.Is(err, ErrSignInRefused) {
		t.Errorf("error %v", err)
	}
	if len(f.audit.events) != 0 {
		t.Errorf("events %v", f.audit.kinds())
	}
}

func TestSignOutEndsSessionAndIsRecorded(t *testing.T) {
	f := newAuth(t, AuthOptions{})
	rec, err := f.signIn(f.code.Generate(), testAddr)
	if err != nil {
		t.Fatal(err)
	}
	c := setCookie(t, rec)
	out := httptest.NewRecorder()
	if err := f.auth.SignOut(out, getWith("/out", c)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := f.auth.sessions.store.Get(keyOf(c.Value)); ok {
		t.Error("session survives sign-out")
	}
	if err := f.auth.SignOut(httptest.NewRecorder(), get("/out")); err != nil {
		t.Fatal(err)
	}
	if got := f.audit.kinds(); fmt.Sprint(got) != "[login logout]" {
		t.Errorf("events %v", got)
	}
}

func TestAuditHoldsNoSecrets(t *testing.T) {
	f := newAuth(t, AuthOptions{})
	wrong := "ZZZZ-9999"
	code := f.code.Generate()
	_, _ = f.signIn(wrong, testAddr)
	rec, err := f.signIn(code, testAddr)
	if err != nil {
		t.Fatal(err)
	}
	ck := setCookie(t, rec)
	id := ck.Value
	_ = f.auth.SignOut(httptest.NewRecorder(), getWith("/out", ck))
	dump := fmt.Sprintf("%+v %#v", f.audit.events, f.audit.events)
	for _, secret := range []string{wrong, "ZZZZ9999", code, strings.ReplaceAll(code, "-", ""), id, keyOf(id)} {
		if strings.Contains(dump, secret) {
			t.Errorf("protocol holds %q", secret)
		}
	}
	if len(f.audit.events) != 3 {
		t.Errorf("events %v", f.audit.kinds())
	}
}

func TestNilAuditIsAllowed(t *testing.T) {
	f := newAuth(t, AuthOptions{})
	f.auth.audit = nil
	if _, err := f.signIn(f.code.Generate(), testAddr); err != nil {
		t.Fatal(err)
	}
	_ = f.auth.SignOut(httptest.NewRecorder(), get("/out"))
}

type countingAddrs struct {
	addrCounters
	made map[string]bool
}

func (c *countingAddrs) For(key string) *ratelimit.Counter {
	c.made[key] = true
	return c.addrCounters.For(key)
}

func TestExhaustedGlobalLimitCreatesNoAddressCounters(t *testing.T) {
	f := newAuth(t, AuthOptions{})
	f.auth.global = ratelimit.NewCounter(2, ratelimit.Sliding(10*time.Minute), f.clock.now)
	seen := &countingAddrs{addrCounters: f.auth.perAddr, made: map[string]bool{}}
	f.auth.perAddr = seen
	for i := range 50 {
		_, _ = f.signIn("2222-2222", fmt.Sprintf("2001:db8::%x", i))
	}
	if len(seen.made) != 2 {
		t.Errorf("%d address counters for 50 addresses, want 2 (the global limit)", len(seen.made))
	}
}
