package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func signedIn(t *testing.T, f *pagesFixture) *http.Cookie {
	t.Helper()
	return setCookie(t, do(f.server, loginForm(t, f, f.code.Generate())))
}

// browserPost is a form post as a browser sends it from a page of the same origin.
func browserPost(path string, v url.Values, c ...*http.Cookie) *http.Request {
	r := formPost(path, v, c...)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Origin", "http://localhost")
	return r
}

func TestSessionCSRFToken(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	tok := privateToken(t, f, c)
	if len(tok) < 22 { // 128 bit in unpadded base64
		t.Fatalf("token of %d symbols", len(tok))
	}
	if again := privateToken(t, f, c); again != tok {
		t.Error("token changes within a session")
	}
	if rec := do(f.server, browserPost("/private", url.Values{CSRFField: {tok}}, c)); rec.Code != http.StatusNoContent {
		t.Errorf("token in form: status %d", rec.Code)
	}
	req := browserPost("/private", nil, c)
	req.Header.Set(CSRFHeader, tok)
	if rec := do(f.server, req); rec.Code != http.StatusNoContent {
		t.Errorf("token in header: status %d", rec.Code)
	}
	if got := do(f.server, getWith("/private", c)).Code; got != http.StatusNoContent {
		t.Errorf("GET needs no token: status %d", got)
	}
	if rec := do(f.server, get("/token")); rec.Code == http.StatusOK {
		t.Error("a token without a session")
	}
}

func TestSessionCSRFRefusals(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	tok := privateToken(t, f, c)
	other := signedIn(t, f) // a second browser
	otherTok := privateToken(t, f, other)

	// The session before this sign-in: its token must not work on the new one.
	oldCookie := c
	again := setCookie(t, do(f.server, func() *http.Request {
		r := loginForm(t, f, f.code.Generate())
		r.AddCookie(oldCookie)
		return r
	}()))
	if privateToken(t, f, again) == tok {
		t.Error("token survives a new sign-in")
	}

	for name, req := range map[string]*http.Request{
		"missing":   browserPost("/private", nil, other),
		"wrong":     browserPost("/private", url.Values{CSRFField: {strings.Repeat("A", 43)}}, other),
		"empty":     browserPost("/private", url.Values{CSRFField: {""}}, other),
		"other":     browserPost("/private", url.Values{CSRFField: {tok}}, other),
		"old":       browserPost("/private", url.Values{CSRFField: {tok}}, again),
		"in query":  browserPost("/private?"+CSRFField+"="+otherTok, nil, other),
		"no cookie": browserPost("/private", url.Values{CSRFField: {otherTok}}),
	} {
		f.audit.events = nil
		rec := do(f.server, req)
		want := http.StatusForbidden
		if name == "no cookie" {
			want = http.StatusUnauthorized
		}
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d", name, rec.Code, want)
		}
		if name != "no cookie" && strings.Join(kindStrings(f.audit.kinds()), ",") != string(CSRFRefused) {
			t.Errorf("%s: protocol %v", name, f.audit.kinds())
		}
	}
	// A session that ended refuses even its own token.
	do(f.server, browserPost("/logout", url.Values{CSRFField: {otherTok}}, other))
	if rec := do(f.server, browserPost("/private", url.Values{CSRFField: {otherTok}}, other)); rec.Code == http.StatusNoContent {
		t.Error("token of an ended session accepted")
	}
}

func kindStrings(ks []LoginEventKind) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = string(k)
	}
	return out
}

func TestCSRFDoesNotReplaceOriginCheck(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	req := browserPost("/private", url.Values{CSRFField: {privateToken(t, f, c)}}, c)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example.test")
	if rec := do(f.server, req); rec.Code != http.StatusForbidden {
		t.Errorf("valid token from another origin: status %d", rec.Code)
	}
}

func TestLoginCSRF(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{Home: "/private"})
	code := f.code.Generate()
	page := do(f.server, get("/login"))
	pageCookie := setCookie(t, page)
	m := tokenInPage.FindStringSubmatch(page.Body.String())
	if m == nil || len(m[1]) < 22 {
		t.Fatalf("token in page: %v", m)
	}
	if pageCookie.Name != looseLoginCookie || !pageCookie.HttpOnly || pageCookie.SameSite != http.SameSiteStrictMode || pageCookie.Path != "/" {
		t.Errorf("cookie %+v", pageCookie)
	}
	if strings.Contains(page.Body.String(), pageCookie.Value) {
		t.Error("the cookie secret is in the page")
	}
	otherPage := do(f.server, get("/login"))
	otherCookie := setCookie(t, otherPage)

	for name, req := range map[string]*http.Request{
		"no token":     formPost("/login", url.Values{CodeField: {code}}, pageCookie),
		"wrong token":  formPost("/login", url.Values{CodeField: {code}, CSRFField: {strings.Repeat("A", 43)}}, pageCookie),
		"no cookie":    formPost("/login", url.Values{CodeField: {code}, CSRFField: {m[1]}}),
		"other cookie": formPost("/login", url.Values{CodeField: {code}, CSRFField: {m[1]}}, otherCookie),
	} {
		f.audit.events = nil
		rec := do(f.server, req)
		if rec.Code != http.StatusForbidden || len(rec.Result().Cookies()) > 1 ||
			strings.Join(kindStrings(f.audit.kinds()), ",") != string(CSRFRefused) {
			t.Errorf("%s: status %d, protocol %v", name, rec.Code, f.audit.kinds())
		}
		if strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: the page shows the code", name)
		}
	}
	// The refusals did not use the code or the throttle.
	req := formPost("/login", url.Values{CodeField: {code}, CSRFField: {m[1]}}, pageCookie)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://localhost")
	if rec := do(f.server, req); rec.Code != http.StatusSeeOther {
		t.Errorf("valid form: status %d", rec.Code)
	}
}

func TestCSRFAuditHoldsNoSecrets(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	bad := "tok-" + strings.Repeat("Q", 39)
	do(f.server, browserPost("/private", url.Values{CSRFField: {bad}}, c))
	for _, e := range f.audit.events {
		if e.Kind == CSRFRefused && (e.Remote == "" || strings.ContainsAny(e.Remote, "Q") || e.Time.IsZero()) {
			t.Errorf("entry %+v", e)
		}
	}
}
