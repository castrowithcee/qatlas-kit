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
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const testChat = -100123

var demoUser = map[string]any{"id": 7001, "is_bot": false, "first_name": "Demo", "last_name": "User"}

func textMessage(id int64, chat int64, from map[string]any, text string) map[string]any {
	m := map[string]any{"message_id": id, "date": fixedNow.Unix(), "chat": map[string]any{"id": chat, "type": "group"}, "text": text}
	if from != nil {
		m["from"] = from
	}
	return m
}

func update(id int64, kind string, message map[string]any) map[string]any {
	return map[string]any{"update_id": id, kind: message}
}

// updates is a successful getUpdates answer.
func updates(items ...map[string]any) string {
	if items == nil {
		items = []map[string]any{}
	}
	out, _ := json.Marshal(map[string]any{"ok": true, "result": items})
	return string(out)
}

type pollBody struct {
	Offset         *int64   `json:"offset"`
	Limit          int      `json:"limit"`
	Timeout        int      `json:"timeout"`
	AllowedUpdates []string `json:"allowed_updates"`
}

func pollOf(t *testing.T, r request) pollBody {
	t.Helper()
	var body pollBody
	if err := json.Unmarshal(r.body, &body); err != nil || body.Offset == nil {
		t.Fatalf("poll body %s: %v", r.body, err)
	}
	return body
}

// newReceiver returns a receiver whose waits after failed polls are recorded instead of slept.
func newReceiver(t *testing.T, s *Sender, c TelegramReceiver) (*Receiver, *[]time.Duration) {
	t.Helper()
	if c.Token == "" {
		c.Token = invented("bot") + ":part/with?specials&=+%"
	}
	if c.Chats == nil {
		c.Chats = []int64{testChat}
	}
	r, err := s.Receiver(c)
	if err != nil {
		t.Fatal(err)
	}
	var waits []time.Duration
	r.sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	return r, &waits
}

func write(w http.ResponseWriter, answer string) { _, _ = io.WriteString(w, answer) }

func TestReceiverDeliversAReplyWithAllFields(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reply := textMessage(501, testChat, demoUser, "yes, deploy it")
	reply["reply_to_message"] = textMessage(4711, testChat, map[string]any{"id": 9001, "is_bot": true, "first_name": "Demo Bot"}, "deploy demo-app?")
	srv, rec := serve(t, func(w http.ResponseWriter, n int) {
		if n == 1 {
			write(w, updates(update(41, "message", reply), update(42, "message", textMessage(502, testChat, demoUser, "plain"))))
			return
		}
		cancel()
		write(w, updates())
	})
	token := invented("bot") + ":part/with?specials&=+%"
	r, _ := newReceiver(t, newSender(t, Options{UserAgent: "demo-agent/1.0"}), TelegramReceiver{Token: token, BaseURL: srv.URL + "/"})
	var got []Incoming
	err := r.Run(ctx, func(in Incoming) error { got = append(got, in); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	want := Incoming{UpdateID: 41, MessageID: 501, ChatID: testChat, ReplyToMessageID: 4711, FromID: 7001,
		FromName: "Demo User", Time: fixedNow, Text: "yes, deploy it"}
	if len(got) != 2 || got[0] != want || got[1].ReplyToMessageID != 0 || got[1].UpdateID != 42 {
		t.Fatalf("got %+v", got)
	}
	all := rec.all()
	if len(all) != 2 {
		t.Fatalf("requests = %d", len(all))
	}
	if p := all[0].path; !strings.HasPrefix(p, "/bot") || !strings.HasSuffix(p, "/getUpdates") || strings.Count(p, "/") != 2 {
		t.Errorf("path = %s", p)
	}
	if all[0].header.Get("User-Agent") != "demo-agent/1.0" || all[0].header.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", all[0].header)
	}
	first, second := pollOf(t, all[0]), pollOf(t, all[1])
	if *first.Offset != 0 || first.Timeout != 50 || first.Limit != pollLimit || fmt.Sprint(first.AllowedUpdates) != "[message]" {
		t.Errorf("first poll = %+v", first)
	}
	if *second.Offset != 43 {
		t.Errorf("second offset = %d", *second.Offset)
	}
}

func TestReceiverConfirmsOnlyAfterHandleSucceeded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, rec := serve(t, func(w http.ResponseWriter, n int) {
		if n == 1 {
			write(w, updates(update(10, "message", textMessage(1, testChat, demoUser, "a")),
				update(11, "message", textMessage(2, testChat, demoUser, "b")),
				update(12, "message", textMessage(3, testChat, demoUser, "c"))))
			return
		}
		cancel()
		write(w, updates())
	})
	s := newSender(t, Options{})
	r, _ := newReceiver(t, s, TelegramReceiver{BaseURL: srv.URL})
	failed := errors.New("demo store is full")
	var handled []int64
	err := r.Run(ctx, func(in Incoming) error {
		handled = append(handled, in.UpdateID)
		if in.UpdateID == 11 {
			return failed
		}
		return nil
	})
	if err != failed || fmt.Sprint(handled) != "[10 11]" || len(rec.all()) != 1 {
		t.Fatalf("err = %v, handled %v, requests %d", err, handled, len(rec.all()))
	}

	// The application resumes from what it kept: the failed update is fetched again.
	again, _ := newReceiver(t, s, TelegramReceiver{BaseURL: srv.URL, Offset: 11})
	if err := again.Run(ctx, func(Incoming) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if offset := *pollOf(t, rec.all()[1]).Offset; offset != 11 {
		t.Errorf("offset = %d", offset)
	}
}

func TestReceiverSkipsAndConfirmsUnwantedUpdates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	photo := textMessage(5, testChat, demoUser, "")
	delete(photo, "text")
	photo["caption"] = "demo caption text"
	srv, rec := serve(t, func(w http.ResponseWriter, n int) {
		if n == 1 {
			write(w, updates(
				update(20, "message", textMessage(1, -100999, demoUser, "demo foreign text")),
				update(21, "message", textMessage(2, testChat, map[string]any{"id": 9001, "is_bot": true, "first_name": "Other Bot"}, "demo bot text")),
				update(22, "message", textMessage(3, testChat, nil, "demo anonymous text")),
				update(23, "edited_message", textMessage(4, testChat, demoUser, "demo edited text")),
				update(24, "message", photo),
				update(25, "message", textMessage(6, testChat, demoUser, "wanted")),
			))
			return
		}
		cancel()
		write(w, updates())
	})
	var skipped []Skipped
	r, _ := newReceiver(t, newSender(t, Options{}), TelegramReceiver{BaseURL: srv.URL, OnSkipped: func(s Skipped) { skipped = append(skipped, s) }})
	var handled []string
	if err := r.Run(ctx, func(in Incoming) error { handled = append(handled, in.Text); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	want := []Skipped{
		{UpdateID: 20, ChatID: -100999, FromID: 7001, Reason: SkipForeignChat},
		{UpdateID: 21, ChatID: testChat, FromID: 9001, Reason: SkipBotSender},
		{UpdateID: 22, ChatID: testChat, Reason: SkipNoSender},
		{UpdateID: 23, Reason: SkipNotMessage},
		{UpdateID: 24, ChatID: testChat, FromID: 7001, Reason: SkipNotText},
	}
	if fmt.Sprint(skipped) != fmt.Sprint(want) || fmt.Sprint(handled) != "[wanted]" {
		t.Errorf("skipped %+v, handled %v", skipped, handled)
	}
	if strings.Contains(fmt.Sprintf("%+v", skipped), "demo") {
		t.Errorf("a skip report carries text: %+v", skipped)
	}
	if offset := *pollOf(t, rec.all()[1]).Offset; offset != 26 {
		t.Errorf("offset = %d", offset)
	}
}

func TestReceiverRefusesUnsafeConfiguration(t *testing.T) {
	token := invented("bot") + ":a/b?c"
	s := newSender(t, Options{})
	cases := map[string]TelegramReceiver{
		"no chats":              {Token: token},
		"empty chats":           {Token: token, Chats: []int64{}},
		"chat without id":       {Token: token, Chats: []int64{testChat, 0}},
		"no token":              {Chats: []int64{testChat}},
		"plain http":            {Token: token, Chats: []int64{testChat}, BaseURL: "http://example.com"},
		"user part":             {Token: token, Chats: []int64{testChat}, BaseURL: "https://demo:" + invented("pass") + "@example.com"},
		"other scheme":          {Token: token, Chats: []int64{testChat}, BaseURL: "ftp://example.com"},
		"negative offset":       {Token: token, Chats: []int64{testChat}, Offset: -1},
		"negative poll timeout": {Token: token, Chats: []int64{testChat}, PollTimeout: -time.Second},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := s.Receiver(c)
			if code := codeOfErr(t, err); code != output.CodeConfigInvalid || r != nil {
				t.Errorf("code = %s (%v)", code, err)
			}
			if strings.Contains(err.Error(), "example.com") || strings.Contains(err.Error(), token) {
				t.Errorf("the error names the address or the token: %v", err)
			}
		})
	}

	srv, rec := serve(t, status(http.StatusOK))
	r, _ := newReceiver(t, s, TelegramReceiver{BaseURL: srv.URL})
	if code := codeOfErr(t, r.Run(context.Background(), nil)); code != output.CodeInvalidRequest || len(rec.all()) != 0 {
		t.Errorf("no handler: code = %s, requests = %d", code, len(rec.all()))
	}
}

func TestReceiverEndsOnRedirectUntrustedCertificateAndOversize(t *testing.T) {
	never := func(Incoming) error { t.Error("a message was delivered"); return nil }
	var failures int
	s := newSender(t, Options{OnFailure: func(int, *Error) { failures++ }})

	target, reached := serve(t, func(w http.ResponseWriter, _ int) { write(w, updates()) })
	redirect, origin := serve(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	r, waits := newReceiver(t, s, TelegramReceiver{BaseURL: redirect.URL})
	if err := r.Run(context.Background(), never); codeOfErr(t, err) != output.CodeRuntime {
		t.Errorf("redirect: %v", err)
	}
	if len(reached.all()) != 0 || len(origin.all()) != 1 || len(*waits) != 0 {
		t.Errorf("redirect: target reached %d times, origin %d times, waits %v", len(reached.all()), len(origin.all()), *waits)
	}

	var hits int
	var mu sync.Mutex
	tlsSrv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	tlsSrv.Config.ErrorLog = log.New(io.Discard, "", 0)
	tlsSrv.StartTLS()
	t.Cleanup(tlsSrv.Close)
	r, waits = newReceiver(t, s, TelegramReceiver{BaseURL: tlsSrv.URL})
	if code := codeOfErr(t, r.Run(context.Background(), never)); code != output.CodeTLS || len(*waits) != 0 {
		t.Errorf("untrusted certificate: code = %s, waits %v", code, *waits)
	}
	mu.Lock()
	if hits != 0 {
		t.Errorf("untrusted certificate: %d requests arrived", hits)
	}
	mu.Unlock()

	big, rec := serve(t, func(w http.ResponseWriter, _ int) { write(w, strings.Repeat("x", maxPollAnswer+1)) })
	r, waits = newReceiver(t, s, TelegramReceiver{BaseURL: big.URL})
	err := r.Run(context.Background(), never)
	if code := codeOfErr(t, err); code != output.CodeRuntime || !strings.Contains(err.Error(), "size limit") {
		t.Errorf("oversize: code = %s (%v)", code, err)
	}
	if len(rec.all()) != 1 || len(*waits) != 0 || failures != 3 {
		t.Errorf("oversize: requests = %d, waits %v, failures reported %d", len(rec.all()), *waits, failures)
	}

	// With the server's root trusted, the poll passes TLS and fails only on the empty answer.
	roots := tlsSrv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	r, _ = newReceiver(t, newSender(t, Options{TLS: &tls.Config{RootCAs: roots}}), TelegramReceiver{BaseURL: tlsSrv.URL})
	if code := codeOfErr(t, r.Run(context.Background(), never)); code != output.CodeRuntime {
		t.Errorf("trusted root: code = %s", code)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("trusted root: %d requests arrived", hits)
	}
}

func TestReceiverReadsAFullBatchOfLongReplies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Every character escaped is the largest form a text of the longest allowed length takes.
	long := strings.Repeat(`😀`, 2048)
	var items []string
	for i := range pollLimit {
		items = append(items, fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"date":1,"chat":{"id":%d},`+
			`"from":{"id":7001,"is_bot":false,"first_name":"Demo"},"text":"%s",`+
			`"reply_to_message":{"message_id":4711,"date":1,"chat":{"id":%d},"text":"%s"}}}`, i+1, i+100, testChat, long, testChat, long))
	}
	answer := `{"ok":true,"result":[` + strings.Join(items, ",") + `]}`
	if len(answer) <= maxAnswer {
		t.Fatalf("the answer of %d bytes does not exceed the sender's limit", len(answer))
	}
	srv, _ := serve(t, func(w http.ResponseWriter, n int) {
		if n > 1 {
			cancel()
			write(w, updates())
			return
		}
		write(w, answer)
	})
	r, _ := newReceiver(t, newSender(t, Options{}), TelegramReceiver{BaseURL: srv.URL})
	var got []Incoming
	if err := r.Run(ctx, func(in Incoming) error { got = append(got, in); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(got) != pollLimit || got[0].ReplyToMessageID != 4711 {
		t.Fatalf("delivered %d messages", len(got))
	}
	if n := len(utf16.Encode([]rune(got[0].Text))); n > MaxText || !strings.HasSuffix(got[0].Text, "...") {
		t.Errorf("text has %d UTF-16 code units", n)
	}
}

func TestReceiverEndsOnRefusals(t *testing.T) {
	never := func(Incoming) error { t.Error("a message was delivered"); return nil }
	cases := map[string]struct {
		status int
		answer string
		code   output.Code
		says   string
	}{
		"unauthorized":   {http.StatusUnauthorized, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, output.CodeAuth, "token"},
		"conflict":       {http.StatusConflict, `{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request"}`, output.CodeRuntime, "webhook"},
		"forbidden":      {http.StatusForbidden, ``, output.CodePermission, ""},
		"not found":      {http.StatusNotFound, ``, output.CodeNotFound, ""},
		"bad request":    {http.StatusBadRequest, ``, output.CodeRuntime, ""},
		"not ok":         {http.StatusOK, `{"ok":false,"result":[]}`, output.CodeRuntime, ""},
		"not json":       {http.StatusOK, `<html>`, output.CodeRuntime, ""},
		"no list":        {http.StatusOK, `{"ok":true,"result":{}}`, output.CodeRuntime, "list"},
		"null list":      {http.StatusOK, `{"ok":true,"result":null}`, output.CodeRuntime, "list"},
		"no result":      {http.StatusOK, `{"ok":true}`, output.CodeRuntime, "list"},
		"malformed item": {http.StatusOK, `{"ok":true,"result":[{"update_id":"7"}]}`, output.CodeRuntime, "list"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv, rec := serve(t, func(w http.ResponseWriter, _ int) {
				w.WriteHeader(tc.status)
				write(w, tc.answer)
			})
			var reported []output.Code
			s := newSender(t, Options{OnFailure: func(_ int, err *Error) { reported = append(reported, err.Code) }})
			r, waits := newReceiver(t, s, TelegramReceiver{BaseURL: srv.URL})
			err := r.Run(context.Background(), never)
			if code := codeOfErr(t, err); code != tc.code || !strings.Contains(err.Error(), tc.says) {
				t.Errorf("code = %s (%v)", code, err)
			}
			if len(rec.all()) != 1 || len(*waits) != 0 || fmt.Sprint(reported) != fmt.Sprint([]output.Code{tc.code}) {
				t.Errorf("requests = %d, waits %v, reported %v", len(rec.all()), *waits, reported)
			}
		})
	}
}

func TestReceiverHoldsRetryAfter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, rec := serve(t, func(w http.ResponseWriter, n int) {
		switch n {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
			write(w, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`)
		case 2:
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
		case 3:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			cancel()
			write(w, updates())
		}
	})
	var reported []output.Code
	s := newSender(t, Options{Backoff: 20 * time.Second, OnFailure: func(_ int, err *Error) { reported = append(reported, err.Code) }})
	r, waits := newReceiver(t, s, TelegramReceiver{BaseURL: srv.URL})
	if err := r.Run(ctx, func(Incoming) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if fmt.Sprint(*waits) != "[7s 3s 20s]" || len(rec.all()) != 4 {
		t.Errorf("waits %v, requests %d", *waits, len(rec.all()))
	}
	if fmt.Sprint(reported) != "[rate-limited rate-limited rate-limited]" {
		t.Errorf("reported %v", reported)
	}
}

func TestReceiverBacksOffOnTransientFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, _ := serve(t, func(w http.ResponseWriter, n int) {
		switch n {
		case 1, 2, 3, 4, 6:
			w.WriteHeader([]int{0, http.StatusBadGateway, http.StatusInternalServerError,
				http.StatusServiceUnavailable, http.StatusGatewayTimeout, 0, http.StatusBadGateway}[n])
		case 5:
			write(w, updates(update(1, "message", textMessage(1, testChat, demoUser, "back"))))
		default:
			cancel()
			write(w, updates())
		}
	})
	var attempts []int
	s := newSender(t, Options{Backoff: 20 * time.Second, OnFailure: func(attempt int, _ *Error) { attempts = append(attempts, attempt) }})
	r, waits := newReceiver(t, s, TelegramReceiver{BaseURL: srv.URL})
	var handled int
	if err := r.Run(ctx, func(Incoming) error { handled++; return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if fmt.Sprint(*waits) != "[20s 40s 1m0s 1m0s 20s]" || fmt.Sprint(attempts) != "[1 2 3 4 1]" || handled != 1 {
		t.Errorf("waits %v, attempts %v, handled %d", *waits, attempts, handled)
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	var codes []output.Code
	s = newSender(t, Options{Backoff: 20 * time.Second, OnFailure: func(_ int, err *Error) { codes = append(codes, err.Code) }})
	r, _ = newReceiver(t, s, TelegramReceiver{BaseURL: closedURL})
	ended, stop := context.WithCancel(context.Background())
	defer stop()
	var slept []time.Duration
	r.sleep = func(ctx context.Context, d time.Duration) error {
		if slept = append(slept, d); len(slept) == 3 {
			stop()
		}
		return ctx.Err()
	}
	if err := r.Run(ended, func(Incoming) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if fmt.Sprint(slept) != "[20s 40s 1m0s]" || fmt.Sprint(codes) != "[unreachable unreachable unreachable]" {
		t.Errorf("waits %v, codes %v", slept, codes)
	}
}

func TestReceiverRequestTimeoutLiesAboveThePollTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, rec := serve(t, func(w http.ResponseWriter, n int) {
		if n == 1 {
			time.Sleep(300 * time.Millisecond)
			write(w, updates(update(1, "message", textMessage(1, testChat, demoUser, "late"))))
			return
		}
		cancel()
		write(w, updates())
	})
	var failures int
	s := newSender(t, Options{Timeout: 50 * time.Millisecond, OnFailure: func(int, *Error) { failures++ }})
	r, _ := newReceiver(t, s, TelegramReceiver{BaseURL: srv.URL, PollTimeout: 1500 * time.Millisecond})
	var handled int
	if err := r.Run(ctx, func(Incoming) error { handled++; return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if handled != 1 || failures != 0 {
		t.Errorf("handled %d, failures %d", handled, failures)
	}
	if timeout := pollOf(t, rec.all()[0]).Timeout; timeout != 2 {
		t.Errorf("poll timeout = %d", timeout)
	}
}

func TestReceiverStopsAtOnceWhenCancelled(t *testing.T) {
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	srv, _ := serve(t, func(w http.ResponseWriter, _ int) {
		arrived <- struct{}{}
		<-release
	})
	defer close(release)
	var failures int
	s := newSender(t, Options{OnFailure: func(int, *Error) { failures++ }})
	r, _ := newReceiver(t, s, TelegramReceiver{BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-arrived
		cancel()
	}()
	if err := r.Run(ctx, func(Incoming) error { return nil }); !errors.Is(err, context.Canceled) || failures != 0 {
		t.Errorf("pending poll: err = %v, failures = %d", err, failures)
	}

	idle, rec := serve(t, func(w http.ResponseWriter, _ int) { write(w, updates()) })
	r, _ = newReceiver(t, s, TelegramReceiver{BaseURL: idle.URL})
	if err := r.Run(ctx, func(Incoming) error { return nil }); !errors.Is(err, context.Canceled) || len(rec.all()) != 0 {
		t.Errorf("ended context: err = %v, requests = %d", err, len(rec.all()))
	}

	batch, _ := serve(t, func(w http.ResponseWriter, _ int) {
		write(w, updates(update(1, "message", textMessage(1, testChat, demoUser, "a")),
			update(2, "message", textMessage(2, testChat, demoUser, "b"))))
	})
	r, _ = newReceiver(t, s, TelegramReceiver{BaseURL: batch.URL})
	during, stop := context.WithCancel(context.Background())
	defer stop()
	var handled int
	err := r.Run(during, func(Incoming) error { handled++; stop(); return nil })
	if !errors.Is(err, context.Canceled) || handled != 1 {
		t.Errorf("cancelled while handling: err = %v, handled = %d", err, handled)
	}
}

func TestReceiverCleansRedactsAndCutsText(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	token := invented("bot") + ":part/with?specials&=+%"
	appSecret := invented("app-value")
	name := map[string]any{"id": 7001, "is_bot": false, "first_name": "Demo\nInjected", "last_name": "User\x1b[2J"}
	first, _ := json.Marshal(update(1, "message", textMessage(1, testChat, name,
		"line one\r\nline two\x1b[31m\x07 "+token+" and "+appSecret+" INVALID")))
	answer := `{"ok":true,"result":[` + strings.Replace(string(first), "INVALID", "\xff", 1) + "," +
		strings.TrimSuffix(strings.TrimPrefix(updates(update(2, "message", textMessage(2, testChat, demoUser,
			strings.Repeat("a", MaxText+500)))), `{"ok":true,"result":[`), "]}") + "]}"
	srv, _ := serve(t, func(w http.ResponseWriter, n int) {
		if n > 1 {
			cancel()
			write(w, updates())
			return
		}
		write(w, answer)
	})
	var app redact.Redactor
	app.Add(appSecret)
	r, _ := newReceiver(t, newSender(t, Options{Redactor: &app}), TelegramReceiver{Token: token, BaseURL: srv.URL})
	var got []Incoming
	if err := r.Run(ctx, func(in Incoming) error { got = append(got, in); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if want := "line one\nline two [31m  " + redact.Marker + " and " + redact.Marker + " �"; got[0].Text != want {
		t.Errorf("text = %q, want %q", got[0].Text, want)
	}
	if got[0].FromName != "Demo Injected User [2J" || !utf8.ValidString(got[0].Text) {
		t.Errorf("name = %q", got[0].FromName)
	}
	if n := len([]rune(got[1].Text)); n != MaxText || !strings.HasSuffix(got[1].Text, "...") {
		t.Errorf("long text has %d runes", n)
	}
}

func TestReceiverNeverShowsTheToken(t *testing.T) {
	token := invented("bot") + ":a/b?c&d=e"
	appSecret := invented("app-value")
	refused, _ := serve(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"ok":false,"error_code":401,"description":"token %s and %s refused"}`, token, appSecret)
	})
	garbled, _ := serve(t, func(w http.ResponseWriter, _ int) {
		write(w, `{"ok":false,"description":"`+token+`"}`)
	})
	conflict, _ := serve(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusConflict)
		write(w, `{"ok":false,"error_code":409,"description":"Conflict for /bot`+url.PathEscape(token)+`"}`)
	})
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	var app redact.Redactor
	app.Add(appSecret)
	var reported []string
	s := newSender(t, Options{Redactor: &app, OnFailure: func(attempt int, err *Error) {
		reported = append(reported, fmt.Sprintf("attempt %d: %s %+v", attempt, err, err.Original))
	}})
	for name, base := range map[string]string{"refused": refused.URL, "garbled": garbled.URL, "conflict": conflict.URL, "unreachable": closedURL} {
		r, _ := newReceiver(t, s, TelegramReceiver{Token: token, BaseURL: base})
		r.sleep = func(context.Context, time.Duration) error { return context.Canceled }
		err := r.Run(context.Background(), func(Incoming) error { return nil })
		var failure *Error
		if !errors.As(err, &failure) {
			t.Fatalf("%s: %v", name, err)
		}
		shown := fmt.Sprintf("%s %+v %v", failure, failure.Original, reported)
		for _, secret := range []string{token, url.PathEscape(token), url.QueryEscape(token), appSecret} {
			if strings.Contains(shown, secret) {
				t.Errorf("%s: a credential is shown: %s", name, shown)
			}
		}
	}
	if !strings.Contains(fmt.Sprint(reported), redact.Marker) {
		t.Errorf("the remote answer was not kept redacted: %v", reported)
	}
}
