package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/redact"
	"github.com/castrowithcee/qatlas-kit/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "webdemo:", err)
		var usage usageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

type usageError struct{ error }

var homePage = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>Signed in</title>
<style nonce="{{.Nonce}}">
body { font: 1rem/1.5 system-ui, sans-serif; margin: 0 auto; max-width: 28rem; padding: 1rem; }
button { font: inherit; min-height: 2.75rem; padding: .5rem 1.5rem; }
</style>
</head>
<body>
<main>
<h1>Signed in</h1>
<p>This private page needs a session. The session began at {{.Since}}.</p>
<p><a href="/sensitive">Sensitive step</a>, which asks for the code again when the sign-in is older than five minutes.</p>
<p><a href="{{.Unlock}}">Unlock the demo vault</a>, which asks for the code again when the sign-in is older than two minutes.</p>
{{if .Unlocked}}<form method="post" action="/lock"><input type="hidden" name="{{.Field}}" value="{{.Token}}"><button type="submit">Lock the demo vault</button></form>
{{end}}<form method="post" action="{{.Logout}}"><input type="hidden" name="{{.Field}}" value="{{.Token}}"><button type="submit">Sign out</button></form>
</main>
</body>
</html>
`))

var sensitivePage = template.Must(template.New("sensitive").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>Sensitive step</title>
<style nonce="{{.Nonce}}">
body { font: 1rem/1.5 system-ui, sans-serif; margin: 0 auto; max-width: 28rem; padding: 1rem; }
</style>
</head>
<body>
<main>
<h1>Sensitive step</h1>
<p>You confirmed your sign-in at {{.Since}}.</p>
<p><a href="/">Back</a></p>
</main>
</body>
</html>
`))

// printer serializes writes, because requests and the terminal reader run at the same time.
type printer struct {
	mu  sync.Mutex
	out io.Writer
}

func (p *printer) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.Write(b)
}

// Record prints a protocol entry, which carries no code and no session ID.
func (p *printer) Record(e web.LoginEvent) {
	fmt.Fprintf(p, "protocol: %s from %s at %s\n", e.Kind, e.Remote, e.Time.Format(time.TimeOnly))
}

func run(ctx context.Context, args []string, in io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("webdemo", flag.ContinueOnError)
	port := flags.Int("port", 0, "loopback port; 0 picks a free one")
	if err := flags.Parse(args); err != nil {
		return usageError{err}
	}
	if flags.NArg() != 0 {
		return usageError{errors.New("no arguments are accepted")}
	}
	out := &printer{out: stdout}

	var red redact.Redactor
	code := web.NewOnceCode(web.OnceCodeOptions{Redactor: &red})
	sessions := web.NewCookieSessions(web.SessionOptions{Audit: out})
	auth, err := web.NewAuthenticator(web.AuthOptions{Sessions: sessions, Method: code, Audit: out})
	if err != nil {
		return err
	}
	pages, err := web.NewPages(web.PagesOptions{Auth: auth})
	if err != nil {
		return err
	}
	// The demo vault is only a flag. Its passphrase is made up for this run and printed below.
	var raw [10]byte
	_, _ = rand.Read(raw[:])
	passphrase := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]))
	red.Add(passphrase)
	unlocker, err := web.NewUnlocker(pages, web.UnlockOptions{
		Audit: out,
		Unlock: func(p string) error {
			if subtle.ConstantTimeCompare([]byte(p), []byte(passphrase)) != 1 {
				return errors.New("wrong passphrase")
			}
			fmt.Fprintln(out, "demo vault: unlocked")
			return nil
		},
		Lock: func() error { fmt.Fprintln(out, "demo vault: locked"); return nil },
	})
	if err != nil {
		return err
	}
	defer unlocker.Close()
	unlockPath := unlocker.UnlockPath()
	home := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := web.SessionFrom(r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = homePage.Execute(w, struct {
			Nonce, Since, Logout, Field, Token, Unlock string
			Unlocked                                   bool
		}{web.Nonce(r), sess.Created.Format(time.TimeOnly), pages.LogoutPath(), web.CSRFField, web.CSRFToken(r), unlockPath,
			unlocker.Unlocked()})
	})
	// The CSRF token is checked before the handler runs, as for the pages of the kit.
	lock := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := unlocker.Lock(); err != nil {
			web.Error(w, http.StatusInternalServerError)
			return
		}
		_ = web.Redirect(w, r, "/", http.StatusSeeOther)
	})
	sensitive := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := web.SessionFrom(r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = sensitivePage.Execute(w, struct{ Nonce, Since string }{web.Nonce(r), sess.SignedIn.Format(time.TimeOnly)})
	})
	srv, err := web.Listen(web.Options{Port: *port, Sessions: sessions, Redactor: &red, Log: out,
		Routes: append(append(pages.Routes(), unlocker.Routes()...), web.Route{Pattern: "GET /{$}", Handler: home},
			web.Route{Pattern: "POST /lock", Handler: lock},
			web.Route{Pattern: "GET /sensitive", Handler: pages.RequireRecent(sensitive)})})
	if err != nil {
		return err
	}

	// localhost is the name a person types; the server answers it as well as 127.0.0.1.
	fmt.Fprintf(out, "Open %s\nOne-time code: %s\n", strings.Replace(srv.URL(), "127.0.0.1", "localhost", 1), code.Generate())
	fmt.Fprintf(out, "Demo vault passphrase: %s\n", passphrase)
	fmt.Fprintln(out, "Press Enter for a new code, Ctrl+C to quit.")
	go func() {
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			fmt.Fprintf(out, "New one-time code: %s\n", code.Generate())
		}
	}()
	return srv.Serve(ctx)
}
