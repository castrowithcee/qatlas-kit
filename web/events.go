package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Event is one message of an event stream.
type Event struct {
	// ID is what the browser sends back as Last-Event-ID when it reconnects; empty keeps the previous one.
	// It must not contain a line break or NUL.
	ID string
	// Type is the event type the browser listens for; empty means "message". It must not contain a line
	// break or NUL.
	Type string
	// Data is the payload. Each of its lines goes out as a data line of its own.
	Data string
}

// Source supplies the events of a stream.
type Source interface {
	// Subscribe starts the stream for r. lastID is the Last-Event-ID of a reconnecting browser, empty on a
	// first connection, so the stream can resume after it; it comes from the client and is checked only
	// for its length and characters. The events of the returned channel go to the browser in order, and
	// closing the channel ends the stream. Once ctx ends, the stream is gone and nothing more may be sent.
	// An error refuses the stream.
	Subscribe(ctx context.Context, r *http.Request, lastID string) (<-chan Event, error)
}

type streamLimits struct {
	heartbeat, write, duration time.Duration
	conns, eventBytes, idBytes int
}

var defaultStreamLimits = streamLimits{
	heartbeat:  15 * time.Second,
	write:      10 * time.Second,
	duration:   10 * time.Minute,
	conns:      32,
	eventBytes: 64 << 10,
	idBytes:    256,
}

type eventStream struct {
	src    Source
	limits streamLimits
	open   atomic.Int64
}

type serverKey struct{}

// Events returns the handler of an event stream fed by src, for a GET route of a Server. It panics if src
// is nil.
func Events(src Source) http.Handler {
	if src == nil {
		panic("web: Events needs a Source")
	}
	return &eventStream{src: src, limits: defaultStreamLimits}
}

func (e *eventStream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s, _ := r.Context().Value(serverKey{}).(*Server)
	if s == nil {
		// Outside a Server neither the allowed origin nor the shutdown is known.
		Error(w, http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodGet {
		s.refuse(w, r, http.StatusMethodNotAllowed, "event stream by %s", r.Method)
		return
	}
	if err := s.checkStreamOrigin(r); err != nil {
		s.refuse(w, r, http.StatusForbidden, "event stream: %v", err)
		return
	}
	lastID, err := e.lastID(r)
	if err != nil {
		s.refuse(w, r, http.StatusBadRequest, "event stream: %v", err)
		return
	}
	if e.open.Add(1) > int64(e.limits.conns) {
		e.open.Add(-1)
		s.refuse(w, r, http.StatusServiceUnavailable, "event stream: %d streams open", e.limits.conns)
		return
	}
	defer e.open.Add(-1)

	// The server's read and write timeouts would cut the stream; the duration limit bounds it instead,
	// and each write gets its own deadline against a client that stops reading.
	rc := http.NewResponseController(w)
	if err := rc.SetReadDeadline(time.Time{}); err != nil {
		s.streamFailed(w, r, err)
		return
	}
	if err := rc.SetWriteDeadline(time.Now().Add(e.limits.write)); err != nil {
		s.streamFailed(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), e.limits.duration)
	defer cancel()
	defer context.AfterFunc(s.streams, cancel)()

	events, err := e.src.Subscribe(ctx, r, lastID)
	if err != nil {
		s.streamFailed(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	// Keeps a buffering reverse proxy such as nginx from holding events back.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(b []byte) error {
		if err := rc.SetWriteDeadline(time.Now().Add(e.limits.write)); err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		return rc.Flush()
	}

	heartbeat := time.NewTicker(e.limits.heartbeat)
	defer heartbeat.Stop()
	var buf []byte
	err = rc.Flush()
	for err == nil {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			err = send([]byte(":\n\n"))
		case ev, ok := <-events:
			if !ok {
				return
			}
			if buf, err = e.appendEvent(buf[:0], ev); err != nil {
				s.log.Printf("web: event stream %q ended: %v", r.URL.Path, err)
				return
			}
			err = send(buf)
		}
	}
}

func (s *Server) streamFailed(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Printf("web: event stream %q: %v", r.URL.Path, err)
	Error(w, http.StatusInternalServerError)
}

// checkStreamOrigin checks the handshake of an event stream, which checkOrigin passes as a GET, so a page of
// another origin cannot open a stream with the cookies of the browser. A browser leaves out Origin on a
// same-origin GET but sends Sec-Fetch-Site; a request without either header comes from a client that is
// not a browser.
func (s *Server) checkStreamOrigin(r *http.Request) error {
	switch site := r.Header.Get("Sec-Fetch-Site"); site {
	case "", "same-origin", "none":
	default:
		return fmt.Errorf("Sec-Fetch-Site %q", site)
	}
	switch values := r.Header.Values("Origin"); {
	case len(values) > 1:
		return errors.New("more than one Origin header")
	case len(values) == 1 && !s.originAllowed(values[0], r.Host):
		return fmt.Errorf("origin %q not allowed", values[0])
	}
	return nil
}

func (e *eventStream) lastID(r *http.Request) (string, error) {
	values := r.Header.Values("Last-Event-ID")
	switch {
	case len(values) == 0:
		return "", nil
	case len(values) > 1:
		return "", errors.New("more than one Last-Event-ID header")
	case len(values[0]) > e.limits.idBytes:
		return "", fmt.Errorf("Last-Event-ID of %d bytes", len(values[0]))
	case !streamField(values[0]):
		return "", errors.New("Last-Event-ID holds a line break or NUL")
	}
	return values[0], nil
}

// appendEvent encodes ev as text/event-stream. Fields that would end their line early are refused, since
// they could inject fields or events of their own.
func (e *eventStream) appendEvent(b []byte, ev Event) ([]byte, error) {
	switch {
	case len(ev.ID) > e.limits.idBytes:
		return b, fmt.Errorf("event ID of %d bytes", len(ev.ID))
	case len(ev.ID)+len(ev.Type)+len(ev.Data) > e.limits.eventBytes:
		return b, fmt.Errorf("event of %d bytes", len(ev.ID)+len(ev.Type)+len(ev.Data))
	case !streamField(ev.ID) || !streamField(ev.Type):
		return b, errors.New("event ID or type holds a line break or NUL")
	}
	if ev.ID != "" {
		b = append(append(append(b, "id: "...), ev.ID...), '\n')
	}
	if ev.Type != "" {
		b = append(append(append(b, "event: "...), ev.Type...), '\n')
	}
	// A browser ends a line at CRLF, CR or LF alike.
	data := strings.ReplaceAll(strings.ReplaceAll(ev.Data, "\r\n", "\n"), "\r", "\n")
	for line := range strings.SplitSeq(data, "\n") {
		b = append(append(append(b, "data: "...), line...), '\n')
	}
	return append(b, '\n'), nil
}

func streamField(v string) bool { return !strings.ContainsAny(v, "\r\n\x00") }
