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

type pagesFixture struct {
	server *Server
	pages  *Pages
	code   *OnceCode
	audit  *recordingAudit
	auth   *Authenticator
	clock  *testClock
}

func newPagesFixture(t *testing.T, opts PagesOptions) *pagesFixture {
	t.Helper()
	f := &pagesFixture{code: NewOnceCode(OnceCodeOptions{}), audit: &recordingAudit{}}
	cs := NewCookieSessions(SessionOptions{Audit: f.audit})
	f.clock = &testClock{t: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	cs.now = f.clock.now
	var err error
	f.auth, err = NewAuthenticator(AuthOptions{Sessions: cs, Method: f.code, Audit: f.audit})
	if err != nil {
		t.Fatal(err)
	}
	opts.Auth = f.auth
	if f.pages, err = NewPages(opts); err != nil {
		t.Fatal(err)
	}
	private := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	routes := append(f.pages.Routes(),
		Route{Pattern: "GET /private", Handler: private}, Route{Pattern: "POST /private", Handler: private},
		Route{Pattern: "GET /sensitive", Handler: f.pages.RequireRecent(private)},
		Route{Pattern: "POST /sensitive", Handler: f.pages.RequireRecent(private)},
		Route{Pattern: "GET /token", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(CSRFToken(r)))
		})})
	f.server = newServer(t, Options{Sessions: cs, Routes: routes})
	return f
}

func formPost(path string, v url.Values, cookies ...*http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://localhost"+path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}

var tokenInPage = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// loginForm fetches the sign-in page like a browser and returns the form with its token and cookie.
func loginForm(t *testing.T, f *pagesFixture, code string) *http.Request {
	t.Helper()
	page := do(f.server, get("/login"))
	m := tokenInPage.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatal("sign-in page carries no token")
	}
	return formPost("/login", url.Values{CodeField: {code}, CSRFField: {m[1]}}, setCookie(t, page))
}

func TestLoginPageMarkup(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	rec := do(f.server, get("/login"))
	body := rec.Body.String()
	csp := rec.Header().Get("Content-Security-Policy")
	m := regexp.MustCompile(`<style nonce="([^"]+)">`).FindStringSubmatch(body)
	if rec.Code != http.StatusOK || m == nil || !strings.Contains(csp, "style-src 'self' 'nonce-"+m[1]+"'") {
		t.Fatalf("status %d, nonce %v, CSP %q", rec.Code, m, csp)
	}
	if strings.Contains(strings.ToLower(body), "<script") || strings.Contains(body, " style=") {
		t.Error("page carries a script or an inline style attribute the CSP would refuse")
	}
	for _, want := range []string{`<html lang="en">`, `<title>Sign in</title>`, `<label for="code">`, `id="code"`,
		`autocomplete="one-time-code"`, `aria-describedby="hint"`, `method="post" action="/login"`,
		`name="viewport"`, `prefers-color-scheme: dark`, `:focus-visible`} {
		if !strings.Contains(body, want) {
			t.Errorf("markup lacks %q", want)
		}
	}
	for _, bad := range []string{"onpaste", "ondrop", ` aria-invalid=`, `role="alert"`} {
		if strings.Contains(body, bad) {
			t.Errorf("fresh page has %q", bad)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("page may be cached")
	}
}

func TestLoginRedirectsToFixedTarget(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{Home: "/private"})
	code := f.code.Generate()
	// A target in the request means nothing.
	req := loginForm(t, f, code)
	req.URL.RawQuery = "next=https://evil.example.test"
	rec := do(f.server, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/private" {
		t.Fatalf("status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	c := setCookie(t, rec)
	if got := do(f.server, getWith("/private", c)).Code; got != http.StatusNoContent {
		t.Errorf("session from the sign-in: status %d", got)
	}
}

// browserOrigin is the Origin a browser sends when it posts a form of a page served under policy:
// no-referrer turns it into null even for the page's own origin.
func browserOrigin(policy, page string) string {
	if policy == "no-referrer" {
		return "null"
	}
	return page
}

func TestSignInByFormFromTheLoginPage(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{Home: "/private"})
	page := do(f.server, get("/login"))
	req := loginForm(t, f, f.code.Generate())
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", browserOrigin(page.Header().Get("Referrer-Policy"), "http://localhost"))
	rec := do(f.server, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/private" {
		t.Fatalf("status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}

	req = loginForm(t, f, f.code.Generate())
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "null")
	if got := do(f.server, req); got.Code != http.StatusForbidden || len(got.Result().Cookies()) != 0 {
		t.Errorf("Origin: null: status %d, cookies %v", got.Code, got.Result().Cookies())
	}
}

func TestWrongAndThrottledAnswerTheSame(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	code := f.code.Generate()
	secret := "ZZZZ-9999"
	wrong := do(f.server, loginForm(t, f, secret))
	for range 5 {
		do(f.server, loginForm(t, f, secret))
	}
	throttled := do(f.server, loginForm(t, f, code))
	if wrong.Code != http.StatusForbidden || throttled.Code != wrong.Code || noNonce(throttled.Body.String()) != noNonce(wrong.Body.String()) {
		t.Fatalf("answers differ: %d/%d", wrong.Code, throttled.Code)
	}
	body := wrong.Body.String()
	for _, want := range []string{`role="alert"`, `aria-invalid="true"`, `aria-describedby="hint error"`, `id="error"`} {
		if !strings.Contains(body, want) {
			t.Errorf("error page lacks %q", want)
		}
	}
	for _, rec := range []*httptest.ResponseRecorder{wrong, throttled} {
		if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), code) ||
			strings.Contains(rec.Body.String(), "9999") || len(rec.Result().Cookies()) != 0 {
			t.Error("a refused attempt shows the code or sets a cookie")
		}
	}
}

func TestLogoutByPostEndsSession(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := setCookie(t, do(f.server, loginForm(t, f, f.code.Generate())))

	if got := do(f.server, getWith("/logout", c)); got.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /logout: status %d", got.Code)
	}
	if got := do(f.server, getWith("/private", c)).Code; got != http.StatusNoContent {
		t.Fatalf("GET /logout ended the session: status %d", got)
	}
	if got := do(f.server, formPost("/logout", nil, c)).Code; got != http.StatusForbidden {
		t.Errorf("POST /logout without token: status %d", got)
	}
	if got := do(f.server, getWith("/private", c)).Code; got != http.StatusNoContent {
		t.Fatalf("a refused sign-out ended the session: status %d", got)
	}
	rec := do(f.server, formPost("/logout", url.Values{CSRFField: {privateToken(t, f, c)}}, c))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := do(f.server, getWith("/private", c)).Code; got == http.StatusNoContent {
		t.Error("session survives the sign-out")
	}
}

func TestUnsignedNavigationGoesToLogin(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{Prefix: "/auth"})
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rec := do(f.server, httptest.NewRequest(m, "http://localhost/private", nil))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/auth/login" {
			t.Errorf("%s: status %d, Location %q", m, rec.Code, rec.Header().Get("Location"))
		}
	}
	if got := do(f.server, formPost("/private", nil)).Code; got != http.StatusUnauthorized {
		t.Errorf("POST: status %d", got)
	}
	if got := do(f.server, get("/auth/login")).Code; got != http.StatusOK {
		t.Errorf("prefixed page: status %d", got)
	}
	if got := f.pages.LogoutPath(); got != "/auth/logout" {
		t.Errorf("LogoutPath %q", got)
	}
}

func TestNewPagesChecksOptions(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	for name, o := range map[string]PagesOptions{
		"no Auth":           {},
		"external Home":     {Auth: f.auth, Home: "https://evil.example.test/"},
		"protocol-relative": {Auth: f.auth, Home: "//evil.example.test"},
		"slash Prefix":      {Auth: f.auth, Prefix: "/auth/"},
		"relative Prefix":   {Auth: f.auth, Prefix: "auth"},
		"wildcard Prefix":   {Auth: f.auth, Prefix: "/{x}"},
	} {
		if _, err := NewPages(o); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// noNonce removes the nonce and the token, which differ per response.
func noNonce(s string) string {
	return tokenInPage.ReplaceAllString(regexp.MustCompile(`nonce="[^"]*"`).ReplaceAllString(s, ""), "")
}

// privateToken returns the CSRF token of the session in c, as a page of the application would embed it.
func privateToken(t *testing.T, f *pagesFixture, c *http.Cookie) string {
	t.Helper()
	rec := do(f.server, getWith("/token", c))
	if rec.Code != http.StatusOK || rec.Body.Len() < 43 {
		t.Fatalf("token: status %d, body %q", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
