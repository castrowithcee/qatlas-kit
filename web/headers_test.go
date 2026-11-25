package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFixedHeadersCarryANoncePerResponse(t *testing.T) {
	var nonce string
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nonce = Nonce(r) })
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /page", Handler: page}}})

	seen := map[string]bool{}
	for range 3 {
		req := httptest.NewRequest(http.MethodGet, "http://localhost/page", nil)
		req.Header.Set("Origin", "http://evil.example.test")
		h := do(s, req).Header()
		csp := h.Get("Content-Security-Policy")
		if len(nonce) < 22 || seen[nonce] || !strings.Contains(csp, "script-src 'self' 'nonce-"+nonce+"'") {
			t.Fatalf("nonce %q not fresh or not in %q", nonce, csp)
		}
		seen[nonce] = true
		for _, d := range []string{"default-src 'self'", "object-src 'none'", "base-uri 'none'", "form-action 'self'",
			"frame-ancestors 'none'"} {
			if !strings.Contains(csp, d) {
				t.Errorf("CSP %q lacks %q", csp, d)
			}
		}
		if strings.Contains(csp, "unsafe-") || strings.Contains(csp, "*") {
			t.Errorf("CSP %q is not strict", csp)
		}
		for name, want := range map[string]string{"X-Content-Type-Options": "nosniff",
			"Referrer-Policy": "no-referrer", "Cross-Origin-Resource-Policy": "same-origin"} {
			if got := h.Get(name); got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
		for name := range h {
			if strings.HasPrefix(name, "Access-Control-") {
				t.Errorf("CORS header %s granted", name)
			}
		}
	}
}

func TestRefusalsCarryFixedHeaders(t *testing.T) {
	s := newServer(t, Options{})
	req := httptest.NewRequest(http.MethodOptions, "http://evil.example.test/items", nil)
	req.Header.Set("Origin", "http://evil.example.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := do(s, req)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Security-Policy") == "" ||
		rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("status %d headers %v", rec.Code, rec.Header())
	}
}
