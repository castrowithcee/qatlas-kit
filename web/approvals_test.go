package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/approval"
	"github.com/castrowithcee/qatlas-kit/vault"
)

const testApprovalPass = "example passphrase"

type approvalFixture struct {
	*pagesFixture
	ap     *Approvals
	cookie *http.Cookie
}

func newApprovalFixture(t *testing.T, timeout time.Duration) *approvalFixture {
	t.Helper()
	f := &approvalFixture{pagesFixture: &pagesFixture{code: NewOnceCode(OnceCodeOptions{}), audit: &recordingAudit{}}}
	cs := NewCookieSessions(SessionOptions{Audit: f.audit})
	var err error
	if f.auth, err = NewAuthenticator(AuthOptions{Sessions: cs, Method: f.code, Audit: f.audit}); err != nil {
		t.Fatal(err)
	}
	if f.pages, err = NewPages(PagesOptions{Auth: f.auth}); err != nil {
		t.Fatal(err)
	}
	if f.ap, err = NewApprovals(ApprovalOptions{Path: "/approve", Timeout: timeout, Audit: f.audit}); err != nil {
		t.Fatal(err)
	}
	f.server = newServer(t, Options{Sessions: cs, Routes: append(f.pages.Routes(), f.ap.Routes()...)})
	f.cookie = signedIn(t, f.pagesFixture)
	return f
}

type askResult struct {
	pass string
	err  error
}

// ask starts a request and returns once the page shows it.
func (f *approvalFixture) ask(t *testing.T, ctx context.Context, prompt string) <-chan askResult {
	t.Helper()
	ch := make(chan askResult, 1)
	go func() {
		p, err := f.ap.PersonChannel(ctx)(prompt)
		ch <- askResult{p, err}
	}()
	f.waitPending(t)
	return ch
}

func (f *approvalFixture) waitPending(t *testing.T) {
	t.Helper()
	for i := 0; i < 500; i++ {
		f.ap.mu.Lock()
		p := f.ap.cur != nil
		f.ap.mu.Unlock()
		if p {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("request never became visible")
}

func (f *approvalFixture) pending() bool {
	f.ap.mu.Lock()
	defer f.ap.mu.Unlock()
	return f.ap.cur != nil
}

var requestInPage = regexp.MustCompile(`name="request" value="([^"]+)"`)

// form reads the page like a browser and returns the values its form would send.
func (f *approvalFixture) form(t *testing.T) url.Values {
	t.Helper()
	rec := do(f.server, getWith("/approve", f.cookie))
	m := requestInPage.FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || m == nil {
		t.Fatalf("page: status %d, request %v", rec.Code, m)
	}
	return url.Values{CSRFField: {privateTokenOf(t, f)}, "request": {m[1]}, "passphrase": {testApprovalPass}}
}

func privateTokenOf(t *testing.T, f *approvalFixture) string {
	t.Helper()
	return csrfOf("session", f.cookie.Value)
}

func result(t *testing.T, ch <-chan askResult) askResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("request did not return")
		return askResult{}
	}
}

func TestApprovalOnlyWithSessionTokenAndOrigin(t *testing.T) {
	f := newApprovalFixture(t, time.Minute)
	ch := f.ask(t, context.Background(), "unlock")
	good := f.form(t)

	wrongToken := url.Values{CSRFField: {"x" + good.Get(CSRFField)}, "request": good["request"], "passphrase": good["passphrase"]}
	noToken := url.Values{"request": good["request"], "passphrase": good["passphrase"]}
	cases := []struct {
		name   string
		req    *http.Request
		status int
	}{
		{"no session", browserPost("/approve", good), http.StatusUnauthorized},
		{"unknown session", browserPost("/approve", good, &http.Cookie{Name: f.cookie.Name, Value: strings.Repeat("a", 43)}), http.StatusUnauthorized},
		{"no token", browserPost("/approve", noToken, f.cookie), http.StatusForbidden},
		{"wrong token", browserPost("/approve", wrongToken, f.cookie), http.StatusForbidden},
		{"foreign origin", func() *http.Request {
			r := browserPost("/approve", good, f.cookie)
			r.Header.Set("Origin", "https://evil.example.test")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			return r
		}(), http.StatusForbidden},
		{"foreign origin header only", func() *http.Request {
			r := browserPost("/approve", good, f.cookie)
			r.Header.Del("Sec-Fetch-Site")
			r.Header.Set("Origin", "https://evil.example.test")
			return r
		}(), http.StatusForbidden},
		{"origin null", func() *http.Request {
			r := browserPost("/approve", good, f.cookie)
			r.Header.Del("Sec-Fetch-Site")
			r.Header.Set("Origin", "null")
			return r
		}(), http.StatusForbidden},
	}
	for _, tc := range cases {
		rec := do(f.server, tc.req)
		if rec.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, rec.Code, tc.status)
		}
		if !f.pending() {
			t.Fatalf("%s: the request was answered", tc.name)
		}
	}
	select {
	case r := <-ch:
		t.Fatalf("a refused post delivered %+v", r)
	default:
	}

	rec := do(f.server, browserPost("/approve", good, f.cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/approve" {
		t.Fatalf("valid post: status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if r := result(t, ch); r.err != nil || r.pass != testApprovalPass {
		t.Fatalf("result %+v", r)
	}
	if strings.Contains(rec.Body.String(), testApprovalPass) || strings.Contains(rec.Header().Get("Location"), testApprovalPass) {
		t.Error("passphrase in the answer")
	}
}

func TestApprovalRefusedWithoutSessionOnLoginOff(t *testing.T) {
	ap, err := NewApprovals(ApprovalOptions{Path: "/approve", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(t, Options{Login: LoginOff, Routes: ap.Routes()})
	f := &approvalFixture{ap: ap}
	ch := f.ask(t, context.Background(), "unlock")
	ap.mu.Lock()
	id := ap.cur.id
	ap.mu.Unlock()

	if rec := do(s, getWith("/approve")); rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), id) {
		t.Errorf("page without session: status %d", rec.Code)
	}
	form := url.Values{"request": {id}, "passphrase": {testApprovalPass}}
	if rec := do(s, browserPost("/approve", form)); rec.Code != http.StatusForbidden {
		t.Errorf("post without session: status %d, want %d", rec.Code, http.StatusForbidden)
	}
	if !f.pending() {
		t.Fatal("a post without session answered the request")
	}
	select {
	case r := <-ch:
		t.Fatalf("a refused post delivered %+v", r)
	default:
	}
}

func TestApprovalProtocolHoldsNoSecret(t *testing.T) {
	f := newApprovalFixture(t, time.Minute)
	f.audit.events = nil
	ch := f.ask(t, context.Background(), "unlock")
	form := f.form(t)
	do(f.server, browserPost("/approve", form, f.cookie))
	result(t, ch)
	if k := f.audit.kinds(); len(k) != 1 || k[0] != ApprovalSubmitted {
		t.Fatalf("protocol %v", k)
	}
	e := f.audit.events[0]
	if e.Remote == "" || e.Time.IsZero() {
		t.Errorf("entry %+v", e)
	}
	for _, secret := range []string{testApprovalPass, form.Get(CSRFField), f.cookie.Value} {
		if strings.Contains(e.Remote+string(e.Kind), secret) {
			t.Error("entry holds a secret")
		}
	}
}

func TestApprovalDeclined(t *testing.T) {
	f := newApprovalFixture(t, time.Minute)
	f.audit.events = nil
	ch := f.ask(t, context.Background(), "unlock")
	form := f.form(t)
	form.Set("decision", "decline")
	form.Del("passphrase")
	if rec := do(f.server, browserPost("/approve", form, f.cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	if r := result(t, ch); !errors.Is(r.err, ErrApprovalDeclined) || r.pass != "" {
		t.Fatalf("result %+v", r)
	}
	if k := f.audit.kinds(); len(k) != 1 || k[0] != ApprovalDeclined {
		t.Fatalf("protocol %v", k)
	}
}

func TestApprovalTimeoutAndCancel(t *testing.T) {
	f := newApprovalFixture(t, 20*time.Millisecond)
	f.audit.events = nil
	if r := result(t, f.ask(t, context.Background(), "unlock")); !errors.Is(r.err, ErrApprovalTimeout) {
		t.Fatalf("timeout: %+v", r)
	}
	if f.pending() {
		t.Error("expired request still shown")
	}
	f.ap.timeout = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	ch := f.ask(t, ctx, "unlock")
	cancel()
	if r := result(t, ch); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("cancel: %+v", r)
	}
	if k := f.audit.kinds(); len(k) != 2 || k[0] != ApprovalExpired || k[1] != ApprovalCancelled {
		t.Fatalf("protocol %v", k)
	}
}

func TestApprovalStaleEmptyAndBusy(t *testing.T) {
	f := newApprovalFixture(t, time.Minute)
	// Nothing waits: a post is refused and delivers nothing.
	stale := url.Values{CSRFField: {privateTokenOf(t, f)}, "request": {"old"}, "passphrase": {testApprovalPass}}
	if rec := do(f.server, browserPost("/approve", stale, f.cookie)); rec.Code != http.StatusConflict {
		t.Errorf("stale: status %d", rec.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := f.ask(t, ctx, "unlock")
	if _, err := f.ap.PersonChannel(ctx)("second"); !errors.Is(err, ErrApprovalBusy) {
		t.Errorf("second request: %v", err)
	}
	form := f.form(t)
	form.Set("passphrase", "")
	rec := do(f.server, browserPost("/approve", form, f.cookie))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `role="alert"`) || !f.pending() {
		t.Errorf("empty: status %d, pending %v", rec.Code, f.pending())
	}
	form.Set("request", "old")
	form.Set("passphrase", testApprovalPass)
	if rec := do(f.server, browserPost("/approve", form, f.cookie)); rec.Code != http.StatusConflict || !f.pending() {
		t.Errorf("wrong request id: status %d", rec.Code)
	}
	cancel()
	result(t, ch)
}

func TestApprovalPageMarkup(t *testing.T) {
	f := newApprovalFixture(t, time.Minute)
	idle := do(f.server, getWith("/approve", f.cookie))
	if idle.Code != http.StatusOK || strings.Contains(idle.Body.String(), "<form") {
		t.Errorf("idle page: status %d", idle.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := f.ask(t, ctx, `Vault <b>"unlock"</b> & more`)
	rec := do(f.server, getWith("/approve", f.cookie))
	body := rec.Body.String()
	for _, want := range []string{`<html lang="en">`, `<label for="passphrase">`, `type="password"`, `autocomplete="new-password"`,
		`aria-describedby="hint prompt"`, `method="post" action="/approve"`, `name="csrf_token"`, `formnovalidate`,
		`Vault &lt;b&gt;&#34;unlock&#34;&lt;/b&gt; &amp; more`, `:focus-visible`, `min-height: 2.75rem`} {
		if !strings.Contains(body, want) {
			t.Errorf("markup lacks %q", want)
		}
	}
	m := regexp.MustCompile(`<style nonce="([^"]+)">`).FindStringSubmatch(body)
	if m == nil || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "'nonce-"+m[1]+"'") {
		t.Error("style without the nonce of the policy")
	}
	if strings.Contains(strings.ToLower(body), "<script") || regexp.MustCompile(`name="passphrase"[^>]*value=`).MatchString(body) {
		t.Error("script or prefilled passphrase")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("page may be cached")
	}
	// Without a session the page is not shown.
	if rec := do(f.server, get("/approve")); strings.Contains(rec.Body.String(), "unlock") {
		t.Error("prompt shown without a session")
	}
	cancel()
	result(t, ch)
}

func TestNewApprovalsRefusesBadOptions(t *testing.T) {
	for _, o := range []ApprovalOptions{{}, {Path: "approve"}, {Path: "/"}, {Path: "/a/"}, {Path: "/{x}"},
		{Path: "/approve", Timeout: -time.Second}} {
		if _, err := NewApprovals(o); err == nil {
			t.Errorf("%+v accepted", o)
		}
	}
	if a, err := NewApprovals(ApprovalOptions{Path: "/approve"}); err != nil || a.timeout != 2*time.Minute {
		t.Errorf("default timeout %v, %v", a, err)
	}
}

// The channel is the function approval.Authorize expects: a form post delivers the passphrase, and the
// vault, not the channel, decides whether it is right.
func TestApprovalChannelWithAuthorize(t *testing.T) {
	v, err := vault.New(vault.Options{
		Dir: filepath.Join(t.TempDir(), "vault"),
		Files: vault.Files{Key: "key.age", Recipient: "recipient", Document: "secrets.age", Pending: "pending",
			Plain: "secrets.json", Sealed: []string{"approvals.age"}},
		Schemas: []int{1}, WorkFactor: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Create(testApprovalPass, []byte(`{"schema":1}`)); err != nil {
		t.Fatal(err)
	}
	f := newApprovalFixture(t, time.Minute)
	for _, tc := range []struct {
		pass    string
		wantErr error
	}{{testApprovalPass, nil}, {"example wrong passphrase", vault.ErrWrongPassphrase}} {
		done := make(chan error, 1)
		go func() {
			_, err := approval.Authorize(v, approval.AuthorizeOptions{Prompt: "vault passphrase", PersonChannel: f.ap.PersonChannel(context.Background())})
			done <- err
		}()
		f.waitPending(t)
		form := f.form(t)
		form.Set("passphrase", tc.pass)
		if rec := do(f.server, browserPost("/approve", form, f.cookie)); rec.Code != http.StatusSeeOther {
			t.Fatalf("post: status %d", rec.Code)
		}
		select {
		case err := <-done:
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Authorize with %q: %v, want %v", tc.pass, err, tc.wantErr)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Authorize did not return")
		}
	}
	// An agent is refused before the form is shown.
	_, err = approval.Authorize(v, approval.AuthorizeOptions{Prompt: "p", Agent: true, PersonChannel: f.ap.PersonChannel(context.Background())})
	if !errors.Is(err, approval.ErrAgent) || f.pending() {
		t.Errorf("agent: %v, pending %v", err, f.pending())
	}
}
