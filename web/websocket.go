package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultWebSocketMessageBytes = 64 << 10
	defaultWebSocketIdle         = 60 * time.Second
	webSocketWrite               = 10 * time.Second
)

// WebSocketOptions configures the handler returned by WebSocket. Zero values select the safe defaults.
type WebSocketOptions struct {
	// Handler runs once per accepted connection and owns the conversation. The connection closes when it
	// returns. Required.
	Handler func(ctx context.Context, c *WebSocketConn)
	// MaxMessageBytes bounds the size of a received message; a larger one closes the connection.
	// Default 64 KiB.
	MaxMessageBytes int64
	// IdleTimeout bounds the wait for the next message; when it passes, the connection closes.
	// Default 60 seconds.
	IdleTimeout time.Duration
}

// WebSocketConn is an accepted connection. Read and Write may be used from one goroutine each.
type WebSocketConn struct {
	c    *websocket.Conn
	idle time.Duration
}

// Read returns the next message, text or binary. It fails once IdleTimeout passes without one, when a
// message exceeds MaxMessageBytes, when the peer closes, and when ctx ends or the server shuts down.
func (c *WebSocketConn) Read(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.idle)
	defer cancel()
	_, b, err := c.c.Read(ctx)
	return b, err
}

// Write sends data as one text message, within a fixed deadline against a peer that stops reading.
func (c *WebSocketConn) Write(ctx context.Context, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, webSocketWrite)
	defer cancel()
	return c.c.Write(ctx, websocket.MessageText, data)
}

// WebSocket returns the handler of a WebSocket endpoint, for a GET route of a Server. Without it the
// server speaks no WebSocket. It panics if opt.Handler is nil.
func WebSocket(opt WebSocketOptions) http.Handler {
	if opt.Handler == nil {
		panic("web: WebSocket needs a Handler")
	}
	if opt.MaxMessageBytes <= 0 {
		opt.MaxMessageBytes = defaultWebSocketMessageBytes
	}
	if opt.IdleTimeout <= 0 {
		opt.IdleTimeout = defaultWebSocketIdle
	}
	return webSocket(opt)
}

type webSocket WebSocketOptions

func (h webSocket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s, _ := r.Context().Value(serverKey{}).(*Server)
	if s == nil {
		Error(w, http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodGet {
		s.refuse(w, r, http.StatusMethodNotAllowed, "websocket by %s", r.Method)
		return
	}
	if err := s.checkWebSocketOrigin(r); err != nil {
		s.refuse(w, r, http.StatusForbidden, "websocket: %v", err)
		return
	}
	// The library checks the Origin host against the Host header as well; that check stays on.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		// Accept has answered the handshake itself.
		s.log.Printf("web: websocket %q: %v", r.URL.Path, err)
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(h.MaxMessageBytes)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer context.AfterFunc(s.streams, cancel)()
	h.Handler(ctx, &WebSocketConn{c: c, idle: h.IdleTimeout})
	c.Close(websocket.StatusNormalClosure, "")
}

// checkWebSocketOrigin is the stream handshake check, and the Origin must be present: a browser always
// sends it on a WebSocket handshake, so a request without one is not a page and has no cookies to abuse.
func (s *Server) checkWebSocketOrigin(r *http.Request) error {
	if err := s.checkStreamOrigin(r); err != nil {
		return err
	}
	if len(r.Header.Values("Origin")) == 0 {
		return errors.New("no Origin header")
	}
	return nil
}
