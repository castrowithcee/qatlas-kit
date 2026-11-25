package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

type pagesFixture struct {
	server *Server
	pages  *Pages
	code   *OnceCode
	audit  *recordingAudit
	auth   *Authenticator
}

func newPagesFixture(t *testing.T, opts PagesOptions) *pagesFixture {
	t.Helper()
	f := &pagesFixture{code: NewOnceCode(OnceCodeOptions{}), audit: &recordingAudit{}}
	cs := NewCookieSessions(SessionOptions{})
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
		Route{Pattern: "GET /private", Handler: private}, Route{Pattern: "POST /private", Handler: private})
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
	rec := do(f.server, formPost("/login?next=https://evil.example.test", url.Values{CodeField: {code},
		"next": {"https://evil.example.test"}}))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/private" {
		t.Fatalf("status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	c := setCookie(t, rec)
	if got := do(f.server, getWith("/private", c)).Code; got != http.StatusNoContent {
		t.Errorf("session from the sign-in: status %d", got)
	}
}

func TestWrongAndThrottledAnswerTheSame(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	code := f.code.Generate()
	secret := "ZZZZ-9999"
	wrong := do(f.server, formPost("/login", url.Values{CodeField: {secret}}))
	for range 5 {
		do(f.server, formPost("/login", url.Values{CodeField: {secret}}))
	}
	throttled := do(f.server, formPost("/login", url.Values{CodeField: {code}}))
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
	c := setCookie(t, do(f.server, formPost("/login", url.Values{CodeField: {f.code.Generate()}})))

	if got := do(f.server, getWith("/logout", c)); got.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /logout: status %d", got.Code)
	}
	if got := do(f.server, getWith("/private", c)).Code; got != http.StatusNoContent {
		t.Fatalf("GET /logout ended the session: status %d", got)
	}
	rec := do(f.server, formPost("/logout", nil, c))
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

// noNonce removes the nonce, which differs per response.
func noNonce(s string) string { return regexp.MustCompile(`nonce="[^"]*"`).ReplaceAllString(s, "") }
