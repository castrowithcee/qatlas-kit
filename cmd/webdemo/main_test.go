package main

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuffer struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

// waitFor polls the output until re matches; the deadline only guards against a hang.
func waitFor(t *testing.T, out *syncBuffer, re *regexp.Regexp) []string {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if m := re.FindAllStringSubmatch(out.String(), -1); m != nil {
			return m[len(m)-1]
		}
	}
	t.Fatalf("no %v in output %q", re, out.String())
	return nil
}

func TestSignInAndOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stdin, feed := io.Pipe()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- run(ctx, nil, stdin, out) }()
	t.Cleanup(func() {
		cancel()
		feed.Close()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	})

	base := waitFor(t, out, regexp.MustCompile(`Open (http://localhost:\d+)`))[1]
	code := waitFor(t, out, regexp.MustCompile(`One-time code: ([0-9A-Z]{4}-[0-9A-Z]{4})`))[1]

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	status := func(resp *http.Response, err error) (int, string) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	tokenRe := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	token := func(body string) string {
		t.Helper()
		m := tokenRe.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no token in %q", body)
		}
		return m[1]
	}
	// signIn posts the form of the sign-in page the way a browser does.
	signIn := func(code string) (int, string) {
		_, page := status(client.Get(base + "/login"))
		return status(client.PostForm(base+"/login", url.Values{"code": {code}, "csrf_token": {token(page)}}))
	}

	_, body := status(client.Get(base + "/"))
	if !strings.Contains(body, "One-time code") {
		t.Fatalf("an unsigned-in visit does not end at the sign-in page: %q", body)
	}
	if got, _ := status(client.PostForm(base+"/login", url.Values{"code": {code}})); got != http.StatusForbidden {
		t.Errorf("sign-in without token: status %d", got)
	}
	if got, _ := signIn("2222-2222"); got != http.StatusForbidden {
		t.Errorf("wrong code: status %d", got)
	}
	got, body := signIn(code)
	if got != http.StatusOK || !strings.Contains(body, "Signed in") ||
		!strings.Contains(body, `action="/logout"`) {
		t.Fatalf("sign-in: status %d, body %q", got, body)
	}
	home := token(body)
	if _, body := status(client.Get(base + "/sensitive")); !strings.Contains(body, "Sensitive step") {
		t.Errorf("a fresh sign-in does not reach the sensitive step: %q", body)
	}
	if got, _ := signIn(code); got != http.StatusForbidden {
		t.Errorf("a used code: status %d", got)
	}

	feed.Write([]byte("\n"))
	waitFor(t, out, regexp.MustCompile(`New one-time code: ([0-9A-Z]{4}-[0-9A-Z]{4})`))

	if got, _ := status(client.PostForm(base+"/logout", nil)); got != http.StatusForbidden {
		t.Errorf("sign-out without token: status %d", got)
	}
	if _, body := status(client.PostForm(base+"/logout", url.Values{"csrf_token": {home}})); !strings.Contains(body, "One-time code") {
		t.Errorf("sign-out does not end at the sign-in page")
	}
	if _, body := status(client.Get(base + "/")); strings.Contains(body, "Signed in") {
		t.Error("session survives the sign-out")
	}
	for _, w := range []string{"login-failed", "login from", "logout", "csrf-refused"} {
		waitFor(t, out, regexp.MustCompile(`protocol: (`+w+`)`))
	}
	if strings.Count(out.String(), code) != 1 {
		t.Errorf("the code is printed other than once: %q", out.String())
	}
}

func TestUnlockDemoVault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stdin, feed := io.Pipe()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- run(ctx, nil, stdin, out) }()
	stopped := false
	stop := func() {
		stopped = true
		cancel()
		feed.Close()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	}
	t.Cleanup(func() {
		if !stopped {
			stop()
		}
	})
	base := waitFor(t, out, regexp.MustCompile(`Open (http://localhost:\d+)`))[1]
	code := waitFor(t, out, regexp.MustCompile(`One-time code: ([0-9A-Z]{4}-[0-9A-Z]{4})`))[1]
	pass := waitFor(t, out, regexp.MustCompile(`Demo vault passphrase: ([a-z2-7]+)`))[1]

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	read := func(resp *http.Response, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	tokenRe := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	token := func(body string) string {
		t.Helper()
		m := tokenRe.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no token in %q", body)
		}
		return m[1]
	}
	page := read(client.Get(base + "/login"))
	read(client.PostForm(base+"/login", url.Values{"code": {code}, "csrf_token": {token(page)}}))

	form := read(client.Get(base + "/unlock"))
	if !strings.Contains(form, `type="password"`) || !strings.Contains(form, `autocomplete="new-password"`) {
		t.Fatalf("no passphrase field: %q", form)
	}
	body := read(client.PostForm(base+"/unlock", url.Values{"csrf_token": {token(form)}, "passphrase": {"not-the-one"}}))
	if !strings.Contains(body, `role="alert"`) || strings.Contains(body, "not-the-one") {
		t.Errorf("wrong passphrase: %q", body)
	}
	body = read(client.PostForm(base+"/unlock", url.Values{"csrf_token": {token(form)}, "passphrase": {pass}}))
	if !strings.Contains(body, "The vault is unlocked") || strings.Contains(body, pass) {
		t.Errorf("unlock: %q", body)
	}
	waitFor(t, out, regexp.MustCompile(`demo vault: (unlocked)`))
	for _, w := range []string{"vault-unlock-failed", "vault-unlock"} {
		waitFor(t, out, regexp.MustCompile(`protocol: (`+w+`) from`))
	}
	if strings.Contains(out.String(), "demo vault: locked") {
		t.Error("the demo vault locked before it was told to")
	}
	home := read(client.Get(base + "/"))
	if !strings.Contains(home, "Lock the demo vault") {
		t.Fatalf("no lock form while unlocked: %q", home)
	}
	read(client.PostForm(base+"/lock", url.Values{"csrf_token": {token(home)}}))
	waitFor(t, out, regexp.MustCompile(`demo vault: (locked)`))
	waitFor(t, out, regexp.MustCompile(`protocol: (vault-lock) from`))
	if home = read(client.Get(base + "/")); strings.Contains(home, "Lock the demo vault") {
		t.Error("lock form while locked")
	}
	if form = read(client.Get(base + "/unlock")); !strings.Contains(form, `type="password"`) {
		t.Fatalf("no passphrase field after the lock: %q", form)
	}
	// Unlock again and end the program: Close locks the vault.
	read(client.PostForm(base+"/unlock", url.Values{"csrf_token": {token(form)}, "passphrase": {pass}}))
	stop()
	if n := strings.Count(out.String(), "demo vault: locked"); n != 2 {
		t.Errorf("%d locks, want the one from the page and the one at the end: %q", n, out.String())
	}
	if strings.Count(out.String(), pass) != 1 {
		t.Errorf("the passphrase is printed other than once: %q", out.String())
	}
}

func TestRejectsArguments(t *testing.T) {
	err := run(context.Background(), []string{"extra"}, strings.NewReader(""), io.Discard)
	if _, ok := err.(usageError); !ok {
		t.Errorf("error %v", err)
	}
}
