package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type sessionKey struct{}

type fakeSessions struct {
	calls int
	fn    func(w http.ResponseWriter, r *http.Request) (*http.Request, bool)
}

func (f *fakeSessions) Session(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	f.calls++
	return f.fn(w, r)
}

func gated(handler http.Handler) []Route {
	return []Route{
		{Pattern: "GET /private", Handler: handler},
		{Pattern: "POST /private", Handler: handler},
		{Pattern: "GET /public", Handler: handler, Public: true},
	}
}

func get(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
}

func TestRoutesAreDeniedByDefault(t *testing.T) {
	s := newServer(t, Options{Routes: gated(noContent)})
	if got := do(s, get("/private")).Code; got != http.StatusForbidden {
		t.Errorf("private route without Sessions: status %d", got)
	}
	if got := do(s, get("/public")).Code; got != http.StatusNoContent {
		t.Errorf("public route: status %d", got)
	}
}

func TestLoginOffServesOnLoopback(t *testing.T) {
	s := newServer(t, Options{Login: LoginOff, Routes: gated(noContent)})
	if got := do(s, get("/private")).Code; got != http.StatusNoContent {
		t.Fatalf("status %d", got)
	}
}

func TestSessionsDecide(t *testing.T) {
	var value any
	ran := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran, value = true, r.Context().Value(sessionKey{})
		w.WriteHeader(http.StatusNoContent)
	})
	cases := []struct {
		name     string
		fn       func(w http.ResponseWriter, r *http.Request) (*http.Request, bool)
		want     int
		location string
		ran      bool
		value    any
	}{
		{"refused silently", func(w http.ResponseWriter, r *http.Request) (*http.Request, bool) { return nil, false },
			http.StatusUnauthorized, "", false, nil},
		{"refused with redirect", func(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
			http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
			return nil, false
		}, http.StatusSeeOther, "/sign-in", false, nil},
		{"accepted with session", func(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
			return r.WithContext(context.WithValue(r.Context(), sessionKey{}, "session-example")), true
		}, http.StatusNoContent, "", true, "session-example"},
		{"accepted keeping the request", func(w http.ResponseWriter, r *http.Request) (*http.Request, bool) { return nil, true },
			http.StatusNoContent, "", true, nil},
	}
	for _, c := range cases {
		ran, value = false, nil
		sessions := &fakeSessions{fn: c.fn}
		s := newServer(t, Options{Sessions: sessions, Routes: gated(handler)})
		rec := do(s, get("/private"))
		if rec.Code != c.want || rec.Header().Get("Location") != c.location || ran != c.ran || value != c.value {
			t.Errorf("%s: status %d location %q ran %v value %v", c.name, rec.Code, rec.Header().Get("Location"), ran, value)
		}

		do(s, get("/public"))
		cross := httptest.NewRequest(http.MethodPost, "http://localhost/private", nil)
		cross.Header.Set("Sec-Fetch-Site", "cross-site")
		do(s, cross)
		if sessions.calls != 1 {
			t.Errorf("%s: Sessions consulted %d times, want only for the private route", c.name, sessions.calls)
		}
	}
}
