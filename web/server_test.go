package web

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/redact"
)

var noContent = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

type allowAll struct{}

func (allowAll) Session(w http.ResponseWriter, r *http.Request) (*http.Request, bool) { return r, true }

func newServer(t *testing.T, opts Options) *Server {
	t.Helper()
	s, err := Listen(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, l := range s.listeners {
			l.Close()
		}
	})
	return s
}

func do(s *Server, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.serve(rec, r)
	return rec
}

// run serves s until the test ends and fails the test if Serve does not return nil.
func run(t *testing.T, s *Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
}

func TestListenRefusesUnsafeOptions(t *testing.T) {
	tlsConf := &tls.Config{Certificates: []tls.Certificate{{}}}
	origins := []string{"https://tool.example.test"}
	network := func(n Network) *Network {
		if n.Addr == "" {
			n.Addr = "127.0.0.1:0"
		}
		return &n
	}
	cases := map[string]Options{
		"network without TLS or proxy": {Sessions: allowAll{}, Network: network(Network{Origins: origins})},
		"network without login": {Login: LoginOff,
			Network: network(Network{TLS: tlsConf, Origins: origins})},
		"network without sessions":   {Network: network(Network{TLS: tlsConf, Origins: origins})},
		"network without origins":    {Sessions: allowAll{}, Network: network(Network{TLS: tlsConf})},
		"network with http origin":   {Sessions: allowAll{}, Network: network(Network{TLS: tlsConf, Origins: []string{"http://tool.example.test"}})},
		"network with origin path":   {Sessions: allowAll{}, Network: network(Network{TLS: tlsConf, Origins: []string{"https://tool.example.test/app"}})},
		"network with null origin":   {Sessions: allowAll{}, Network: network(Network{TLS: tlsConf, Origins: []string{"null"}})},
		"network TLS without cert":   {Sessions: allowAll{}, Network: network(Network{TLS: &tls.Config{}, Origins: origins})},
		"network with invalid proxy": {Sessions: allowAll{}, Network: network(Network{Proxies: []netip.Prefix{{}}, Origins: origins})},
		"network with port":          {Port: 8080, Sessions: allowAll{}, Network: network(Network{TLS: tlsConf, Origins: origins})},
		"network without port":       {Sessions: allowAll{}, Network: network(Network{Addr: "127.0.0.1", TLS: tlsConf, Origins: origins})},
		"login off with sessions":    {Login: LoginOff, Sessions: allowAll{}},
		"unknown login mode":         {Login: Login(7)},
		"port out of range":          {Port: 70000},
		"route without method":       {Routes: []Route{{Pattern: "/items", Handler: noContent}}},
		"route with lower method":    {Routes: []Route{{Pattern: "get /items", Handler: noContent}}},
		"route without handler":      {Routes: []Route{{Pattern: "GET /items"}}},
		"route twice": {Routes: []Route{{Pattern: "GET /items", Handler: noContent},
			{Pattern: "GET /items", Handler: noContent}}},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			if s, err := Listen(opts); err == nil {
				for _, l := range s.listeners {
					l.Close()
				}
				t.Fatal("Listen accepted the options")
			}
		})
	}
}

func TestListenBindsLoopbackOnly(t *testing.T) {
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /items", Handler: noContent}}})
	if !strings.HasPrefix(s.URL(), "http://127.0.0.1:") {
		t.Fatalf("URL = %q", s.URL())
	}
	port := s.listeners[0].Addr().(*net.TCPAddr).Port
	urls := []string{s.URL()}
	for _, l := range s.listeners {
		a := l.Addr().(*net.TCPAddr)
		if !a.IP.IsLoopback() || a.Port != port {
			t.Fatalf("listener on %v, want loopback port %d", a, port)
		}
		if a.IP.To4() == nil {
			urls = append(urls, "http://"+a.String())
		}
	}
	if probe, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		probe.Close()
		if len(urls) != 2 {
			t.Fatalf("IPv6 loopback is available but not bound: %v", s.listeners)
		}
	}
	run(t, s)
	for _, u := range urls {
		resp, err := http.Get(u + "/items")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("%s: status %d", u, resp.StatusCode)
		}
	}
}

func TestServeShutsDownGracefully(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /slow", Handler: slow}}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()

	status := make(chan int, 1)
	go func() {
		resp, err := http.Get(s.URL() + "/slow")
		if err != nil {
			status <- 0
			return
		}
		resp.Body.Close()
		status <- resp.StatusCode
	}()
	<-started
	cancel()
	time.Sleep(50 * time.Millisecond)
	close(release)
	if got := <-status; got != http.StatusNoContent {
		t.Fatalf("running request ended with %d", got)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if _, err := http.Get(s.URL() + "/slow"); err == nil {
		t.Fatal("server still answers after shutdown")
	}
}

func TestLimitsAreSet(t *testing.T) {
	s := newServer(t, Options{})
	srv := s.server
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 ||
		srv.MaxHeaderBytes <= 0 || srv.MaxHeaderBytes > 64<<10 {
		t.Fatalf("limits not set: %+v", srv)
	}
}

func TestSlowHeaderIsCut(t *testing.T) {
	s := newServer(t, Options{Login: LoginOff})
	s.server.ReadHeaderTimeout = 100 * time.Millisecond
	run(t, s)
	conn, err := net.Dial("tcp", s.listeners[0].Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n"); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = io.ReadAll(conn)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("server kept the connection of a slow header open")
	}
}

func TestLargeHeaderIsRefused(t *testing.T) {
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /items", Handler: noContent}}})
	run(t, s)
	req, _ := http.NewRequest(http.MethodGet, s.URL()+"/items", nil)
	req.Header.Set("X-Filler", strings.Repeat("a", 32<<10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestLargeBodyIsRefused(t *testing.T) {
	var readErr error
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "POST /upload", Handler: read}}})
	s.limits.maxBodyBytes = 16

	declared := httptest.NewRequest(http.MethodPost, "http://localhost/upload", strings.NewReader(strings.Repeat("a", 17)))
	if rec := do(s, declared); rec.Code != http.StatusRequestEntityTooLarge || readErr != nil {
		t.Fatalf("declared length: status %d", rec.Code)
	}

	streamed := httptest.NewRequest(http.MethodPost, "http://localhost/upload", io.MultiReader(strings.NewReader(strings.Repeat("a", 17))))
	streamed.ContentLength = -1
	do(s, streamed)
	var tooLarge *http.MaxBytesError
	if !errors.As(readErr, &tooLarge) {
		t.Fatalf("streamed body read: %v", readErr)
	}
}

func TestLogIsRedactedAndQuoted(t *testing.T) {
	var buf bytes.Buffer
	var red redact.Redactor
	red.Add("s3cret-token-example")
	s := newServer(t, Options{Log: &buf, Redactor: &red})
	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	req.Host = "s3cret-token-example.example.test"
	req.URL.Path = "/\x1b[31mred"
	do(s, req)
	got := buf.String()
	if strings.Contains(got, "s3cret-token-example") || !strings.Contains(got, redact.Marker) {
		t.Fatalf("log not redacted: %q", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("log carries a control character: %q", got)
	}
}

func TestNetworkServesTLS(t *testing.T) {
	ts := httptest.NewUnstartedServer(nil)
	ts.StartTLS()
	cert, client := ts.TLS.Certificates[0], ts.Client()
	ts.Close()

	s := newServer(t, Options{
		Sessions: allowAll{},
		Network: &Network{Addr: "127.0.0.1:0", TLS: &tls.Config{Certificates: []tls.Certificate{cert}},
			Origins: []string{"https://tool.example.test"}},
		Routes: []Route{{Pattern: "GET /items", Handler: noContent}},
	})
	if s.URL() != "https://tool.example.test" {
		t.Fatalf("URL = %q", s.URL())
	}
	run(t, s)
	for host, want := range map[string]int{"tool.example.test": http.StatusNoContent,
		"tool.example.test:443": http.StatusNoContent, "localhost": http.StatusBadRequest,
		"evil.example.test": http.StatusBadRequest} {
		req, _ := http.NewRequest(http.MethodGet, "https://"+s.listeners[0].Addr().String()+"/items", nil)
		req.Host = host
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("host %q: status %d, want %d", host, resp.StatusCode, want)
		}
	}
	resp, err := http.Get("http://" + s.listeners[0].Addr().String() + "/items")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("plain HTTP on the TLS port: status %d", resp.StatusCode)
		}
	}
}
