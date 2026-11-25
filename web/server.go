package web

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/redact"
)

// Options configure a Server. The zero value serves nothing on a free loopback port and refuses every
// route that is not public.
type Options struct {
	// Port is the loopback port; 0 picks a free one. It must stay 0 with Network.
	Port int
	// Network binds the server to another address instead of loopback.
	Network *Network
	// Routes are the handlers of the application.
	Routes []Route
	// Sessions decides about every route that is not public. Without it such routes are refused unless
	// Login is LoginOff.
	Sessions Sessions
	// Login selects whether routes that are not public need a session.
	Login Login
	// Log receives refusals and the errors of the HTTP server; nil discards them.
	Log io.Writer
	// Redactor removes known secrets from every log line; nil leaves the text as it is.
	Redactor *redact.Redactor
}

// Network binds the server to an address other than loopback. It starts only with TLS or with declared
// proxies, and only for the origins it names.
type Network struct {
	// Addr is the host:port to bind.
	Addr string
	// TLS serves HTTPS directly. It must hold a certificate or a way to get one.
	TLS *tls.Config
	// Proxies are the addresses of reverse proxies that terminate TLS. Only requests from them may carry
	// X-Forwarded-* and Forwarded headers; without TLS no other client is served.
	Proxies []netip.Prefix
	// Origins are the origins browsers use, in the form https://host[:port].
	Origins []string
}

// Route is one handler of the application.
type Route struct {
	// Pattern is a net/http ServeMux pattern that starts with its method, such as "POST /items", so a
	// handler that changes state is never reached by GET.
	Pattern string
	Handler http.Handler
	// Public routes are served without a session.
	Public bool
}

// Login selects whether routes that are not public need a session.
type Login int

const (
	// LoginRequired lets only requests with a valid session reach routes that are not public.
	LoginRequired Login = iota
	// LoginOff serves every route without a session. It is refused unless the server binds to loopback
	// alone.
	LoginOff
)

type limits struct {
	readHeader, read, write, idle, shutdown time.Duration
	maxHeaderBytes                          int
	maxBodyBytes                            int64
}

var defaultLimits = limits{
	readHeader:     10 * time.Second,
	read:           30 * time.Second,
	write:          60 * time.Second,
	idle:           120 * time.Second,
	shutdown:       5 * time.Second,
	maxHeaderBytes: 16 << 10,
	maxBodyBytes:   1 << 20,
}

var methods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true,
}

// Server is a bound HTTP server for browsers. Listen creates it and Serve runs it.
type Server struct {
	listeners []net.Listener
	server    *http.Server
	mux       *http.ServeMux
	cop       *http.CrossOriginProtection
	limits    limits
	log       *log.Logger
	sessions  Sessions
	login     Login
	url       string
	streams   context.Context

	network bool
	tls     bool
	proxies []netip.Prefix
	origins map[string]bool
	hosts   map[string]bool
}

// Listen checks opts and binds the server. Nothing is served until Serve runs.
func Listen(opts Options) (*Server, error) {
	s := &Server{
		mux:      http.NewServeMux(),
		cop:      http.NewCrossOriginProtection(),
		limits:   defaultLimits,
		sessions: opts.Sessions,
		login:    opts.Login,
	}
	switch opts.Login {
	case LoginRequired:
	case LoginOff:
		if opts.Sessions != nil {
			return nil, errors.New("web: LoginOff and Sessions exclude each other")
		}
	default:
		return nil, fmt.Errorf("web: unknown login mode %d", opts.Login)
	}
	if opts.Port < 0 || opts.Port > 65535 {
		return nil, fmt.Errorf("web: port %d out of range", opts.Port)
	}
	if opts.Network != nil {
		if err := s.checkNetwork(opts); err != nil {
			return nil, err
		}
	}
	for _, rt := range opts.Routes {
		if err := s.handle(rt); err != nil {
			return nil, err
		}
	}

	w := opts.Log
	if w == nil {
		w = io.Discard
	}
	if opts.Redactor != nil {
		w = opts.Redactor.Writer(w)
	}
	s.log = log.New(w, "", log.LstdFlags)

	var err error
	if opts.Network != nil {
		var l net.Listener
		l, err = net.Listen("tcp", opts.Network.Addr)
		s.listeners = []net.Listener{l}
	} else {
		s.listeners, err = listenLoopback(opts.Port)
		if err == nil {
			s.url = "http://" + s.listeners[0].Addr().String()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("web: listen: %w", err)
	}

	s.server = &http.Server{
		Handler:           http.HandlerFunc(s.serve),
		ReadHeaderTimeout: s.limits.readHeader,
		ReadTimeout:       s.limits.read,
		WriteTimeout:      s.limits.write,
		IdleTimeout:       s.limits.idle,
		MaxHeaderBytes:    s.limits.maxHeaderBytes,
		ErrorLog:          s.log,
	}
	// Shutdown would otherwise wait for open event streams until its deadline.
	var stopStreams context.CancelFunc
	s.streams, stopStreams = context.WithCancel(context.Background())
	s.server.RegisterOnShutdown(stopStreams)
	if s.tls {
		cfg := opts.Network.TLS.Clone()
		cfg.MinVersion = max(cfg.MinVersion, tls.VersionTLS12)
		s.server.TLSConfig = cfg
	}
	return s, nil
}

func (s *Server) checkNetwork(opts Options) error {
	n := opts.Network
	if opts.Port != 0 {
		return errors.New("web: Port applies to loopback only; Network carries its own address")
	}
	if opts.Login != LoginRequired || opts.Sessions == nil {
		return errors.New("web: a network server needs Sessions")
	}
	if _, _, err := net.SplitHostPort(n.Addr); err != nil {
		return fmt.Errorf("web: network address: %w", err)
	}
	if n.TLS != nil {
		if len(n.TLS.Certificates) == 0 && n.TLS.GetCertificate == nil && n.TLS.GetConfigForClient == nil {
			return errors.New("web: the TLS configuration holds no certificate")
		}
		s.tls = true
	}
	for _, p := range n.Proxies {
		if !p.IsValid() {
			return errors.New("web: invalid proxy prefix")
		}
	}
	if !s.tls && len(n.Proxies) == 0 {
		return errors.New("web: a network server needs TLS or declared proxies")
	}
	if len(n.Origins) == 0 {
		return errors.New("web: a network server needs its allowed origins")
	}
	s.network = true
	s.proxies = n.Proxies
	s.origins = map[string]bool{}
	s.hosts = map[string]bool{}
	for _, o := range n.Origins {
		u, err := url.Parse(o)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" ||
			u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return fmt.Errorf("web: origin %q is not of the form https://host[:port]", o)
		}
		host := strings.TrimSuffix(strings.ToLower(u.Host), ":443")
		s.origins["https://"+host] = true
		s.hosts[host] = true
		if u.Port() == "" || u.Port() == "443" {
			s.hosts[host+":443"] = true
		}
		if s.url == "" {
			s.url = "https://" + host
		}
	}
	return nil
}

func (s *Server) handle(rt Route) (err error) {
	method, _, ok := strings.Cut(rt.Pattern, " ")
	if !ok || !methods[method] {
		return fmt.Errorf("web: route %q does not start with its method", rt.Pattern)
	}
	if rt.Handler == nil {
		return fmt.Errorf("web: route %q has no handler", rt.Pattern)
	}
	// ServeMux reports an invalid or conflicting pattern only by panicking.
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("web: route %q: %v", rt.Pattern, v)
		}
	}()
	s.mux.Handle(rt.Pattern, s.gate(rt))
	return nil
}

// listenLoopback binds 127.0.0.1 and ::1 to the same port, so localhost reaches this server whichever
// family the browser resolves it to.
func listenLoopback(port int) ([]net.Listener, error) {
	v4addr := func(p int) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(p)) }
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		// Without IPv6 loopback no other process can take [::1] either.
		l, err := net.Listen("tcp4", v4addr(port))
		if err != nil {
			return nil, err
		}
		return []net.Listener{l}, nil
	}
	probe.Close()
	for range 8 {
		v4, err := net.Listen("tcp4", v4addr(port))
		if err != nil {
			return nil, err
		}
		p := v4.Addr().(*net.TCPAddr).Port
		v6, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(p)))
		if err == nil {
			return []net.Listener{v4, v6}, nil
		}
		v4.Close()
		if port != 0 {
			return nil, err
		}
	}
	return nil, errors.New("no port free on both loopback addresses")
}

// URL is the address a browser opens: http://127.0.0.1:<port> on loopback, the first origin on the network.
func (s *Server) URL() string { return s.url }

// Serve answers requests until ctx ends, then stops accepting, waits a short time for running requests and
// closes the remaining connections. It returns nil after such a shutdown.
func (s *Server) Serve(ctx context.Context) error {
	errs := make(chan error, len(s.listeners))
	var wg sync.WaitGroup
	for _, l := range s.listeners {
		wg.Go(func() {
			var err error
			if s.tls {
				err = s.server.ServeTLS(l, "", "")
			} else {
				err = s.server.Serve(l)
			}
			if !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		})
	}
	var err error
	select {
	case <-ctx.Done():
	case err = <-errs:
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.limits.shutdown)
	defer cancel()
	if s.server.Shutdown(shutdown) != nil {
		s.server.Close()
	}
	wg.Wait()
	return err
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	r = fixedHeaders(w, r)
	r = r.WithContext(context.WithValue(r.Context(), serverKey{}, s))
	r, ok := s.forwarded(w, r)
	if !ok {
		return
	}
	if !s.hostAllowed(r.Host) {
		s.refuse(w, r, http.StatusBadRequest, "host %q not allowed", r.Host)
		return
	}
	if r.ContentLength > s.limits.maxBodyBytes {
		s.refuse(w, r, http.StatusRequestEntityTooLarge, "body of %d bytes", r.ContentLength)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.limits.maxBodyBytes)
	if err := s.checkOrigin(r); err != nil {
		s.refuse(w, r, http.StatusForbidden, "%v", err)
		return
	}
	if _, pattern := s.mux.Handler(r); pattern == "" {
		s.mux.ServeHTTP(&statusPage{ResponseWriter: w}, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) refuse(w http.ResponseWriter, r *http.Request, status int, format string, args ...any) {
	s.log.Printf("web: refused %s %q: %s", r.Method, r.URL.Path, fmt.Sprintf(format, args...))
	Error(w, status)
}
