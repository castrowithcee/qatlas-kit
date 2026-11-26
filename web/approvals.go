package web

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// ApprovalSubmitted reports a passphrase that a person sent through the form. Whether the vault accepts
	// it is for the application to say: this entry holds neither the passphrase nor the verdict.
	ApprovalSubmitted LoginEventKind = "approval-submitted"
	// ApprovalDeclined is a request the person refused with the button of the form.
	ApprovalDeclined LoginEventKind = "approval-declined"
	// ApprovalExpired is a request that nobody answered within the time allowed.
	ApprovalExpired LoginEventKind = "approval-expired"
	// ApprovalCancelled is a request that its context ended, for example at shutdown.
	ApprovalCancelled LoginEventKind = "approval-cancelled"

	defaultApprovalTimeout = 2 * time.Minute
	approvalPassField      = "passphrase"
	approvalRequestField   = "request"
	approvalDeclineValue   = "decline"
)

var (
	// ErrApprovalDeclined means the person refused the request in the form.
	ErrApprovalDeclined = errors.New("web: approval declined")
	// ErrApprovalTimeout means nobody answered in time.
	ErrApprovalTimeout = errors.New("web: approval timed out")
	// ErrApprovalBusy means another request still waits; only one is shown at a time.
	ErrApprovalBusy = errors.New("web: another approval request is waiting")
)

var approvalPage = template.Must(template.New("approval").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>Approval</title>
<style nonce="{{.Nonce}}">
` + pageCSS + `.prompt { white-space: pre-wrap; font-weight: 600; }
button.secondary { margin-left: .5rem; background: var(--bg); color: var(--accent); }
</style>
</head>
<body>
<main>
<h1>Approval</h1>
{{if .Pending}}<p class="hint" id="hint">The program asks for the passphrase of its vault. Only enter it if you started this step yourself.</p>
<p class="prompt" id="prompt">{{.Prompt}}</p>
<form method="post" action="{{.Action}}" autocomplete="off">
<input type="hidden" name="{{.Field}}" value="{{.Token}}">
<input type="hidden" name="request" value="{{.Request}}">
<label for="passphrase">Passphrase</label>
<input id="passphrase" name="passphrase" type="password" autocomplete="new-password" data-1p-ignore data-lpignore="true" data-bwignore autocapitalize="off" spellcheck="false" autofocus required aria-describedby="hint prompt{{if .Empty}} error{{end}}"{{if .Empty}} aria-invalid="true"{{end}}>
{{if .Empty}}<p class="error" id="error" role="alert">Enter the passphrase, or decline the request.</p>
{{end}}{{if .Stale}}<p class="error" id="stale" role="alert">That request is no longer waiting. This page shows the current one.</p>
{{end}}<button type="submit">Approve</button>
<button type="submit" class="secondary" name="decision" value="decline" formnovalidate>Decline</button>
</form>
{{else}}{{if .Stale}}<p class="error" id="stale" role="alert">That request is no longer waiting.</p>
{{end}}<p class="hint">No approval is waiting. Return to the program that asked for it.</p>
{{end}}</main>
</body>
</html>
`))

// ApprovalOptions configure Approvals.
type ApprovalOptions struct {
	// Path is where the approval page lives, an absolute path on this server without trailing slash,
	// query or wildcard, such as "/approve". Required.
	Path string
	// Timeout is how long a request waits for an answer; 0 means 2 minutes, negative is an error.
	Timeout time.Duration
	// Audit receives the outcomes; nil keeps no protocol.
	Audit LoginAudit
}

// Approvals shows a request for a passphrase to the person at a browser session and hands the answer to the
// waiting caller. Its PersonChannel is the function approval.AuthorizeOptions expects.
type Approvals struct {
	path    string
	timeout time.Duration
	audit   LoginAudit
	now     func() time.Time

	mu  sync.Mutex
	cur *approvalRequest
}

type approvalRequest struct {
	id     string
	prompt string
	answer chan approvalAnswer // buffered, so the handler never blocks
}

type approvalAnswer struct {
	passphrase string
	err        error
}

// NewApprovals checks opts.
func NewApprovals(opts ApprovalOptions) (*Approvals, error) {
	if !localPath(opts.Path) || opts.Path == "/" || strings.HasSuffix(opts.Path, "/") ||
		strings.ContainsAny(opts.Path, "{}?#%") {
		return nil, errors.New("web: Approvals needs an absolute Path without trailing slash or wildcard")
	}
	if opts.Timeout < 0 {
		return nil, errors.New("web: the approval timeout must not be negative")
	}
	if opts.Timeout == 0 {
		opts.Timeout = defaultApprovalTimeout
	}
	return &Approvals{path: opts.Path, timeout: opts.Timeout, audit: opts.Audit, now: time.Now}, nil
}

// Path is the path of the approval page.
func (a *Approvals) Path() string { return a.path }

// Routes returns the page (GET) and its form (POST). Neither is public: they run behind the session, the CSRF
// token and the origin check of the server, none of which Approvals replaces; without a session, as on a
// server with LoginOff, they refuse the request. A tool may wrap the handlers with Pages.RequireRecent to ask
// for a recent sign-in first; Approvals asks for none itself.
func (a *Approvals) Routes() []Route {
	return []Route{
		{Pattern: "GET " + a.path, Handler: http.HandlerFunc(a.show)},
		{Pattern: "POST " + a.path, Handler: http.HandlerFunc(a.submit)},
	}
}

// PersonChannel returns a function for approval.AuthorizeOptions.PersonChannel (this package does not
// import approval). It makes the prompt visible on the approval page and waits for the form: it returns the
// passphrase the person sent, ErrApprovalDeclined if they declined, ErrApprovalTimeout after the timeout,
// ErrApprovalBusy if another request waits, or the error of ctx, which a tool ties to its server's shutdown.
// The prompt is escaped when shown.
func (a *Approvals) PersonChannel(ctx context.Context) func(prompt string) (string, error) {
	return func(prompt string) (string, error) { return a.ask(ctx, prompt) }
}

func (a *Approvals) ask(ctx context.Context, prompt string) (string, error) {
	req := &approvalRequest{id: newLoginSecret(), prompt: prompt, answer: make(chan approvalAnswer, 1)}
	a.mu.Lock()
	if a.cur != nil {
		a.mu.Unlock()
		return "", ErrApprovalBusy
	}
	a.cur = req
	a.mu.Unlock()

	timer := time.NewTimer(a.timeout)
	defer timer.Stop()
	var kind LoginEventKind
	var err error
	select {
	case ans := <-req.answer:
		return ans.passphrase, ans.err
	case <-timer.C:
		kind, err = ApprovalExpired, ErrApprovalTimeout
	case <-ctx.Done():
		kind, err = ApprovalCancelled, ctx.Err()
	}
	a.mu.Lock()
	if a.cur != req { // an answer came in at the same moment and wins
		a.mu.Unlock()
		ans := <-req.answer
		return ans.passphrase, ans.err
	}
	a.cur = nil
	a.mu.Unlock()
	a.record(kind, "")
	return "", err
}

// take removes the waiting request if id names it.
func (a *Approvals) take(id string) *approvalRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cur == nil || a.cur.id != id {
		return nil
	}
	req := a.cur
	a.cur = nil
	return req
}

func (a *Approvals) record(kind LoginEventKind, remote string) {
	if a.audit != nil {
		a.audit.Record(LoginEvent{Kind: kind, Time: a.now(), Remote: remote})
	}
}

// sessionOK refuses a request without a session. A server with LoginOff serves every route without one, and
// then neither the session nor its CSRF token would guard an approval.
func sessionOK(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := SessionFrom(r); !ok {
		Error(w, http.StatusForbidden)
		return false
	}
	return true
}

func (a *Approvals) show(w http.ResponseWriter, r *http.Request) {
	if !sessionOK(w, r) {
		return
	}
	a.render(w, r, http.StatusOK, false, false)
}

func (a *Approvals) submit(w http.ResponseWriter, r *http.Request) {
	if !sessionOK(w, r) {
		return
	}
	id := r.PostFormValue(approvalRequestField)
	declined := r.PostFormValue("decision") == approvalDeclineValue
	pass := r.PostFormValue(approvalPassField)
	if !declined && pass == "" {
		a.mu.Lock()
		same := a.cur != nil && a.cur.id == id
		a.mu.Unlock()
		a.render(w, r, http.StatusUnprocessableEntity, !same, same)
		return
	}
	req := a.take(id)
	if req == nil {
		a.render(w, r, http.StatusConflict, true, false)
		return
	}
	if declined {
		a.record(ApprovalDeclined, remoteHost(r))
		req.answer <- approvalAnswer{err: ErrApprovalDeclined}
	} else {
		a.record(ApprovalSubmitted, remoteHost(r))
		req.answer <- approvalAnswer{passphrase: pass}
	}
	_ = Redirect(w, r, a.path, http.StatusSeeOther)
}

func (a *Approvals) render(w http.ResponseWriter, r *http.Request, status int, stale, empty bool) {
	a.mu.Lock()
	cur := a.cur
	a.mu.Unlock()
	v := struct {
		Nonce, Field, Token, Action, Prompt, Request string
		Pending, Stale, Empty                        bool
	}{Nonce: Nonce(r), Field: CSRFField, Token: CSRFToken(r), Action: a.path, Stale: stale, Empty: empty}
	if cur != nil {
		v.Pending, v.Prompt, v.Request = true, cur.prompt, cur.id
	}
	var buf bytes.Buffer
	if err := approvalPage.Execute(&buf, v); err != nil {
		Error(w, http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
