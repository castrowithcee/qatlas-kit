package web

import (
	"context"
	"crypto/rand"
	"net/http"
)

type nonceKey struct{}

// Nonce returns the CSP nonce of the response to r. A script or style element carries it in its nonce
// attribute to run inline; it is empty for a request the server did not handle.
func Nonce(r *http.Request) string {
	n, _ := r.Context().Value(nonceKey{}).(string)
	return n
}

// fixedHeaders sets the fixed headers on every response, with a fresh nonce for its policy.
func fixedHeaders(w http.ResponseWriter, r *http.Request) *http.Request {
	nonce := rand.Text()
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'nonce-"+nonce+"'; style-src 'self' 'nonce-"+
		nonce+"'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	return r.WithContext(context.WithValue(r.Context(), nonceKey{}, nonce))
}
