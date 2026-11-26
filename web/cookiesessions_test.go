package web

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

// sessionServer serves a public sign-in and sign-out and a private page that names the session.
func sessionServer(t *testing.T, network *Network) (*Server, *CookieSessions, *testClock) {
	t.Helper()
	clock := &testClock{t: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	cs := NewCookieSessions(SessionOptions{})
	cs.now = clock.now
	fail := func(w http.ResponseWriter, err error) bool {
		if err != nil {
			Error(w, http.StatusInternalServerError)
		}
		return err != nil
	}
	s := newServer(t, Options{Sessions: cs, Network: network, Routes: []Route{
		{Pattern: "POST /in", Public: true, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !fail(w, cs.Issue(w, r)) {
				w.WriteHeader(http.StatusNoContent)
			}
		})},
		{Pattern: "POST /out", Public: true, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !fail(w, cs.End(w, r)) {
				w.WriteHeader(http.StatusNoContent)
			}
		})},
		{Pattern: "GET /private", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := SessionFrom(r); !ok {
				Error(w, http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})},
	}})
	return s, cs, clock
}

func post(path string, cookies ...*http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://localhost"+path, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}

func getWith(path string, cookies ...*http.Cookie) *http.Request {
	r := get(path)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}

func setCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cs := rec.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("%d Set-Cookie headers, want 1", len(cs))
	}
	return cs[0]
}

func TestCookieAttributesByMode(t *testing.T) {
	tlsNet := &Network{Addr: "127.0.0.1:0", TLS: &tls.Config{Certificates: []tls.Certificate{{}}},
		Origins: []string{"https://tool.example.test"}}
	proxyNet := &Network{Addr: "127.0.0.1:0", Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
		Origins: []string{"https://tool.example.test"}}
	for name, tc := range map[string]struct {
		network *Network
		host    string
		strict  bool
	}{
		"loopback over HTTP": {nil, "localhost", false},
		"TLS":                {tlsNet, "tool.example.test", true},
		"declared proxy":     {proxyNet, "tool.example.test", true},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := sessionServer(t, tc.network)
			req := post("/in")
			req.Host = tc.host
			c := setCookie(t, do(s, req))
			wantName := map[bool]string{true: "__Host-session", false: "session"}[tc.strict]
			if c.Name != wantName || c.Secure != tc.strict {
				t.Errorf("cookie %q Secure=%v, want %q Secure=%v", c.Name, c.Secure, wantName, tc.strict)
			}
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" {
				t.Errorf("attributes: %+v", c)
			}
		})
	}
}

func TestModeIgnoresForgeableHeaders(t *testing.T) {
	s, _, _ := sessionServer(t, nil)
	req := post("/in")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Forwarded", "proto=https")
	req.TLS = nil
	if c := setCookie(t, do(s, req)); c.Name != "session" || c.Secure {
		t.Errorf("loopback server follows a client header: %+v", c)
	}
}

func TestStrictOutsideServer(t *testing.T) {
	cs := NewCookieSessions(SessionOptions{})
	rec := httptest.NewRecorder()
	if err := cs.Issue(rec, post("/in")); err != nil {
		t.Fatal(err)
	}
	if c := setCookie(t, rec); c.Name != "__Host-session" || !c.Secure {
		t.Errorf("without a Server the cookie is %+v", c)
	}
}

func TestLooseCookieNotAcceptedOnNetwork(t *testing.T) {
	// A server on the network must not honour the weaker cookie, even with a valid ID.
	loop, _, _ := sessionServer(t, nil)
	c := setCookie(t, do(loop, post("/in")))
	netw, _, _ := sessionServer(t, &Network{Addr: "127.0.0.1:0",
		Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, Origins: []string{"https://tool.example.test"}})
	req := getWith("/private", c)
	req.Host = "tool.example.test"
	if got := do(netw, req).Code; got != http.StatusUnauthorized {
		t.Errorf("status %d", got)
	}
}

func TestSessionIDsAreLongAndDistinct(t *testing.T) {
	s, cs, _ := sessionServer(t, nil)
	seen := map[string]bool{}
	for range 50 {
		c := setCookie(t, do(s, post("/in")))
		if len(c.Value) != idLen { // 43 base64 characters hold 256 bit
			t.Fatalf("ID length %d", len(c.Value))
		}
		if seen[c.Value] {
			t.Fatal("ID repeated")
		}
		seen[c.Value] = true
		if _, ok, _ := cs.store.Get(c.Value); ok {
			t.Fatal("store is keyed by the plain ID")
		}
		if _, ok, _ := cs.store.Get(keyOf(c.Value)); !ok {
			t.Fatal("store does not hold the hashed key")
		}
	}
}

func TestUnknownIDIsRefusedAndCreatesNothing(t *testing.T) {
	s, cs, _ := sessionServer(t, nil)
	for name, v := range map[string]string{"unknown": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "short": "abc", "empty": ""} {
		rec := do(s, getWith("/private", &http.Cookie{Name: "session", Value: v}))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
	if n := len(cs.store.(*MemorySessionStore).recs); n != 0 {
		t.Errorf("%d sessions created by refused requests", n)
	}
	if rec := do(s, get("/private")); rec.Code != http.StatusUnauthorized {
		t.Errorf("no cookie: status %d", rec.Code)
	}
}

func TestIssueEndsBroughtSession(t *testing.T) {
	s, _, _ := sessionServer(t, nil)
	old := setCookie(t, do(s, post("/in")))
	if got := do(s, getWith("/private", old)).Code; got != http.StatusNoContent {
		t.Fatalf("session: status %d", got)
	}
	fresh := setCookie(t, do(s, post("/in", old)))
	if fresh.Value == old.Value {
		t.Fatal("ID kept after sign-in")
	}
	if got := do(s, getWith("/private", old)).Code; got != http.StatusUnauthorized {
		t.Errorf("old ID still valid: status %d", got)
	}
	if got := do(s, getWith("/private", fresh)).Code; got != http.StatusNoContent {
		t.Errorf("new ID: status %d", got)
	}
}

func TestIdleAndAbsoluteLimits(t *testing.T) {
	s, cs, clock := sessionServer(t, nil)
	c := setCookie(t, do(s, post("/in")))

	// Use renews the idle period.
	for range 3 {
		clock.t = clock.t.Add(cs.idle - time.Second)
		if got := do(s, getWith("/private", c)).Code; got != http.StatusNoContent {
			t.Fatalf("active session: status %d", got)
		}
	}
	clock.t = clock.t.Add(cs.idle)
	rec := do(s, getWith("/private", c))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("idle session: status %d", rec.Code)
	}
	if cleared := rec.Result().Cookies(); len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Errorf("stale cookie not cleared: %v", cleared)
	}

	// Activity never extends past the absolute limit.
	c = setCookie(t, do(s, post("/in")))
	issued := clock.t
	for clock.t.Add(cs.idle / 2).Before(issued.Add(cs.abs)) {
		clock.t = clock.t.Add(cs.idle / 2)
		if got := do(s, getWith("/private", c)).Code; got != http.StatusNoContent {
			t.Fatalf("session before absolute limit: status %d", got)
		}
	}
	clock.t = issued.Add(cs.abs)
	if got := do(s, getWith("/private", c)).Code; got != http.StatusUnauthorized {
		t.Errorf("session at absolute limit: status %d", got)
	}
}

func TestSecureDefaultLimits(t *testing.T) {
	cs := NewCookieSessions(SessionOptions{})
	if cs.idle != 30*time.Minute || cs.abs != 8*time.Hour {
		t.Errorf("idle %v, absolute %v", cs.idle, cs.abs)
	}
}

func TestEndInvalidatesOnServer(t *testing.T) {
	s, cs, _ := sessionServer(t, nil)
	c := setCookie(t, do(s, post("/in")))
	rec := do(s, post("/out", c))
	if cleared := setCookie(t, rec); cleared.MaxAge >= 0 || cleared.Name != "session" {
		t.Errorf("cookie not deleted: %+v", cleared)
	}
	// The browser may ignore the deletion; the ID must be dead anyway.
	if got := do(s, getWith("/private", c)).Code; got != http.StatusUnauthorized {
		t.Errorf("ended session: status %d", got)
	}
	if n := len(cs.store.(*MemorySessionStore).recs); n != 0 {
		t.Errorf("%d records left", n)
	}
	if got := do(s, post("/out")).Code; got != http.StatusNoContent {
		t.Errorf("sign-out without session: status %d", got)
	}
}

func TestMemoryStoreDropsExpired(t *testing.T) {
	clock := &testClock{t: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	m := NewMemorySessionStore()
	m.now = clock.now
	_ = m.Put("a", SessionRecord{Expires: clock.t.Add(time.Second)})
	clock.t = clock.t.Add(2 * time.Minute)
	_ = m.Put("b", SessionRecord{Expires: clock.t.Add(time.Hour)})
	if _, ok, _ := m.Get("a"); ok {
		t.Error("expired record kept")
	}
	if _, ok, _ := m.Get("b"); !ok {
		t.Error("live record lost")
	}
}

// endingStore deletes the record after Get, as a sign-out on another request would.
type endingStore struct {
	*MemorySessionStore
	afterGet func(key string)
}

func (e *endingStore) Get(key string) (SessionRecord, bool, error) {
	rec, ok, err := e.MemorySessionStore.Get(key)
	if e.afterGet != nil {
		e.afterGet(key)
	}
	return rec, ok, err
}

func TestRenewalNeverRevivesEndedSession(t *testing.T) {
	st := &endingStore{MemorySessionStore: NewMemorySessionStore()}
	cs := NewCookieSessions(SessionOptions{Store: st})
	rec := httptest.NewRecorder()
	if err := cs.Issue(rec, post("/in")); err != nil {
		t.Fatal(err)
	}
	c := setCookie(t, rec)
	st.afterGet = func(key string) { _ = st.Delete(key) }
	w := httptest.NewRecorder()
	if _, ok := cs.Session(w, getWith("/private", c)); ok {
		t.Error("session ended during renewal was accepted")
	}
	st.afterGet = nil
	if n := len(st.recs); n != 0 {
		t.Errorf("%d records after renewal of an ended session", n)
	}
	if _, ok := cs.Session(httptest.NewRecorder(), getWith("/private", c)); ok {
		t.Error("ended session returned")
	}
}
