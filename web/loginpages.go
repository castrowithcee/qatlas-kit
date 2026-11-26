package web

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"
)

// pageCSS is shared by the pages of this package, so they keep one look.
const pageCSS = `:root { --bg: #fff; --text: #1a1a1a; --muted: #4a4a4a; --border: #767676; --accent: #0b5cad; --on-accent: #fff; --error: #a4000f; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #121212; --text: #ececec; --muted: #b8b8b8; --border: #8a8a8a; --accent: #8ab4f8; --on-accent: #0b1220; --error: #ff9a9a; }
}
* { box-sizing: border-box; }
html { height: 100%; background: var(--bg); }
body { margin: 0; min-height: 100%; background: var(--bg); color: var(--text); font: 1rem/1.5 system-ui, sans-serif; }
main { max-width: 28rem; margin: 0 auto; padding: 1rem; }
p { overflow-wrap: anywhere; }
.hint { color: var(--muted); }
label { display: block; margin: 1.5rem 0 .25rem; font-weight: 600; }
input, button { font: inherit; min-height: 2.75rem; border-radius: .25rem; }
input { width: 100%; padding: .5rem; border: 2px solid var(--border); background: var(--bg); color: var(--text); letter-spacing: .1em; }
input[aria-invalid="true"] { border-color: var(--error); }
button { min-width: 2.75rem; margin-top: 1rem; padding: .5rem 1.5rem; border: 2px solid var(--accent); background: var(--accent); color: var(--on-accent); font-weight: 600; cursor: pointer; }
:focus-visible { outline: 3px solid var(--accent); outline-offset: 2px; }
.error { margin: .5rem 0 0; color: var(--error); font-weight: 600; }
`

var loginPage = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>{{.Title}}</title>
<style nonce="{{.Nonce}}">
` + pageCSS + `</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p class="hint" id="hint">{{.Hint}}</p>
<form method="post" action="{{.Action}}">
<input type="hidden" name="{{.Field}}" value="{{.Token}}">
{{if .Next}}<input type="hidden" name="next" value="{{.Next}}">
{{end}}<label for="code">One-time code</label>
<input id="code" name="code" type="text" autocomplete="one-time-code" autocapitalize="characters" spellcheck="false" autofocus required aria-describedby="hint{{if .Failed}} error{{end}}"{{if .Failed}} aria-invalid="true"{{end}}>
{{if .Failed}}<p class="error" id="error" role="alert">The code was not accepted. Check it, or ask for a new one in the terminal, and try again.</p>
{{end}}{{if .Expired}}<p class="error" id="expired" role="alert">This page was out of date. Enter the code again.</p>
{{end}}<button type="submit">{{.Button}}</button>
</form>
</main>
</body>
</html>
`))

// PagesOptions configure Pages.
type PagesOptions struct {
	// Auth checks the codes and ends the sessions. Required.
	Auth *Authenticator
	// Prefix is put before the paths of the pages, such as "/auth" for "/auth/login". It is empty or
	// an absolute path without a trailing slash, query or wildcard.
	Prefix string
	// ReauthWithin is how recent the sign-in of a session must be for a route marked with RequireRecent;
	// 0 means 5 minutes.
	ReauthWithin time.Duration
	// Home is where a browser goes after signing in; it is fixed, never taken from the request. Empty
	// means "/". It must be an absolute path on this server.
	Home string
}

// Pages serves the sign-in page and the sign-out of a Server, plain and accessible, with the CSP nonce of
// the server. Its routes are public; mount Routes next to the routes of the application.
type Pages struct {
	auth   *Authenticator
	prefix string
	home   string
	within time.Duration
}

// NewPages checks opts. It also makes the sessions of opts.Auth send an unsigned-in GET or HEAD request
// for a route that is not public to the sign-in page instead of answering 401, because a browser has
// nothing else to do there. Other methods stay at 401. Call it before the server starts.
func NewPages(opts PagesOptions) (*Pages, error) {
	if opts.Auth == nil {
		return nil, errors.New("web: Pages needs an Authenticator")
	}
	p := &Pages{auth: opts.Auth, prefix: opts.Prefix, home: opts.Home, within: opts.ReauthWithin}
	if p.within <= 0 {
		p.within = 5 * time.Minute
	}
	if p.home == "" {
		p.home = "/"
	}
	if !localPath(p.home) {
		return nil, errors.New("web: Home is not an absolute path on this server")
	}
	if p.prefix != "" && (!localPath(p.prefix) || strings.HasSuffix(p.prefix, "/") ||
		strings.ContainsAny(p.prefix, "{}?#%")) {
		return nil, errors.New("web: Prefix is not an absolute path without trailing slash or wildcard")
	}
	opts.Auth.sessions.loginPath = p.LoginPath()
	return p, nil
}

// LoginPath is the path of the sign-in page.
func (p *Pages) LoginPath() string { return p.prefix + "/login" }

// LogoutPath is the path that ends the session. It takes POST only, so a tool offers a form
// <form method="post" action="..."> with a button.
func (p *Pages) LogoutPath() string { return p.prefix + "/logout" }

// Routes returns the routes of the pages. GET and POST on LoginPath and POST on LogoutPath are public; GET
// and POST on ReauthPath need a session.
func (p *Pages) Routes() []Route {
	return []Route{
		{Pattern: "GET " + p.LoginPath(), Public: true, Handler: http.HandlerFunc(p.showLogin)},
		{Pattern: "POST " + p.LoginPath(), Public: true, Handler: http.HandlerFunc(p.submitLogin)},
		{Pattern: "POST " + p.LogoutPath(), Public: true, Handler: http.HandlerFunc(p.logout)},
		{Pattern: "GET " + p.ReauthPath(), Handler: http.HandlerFunc(p.showReauth)},
		{Pattern: "POST " + p.ReauthPath(), Handler: http.HandlerFunc(p.submitReauth)},
	}
}

func (p *Pages) showLogin(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, http.StatusOK, false, false)
}

// loginSecret returns the secret of the page's cookie, or "" if the request has none of the right form.
func loginSecret(r *http.Request, strict bool) string {
	if ck, err := r.Cookie(loginCookieName(strict)); err == nil && len(ck.Value) == idLen {
		return ck.Value
	}
	return ""
}

func (p *Pages) submitLogin(w http.ResponseWriter, r *http.Request) {
	// Checked before the credentials, so a forged form neither uses up the throttle nor tests a code.
	if sec := loginSecret(r, strictFor(r)); sec == "" || !csrfMatches(r, csrfOf("login", sec)) {
		p.auth.record(CSRFRefused, remoteHost(r))
		p.render(w, r, http.StatusForbidden, false, true)
		return
	}
	switch err := p.auth.SignIn(w, r); {
	case err == nil:
		_ = Redirect(w, r, p.home, http.StatusSeeOther)
	case errors.Is(err, ErrSignInRefused):
		// Wrong and throttled attempts look the same.
		p.render(w, r, http.StatusForbidden, true, false)
	default:
		Error(w, http.StatusInternalServerError)
	}
}

func (p *Pages) logout(w http.ResponseWriter, r *http.Request) {
	if id, ok := p.auth.sessions.cookieID(r, strictFor(r)); ok && !csrfMatches(r, csrfOf("session", id)) {
		p.auth.sessions.refuseCSRF(w, r)
		return
	}
	if err := p.auth.SignOut(w, r); err != nil {
		Error(w, http.StatusInternalServerError)
		return
	}
	_ = Redirect(w, r, p.LoginPath(), http.StatusSeeOther)
}

// render never puts the submitted code on the page. The token is bound to a cookie of the page itself.
func (p *Pages) render(w http.ResponseWriter, r *http.Request, status int, failed, expired bool) {
	strict := strictFor(r)
	sec := loginSecret(r, strict)
	if sec == "" {
		sec = newLoginSecret()
		http.SetCookie(w, namedCookie(loginCookieName(strict), sec, strict))
	}
	p.execute(w, r, status, loginView{Title: "Sign in", Button: "Sign in", Action: p.LoginPath(),
		Hint:  "Enter the one-time code shown in the terminal where the program runs.",
		Token: csrfOf("login", sec), Failed: failed, Expired: expired})
}

// loginView is what the sign-in and the confirmation page show; both are the same form.
type loginView struct {
	Title, Hint, Button, Action, Token, Next string
	Failed, Expired                          bool
}

func (p *Pages) execute(w http.ResponseWriter, r *http.Request, status int, v loginView) {
	var buf bytes.Buffer
	err := loginPage.Execute(&buf, struct {
		loginView
		Nonce, Field string
	}{v, Nonce(r), CSRFField})
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
