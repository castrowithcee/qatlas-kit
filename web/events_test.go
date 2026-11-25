package web

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

type sourceFunc func(ctx context.Context, r *http.Request, lastID string) (<-chan Event, error)

func (f sourceFunc) Subscribe(ctx context.Context, r *http.Request, lastID string) (<-chan Event, error) {
	return f(ctx, r, lastID)
}

// replay sends events and then ends the stream; it records the Last-Event-ID it was given.
func replay(lastID *string, events ...Event) Source {
	return sourceFunc(func(ctx context.Context, r *http.Request, id string) (<-chan Event, error) {
		*lastID = id
		ch := make(chan Event, len(events))
		for _, ev := range events {
			ch <- ev
		}
		close(ch)
		return ch, nil
	})
}

// hold keeps the stream open until it ends and reports each subscription.
func hold(subscribed chan<- string, events <-chan Event) Source {
	return sourceFunc(func(ctx context.Context, r *http.Request, id string) (<-chan Event, error) {
		subscribed <- id
		out := make(chan Event)
		go func() {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case ev := <-events:
					select {
					case out <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return out, nil
	})
}

func stream(src Source, adjust func(*streamLimits)) *eventStream {
	e := Events(src).(*eventStream)
	if adjust != nil {
		adjust(&e.limits)
	}
	return e
}

// streamRecorder lets a stream run against httptest.ResponseRecorder.
type streamRecorder struct{ *httptest.ResponseRecorder }

func (streamRecorder) SetReadDeadline(time.Time) error  { return nil }
func (streamRecorder) SetWriteDeadline(time.Time) error { return nil }

func doStream(s *Server, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.serve(streamRecorder{rec}, r)
	return rec
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func openStream(t *testing.T, s *Server, path string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.URL()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// readUntilEnd reads the stream to its end and fails the test if that takes longer than limit.
func readUntilEnd(t *testing.T, body io.Reader, limit time.Duration) string {
	t.Helper()
	got := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(body)
		got <- b
	}()
	select {
	case b := <-got:
		return string(b)
	case <-time.After(limit):
		t.Fatalf("stream still open after %v", limit)
		return ""
	}
}

func TestEventsAreEncoded(t *testing.T) {
	e := stream(sourceFunc(nil), nil)
	cases := map[string]Event{
		"data: plain\n\n": {Data: "plain"},
		"data: \n\n":      {},
		"id: 7\nevent: update\ndata: a\ndata: b\ndata: c\ndata: \ndata: d\n\n": {
			ID: "7", Type: "update", Data: "a\nb\r\nc\r\rd"},
		"data: id: 9\ndata: \ndata: event: forged\n\n": {Data: "id: 9\n\nevent: forged"},
		"data:  indented\ndata: \n\n":                  {Data: " indented\n"},
	}
	for want, ev := range cases {
		got, err := e.appendEvent(nil, ev)
		if err != nil || string(got) != want {
			t.Errorf("%+v: got %q, %v, want %q", ev, got, err, want)
		}
	}
}

func TestEventsWithUnsafeFieldsAreRefused(t *testing.T) {
	e := stream(sourceFunc(nil), func(l *streamLimits) { l.eventBytes, l.idBytes = 32, 8 })
	for name, ev := range map[string]Event{
		"line feed in ID":       {ID: "1\ndata: forged"},
		"carriage return in ID": {ID: "1\rdata: forged"},
		"NUL in ID":             {ID: "1\x00"},
		"line feed in type":     {Type: "update\n\ndata: forged"},
		"carriage return type":  {Type: "update\r"},
		"NUL in type":           {Type: "up\x00date"},
		"long ID":               {ID: "123456789"},
		"large event":           {ID: "1", Type: "update", Data: strings.Repeat("x", 26)},
	} {
		if got, err := e.appendEvent(nil, ev); err == nil {
			t.Errorf("%s: encoded as %q", name, got)
		}
	}
	if _, err := e.appendEvent(nil, Event{ID: "12345678", Data: strings.Repeat("x", 24)}); err != nil {
		t.Errorf("event at the limits refused: %v", err)
	}
}

func TestStreamHandshakeNeedsItsOrigin(t *testing.T) {
	var lastID string
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /events", Handler: Events(replay(&lastID))}}})
	cases := []struct {
		name, site string
		origins    []string
		want       int
	}{
		{"no browser headers", "", nil, http.StatusOK},
		{"same origin without Origin", "same-origin", nil, http.StatusOK},
		{"typed by the user", "none", nil, http.StatusOK},
		{"origin matches host", "same-origin", []string{"http://127.0.0.1:8080"}, http.StatusOK},
		{"origin matches host without site", "", []string{"http://127.0.0.1:8080"}, http.StatusOK},
		{"foreign origin", "cross-site", []string{"http://evil.example.test"}, http.StatusForbidden},
		{"foreign origin without site", "", []string{"http://evil.example.test"}, http.StatusForbidden},
		{"other port", "same-site", []string{"http://127.0.0.1:9090"}, http.StatusForbidden},
		{"other port without site", "", []string{"http://127.0.0.1:9090"}, http.StatusForbidden},
		{"https scheme", "", []string{"https://127.0.0.1:8080"}, http.StatusForbidden},
		{"null origin", "", []string{"null"}, http.StatusForbidden},
		{"null origin same origin", "same-origin", []string{"null"}, http.StatusForbidden},
		{"cross site without Origin", "cross-site", nil, http.StatusForbidden},
		{"same site without Origin", "same-site", nil, http.StatusForbidden},
		{"two origins", "", []string{"http://127.0.0.1:8080", "http://127.0.0.1:8080"}, http.StatusForbidden},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/events", nil)
		if c.site != "" {
			req.Header.Set("Sec-Fetch-Site", c.site)
		}
		for _, o := range c.origins {
			req.Header.Add("Origin", o)
		}
		if got := doStream(s, req).Code; got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
}

func TestNetworkStreamHandshakeNeedsAListedOrigin(t *testing.T) {
	var lastID string
	s := newServer(t, Options{Sessions: allowAll{}, Routes: []Route{{Pattern: "GET /events", Handler: Events(replay(&lastID))}},
		Network: &Network{Addr: "127.0.0.1:0", Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
			Origins: []string{"https://tool.example.test"}}})
	for origin, want := range map[string]int{
		"https://tool.example.test": http.StatusOK, "https://TOOL.example.test": http.StatusOK,
		"http://tool.example.test": http.StatusForbidden, "https://tool.example.test:8443": http.StatusForbidden,
		"https://evil.example.test": http.StatusForbidden, "null": http.StatusForbidden,
	} {
		req := httptest.NewRequest(http.MethodGet, "http://tool.example.test/events", nil)
		req.RemoteAddr = "192.0.2.7:40000"
		req.Header.Set("Origin", origin)
		if got := doStream(s, req).Code; got != want {
			t.Errorf("origin %q: status %d, want %d", origin, got, want)
		}
	}
}

func TestStreamHandshakeNeedsASession(t *testing.T) {
	subscribed := false
	src := sourceFunc(func(ctx context.Context, r *http.Request, id string) (<-chan Event, error) {
		subscribed = r.Context().Value(sessionKey{}) == "user"
		ch := make(chan Event)
		close(ch)
		return ch, nil
	})
	deny := &fakeSessions{fn: func(w http.ResponseWriter, r *http.Request) (*http.Request, bool) { return nil, false }}
	s := newServer(t, Options{Sessions: deny, Routes: []Route{{Pattern: "GET /events", Handler: Events(src)}}})
	if got := doStream(s, get("/events")).Code; got != http.StatusUnauthorized || subscribed {
		t.Fatalf("without session: status %d, subscribed %v", got, subscribed)
	}

	allow := &fakeSessions{fn: func(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
		return r.WithContext(context.WithValue(r.Context(), sessionKey{}, "user")), true
	}}
	s = newServer(t, Options{Sessions: allow, Routes: []Route{{Pattern: "GET /events", Handler: Events(src)}}})
	if got := doStream(s, get("/events")).Code; got != http.StatusOK || !subscribed {
		t.Fatalf("with session: status %d, subscribed %v", got, subscribed)
	}
}

func TestLastEventIDIsBounded(t *testing.T) {
	var lastID string
	e := stream(replay(&lastID), func(l *streamLimits) { l.idBytes = 8 })
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /events", Handler: e}}})
	cases := []struct {
		name string
		ids  []string
		want int
	}{
		{"none", nil, http.StatusOK},
		{"at the limit", []string{"12345678"}, http.StatusOK},
		{"too long", []string{"123456789"}, http.StatusBadRequest},
		{"NUL", []string{"12\x0034"}, http.StatusBadRequest},
		{"line feed", []string{"12\n34"}, http.StatusBadRequest},
		{"twice", []string{"1", "2"}, http.StatusBadRequest},
	}
	for _, c := range cases {
		lastID = "unset"
		req := get("/events")
		for _, id := range c.ids {
			req.Header.Add("Last-Event-ID", id)
		}
		got := doStream(s, req).Code
		wantID := "unset"
		if c.want == http.StatusOK {
			wantID = strings.Join(c.ids, "")
		}
		if got != c.want || lastID != wantID {
			t.Errorf("%s: status %d, source got %q, want %d %q", c.name, got, lastID, c.want, wantID)
		}
	}
}

func TestStreamRefusals(t *testing.T) {
	var lastID string
	logs := &syncBuffer{}
	failing := sourceFunc(func(ctx context.Context, r *http.Request, id string) (<-chan Event, error) {
		return nil, errors.New("topic unknown")
	})
	s := newServer(t, Options{Login: LoginOff, Log: logs, Routes: []Route{
		{Pattern: "GET /events", Handler: Events(replay(&lastID))},
		{Pattern: "POST /events", Handler: Events(replay(&lastID))},
		{Pattern: "GET /failing", Handler: Events(failing)},
	}})
	if got := doStream(s, httptest.NewRequest(http.MethodHead, "http://localhost/events", nil)).Code; got != http.StatusMethodNotAllowed {
		t.Errorf("HEAD: status %d", got)
	}
	if got := doStream(s, httptest.NewRequest(http.MethodPost, "http://localhost/events", nil)).Code; got != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d", got)
	}
	if got := doStream(s, get("/failing")).Code; got != http.StatusInternalServerError || !strings.Contains(logs.String(), "topic unknown") {
		t.Errorf("failing source: status %d, log %q", got, logs.String())
	}
	if got := do(s, get("/events")).Code; got != http.StatusInternalServerError {
		t.Errorf("writer without deadlines: status %d", got)
	}
	rec := httptest.NewRecorder()
	Events(replay(&lastID)).ServeHTTP(streamRecorder{rec}, get("/events"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("outside a server: status %d", rec.Code)
	}
}

func TestStreamsAreLimitedPerHandler(t *testing.T) {
	subscribed := make(chan string, 2)
	e := stream(hold(subscribed, nil), func(l *streamLimits) { l.conns = 1 })
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /events", Handler: e}}})
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan int, 1)
		go func() { done <- doStream(s, get("/events").WithContext(ctx)).Code }()
		<-subscribed
		if got := doStream(s, get("/events")).Code; got != http.StatusServiceUnavailable {
			t.Errorf("stream beyond the limit: status %d", got)
		}
		// Ending the open stream frees its slot for the next round.
		cancel()
		if got := <-done; got != http.StatusOK {
			t.Errorf("open stream: status %d", got)
		}
	}
}

func TestStreamResumesAfterLastEventID(t *testing.T) {
	var lastID string
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /events", Handler: Events(replay(&lastID,
		Event{ID: "42", Data: "first\nsecond"}, Event{ID: "43", Type: "update", Data: "third"}))}}})
	run(t, s)
	resp := openStream(t, s, "/events", http.Header{"Last-Event-Id": {"41"}})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" ||
		resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("status %d, headers %v", resp.StatusCode, resp.Header)
	}
	want := "id: 42\ndata: first\ndata: second\n\nid: 43\nevent: update\ndata: third\n\n"
	if got := readUntilEnd(t, resp.Body, 5*time.Second); got != want || lastID != "41" {
		t.Fatalf("stream %q, source got %q", got, lastID)
	}
}

func TestStreamOutlivesServerTimeoutsWithHeartbeat(t *testing.T) {
	t.Run("HTTP/1.1 on loopback", func(t *testing.T) {
		subscribed, events := make(chan string, 1), make(chan Event)
		s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /events", Handler: beating(subscribed, events)}}})
		testHeartbeat(t, s, http.DefaultClient, s.URL(), "", subscribed, events)
	})
	t.Run("HTTP/2 over TLS", func(t *testing.T) {
		ts := httptest.NewUnstartedServer(nil)
		ts.EnableHTTP2 = true
		ts.StartTLS()
		cert, client := ts.TLS.Certificates[0], ts.Client()
		ts.Close()
		subscribed, events := make(chan string, 1), make(chan Event)
		s := newServer(t, Options{Sessions: allowAll{},
			Network: &Network{Addr: "127.0.0.1:0", TLS: &tls.Config{Certificates: []tls.Certificate{cert}},
				Origins: []string{"https://tool.example.test"}},
			Routes: []Route{{Pattern: "GET /events", Handler: beating(subscribed, events)}}})
		testHeartbeat(t, s, client, "https://"+s.listeners[0].Addr().String(), "tool.example.test", subscribed, events)
	})
}

func beating(subscribed chan<- string, events <-chan Event) http.Handler {
	return stream(hold(subscribed, events), func(l *streamLimits) {
		l.heartbeat, l.write = 20*time.Millisecond, 60*time.Millisecond
	})
}

// testHeartbeat keeps a stream open well past the read and write timeouts of the server and the write
// limit of the stream.
func testHeartbeat(t *testing.T, s *Server, client *http.Client, base, host string, subscribed <-chan string, events chan<- Event) {
	s.server.ReadTimeout, s.server.WriteTimeout = 100*time.Millisecond, 100*time.Millisecond
	run(t, s)
	req, _ := http.NewRequest(http.MethodGet, base+"/events", nil)
	if host != "" {
		req.Host = host
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || (host != "" && resp.ProtoMajor != 2) {
		t.Fatalf("status %d over %s", resp.StatusCode, resp.Proto)
	}
	<-subscribed
	lines := bufio.NewScanner(resp.Body)
	beats := 0
	start := time.Now()
	for time.Since(start) < 300*time.Millisecond {
		if !lines.Scan() {
			t.Fatalf("stream ended after %v: %v", time.Since(start), lines.Err())
		}
		if lines.Text() == ":" {
			beats++
		}
	}
	if beats < 3 {
		t.Fatalf("%d heartbeats in %v", beats, time.Since(start))
	}
	events <- Event{Data: "late"}
	for lines.Scan() {
		if lines.Text() == "data: late" {
			return
		}
	}
	t.Fatalf("event lost: %v", lines.Err())
}

func TestStreamEndsAfterItsDuration(t *testing.T) {
	subscribed := make(chan string, 1)
	e := stream(hold(subscribed, nil), func(l *streamLimits) { l.duration = 50 * time.Millisecond })
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /events", Handler: e}}})
	run(t, s)
	resp := openStream(t, s, "/events", nil)
	readUntilEnd(t, resp.Body, 5*time.Second)
}

func TestStreamEndsOnUnsafeEvent(t *testing.T) {
	subscribed, events := make(chan string, 1), make(chan Event)
	logs := &syncBuffer{}
	s := newServer(t, Options{Login: LoginOff, Log: logs, Routes: []Route{{Pattern: "GET /events", Handler: Events(hold(subscribed, events))}}})
	run(t, s)
	resp := openStream(t, s, "/events", nil)
	<-subscribed
	events <- Event{Data: "fine"}
	events <- Event{ID: "1\ndata: forged", Data: "x"}
	if got := readUntilEnd(t, resp.Body, 5*time.Second); got != "data: fine\n\n" {
		t.Fatalf("stream %q", got)
	}
	if !strings.Contains(logs.String(), "line break or NUL") {
		t.Fatalf("log %q", logs.String())
	}
}

func TestShutdownEndsOpenStreams(t *testing.T) {
	subscribed := make(chan string, 1)
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /events", Handler: Events(hold(subscribed, nil))}}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	resp := openStream(t, s, "/events", nil)
	<-subscribed
	start := time.Now()
	cancel()
	// Well below the shutdown deadline of five seconds, after which Serve would close connections hard.
	readUntilEnd(t, resp.Body, 2*time.Second)
	if err := <-done; err != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("Serve = %v after %v", err, time.Since(start))
	}
}
