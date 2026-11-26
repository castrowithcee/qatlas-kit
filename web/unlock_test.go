package web

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type unlockFixture struct {
	*pagesFixture
	u      *Unlocker
	secret string
	log    *bytes.Buffer
	vault  struct {
		sync.Mutex
		got     []string
		locks   int
		lockErr error
	}
}

// newUnlockFixture serves the pages and an Unlocker with a fake vault that accepts one made-up passphrase.
func newUnlockFixture(t *testing.T, po PagesOptions, uo UnlockOptions) *unlockFixture {
	t.Helper()
	var b [8]byte
	_, _ = rand.Read(b[:])
	f := &unlockFixture{secret: fmt.Sprintf("demo-%x", b), log: &bytes.Buffer{}}
	f.pagesFixture = &pagesFixture{code: NewOnceCode(OnceCodeOptions{}), audit: &recordingAudit{}}
	cs := NewCookieSessions(SessionOptions{Audit: f.audit})
	f.clock = &testClock{t: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	cs.now = f.clock.now
	var err error
	if f.auth, err = NewAuthenticator(AuthOptions{Sessions: cs, Method: f.code, Audit: f.audit}); err != nil {
		t.Fatal(err)
	}
	po.Auth = f.auth
	if f.pages, err = NewPages(po); err != nil {
		t.Fatal(err)
	}
	uo.Unlock = func(p string) error {
		f.vault.Lock()
		defer f.vault.Unlock()
		f.vault.got = append(f.vault.got, p)
		if p != f.secret {
			return errors.New("wrong passphrase " + p) // a careless tool: the text must not reach the person
		}
		return nil
	}
	uo.Lock = func() error {
		f.vault.Lock()
		defer f.vault.Unlock()
		f.vault.locks++
		return f.vault.lockErr
	}
	uo.Audit = f.audit
	if f.u, err = NewUnlocker(f.pages, uo); err != nil {
		t.Fatal(err)
	}
	f.u.now = f.clock.now
	f.server = newServer(t, Options{Sessions: cs, Log: f.log, Routes: append(append(f.pages.Routes(),
		Route{Pattern: "GET /token", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(CSRFToken(r)))
		})}), f.u.Routes()...)})
	return f
}

func (f *unlockFixture) attempts() []string {
	f.vault.Lock()
	defer f.vault.Unlock()
	return append([]string(nil), f.vault.got...)
}

func (f *unlockFixture) locks() int {
	f.vault.Lock()
	defer f.vault.Unlock()
	return f.vault.locks
}

func (f *unlockFixture) post(t *testing.T, c *http.Cookie, pass string) *http.Request {
	t.Helper()
	return browserPost("/unlock", url.Values{CSRFField: {privateToken(t, f.pagesFixture, c)}, PassphraseField: {pass}}, c)
}

// noSecret fails if any value the test knows is a passphrase appears in what a person, a log or the protocol sees.
func (f *unlockFixture) noSecret(t *testing.T, where string, texts ...string) {
	t.Helper()
	all := strings.Join(texts, "\n") + f.log.String() + fmt.Sprintf("%+v", f.audit.events)
	for _, s := range append([]string{f.secret}, "wrong-guess") {
		if strings.Contains(all, s) {
			t.Errorf("%s: a passphrase appears in the output", where)
		}
	}
}

func dump(rec interface {
	Header() http.Header
}, body string) string {
	return fmt.Sprint(rec.Header()) + body
}

func TestNoUnlockWithoutTheOption(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	c := signedIn(t, f)
	if got := do(f.server, getWith("/unlock", c)).Code; got != http.StatusNotFound {
		t.Errorf("GET: status %d", got)
	}
	if got := do(f.server, browserPost("/unlock", url.Values{CSRFField: {privateToken(t, f, c)}}, c)).Code; got == http.StatusOK || got == http.StatusSeeOther {
		t.Errorf("POST: status %d", got)
	}
}

func TestUnlockNeedsASession(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	if rec := do(f.server, get("/unlock")); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("GET: status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := do(f.server, browserPost("/unlock", url.Values{PassphraseField: {f.secret}})).Code; got != http.StatusUnauthorized {
		t.Errorf("POST: status %d", got)
	}
	if len(f.attempts()) != 0 {
		t.Error("the vault was asked without a session")
	}
}

func TestUnlockNeedsAVeryRecentSignIn(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	f.clock.t = f.clock.t.Add(2 * time.Minute) // the limit itself passes
	if got := do(f.server, getWith("/unlock", c)).Code; got != http.StatusOK {
		t.Fatalf("at the limit: status %d", got)
	}
	f.clock.t = f.clock.t.Add(time.Second)
	rec := do(f.server, getWith("/unlock", c))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/reauth?next=%2Funlock" {
		t.Fatalf("GET: status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	// The sign-in of the route has no say in this: 5 minutes would still pass RequireRecent.
	if got := do(f.server, f.post(t, c, f.secret)).Code; got != http.StatusForbidden {
		t.Errorf("POST: status %d", got)
	}
	if len(f.attempts()) != 0 {
		t.Error("the vault was asked with an old sign-in")
	}
	// Confirming again leads back to the page, and the POST works.
	rec = do(f.server, reauthPost(t, f.pagesFixture, c, f.code.Generate(), "/unlock"))
	if rec.Header().Get("Location") != "/unlock" {
		t.Fatalf("confirmation goes to %q", rec.Header().Get("Location"))
	}
	if got := do(f.server, f.post(t, c, f.secret)).Code; got != http.StatusSeeOther {
		t.Errorf("POST after confirming: status %d", got)
	}
}

func TestUnlockLimitFollowsAShorterReauthWithin(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{ReauthWithin: 30 * time.Second}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	f.clock.t = f.clock.t.Add(31 * time.Second)
	if got := do(f.server, getWith("/unlock", c)).Code; got != http.StatusSeeOther {
		t.Errorf("status %d", got)
	}
}

func TestUnlockRefusesMissingOrWrongCSRFToken(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	f.audit.events = nil
	for name, v := range map[string]url.Values{
		"missing": {PassphraseField: {f.secret}},
		"wrong":   {PassphraseField: {f.secret}, CSRFField: {"x" + privateToken(t, f.pagesFixture, c)}},
	} {
		if got := do(f.server, browserPost("/unlock", v, c)).Code; got != http.StatusForbidden {
			t.Errorf("%s token: status %d", name, got)
		}
	}
	if len(f.attempts()) != 0 {
		t.Error("the vault was asked without a valid token")
	}
	if k := f.audit.kinds(); len(k) != 2 || k[0] != CSRFRefused {
		t.Errorf("protocol %v", k)
	}
}

func TestWrongPassphraseIsRefusedWithoutEchoingIt(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	f.audit.events = nil
	rec := do(f.server, f.post(t, c, "wrong-guess"))
	body := rec.Body.String()
	if rec.Code != http.StatusForbidden || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `aria-invalid="true"`) ||
		!strings.Contains(body, `type="password"`) {
		t.Fatalf("status %d, body %q", rec.Code, body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("page may be cached")
	}
	if f.u.Unlocked() {
		t.Error("a wrong passphrase unlocked")
	}
	if k := f.audit.kinds(); len(k) != 1 || k[0] != VaultUnlockFailed {
		t.Errorf("protocol %v", k)
	}
	f.noSecret(t, "wrong passphrase", dump(rec, body))
}

func TestEmptyAndHugePassphrasesAreRefusedWithoutACall(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	for _, p := range []string{"", strings.Repeat("a", maxPassphrase+1)} {
		if got := do(f.server, f.post(t, c, p)).Code; got != http.StatusForbidden {
			t.Errorf("%d bytes: status %d", len(p), got)
		}
	}
	if len(f.attempts()) != 0 {
		t.Error("the vault was asked")
	}
}

func TestWrongPassphrasesAreThrottled(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	for range 5 {
		do(f.server, f.post(t, c, "wrong-guess"))
	}
	f.audit.events = nil
	// The right one is refused too, and the vault is not asked: the answer tells nothing.
	rec := do(f.server, f.post(t, c, f.secret))
	if rec.Code != http.StatusForbidden || f.u.Unlocked() || len(f.attempts()) != 5 {
		t.Fatalf("status %d, unlocked %v, %d calls", rec.Code, f.u.Unlocked(), len(f.attempts()))
	}
	if k := f.audit.kinds(); len(k) != 1 || k[0] != VaultUnlockThrottled {
		t.Errorf("protocol %v", k)
	}
	// The sign-in throttle is a separate budget and has not been touched.
	if got := do(f.server, reauthPost(t, f.pagesFixture, c, f.code.Generate(), "/unlock")).Code; got != http.StatusSeeOther {
		t.Errorf("confirmation: status %d", got)
	}
	f.clock.t = f.clock.t.Add(10*time.Minute + time.Second)
	do(f.server, reauthPost(t, f.pagesFixture, c, f.code.Generate(), "/unlock"))
	if got := do(f.server, f.post(t, c, f.secret)).Code; got != http.StatusSeeOther || !f.u.Unlocked() {
		t.Errorf("after the window: status %d", got)
	}
}

func TestSuccessCallsTheVaultOnceAndShowsNoField(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	f.audit.events = nil
	rec := do(f.server, f.post(t, c, f.secret))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/unlock" {
		t.Fatalf("status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := f.attempts(); len(got) != 1 || got[0] != f.secret || !f.u.Unlocked() {
		t.Fatalf("calls %d, unlocked %v", len(got), f.u.Unlocked())
	}
	page := do(f.server, getWith("/unlock", c))
	body := page.Body.String()
	if !strings.Contains(body, "The vault is unlocked.") || strings.Contains(body, "<input") {
		t.Errorf("page of an unlocked vault: %q", body)
	}
	// A second post does not ask the vault again.
	if got := do(f.server, f.post(t, c, f.secret)); got.Code != http.StatusOK || len(f.attempts()) != 1 {
		t.Errorf("second post: status %d, %d calls", got.Code, len(f.attempts()))
	}
	if k := f.audit.kinds(); len(k) != 1 || k[0] != VaultUnlocked {
		t.Errorf("protocol %v", k)
	}
	f.noSecret(t, "success", dump(rec, rec.Body.String()), dump(page, body))
}

func TestVaultNeverLocksByItself(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	do(f.server, f.post(t, c, f.secret))
	f.audit.events = nil
	f.clock.t = f.clock.t.Add(48 * time.Hour)
	do(f.server, getWith("/unlock", c))
	if f.locks() != 0 || !f.u.Unlocked() || len(f.audit.kinds()) != 0 {
		t.Fatalf("locks %d, unlocked %v, protocol %v", f.locks(), f.u.Unlocked(), f.audit.kinds())
	}
}

func TestLockLocksAndReports(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	do(f.server, f.post(t, c, f.secret))
	f.audit.events = nil
	if err := f.u.Lock(); err != nil || f.locks() != 1 || f.u.Unlocked() {
		t.Fatalf("Lock: %v, %d locks, unlocked %v", err, f.locks(), f.u.Unlocked())
	}
	if k := f.audit.kinds(); len(k) != 1 || k[0] != VaultLocked {
		t.Errorf("protocol %v", k)
	}
	do(f.server, reauthPost(t, f.pagesFixture, c, f.code.Generate(), "/unlock"))
	if body := do(f.server, getWith("/unlock", c)).Body.String(); !strings.Contains(body, `type="password"`) {
		t.Error("page of a locked vault has no field")
	}
}

func TestCloseLocks(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	if err := f.u.Close(); err != nil || f.locks() != 0 {
		t.Fatalf("a locked vault is locked again: %v, %d", err, f.locks())
	}
	c := signedIn(t, f.pagesFixture)
	do(f.server, f.post(t, c, f.secret))
	if err := f.u.Close(); err != nil || f.locks() != 1 || f.u.Unlocked() {
		t.Fatalf("Close: %v, %d locks, unlocked %v", err, f.locks(), f.u.Unlocked())
	}
}

func TestFailedLockKeepsTheVaultUnlockedWithoutRetry(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	do(f.server, f.post(t, c, f.secret))
	f.audit.events = nil
	f.vault.lockErr = errors.New("busy")
	if err := f.u.Lock(); err == nil || !f.u.Unlocked() || f.locks() != 1 {
		t.Fatalf("Lock: %v, unlocked %v, %d locks", err, f.u.Unlocked(), f.locks())
	}
	if err := f.u.Close(); err == nil || !f.u.Unlocked() || f.locks() != 2 {
		t.Fatalf("Close: %v, unlocked %v, %d locks", err, f.u.Unlocked(), f.locks())
	}
	f.clock.t = f.clock.t.Add(time.Hour)
	if f.locks() != 2 || len(f.audit.kinds()) != 0 {
		t.Errorf("%d locks, protocol %v", f.locks(), f.audit.kinds())
	}
	f.vault.lockErr = nil
	if err := f.u.Close(); err != nil || f.u.Unlocked() || f.locks() != 3 {
		t.Errorf("Close: %v, unlocked %v, %d locks", err, f.u.Unlocked(), f.locks())
	}
}

func TestUnlockPageMarkup(t *testing.T) {
	f := newUnlockFixture(t, PagesOptions{Prefix: ""}, UnlockOptions{})
	c := signedIn(t, f.pagesFixture)
	rec := do(f.server, getWith("/unlock", c))
	body := rec.Body.String()
	tok := tokenInPage.FindStringSubmatch(body)
	if rec.Code != http.StatusOK || tok == nil || tok[1] != privateToken(t, f.pagesFixture, c) {
		t.Fatalf("status %d, token %v", rec.Code, tok)
	}
	for _, want := range []string{`<html lang="en">`, `<title>Unlock vault</title>`, `<h1>Unlock vault</h1>`, `<label for="passphrase">`,
		`id="passphrase"`, `name="passphrase"`, `type="password"`, `autocomplete="new-password"`, `autocomplete="off"`,
		`aria-describedby="hint"`, `method="post" action="/unlock"`, `autofocus required`, `:focus-visible`,
		`prefers-color-scheme: dark`, `min-height: 2.75rem`, `name="viewport"`} {
		if !strings.Contains(body, want) {
			t.Errorf("markup lacks %q", want)
		}
	}
	input := body[strings.Index(body, `id="passphrase"`):]
	input = input[:strings.Index(input, ">")]
	if strings.Contains(input, "value=") || strings.Contains(input, "onpaste") || strings.Contains(input, "ondrop") {
		t.Errorf("field %q has a value or blocks pasting", input)
	}
	for _, bad := range []string{` aria-invalid="true"`, `role="alert"`} {
		if strings.Contains(body, bad) {
			t.Errorf("fresh page has %q", bad)
		}
	}
	if strings.Contains(strings.ToLower(body), "<script") || strings.Contains(body, " style=") {
		t.Error("page carries a script or an inline style attribute the CSP would refuse")
	}
	if rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "'nonce-") {
		t.Errorf("headers %v", rec.Header())
	}
}

func TestNewUnlockerChecksItsOptions(t *testing.T) {
	f := newPagesFixture(t, PagesOptions{})
	ok := func(string) error { return nil }
	lock := func() error { return nil }
	for name, c := range map[string]struct {
		p *Pages
		o UnlockOptions
	}{
		"no pages":  {nil, UnlockOptions{Unlock: ok, Lock: lock}},
		"no unlock": {f.pages, UnlockOptions{Lock: lock}},
		"no lock":   {f.pages, UnlockOptions{Unlock: ok}},
	} {
		if _, err := NewUnlocker(c.p, c.o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
