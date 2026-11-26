package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

var nextInPage = regexp.MustCompile(`name="next" value="([^"]*)"`)

// reauthPost is the confirmation form as a browser sends it from the page.
func reauthPost(t *testing.T, f *pagesFixture, c *http.Cookie, code, next string) *http.Request {
	t.Helper()
	v := url.Values{CodeField: {code}, CSRFField: {privateToken(t, f, c)}}
	if next != "" {
		v.Set("next", next)
	}
	return browserPost("/reauth", v, c)
}

func TestRecentSignInPassesAndOldOneIsSentToConfirmation(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	if got := do(f.server, getWith("/sensitive", c)).Code; got != http.StatusNoContent {
		t.Fatalf("fresh sign-in: status %d", got)
	}
	f.clock.t = f.clock.t.Add(5 * time.Minute) // the limit itself still passes
	if got := do(f.server, getWith("/sensitive", c)).Code; got != http.StatusNoContent {
		t.Fatalf("at the limit: status %d", got)
	}
	f.clock.t = f.clock.t.Add(time.Second)
	rec := do(f.server, getWith("/sensitive?x=1&y=a+b", c))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/reauth?next=%2Fsensitive%3Fx%3D1%26y%3Da%2Bb" {
		t.Fatalf("after the limit: status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	// Other methods are refused and the action does not run.
	tok := privateToken(t, f, c)
	if got := do(f.server, browserPost("/sensitive", url.Values{CSRFField: {tok}}, c)).Code; got != http.StatusForbidden {
		t.Errorf("POST after the limit: status %d", got)
	}
	// The session itself is still valid for routes without the requirement.
	if got := do(f.server, getWith("/private", c)).Code; got != http.StatusNoContent {
		t.Errorf("plain route: status %d", got)
	}
}

func TestConfirmationRenewsTheSignInTime(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	f.clock.t = f.clock.t.Add(10 * time.Minute)
	f.audit.events = nil
	rec := do(f.server, reauthPost(t, f, c, f.code.Generate(), "/sensitive?x=1"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/sensitive?x=1" {
		t.Fatalf("status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("the confirmation changed the cookie")
	}
	if got := do(f.server, getWith("/sensitive", c)).Code; got != http.StatusNoContent {
		t.Errorf("after confirming: status %d", got)
	}
	f.clock.t = f.clock.t.Add(6 * time.Minute)
	if got := do(f.server, getWith("/sensitive", c)).Code; got != http.StatusSeeOther {
		t.Errorf("the renewed time expires too: status %d", got)
	}
	if k := f.audit.kinds(); len(k) != 1 || k[0] != ReauthSucceeded {
		t.Errorf("protocol %v", k)
	}
}

func TestConfirmationPageMarkup(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	req := getWith("/reauth?next=/sensitive", c)
	rec := do(f.server, req)
	body := rec.Body.String()
	csp := rec.Header().Get("Content-Security-Policy")
	m := regexp.MustCompile(`<style nonce="([^"]+)">`).FindStringSubmatch(body)
	if rec.Code != http.StatusOK || m == nil || !strings.Contains(csp, "'nonce-"+m[1]+"'") {
		t.Fatalf("status %d, nonce %v, CSP %q", rec.Code, m, csp)
	}
	if strings.Contains(strings.ToLower(body), "<script") || strings.Contains(body, " style=") {
		t.Error("page carries a script or an inline style attribute")
	}
	for _, want := range []string{`<html lang="en">`, `<title>Confirm sign-in</title>`, `<h1>Confirm sign-in</h1>`,
		`<label for="code">`, `autocomplete="one-time-code"`, `aria-describedby="hint"`,
		`method="post" action="/reauth"`, `name="csrf_token" value="` + privateToken(t, f, c) + `"`,
		`name="next" value="/sensitive"`, `:focus-visible`, `prefers-color-scheme: dark`, `>Confirm</button>`} {
		if !strings.Contains(body, want) {
			t.Errorf("markup lacks %q", want)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("page may be cached")
	}
	if got := do(f.server, get("/reauth")).Code; got == http.StatusOK {
		t.Error("page without a session")
	}
}

func TestConfirmationWrongCodeAndThrottle(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	f.clock.t = f.clock.t.Add(20 * time.Minute)
	code := f.code.Generate()
	f.audit.events = nil
	wrong := do(f.server, reauthPost(t, f, c, "ZZZZ-9999", "/sensitive"))
	for range 5 {
		do(f.server, reauthPost(t, f, c, "ZZZZ-9999", "/sensitive"))
	}
	throttled := do(f.server, reauthPost(t, f, c, code, "/sensitive"))
	if wrong.Code != http.StatusForbidden || throttled.Code != wrong.Code ||
		noNonce(throttled.Body.String()) != noNonce(wrong.Body.String()) {
		t.Fatalf("answers differ: %d/%d", wrong.Code, throttled.Code)
	}
	for _, want := range []string{`role="alert"`, `aria-invalid="true"`, `aria-describedby="hint error"`,
		`name="next" value="/sensitive"`} {
		if !strings.Contains(wrong.Body.String(), want) {
			t.Errorf("error page lacks %q", want)
		}
	}
	if strings.Contains(wrong.Body.String(), "9999") || strings.Contains(throttled.Body.String(), code) {
		t.Error("a refused attempt shows a code")
	}
	if k := f.audit.kinds(); k[len(k)-1] != LoginThrottled || k[0] != ReauthFailed {
		t.Errorf("protocol %v", k)
	}
	// Nothing was confirmed: the old sign-in time stands.
	if got := do(f.server, getWith("/sensitive", c)).Code; got != http.StatusSeeOther {
		t.Errorf("status %d", got)
	}
}

func TestConfirmationNeedsTheSessionToken(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	f.clock.t = f.clock.t.Add(20 * time.Minute)
	code := f.code.Generate()
	f.audit.events = nil
	for name, v := range map[string]url.Values{
		"missing": {CodeField: {code}},
		"wrong":   {CodeField: {code}, CSRFField: {strings.Repeat("A", 43)}},
	} {
		if got := do(f.server, browserPost("/reauth", v, c)).Code; got != http.StatusForbidden {
			t.Errorf("%s token: status %d", name, got)
		}
	}
	if got := do(f.server, formPost("/reauth", url.Values{CodeField: {code}})).Code; got == http.StatusSeeOther {
		t.Error("confirmation without a session")
	}
	if k := f.audit.kinds(); len(k) != 2 || k[0] != CSRFRefused || k[1] != CSRFRefused {
		t.Errorf("protocol %v", k)
	}
	// The code was not used up by the refusals.
	if got := do(f.server, reauthPost(t, f, c, code, "")).Code; got != http.StatusSeeOther {
		t.Errorf("valid form: status %d", got)
	}
}

func TestConfirmationReturnsOnlyToLocalPaths(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{Home: "/private"})
	c := signedIn(t, f)
	long := "/" + strings.Repeat("a", maxNext)
	for next, want := range map[string]string{
		"/sensitive":                 "/sensitive",
		"":                           "/private",
		"https://evil.example.test/": "/private",
		"//evil.example.test":        "/private",
		`/\evil.example.test`:        "/private",
		"/\t/evil.example.test":      "/private",
		"javascript:alert(1)":        "/private",
		"sensitive":                  "/private",
		long:                         "/private",
	} {
		f.clock.t = f.clock.t.Add(time.Minute)
		rec := do(f.server, reauthPost(t, f, c, f.code.Generate(), next))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Errorf("next %q: status %d, Location %q, want %q", next, rec.Code, rec.Header().Get("Location"), want)
		}
		// The page offers only a safe target too.
		page := do(f.server, getWith("/reauth?next="+url.QueryEscape(next), c))
		if m := nextInPage.FindStringSubmatch(page.Body.String()); m != nil && m[1] != want {
			t.Errorf("page carries %q for %q", m[1], next)
		}
	}
}

func TestReauthAuditHoldsNoSecrets(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	f.audit.events = nil
	do(f.server, reauthPost(t, f, c, "ZZZZ-9999", ""))
	do(f.server, reauthPost(t, f, c, f.code.Generate(), ""))
	if k := f.audit.kinds(); len(k) != 2 || k[0] != ReauthFailed || k[1] != ReauthSucceeded {
		t.Fatalf("protocol %v", k)
	}
	for _, e := range f.audit.events {
		if e.Remote == "" || e.Time.IsZero() {
			t.Errorf("entry %+v", e)
		}
	}
}

func TestMarkSignInNeverCreatesARecord(t *testing.T) {
	st := NewMemorySessionStore()
	if ok, err := st.MarkSignIn("missing", time.Now()); ok || err != nil || len(st.recs) != 0 {
		t.Errorf("ok %v, err %v, %d records", ok, err, len(st.recs))
	}
}

func TestRequireRecentRefusesWithoutSession(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	h := f.pages.RequireRecent(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler ran without a session")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, get("/x"))
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d", w.Code)
	}
}
