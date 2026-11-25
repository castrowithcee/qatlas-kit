package web

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

var errorPage = template.Must(template.New("error").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>{{.Status}} {{.Text}}</title>
</head>
<body>
<main>
<h1>{{.Text}}</h1>
<p>The server answered with status {{.Status}}.</p>
</main>
</body>
</html>
`))

// Error answers with an error page that names only the status. A status outside 400 to 599 becomes 500.
func Error(w http.ResponseWriter, status int) {
	text := http.StatusText(status)
	if status < 400 || status > 599 || text == "" {
		status, text = http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)
	}
	h := w.Header()
	h.Del("Content-Length")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = errorPage.Execute(w, struct {
		Status int
		Text   string
	}{status, text})
}

// statusPage replaces the plain-text answer ServeMux gives for an unknown path or method with Error.
type statusPage struct {
	http.ResponseWriter
	done bool
}

func (p *statusPage) WriteHeader(code int) {
	if !p.done {
		p.done = true
		Error(p.ResponseWriter, code)
	}
}

func (p *statusPage) Write(b []byte) (int, error) {
	p.WriteHeader(http.StatusNotFound)
	return len(b), nil
}

// ErrRedirectTarget reports a redirect to anything but a path on this server.
var ErrRedirectTarget = errors.New("web: redirect target is not a path on this server")

// Redirect sends a redirect with code 301, 302, 303, 307 or 308 to target, which must be an absolute path
// on this server such as "/items?page=2". Any other target is refused with ErrRedirectTarget and nothing
// is written, so a parameter can never send a browser elsewhere.
func Redirect(w http.ResponseWriter, r *http.Request, target string, code int) error {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
	default:
		return errors.New("web: not a redirect status")
	}
	if !localPath(target) {
		return ErrRedirectTarget
	}
	http.Redirect(w, r, target, code)
	return nil
}

func localPath(target string) bool {
	// Browsers read a backslash as a slash and drop tabs and line breaks, so "/\host" and "/\t/host" would
	// leave the server.
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.Contains(target, `\`) ||
		strings.ContainsFunc(target, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return false
	}
	u, err := url.Parse(target)
	return err == nil && u.Scheme == "" && u.Host == ""
}
