package notify

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// invented builds a credential for a test; every value is made up.
func invented(role string) string { return "invented-" + role + "-not-real" }

var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

var testHeaders = WebhookHeaders{Event: "X-Demo-Event", Timestamp: "X-Demo-Timestamp", Signature: "X-Demo-Signature"}

var testMessage = Message{
	Event: "deploy.failed", Subject: "demo-app failed", Text: "step pull\nerror: pull failed",
	JSON: []byte(`{"event":"deploy.failed","project":"demo-app"}`),
}

func newSender(t *testing.T, opts Options) *Sender {
	t.Helper()
	if opts.Backoff == 0 {
		opts.Backoff = time.Millisecond
	}
	if opts.Interval == 0 {
		opts.Interval = time.Nanosecond
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return fixedNow }
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type request struct {
	path   string
	header http.Header
	body   []byte
}

// recorder answers every request with handler and keeps what it received.
type recorder struct {
	mu       sync.Mutex
	requests []request
}

func (r *recorder) all() []request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]request(nil), r.requests...)
}

func serve(t *testing.T, handler func(w http.ResponseWriter, n int)) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.requests = append(rec.requests, request{path: r.URL.EscapedPath(), header: r.Header.Clone(), body: body})
		n := len(rec.requests)
		rec.mu.Unlock()
		handler(w, n)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func status(code int) func(http.ResponseWriter, int) {
	return func(w http.ResponseWriter, _ int) { w.WriteHeader(code) }
}

func codeOfErr(t *testing.T, err error) output.Code {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("error %v (%T) is not an *Error", err, err)
	}
	return failure.Code
}

func TestWebhookPostsSignedJSON(t *testing.T) {
	srv, rec := serve(t, status(http.StatusNoContent))
	key := invented("hook-key")
	ch := Webhook{URL: srv.URL, Key: key, Headers: testHeaders}
	if err := newSender(t, Options{UserAgent: "demo-agent/1.0"}).Send(context.Background(), ch, testMessage); err != nil {
		t.Fatal(err)
	}
	got := rec.all()
	if len(got) != 1 {
		t.Fatalf("requests = %d", len(got))
	}
	r := got[0]
	if string(r.body) != string(testMessage.JSON) {
		t.Errorf("body = %s", r.body)
	}
	if r.header.Get("X-Demo-Timestamp") != "1767323045" || r.header.Get("X-Demo-Event") != "deploy.failed" ||
		r.header.Get("Content-Type") != "application/json" || r.header.Get("User-Agent") != "demo-agent/1.0" {
		t.Errorf("headers = %v", r.header)
	}
	err := Verify([]byte(key), r.header.Get("X-Demo-Timestamp"), r.header.Get("X-Demo-Signature"), r.body, fixedNow, 0)
	if err != nil {
		t.Errorf("the receiver refuses the signature: %v", err)
	}
	if strings.Contains(fmt.Sprint(r.header, string(r.body)), key) {
		t.Error("the signing key left the process")
	}
}

func TestWebhookRefusesIncompleteSettings(t *testing.T) {
	srv, rec := serve(t, status(http.StatusOK))
	key := invented("hook-key")
	cases := map[string]struct {
		ch   Webhook
		msg  Message
		code output.Code
	}{
		"no key":            {Webhook{URL: srv.URL, Headers: testHeaders}, testMessage, output.CodeConfigInvalid},
		"no signature name": {Webhook{URL: srv.URL, Key: key, Headers: WebhookHeaders{Timestamp: "X-T"}}, testMessage, output.CodeConfigInvalid},
		"bad header name":   {Webhook{URL: srv.URL, Key: key, Headers: WebhookHeaders{Timestamp: "X T", Signature: "X-S"}}, testMessage, output.CodeConfigInvalid},
		"no JSON":           {Webhook{URL: srv.URL, Key: key, Headers: testHeaders}, Message{Subject: "demo"}, output.CodeInvalidRequest},
		"invalid JSON":      {Webhook{URL: srv.URL, Key: key, Headers: testHeaders}, Message{Subject: "demo", JSON: []byte("{")}, output.CodeInvalidRequest},
		"too large JSON":    {Webhook{URL: srv.URL, Key: key, Headers: testHeaders}, Message{Subject: "demo", JSON: make([]byte, MaxJSON+1)}, output.CodeInvalidRequest},
		"empty message":     {Webhook{URL: srv.URL, Key: key, Headers: testHeaders}, Message{JSON: []byte("{}")}, output.CodeInvalidRequest},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := newSender(t, Options{}).Send(context.Background(), tc.ch, tc.msg)
			if code := codeOfErr(t, err); code != tc.code {
				t.Errorf("code = %s, want %s (%v)", code, tc.code, err)
			}
		})
	}
	if n := len(rec.all()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func TestSlackPostsEscapedPlainText(t *testing.T) {
	srv, rec := serve(t, status(http.StatusOK))
	msg := Message{Subject: "demo-app failed", Text: "<!channel> see <https://example.com|here> & more"}
	if err := newSender(t, Options{}).Send(context.Background(), Slack{URL: srv.URL + "/services/" + invented("slack-path")}, msg); err != nil {
		t.Fatal(err)
	}
	r := rec.all()[0]
	var body map[string]string
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatal(err)
	}
	want := "demo-app failed\n&lt;!channel&gt; see &lt;https://example.com|here&gt; &amp; more"
	if body["text"] != want || r.path != "/services/"+invented("slack-path") {
		t.Errorf("path %s, text %q", r.path, body["text"])
	}
}

func TestTelegramSendsToChat(t *testing.T) {
	srv, rec := serve(t, status(http.StatusOK))
	token := invented("bot") + ":part/with?specials"
	ch := Telegram{Token: token, ChatID: "-100123", BaseURL: srv.URL + "/"}
	if err := newSender(t, Options{}).Send(context.Background(), ch, testMessage); err != nil {
		t.Fatal(err)
	}
	r := rec.all()[0]
	if !strings.HasPrefix(r.path, "/bot") || !strings.HasSuffix(r.path, "/sendMessage") || strings.Count(r.path, "/") != 2 {
		t.Errorf("path = %s", r.path)
	}
	var body map[string]string
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatal(err)
	}
	if body["chat_id"] != "-100123" || body["text"] != "demo-app failed\nstep pull\nerror: pull failed" {
		t.Errorf("body = %v", body)
	}
}

func TestAddressesMustBeSecure(t *testing.T) {
	cases := map[string]Channel{
		"plain http webhook":  Webhook{URL: "http://example.com/hook", Key: invented("hook-key"), Headers: testHeaders},
		"plain http slack":    Slack{URL: "http://example.com/services/demo"},
		"user part":           Slack{URL: "https://demo:" + invented("pass") + "@example.com/services/demo"},
		"no host":             Slack{URL: "https:///services/demo"},
		"other scheme":        Slack{URL: "ftp://example.com/x"},
		"plain http telegram": Telegram{Token: invented("bot"), ChatID: "1", BaseURL: "http://example.com"},
		"telegram without id": Telegram{Token: invented("bot")},
	}
	for name, ch := range cases {
		t.Run(name, func(t *testing.T) {
			err := newSender(t, Options{}).Send(context.Background(), ch, testMessage)
			if code := codeOfErr(t, err); code != output.CodeConfigInvalid {
				t.Errorf("code = %s (%v)", code, err)
			}
			if strings.Contains(err.Error(), "example.com") {
				t.Errorf("the error names the address: %v", err)
			}
		})
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	target, reached := serve(t, status(http.StatusOK))
	srv, rec := serve(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	err := newSender(t, Options{}).Send(context.Background(), Slack{URL: srv.URL}, testMessage)
	if err == nil || codeOfErr(t, err) != output.CodeRuntime {
		t.Fatalf("err = %v", err)
	}
	if len(reached.all()) != 0 || len(rec.all()) != 1 {
		t.Errorf("redirect target reached %d times, origin %d times", len(reached.all()), len(rec.all()))
	}
}

func TestUntrustedCertificateIsRefusedAndNotRetried(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	attempts := 0
	s := newSender(t, Options{OnFailure: func(int, *Error) { attempts++ }})
	err := s.Send(context.Background(), Slack{URL: srv.URL}, testMessage)
	if code := codeOfErr(t, err); code != output.CodeTLS {
		t.Fatalf("code = %s (%v)", code, err)
	}
	if attempts != 1 || hits != 0 {
		t.Errorf("attempts = %d, requests = %d", attempts, hits)
	}

	roots := srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	trusted := newSender(t, Options{TLS: &tls.Config{RootCAs: roots}})
	if err := trusted.Send(context.Background(), Slack{URL: srv.URL}, testMessage); err != nil {
		t.Errorf("trusted root: %v", err)
	}
}

func TestTLSConfigThatSkipsVerificationIsRefused(t *testing.T) {
	_, err := New(Options{TLS: &tls.Config{InsecureSkipVerify: true}})
	if code := codeOfErr(t, err); code != output.CodeConfigInvalid {
		t.Errorf("code = %s", code)
	}
}

func TestTooLargeAnswerIsRefused(t *testing.T) {
	srv, rec := serve(t, func(w http.ResponseWriter, _ int) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxAnswer+1)))
	})
	err := newSender(t, Options{}).Send(context.Background(), Slack{URL: srv.URL}, testMessage)
	if code := codeOfErr(t, err); code != output.CodeRuntime || !strings.Contains(err.Error(), "size limit") {
		t.Errorf("code = %s (%v)", code, err)
	}
	if len(rec.all()) != 1 {
		t.Errorf("requests = %d", len(rec.all()))
	}
}

func TestTransientFailureIsRetried(t *testing.T) {
	srv, rec := serve(t, func(w http.ResponseWriter, n int) {
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
		}
	})
	var failures []int
	s := newSender(t, Options{OnFailure: func(attempt int, _ *Error) { failures = append(failures, attempt) }})
	if err := s.Send(context.Background(), Slack{URL: srv.URL}, testMessage); err != nil {
		t.Fatal(err)
	}
	if len(rec.all()) != 3 || fmt.Sprint(failures) != "[1 2]" {
		t.Errorf("requests = %d, failures = %v", len(rec.all()), failures)
	}
}

func TestRefusalIsNotRetried(t *testing.T) {
	cases := map[int]output.Code{
		http.StatusBadRequest:   output.CodeRuntime,
		http.StatusUnauthorized: output.CodeAuth,
		http.StatusForbidden:    output.CodePermission,
		http.StatusNotFound:     output.CodeNotFound,
	}
	for answer, want := range cases {
		t.Run(http.StatusText(answer), func(t *testing.T) {
			srv, rec := serve(t, status(answer))
			err := newSender(t, Options{}).Send(context.Background(), Slack{URL: srv.URL}, testMessage)
			if code := codeOfErr(t, err); code != want || len(rec.all()) != 1 {
				t.Errorf("code = %s, requests = %d", code, len(rec.all()))
			}
		})
	}
}

func TestLongRetryAfterEndsTheSend(t *testing.T) {
	srv, rec := serve(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	err := newSender(t, Options{}).Send(context.Background(), Slack{URL: srv.URL}, testMessage)
	if code := codeOfErr(t, err); code != output.CodeRateLimited || len(rec.all()) != 1 {
		t.Errorf("code = %s, requests = %d", code, len(rec.all()))
	}
}

func TestRequestsOfOneCredentialAreSpaced(t *testing.T) {
	srv, _ := serve(t, status(http.StatusOK))
	s := newSender(t, Options{Interval: 150 * time.Millisecond})
	start := time.Now()
	for range 2 {
		if err := s.Send(context.Background(), Slack{URL: srv.URL}, testMessage); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Errorf("two requests took %s", elapsed)
	}
}

func TestCancelledContextEndsTheSend(t *testing.T) {
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	srv, _ := serve(t, func(w http.ResponseWriter, _ int) {
		arrived <- struct{}{}
		<-release
	})
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-arrived
		cancel()
	}()
	attempts := 0
	s := newSender(t, Options{OnFailure: func(int, *Error) { attempts++ }})
	err := s.Send(ctx, Slack{URL: srv.URL}, testMessage)
	if !errors.Is(err, context.Canceled) || attempts != 0 {
		t.Errorf("err = %v, failed attempts = %d", err, attempts)
	}

	ended, stop := context.WithCancel(context.Background())
	stop()
	if err := s.Send(ended, Slack{URL: srv.URL}, testMessage); !errors.Is(err, context.Canceled) {
		t.Errorf("an ended context: %v", err)
	}
}

func TestCredentialsNeverReachErrorsOrLog(t *testing.T) {
	token := invented("bot")
	appSecret := invented("app-value")
	telegram, _ := serve(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"ok":false,"error_code":401,"description":"token %s and %s refused"}`, token, appSecret)
	})
	hook, _ := serve(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "no route for /services/%s", invented("slack-path"))
	})
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	var app redact.Redactor
	app.Add(appSecret)
	var log []string
	s := newSender(t, Options{Redactor: &app, OnFailure: func(attempt int, err *Error) {
		log = append(log, fmt.Sprintf("attempt %d: %s %+v", attempt, err, err.Original))
	}})
	channels := map[string]Channel{
		"telegram":    Telegram{Token: token, ChatID: "1", BaseURL: telegram.URL},
		"slack":       Slack{URL: hook.URL + "/services/" + invented("slack-path")},
		"unreachable": Slack{URL: closedURL + "/services/" + invented("slack-path")},
	}
	secrets := []string{token, appSecret, invented("slack-path")}
	for name, ch := range channels {
		err := s.Send(context.Background(), ch, Message{Subject: "demo", Text: "uses " + appSecret})
		var failure *Error
		if !errors.As(err, &failure) {
			t.Fatalf("%s: %v", name, err)
		}
		shown := fmt.Sprintf("%s %+v %v", failure, failure.Original, log)
		for _, secret := range secrets {
			if strings.Contains(shown, secret) {
				t.Errorf("%s: a credential is shown: %s", name, shown)
			}
		}
	}
	if !strings.Contains(fmt.Sprint(log), redact.Marker) {
		t.Errorf("the remote answer was not kept redacted: %v", log)
	}
}

func TestTextIsCleanedRedactedAndCut(t *testing.T) {
	srv, rec := serve(t, status(http.StatusOK))
	var app redact.Redactor
	secret := invented("app-value")
	app.Add(secret)
	s := newSender(t, Options{Redactor: &app})
	sent := func(msg Message) string {
		t.Helper()
		if err := s.Send(context.Background(), Slack{URL: srv.URL}, msg); err != nil {
			t.Fatal(err)
		}
		all := rec.all()
		var body map[string]string
		if err := json.Unmarshal(all[len(all)-1].body, &body); err != nil {
			t.Fatal(err)
		}
		return body["text"]
	}

	text := sent(Message{Subject: "line\r\nInjected: header\x1b[31m", Text: "first\x07\r\nsecond " + secret})
	if want := "line Injected: header [31m\nfirst \nsecond " + redact.Marker; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}

	// Without redaction first, the cut would keep the start of the secret.
	text = sent(Message{Text: strings.Repeat("ä", MaxText-8) + secret})
	if strings.Contains(text, secret[:5]) || !strings.HasSuffix(text, "[reda...") {
		t.Errorf("text ends %q", text[len(text)-20:])
	}
	if n := len([]rune(text)); n != MaxText {
		t.Errorf("text has %d runes", n)
	}

	text = sent(Message{Text: strings.Repeat("😀", MaxText)})
	if n := len(utf16.Encode([]rune(text))); n > MaxText {
		t.Errorf("text of characters outside the basic plane has %d UTF-16 code units", n)
	}
}

func TestNilChannelIsRefused(t *testing.T) {
	if code := codeOfErr(t, newSender(t, Options{}).Send(context.Background(), nil, testMessage)); code != output.CodeInvalidRequest {
		t.Errorf("code = %s", code)
	}
}
