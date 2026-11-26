package web

import (
	"errors"
	"net/http"
	"net/url"
)

// maxNext bounds the target a confirmation carries.
const maxNext = 512

// ReauthPath is the path of the page that asks the person to confirm the sign-in again.
func (p *Pages) ReauthPath() string { return p.prefix + "/reauth" }

// RequireRecent wraps the handler of a route that is not public, such as one that unlocks the vault. The
// handler runs only if the session was signed in or confirmed within ReauthWithin. Otherwise a GET or HEAD
// request goes to the confirmation page and back afterwards, and any other method gets 403 without the
// handler running. Without a session the route is refused, also on a server without sign-in. The check is
// no protection for a route that is Public, which has no session.
func (p *Pages) RequireRecent(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := SessionFrom(r)
		if !ok {
			Error(w, http.StatusForbidden)
			return
		}
		if p.auth.sessions.now().Sub(s.SignedIn) <= p.within {
			h.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			Error(w, http.StatusForbidden)
			return
		}
		target := p.ReauthPath() + "?next=" + url.QueryEscape(r.URL.RequestURI())
		_ = Redirect(w, r, target, http.StatusSeeOther)
	})
}

// next returns target if it is a path on this server, else the fixed home: no scheme, no host, never a
// target a browser could read as another site.
func (p *Pages) next(target string) string {
	if len(target) > maxNext || !localPath(target) {
		return p.home
	}
	return target
}

func (p *Pages) reauthView(r *http.Request, next string, failed bool) loginView {
	if next == p.home {
		next = "" // the fixed home needs no field
	}
	return loginView{Title: "Confirm sign-in", Button: "Confirm", Action: p.ReauthPath(), Next: next,
		Hint:  "This step needs a recent sign-in. Enter the one-time code shown in the terminal where the program runs.",
		Token: CSRFToken(r), Failed: failed}
}

func (p *Pages) showReauth(w http.ResponseWriter, r *http.Request) {
	if _, ok := SessionFrom(r); !ok {
		Error(w, http.StatusForbidden)
		return
	}
	p.execute(w, r, http.StatusOK, p.reauthView(r, p.next(r.URL.Query().Get("next")), false))
}

// submitReauth runs behind the session gate, which has already checked the CSRF token of the session.
func (p *Pages) submitReauth(w http.ResponseWriter, r *http.Request) {
	next := p.next(r.PostFormValue("next"))
	switch err := p.auth.Reauth(r); {
	case err == nil:
		_ = Redirect(w, r, next, http.StatusSeeOther)
	case errors.Is(err, ErrSignInRefused):
		p.execute(w, r, http.StatusForbidden, p.reauthView(r, next, true))
	default:
		Error(w, http.StatusInternalServerError)
	}
}
