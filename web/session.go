package web

import "net/http"

// Sessions decides whether a request to a route that is not public may proceed. The server consults it
// after the host and origin checks and before the route handler.
type Sessions interface {
	// Session reports whether r belongs to a valid session and returns the request the route handler
	// receives, for example with the session in its context; nil keeps r. On false the route handler does
	// not run: Session may answer itself, such as with a redirect to a sign-in page, and otherwise the
	// server answers 401.
	Session(w http.ResponseWriter, r *http.Request) (*http.Request, bool)
}

func (s *Server) gate(rt Route) http.Handler {
	if rt.Public || s.login == LoginOff {
		return rt.Handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.sessions == nil {
			s.refuse(w, r, http.StatusForbidden, "no session check for a route that is not public")
			return
		}
		tw := &trackWriter{ResponseWriter: w}
		next, ok := s.sessions.Session(tw, r)
		if !ok {
			if !tw.wrote {
				s.refuse(w, r, http.StatusUnauthorized, "no valid session")
			}
			return
		}
		if next == nil {
			next = r
		}
		rt.Handler.ServeHTTP(w, next)
	})
}

type trackWriter struct {
	http.ResponseWriter
	wrote bool
}

func (t *trackWriter) WriteHeader(code int) {
	t.wrote = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackWriter) Write(p []byte) (int, error) {
	t.wrote = true
	return t.ResponseWriter.Write(p)
}

func (t *trackWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }
