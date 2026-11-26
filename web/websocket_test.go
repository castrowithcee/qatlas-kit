package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func echo(ctx context.Context, c *WebSocketConn) {
	for {
		b, err := c.Read(ctx)
		if err != nil {
			return
		}
		if c.Write(ctx, b) != nil {
			return
		}
	}
}

func upgrade(origins []string, site string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString([]byte(rand.Text()[:16])))
	if site != "" {
		req.Header.Set("Sec-Fetch-Site", site)
	}
	for _, o := range origins {
		req.Header.Add("Origin", o)
	}
	return req
}

func TestWebSocketHandshakeNeedsItsOrigin(t *testing.T) {
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /ws", Handler: WebSocket(WebSocketOptions{Handler: echo})}}})
	cases := []struct {
		name, site string
		origins    []string
	}{
		{"foreign origin", "", []string{"http://evil.example.test"}},
		{"foreign origin cross-site", "cross-site", []string{"http://evil.example.test"}},
		{"null origin", "", []string{"null"}},
		{"null origin same origin", "same-origin", []string{"null"}},
		{"missing origin", "", nil},
		{"missing origin same origin", "same-origin", nil},
		{"own origin cross-site", "cross-site", []string{"http://127.0.0.1:8080"}},
		{"own origin same-site", "same-site", []string{"http://127.0.0.1:8080"}},
		{"other port", "", []string{"http://127.0.0.1:9090"}},
		{"https scheme", "", []string{"https://127.0.0.1:8080"}},
		{"two origins", "", []string{"http://127.0.0.1:8080", "http://127.0.0.1:8080"}},
	}
	for _, c := range cases {
		if got := do(s, upgrade(c.origins, c.site)).Code; got != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", c.name, got)
		}
	}
}

func TestNetworkWebSocketHandshakeNeedsAListedOrigin(t *testing.T) {
	s := newServer(t, Options{Sessions: allowAll{}, Routes: []Route{{Pattern: "GET /ws", Handler: WebSocket(WebSocketOptions{Handler: echo})}},
		Network: &Network{Addr: "127.0.0.1:0", Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
			Origins: []string{"https://tool.example.test"}}})
	for _, origin := range []string{"http://tool.example.test", "https://tool.example.test:8443",
		"https://evil.example.test", "null", "http://127.0.0.1:8080"} {
		req := upgrade([]string{origin}, "")
		req.Host = "tool.example.test"
		req.RemoteAddr = "192.0.2.7:40000"
		if got := do(s, req).Code; got != http.StatusForbidden {
			t.Errorf("origin %q: status %d, want 403", origin, got)
		}
	}
}

func TestWebSocketHandshakeNeedsASession(t *testing.T) {
	ran := false
	h := WebSocket(WebSocketOptions{Handler: func(context.Context, *WebSocketConn) { ran = true }})
	deny := &fakeSessions{fn: func(w http.ResponseWriter, r *http.Request) (*http.Request, bool) { return nil, false }}
	s := newServer(t, Options{Sessions: deny, Routes: []Route{{Pattern: "GET /ws", Handler: h}}})
	if got := do(s, upgrade([]string{"http://127.0.0.1:8080"}, "same-origin")).Code; got != http.StatusUnauthorized || ran {
		t.Fatalf("without session: status %d, handler ran %v", got, ran)
	}
}

func TestWebSocketIsAbsentWithoutTheOption(t *testing.T) {
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /other", Handler: noContent}}})
	if got := do(s, upgrade([]string{"http://127.0.0.1:8080"}, "same-origin")).Code; got != http.StatusNotFound {
		t.Errorf("status %d, want 404", got)
	}
}

func dialWS(t *testing.T, s *Server, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	c, resp, err := websocket.Dial(ctx, s.URL()+"/ws", &websocket.DialOptions{HTTPHeader: hdr})
	if c != nil {
		t.Cleanup(func() { c.CloseNow() })
	}
	return c, resp, err
}

func serveWS(t *testing.T, opt WebSocketOptions, sessions Sessions) *Server {
	t.Helper()
	s := newServer(t, Options{Sessions: sessions, Routes: []Route{{Pattern: "GET /ws", Handler: WebSocket(opt)}}})
	run(t, s)
	return s
}

func TestWebSocketServesAValidSessionFromItsOwnOrigin(t *testing.T) {
	allow := &fakeSessions{fn: func(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
		return r.WithContext(context.WithValue(r.Context(), sessionKey{}, "user")), true
	}}
	s := serveWS(t, WebSocketOptions{Handler: echo}, allow)
	c, _, err := dialWS(t, s, s.URL())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, b, err := c.Read(ctx); err != nil || string(b) != "hello" {
		t.Fatalf("echo %q, %v", b, err)
	}
	if allow.calls != 1 {
		t.Errorf("Sessions consulted %d times, want 1", allow.calls)
	}
}

func TestWebSocketRefusesForeignAndMissingOriginOnTheWire(t *testing.T) {
	s := serveWS(t, WebSocketOptions{Handler: echo}, allowAll{})
	for _, origin := range []string{"http://evil.example.test", "null", ""} {
		_, resp, err := dialWS(t, s, origin)
		if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Errorf("origin %q: err %v, response %v", origin, err, resp)
		}
	}
}

func TestWebSocketClosesOnAnOversizedMessage(t *testing.T) {
	readErr := make(chan error, 1)
	s := serveWS(t, WebSocketOptions{MaxMessageBytes: 16, Handler: func(ctx context.Context, c *WebSocketConn) {
		_, err := c.Read(ctx)
		readErr <- err
	}}, allowAll{})
	c, _, err := dialWS(t, s, s.URL())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.Write(ctx, websocket.MessageText, make([]byte, 17))
	if err := <-readErr; err == nil {
		t.Error("oversized message was read")
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Errorf("client saw %v, want close status message too big", err)
	}
}

func TestWebSocketClosesWhenIdle(t *testing.T) {
	s := serveWS(t, WebSocketOptions{IdleTimeout: 50 * time.Millisecond, Handler: echo}, allowAll{})
	c, _, err := dialWS(t, s, s.URL())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); ctx.Err() != nil || err == nil {
		t.Errorf("idle connection stayed open: %v", err)
	}
}

func TestWebSocketNeedsAHandler(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic")
		}
	}()
	WebSocket(WebSocketOptions{})
}

func TestWebSocketOutlivesServerTimeouts(t *testing.T) {
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /ws", Handler: WebSocket(WebSocketOptions{Handler: echo})}}})
	s.server.ReadTimeout, s.server.WriteTimeout = 200*time.Millisecond, 200*time.Millisecond
	run(t, s)
	c, _, err := dialWS(t, s, s.URL())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range 8 {
		time.Sleep(100 * time.Millisecond)
		if err := c.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Read(ctx); err != nil {
			t.Fatalf("connection ended after the server timeouts: %v", err)
		}
	}
}
