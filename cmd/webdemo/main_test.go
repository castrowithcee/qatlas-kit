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

	if _, body := status(client.Get(base + "/")); !strings.Contains(body, "One-time code") {
		t.Fatalf("an unsigned-in visit does not end at the sign-in page: %q", body)
	}
	if got, _ := status(client.PostForm(base+"/login", url.Values{"code": {"2222-2222"}})); got != http.StatusForbidden {
		t.Errorf("wrong code: status %d", got)
	}
	if got, body := status(client.PostForm(base+"/login", url.Values{"code": {code}})); got != http.StatusOK || !strings.Contains(body, "Signed in") ||
		!strings.Contains(body, `action="/logout"`) {
		t.Fatalf("sign-in: status %d, body %q", got, body)
	}
	if got, _ := status(client.PostForm(base+"/login", url.Values{"code": {code}})); got != http.StatusForbidden {
		t.Errorf("a used code: status %d", got)
	}

	feed.Write([]byte("\n"))
	waitFor(t, out, regexp.MustCompile(`New one-time code: ([0-9A-Z]{4}-[0-9A-Z]{4})`))

	if _, body := status(client.PostForm(base+"/logout", nil)); !strings.Contains(body, "One-time code") {
		t.Errorf("sign-out does not end at the sign-in page")
	}
	if _, body := status(client.Get(base + "/")); strings.Contains(body, "Signed in") {
		t.Error("session survives the sign-out")
	}
	for _, w := range []string{"login-failed", "login from", "logout"} {
		waitFor(t, out, regexp.MustCompile(`protocol: (`+w+`)`))
	}
	if strings.Count(out.String(), code) != 1 {
		t.Errorf("the code is printed other than once: %q", out.String())
	}
}

func TestRejectsArguments(t *testing.T) {
	err := run(context.Background(), []string{"extra"}, strings.NewReader(""), io.Discard)
	if _, ok := err.(usageError); !ok {
		t.Errorf("error %v", err)
	}
}
