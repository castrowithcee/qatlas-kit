package web

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func anyMethod(handler http.Handler) []Route {
	var routes []Route
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		routes = append(routes, Route{Pattern: m + " /items", Handler: handler, Public: true})
	}
	return routes
}

func TestHostIsChecked(t *testing.T) {
	s := newServer(t, Options{Routes: anyMethod(noContent)})
	cases := map[string]int{
		"localhost:8080": http.StatusNoContent, "LOCALHOST:1": http.StatusNoContent,
		"127.0.0.1:8080": http.StatusNoContent, "[::1]:8080": http.StatusNoContent,
		"localhost": http.StatusNoContent, "127.0.0.1": http.StatusNoContent, "[::1]": http.StatusNoContent,
		"evil.example.test": http.StatusBadRequest, "evil.example.test:8080": http.StatusBadRequest,
		"localhost.example.test:8080": http.StatusBadRequest, "localhost.:8080": http.StatusBadRequest,
		"127.0.0.2:8080": http.StatusBadRequest, "[::2]:8080": http.StatusBadRequest, "::1": http.StatusBadRequest,
		"": http.StatusBadRequest,
	}
	for host, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://localhost/items", nil)
		req.Host = host
		if got := do(s, req).Code; got != want {
			t.Errorf("host %q: status %d, want %d", host, got, want)
		}
	}
}

func TestUnsafeRequestsNeedTheirOrigin(t *testing.T) {
	s := newServer(t, Options{Routes: anyMethod(noContent)})
	cases := []struct {
		name, method, site string
		origins            []string
		want               int
	}{
		{"no browser headers", "POST", "", nil, http.StatusNoContent},
		{"same origin", "POST", "same-origin", []string{"http://127.0.0.1:8080"}, http.StatusNoContent},
		{"typed by the user", "POST", "none", nil, http.StatusNoContent},
		{"origin matches host", "POST", "", []string{"http://127.0.0.1:8080"}, http.StatusNoContent},
		{"cross site", "POST", "cross-site", []string{"http://evil.example.test"}, http.StatusForbidden},
		{"same site", "POST", "same-site", []string{"http://127.0.0.1:9090"}, http.StatusForbidden},
		{"cross site put", "PUT", "cross-site", nil, http.StatusForbidden},
		{"cross site patch", "PATCH", "cross-site", nil, http.StatusForbidden},
		{"cross site delete", "DELETE", "cross-site", nil, http.StatusForbidden},
		{"foreign origin", "POST", "", []string{"http://evil.example.test"}, http.StatusForbidden},
		{"other port", "POST", "same-origin", []string{"http://127.0.0.1:9090"}, http.StatusForbidden},
		{"https scheme", "POST", "", []string{"https://127.0.0.1:8080"}, http.StatusForbidden},
		{"null origin", "POST", "", []string{"null"}, http.StatusForbidden},
		{"null origin same origin", "POST", "same-origin", []string{"null"}, http.StatusForbidden},
		{"two origins", "POST", "", []string{"http://127.0.0.1:8080", "http://evil.example.test"}, http.StatusForbidden},
		{"cross site get", "GET", "cross-site", []string{"http://evil.example.test"}, http.StatusNoContent},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, "http://127.0.0.1:8080/items", nil)
		if c.site != "" {
			req.Header.Set("Sec-Fetch-Site", c.site)
		}
		for _, o := range c.origins {
			req.Header.Add("Origin", o)
		}
		if got := do(s, req).Code; got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
}

func TestNetworkOriginsAreListed(t *testing.T) {
	s := newServer(t, Options{Sessions: allowAll{}, Routes: anyMethod(noContent), Network: &Network{
		Addr: "127.0.0.1:0", Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
		Origins: []string{"https://tool.example.test"}}})
	for origin, want := range map[string]int{
		"https://tool.example.test": http.StatusNoContent, "https://TOOL.example.test": http.StatusNoContent,
		"http://tool.example.test": http.StatusForbidden, "https://tool.example.test:8443": http.StatusForbidden,
		"https://evil.example.test": http.StatusForbidden, "null": http.StatusForbidden,
	} {
		req := httptest.NewRequest(http.MethodPost, "http://tool.example.test/items", nil)
		req.RemoteAddr = "192.0.2.7:40000"
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", origin)
		if got := do(s, req).Code; got != want {
			t.Errorf("origin %q: status %d, want %d", origin, got, want)
		}
	}
}

func TestForwardedHeadersOnlyFromProxies(t *testing.T) {
	var host, forwardedFor string
	seen := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, forwardedFor = r.Host, r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusNoContent)
	})
	proxied := newServer(t, Options{Sessions: allowAll{}, Routes: anyMethod(seen), Network: &Network{
		Addr: "127.0.0.1:0", Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
		Origins: []string{"https://tool.example.test"}}})

	cases := []struct {
		name, remote, forwardedHost string
		want                        int
		wantHost                    string
	}{
		{"from proxy", "192.0.2.7:40000", "tool.example.test", http.StatusNoContent, "tool.example.test"},
		{"client entry before proxy", "192.0.2.7:40000", "evil.example.test, tool.example.test", http.StatusNoContent, "tool.example.test"},
		{"foreign host from proxy", "192.0.2.7:40000", "tool.example.test, evil.example.test", http.StatusBadRequest, ""},
		{"direct without TLS", "198.51.100.9:40000", "tool.example.test", http.StatusForbidden, ""},
	}
	for _, c := range cases {
		host = ""
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/items", nil)
		req.RemoteAddr = c.remote
		req.Header.Set("X-Forwarded-Host", c.forwardedHost)
		req.Header.Set("X-Forwarded-For", "203.0.113.5")
		if got := do(proxied, req).Code; got != c.want || host != c.wantHost {
			t.Errorf("%s: status %d host %q, want %d %q", c.name, got, host, c.want, c.wantHost)
		}
	}
	if forwardedFor != "203.0.113.5" {
		t.Errorf("proxy header dropped: %q", forwardedFor)
	}

	direct := newServer(t, Options{Sessions: allowAll{}, Routes: anyMethod(seen), Network: &Network{
		Addr: "127.0.0.1:0", TLS: &tls.Config{Certificates: []tls.Certificate{{}}},
		Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, Origins: []string{"https://tool.example.test"}}})
	req := httptest.NewRequest(http.MethodGet, "https://tool.example.test/items", nil)
	req.RemoteAddr = "198.51.100.9:40000"
	req.Header.Set("X-Forwarded-Host", "evil.example.test")
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	if got := do(direct, req).Code; got != http.StatusNoContent || host != "tool.example.test" || forwardedFor != "" {
		t.Fatalf("direct over TLS: status %d host %q forwarded for %q", got, host, forwardedFor)
	}

	loopback := newServer(t, Options{Routes: anyMethod(seen)})
	req = httptest.NewRequest(http.MethodGet, "http://localhost:8080/items", nil)
	req.Header.Set("X-Forwarded-Host", "evil.example.test")
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	if got := do(loopback, req).Code; got != http.StatusNoContent || host != "localhost:8080" || forwardedFor != "" {
		t.Fatalf("loopback: status %d host %q forwarded for %q", got, host, forwardedFor)
	}
}
