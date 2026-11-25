package web

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var loopbackHosts = map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}

// hostAllowed checks the Host header against fixed names, so a page reached through DNS rebinding never
// gets an answer: the loopback names with any port, or on the network the hosts of the allowed origins.
func (s *Server) hostAllowed(host string) bool {
	host = strings.ToLower(host)
	if s.network {
		return s.hosts[host]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return loopbackHosts[h]
	}
	return host == "localhost" || host == "127.0.0.1" || host == "[::1]"
}

// checkOrigin refuses an unsafe request from another origin. Requests without Sec-Fetch-Site and Origin
// come from clients that are not browsers and pass.
func (s *Server) checkOrigin(r *http.Request) error {
	if err := s.cop.Check(r); err != nil {
		return err
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	values := r.Header.Values("Origin")
	switch {
	case len(values) == 0:
		return nil
	case len(values) > 1:
		return errors.New("more than one Origin header")
	}
	if !s.originAllowed(values[0], r.Host) {
		return errors.New("origin not allowed")
	}
	return nil
}

// originAllowed compares an Origin header with the origin of this server. It refuses Origin: null as
// well, which Sec-Fetch-Site: same-origin alone would let through.
func (s *Server) originAllowed(origin, host string) bool {
	origin = strings.ToLower(origin)
	if s.network {
		return s.origins[origin]
	}
	return origin == "http://"+strings.ToLower(host)
}

var forwardedHeaders = []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto",
	"X-Forwarded-Port", "X-Real-Ip"}

// forwarded keeps forwarding headers only from declared proxies and takes the host the browser used from
// them. Without TLS it serves no client but a proxy.
func (s *Server) forwarded(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if s.fromProxy(r) {
		if fh := r.Header.Values("X-Forwarded-Host"); len(fh) > 0 {
			// The nearest proxy appends last; earlier entries come from the client.
			parts := strings.Split(fh[len(fh)-1], ",")
			r = r.Clone(r.Context())
			r.Host = strings.TrimSpace(parts[len(parts)-1])
		}
		return r, true
	}
	if s.network && !s.tls {
		s.refuse(w, r, http.StatusForbidden, "direct request without TLS from %q", r.RemoteAddr)
		return r, false
	}
	for _, name := range forwardedHeaders {
		r.Header.Del(name)
	}
	return r, true
}

func (s *Server) fromProxy(r *http.Request) bool {
	if len(s.proxies) == 0 {
		return false
	}
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	for _, p := range s.proxies {
		if p.Contains(ap.Addr().Unmap()) {
			return true
		}
	}
	return false
}
