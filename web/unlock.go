package web

import (
	"bytes"
	"errors"
	"fmt"
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

	defaultVaultIdle = 5 * time.Minute
	maxPassphrase    = 1024 // bytes; a longer value is refused without a call
)

const (
	// VaultUnlocked, VaultUnlockFailed and VaultUnlockThrottled report an attempt to unlock; a throttled
	// attempt is refused without calling the application. VaultLocked reports the lock, whether by idle
	// time, Lock or Close; its Remote is empty then.
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
	// Lock locks the vault again. It runs when the vault was not used for Idle, on Lock and on Close. If it
	// fails, the Unlocker keeps the vault as unlocked and tries again after Idle. Required.
	Lock func() error
	// Idle is how long the vault may stay unlocked without use; 0 means 5 minutes. Use is a call of Touch.
	Idle time.Duration
	// Window is how long a failed attempt counts; 0 means 10 minutes.
	Window time.Duration
	// Attempts is the failed attempts from all addresses together per Window; 0 means 5. It is separate
	// from the throttle of the sign-in, so a wrong guess here never costs a sign-in and the reverse.
	Attempts int
	// Audit receives the events; nil keeps no protocol.
	Audit LoginAudit
}

// Unlocker offers the unlocking of a vault in the browser and locks it again after idle time. Make one with
// NewUnlocker, mount Routes next to those of the pages, and call Close when the server ends.
type Unlocker struct {
	pages  *Pages
	unlock func(string) error
	lock   func() error
	idle   time.Duration
	audit  LoginAudit

	// now and after are replaced by tests.
	now   func() time.Time
	after func(d time.Duration, f func()) (stop func())

	failed *ratelimit.Counter

	attempt sync.Mutex // serializes unlock, lock and the idle check; taken before mu
	mu      sync.Mutex // guards the fields below
	open    bool
	last    time.Time
	stop    func()
}

// NewUnlocker checks opts. It needs the Pages of the server, which ask for a fresh sign-in.
func NewUnlocker(pages *Pages, opts UnlockOptions) (*Unlocker, error) {
	if pages == nil || opts.Unlock == nil || opts.Lock == nil {
		return nil, errors.New("web: an Unlocker needs Pages, Unlock and Lock")
	}
	u := &Unlocker{pages: pages, unlock: opts.Unlock, lock: opts.Lock, idle: opts.Idle, audit: opts.Audit,
		now: time.Now, after: func(d time.Duration, f func()) func() {
			t := time.AfterFunc(d, f)
			return func() { t.Stop() }
		}}
	if u.idle <= 0 {
		u.idle = defaultVaultIdle
	}
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

// Touch reports that the vault was used, which postpones the idle lock. Only a call counts: a request to
// the page of the Unlocker is no use, so reloading it does not keep the vault open. A tool calls Touch
// wherever it reads the vault for a request. It does nothing while the vault is locked.
func (u *Unlocker) Touch() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.open {
		u.last = u.now()
	}
}

// Lock locks the vault now, if it is unlocked, and reports the lock.
func (u *Unlocker) Lock() error {
	u.attempt.Lock()
	defer u.attempt.Unlock()
	return u.lockLocked()
}

// Close locks the vault if it is unlocked and stops the timer. Call it after Serve returns, so the vault
// does not outlive the server. The Unlocker is not meant for use afterwards.
func (u *Unlocker) Close() error { return u.Lock() }

func (u *Unlocker) lockLocked() error {
	u.mu.Lock()
	open, stop := u.open, u.stop
	u.mu.Unlock()
	if !open {
		return nil
	}
	if stop != nil {
		stop()
	}
	if err := u.lock(); err != nil {
		u.mu.Lock()
		u.stop = u.after(u.idle, u.expire)
		u.mu.Unlock()
		return err
	}
	u.mu.Lock()
	u.open, u.stop = false, nil
	u.mu.Unlock()
	u.record(VaultLocked, "")
	return nil
}

// expire runs when the timer fires. A use since the timer was set moves the lock later.
func (u *Unlocker) expire() {
	u.attempt.Lock()
	defer u.attempt.Unlock()
	u.mu.Lock()
	if wait := u.idle - u.now().Sub(u.last); u.open && wait > 0 {
		u.stop = u.after(wait, u.expire)
		u.mu.Unlock()
		return
	}
	u.mu.Unlock()
	_ = u.lockLocked() // a failure is retried by lockLocked; the next request sees no difference
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
{{if .Open}}<p>The vault is unlocked. It locks again after {{.Idle}} without use.</p>
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
		Nonce, Field, Action, Token, Idle, Home string
		Open, Failed                            bool
	}{Nonce(r), CSRFField, u.UnlockPath(), CSRFToken(r), spanText(u.idle), u.pages.home, u.Unlocked(), failed})
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

func spanText(d time.Duration) string {
	if d == time.Minute {
		return "1 minute"
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	}
	return fmt.Sprintf("%d seconds", int(d.Round(time.Second)/time.Second))
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
	u.open, u.last = true, u.now()
	u.stop = u.after(u.idle, u.expire)
	u.mu.Unlock()
	u.record(VaultUnlocked, remote)
	_ = Redirect(w, r, u.UnlockPath(), http.StatusSeeOther)
}
