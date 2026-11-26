package web

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

const (
	// PassphraseField is the name of the form field that carries the passphrase.
	PassphraseField = "passphrase"

	// unlockRecent is how fresh the sign-in must be for the page and its POST. It is fixed, and never
	// longer than ReauthWithin: long enough to fetch a passphrase from where the person keeps it, short
	// enough that a session left open does not reach the field.
	unlockRecent = 2 * time.Minute

	maxPassphrase = 1024 // bytes; a longer value is refused without a call
)

const (
	// VaultUnlocked, VaultUnlockFailed and VaultUnlockThrottled report an attempt to unlock; a throttled
	// attempt is refused without calling the application. VaultLocked reports the lock by Lock or
	// Close; its Remote is empty then.
	VaultUnlocked        LoginEventKind = "vault-unlock"
	VaultUnlockFailed    LoginEventKind = "vault-unlock-failed"
	VaultUnlockThrottled LoginEventKind = "vault-unlock-throttled"
	VaultLocked          LoginEventKind = "vault-lock"
)

// UnlockOptions configure an Unlocker. Without an Unlocker a server has no way to unlock a vault.
type UnlockOptions struct {
	// Unlock receives the passphrase and unlocks the vault, for example through vault.Unlock or a
	// vaultproc process. Any error means the passphrase was not accepted. It must not log the
	// passphrase, put it in an error, or keep it. Required.
	Unlock func(passphrase string) error
	// Lock locks the vault again. It runs only on Lock and on Close, never by itself: when the vault locks
	// otherwise is up to the application. If it fails, the Unlocker keeps the vault as unlocked, returns
	// the error and does not retry. Required.
	Lock func() error
	// Window is how long a failed attempt counts; 0 means 10 minutes.
	Window time.Duration
	// Attempts is the failed attempts from all addresses together per Window; 0 means 5. It is separate
	// from the throttle of the sign-in, so a wrong guess here never costs a sign-in and the reverse.
	Attempts int
	// Audit receives the events; nil keeps no protocol.
	Audit LoginAudit
}

// Unlocker offers the unlocking of a vault in the browser. It has no lock time of its own and locks only
// when told to. Make one with NewUnlocker, mount Routes next to those of the pages, and call Close when
// the server ends.
type Unlocker struct {
	pages  *Pages
	unlock func(string) error
	lock   func() error
	audit  LoginAudit

	// now is replaced by tests.
	now func() time.Time

	failed *ratelimit.Counter

	attempt sync.Mutex // serializes unlock and lock; taken before mu
	mu      sync.Mutex // guards open
	open    bool
}

// NewUnlocker checks opts. It needs the Pages of the server, which ask for a fresh sign-in.
func NewUnlocker(pages *Pages, opts UnlockOptions) (*Unlocker, error) {
	if pages == nil || opts.Unlock == nil || opts.Lock == nil {
		return nil, errors.New("web: an Unlocker needs Pages, Unlock and Lock")
	}
	u := &Unlocker{pages: pages, unlock: opts.Unlock, lock: opts.Lock, audit: opts.Audit, now: time.Now}
	win, n := opts.Window, opts.Attempts
	if win <= 0 {
		win = 10 * time.Minute
	}
	if n <= 0 {
		n = 5
	}
	u.failed = ratelimit.NewCounter(n, ratelimit.Sliding(win), func() time.Time { return u.now() })
	return u, nil
}

// UnlockPath is the path of the page with the passphrase field.
func (u *Unlocker) UnlockPath() string { return u.pages.prefix + "/unlock" }

// Routes returns GET and POST on UnlockPath. Both need a session whose sign-in is at most two minutes old
// (or ReauthWithin, if that is shorter): an older one is sent to the confirmation page, or gets 403 for
// the POST.
func (u *Unlocker) Routes() []Route {
	within := min(unlockRecent, u.pages.within)
	return []Route{
		{Pattern: "GET " + u.UnlockPath(), Handler: u.pages.requireWithin(http.HandlerFunc(u.show), within)},
		{Pattern: "POST " + u.UnlockPath(), Handler: u.pages.requireWithin(http.HandlerFunc(u.submit), within)},
	}
}

// Unlocked reports whether the vault was unlocked through the Unlocker and not locked since.
func (u *Unlocker) Unlocked() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.open
}

// Lock locks the vault now, if it is unlocked, and reports the lock.
func (u *Unlocker) Lock() error {
	u.attempt.Lock()
	defer u.attempt.Unlock()
	return u.lockLocked()
}

// Close locks the vault if it is unlocked. Call it after Serve returns, so the vault
// does not outlive the server. The Unlocker is not meant for use afterwards.
func (u *Unlocker) Close() error { return u.Lock() }

func (u *Unlocker) lockLocked() error {
	if !u.Unlocked() {
		return nil
	}
	if err := u.lock(); err != nil {
		return err
	}
	u.mu.Lock()
	u.open = false
	u.mu.Unlock()
	u.record(VaultLocked, "")
	return nil
}

func (u *Unlocker) record(kind LoginEventKind, remote string) {
	if u.audit != nil {
		u.audit.Record(LoginEvent{Kind: kind, Time: u.now(), Remote: remote})
	}
}

var unlockPage = template.Must(template.New("unlock").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>Unlock vault</title>
<style nonce="{{.Nonce}}">
` + pageCSS + `input[type="password"] { letter-spacing: normal; }
</style>
</head>
<body>
<main>
<h1>Unlock vault</h1>
{{if .Open}}<p>The vault is unlocked.</p>
<p><a href="{{.Home}}">Back</a></p>
{{else}}<p class="hint" id="hint">Enter the passphrase of the vault. The browser is asked not to save or fill it, and the program does not keep it. A browser extension can still read what you type here, so unlock only in a browser you trust.</p>
<form method="post" action="{{.Action}}" autocomplete="off">
<input type="hidden" name="{{.Field}}" value="{{.Token}}">
<label for="passphrase">Passphrase</label>
<input id="passphrase" name="passphrase" type="password" autocomplete="new-password" data-1p-ignore data-lpignore="true" data-bwignore autocapitalize="off" spellcheck="false" autofocus required aria-describedby="hint{{if .Failed}} error{{end}}"{{if .Failed}} aria-invalid="true"{{end}}>
{{if .Failed}}<p class="error" id="error" role="alert">The vault was not unlocked. Check the passphrase and try again. After several wrong attempts, wait a few minutes.</p>
{{end}}<button type="submit">Unlock</button>
</form>
{{end}}</main>
</body>
</html>
`))

// render never puts the submitted passphrase on the page.
func (u *Unlocker) render(w http.ResponseWriter, r *http.Request, status int, failed bool) {
	var buf bytes.Buffer
	err := unlockPage.Execute(&buf, struct {
		Nonce, Field, Action, Token, Home string
		Open, Failed                      bool
	}{Nonce(r), CSRFField, u.UnlockPath(), CSRFToken(r), u.pages.home, u.Unlocked(), failed})
	if err != nil {
		Error(w, http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (u *Unlocker) show(w http.ResponseWriter, r *http.Request) {
	u.render(w, r, http.StatusOK, false)
}

// submit runs behind the session gate, which has checked the CSRF token, and behind the fresh sign-in.
func (u *Unlocker) submit(w http.ResponseWriter, r *http.Request) {
	remote := remoteHost(r)
	u.attempt.Lock()
	defer u.attempt.Unlock()
	if u.Unlocked() {
		u.render(w, r, http.StatusOK, false)
		return
	}
	pass := r.PostFormValue(PassphraseField)
	// Drop the parsed copies so the value does not outlive this handler through the request.
	delete(r.PostForm, PassphraseField)
	delete(r.Form, PassphraseField)
	if pass == "" || len(pass) > maxPassphrase {
		u.render(w, r, http.StatusForbidden, true)
		return
	}
	res, err := u.failed.Reserve(1)
	if err != nil {
		u.record(VaultUnlockThrottled, remote)
		u.render(w, r, http.StatusForbidden, true)
		return
	}
	if err := u.unlock(pass); err != nil {
		u.record(VaultUnlockFailed, remote)
		u.render(w, r, http.StatusForbidden, true)
		return
	}
	res.Release()
	u.mu.Lock()
	u.open = true
	u.mu.Unlock()
	u.record(VaultUnlocked, remote)
	_ = Redirect(w, r, u.UnlockPath(), http.StatusSeeOther)
}
