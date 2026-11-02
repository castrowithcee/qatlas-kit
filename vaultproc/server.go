package vaultproc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

// watchInterval is how often a server checks that its socket is still its own and its program in place.
const watchInterval = 2 * time.Second

// HandlerFunc answers one checked request of an application operation. body is the request's body as the
// client sent it, untrusted. A non-nil result is encoded as the answer's result. An *Error answers with its
// code; any other error answers with the code of ErrFailed, and its text never leaves the process. A handler
// runs alone: no other request is served while it runs, so it keeps its work short.
type HandlerFunc func(ctx context.Context, held *Held, body json.RawMessage) (any, error)

// Config is what an application chooses for its vault process. The zero value is not valid: Endpoint,
// Version and IdleTimeout are required.
type Config struct {
	// Endpoint is the channel the process serves. The application names it; see SocketBase.
	Endpoint localipc.Endpoint
	// Version is the application's protocol version, at least 1. Clients of another version are refused.
	Version int
	// IdleTimeout is how long the process stays unlocked without a request that calls Held.Touch. It must
	// be positive; the user chooses it, the kit sets no default.
	IdleTimeout time.Duration
	// MaxUnlock, when positive, is the longest the process stays unlocked from State.UnlockedAt, whatever
	// touches it. Zero means no limit.
	MaxUnlock time.Duration
	// RequestTimeout bounds each connection. Zero means DefaultRequestTimeout.
	RequestTimeout time.Duration
	// Handlers are the application's operations by name. A name matches [a-z][a-z0-9-]* and is none of
	// OpStatus, OpLock and OpHandover.
	Handlers map[string]HandlerFunc
	// Handover lets the process hand the vault over to a successor started from the program an update
	// installed. Nil means every handover request locks the process.
	Handover *HandoverConfig
}

func (c Config) validate() error {
	if c.Version < 1 {
		return errors.New("vaultproc: the application protocol version must be at least 1")
	}
	if c.IdleTimeout <= 0 {
		return errors.New("vaultproc: the idle timeout must be positive")
	}
	if c.MaxUnlock < 0 || c.RequestTimeout < 0 {
		return errors.New("vaultproc: a duration must not be negative")
	}
	for op, h := range c.Handlers {
		if !wordPattern.MatchString(op) || op == OpStatus || op == OpLock || op == OpHandover {
			return fmt.Errorf("vaultproc: %q is not an operation name an application can register", op)
		}
		if h == nil {
			return fmt.Errorf("vaultproc: operation %q has no handler", op)
		}
	}
	if c.Handover != nil {
		return c.Handover.validate()
	}
	return nil
}

// Held is the vault a handler serves from. It is valid only while the handler runs.
type Held struct{ s *Server }

// Identity returns the vault's key, for example to derive a key with vault.DeriveKey or to reopen the
// vault with vault.OpenWithIdentity.
func (h *Held) Identity() *age.X25519Identity { return h.s.key }

// Document returns the held document. The bytes are the process's own copy: the handler reads them and
// must neither change nor keep them.
func (h *Held) Document() []byte { return h.s.doc.bytes() }

// Replace replaces the held document with a copy of doc and overwrites the previous one.
func (h *Held) Replace(doc []byte) {
	old := h.s.doc
	h.s.doc = newLocked(doc)
	old.release()
}

// Touch restarts the idle period, never past the maximum unlock time.
func (h *Held) Touch() { h.s.touch() }

// Server holds an unlocked vault in memory and answers clients on one listener. Use NewServer.
type Server struct {
	cfg Config

	// program reports this process's program and whether it was replaced; start starts a successor. Both
	// are replaced in tests.
	program    func() (string, bool, error)
	start      func(ctx context.Context, program string, state []byte) (pid int, end func(), err error)
	watchEvery time.Duration

	mu       sync.Mutex
	key      *age.X25519Identity
	doc      *lockedBytes
	locked   bool
	unlocked time.Time
	hardAt   time.Time
	locksAt  time.Time
	timer    *time.Timer
	// handingOver is set once a handover is committed; inFlight counts requests read and not yet answered;
	// pending is the connection of a prepared handover.
	handingOver bool
	inFlight    int
	pending     net.Conn

	listener net.Listener
	stopOnce sync.Once
	stopped  chan struct{}
	handlers sync.WaitGroup
}

// NewServer returns a server holding state. It copies the document into memory of its own and clears
// state.Document. A successor's state must carry LocksAt.
func NewServer(cfg Config, state State, successor bool) (*Server, error) {
	defer clear(state.Document)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if state.Identity == nil {
		return nil, errors.New("vaultproc: the state has no key")
	}
	if successor && state.LocksAt.IsZero() {
		// A successor without its predecessor's lock time would extend the unlock.
		return nil, errors.New("vaultproc: the state handed over does not say when it locks")
	}
	now := time.Now()
	s := &Server{cfg: cfg, program: localipc.ProgramReplaced, key: state.Identity, doc: newLocked(state.Document),
		unlocked: state.UnlockedAt, stopped: make(chan struct{}), watchEvery: watchInterval}
	s.start = s.startSuccessor
	if s.unlocked.IsZero() || s.unlocked.After(now) {
		s.unlocked = now
	}
	maxUnlock := shorter(state.MaxUnlock, cfg.MaxUnlock)
	if maxUnlock > 0 {
		s.hardAt = s.unlocked.Add(maxUnlock)
	}
	s.locksAt = s.nextLocksAt()
	if !state.LocksAt.IsZero() && state.LocksAt.Before(s.locksAt) {
		s.locksAt = state.LocksAt
	}
	return s, nil
}

// shorter is the smaller positive one of two limits; zero means none.
func shorter(a, b time.Duration) time.Duration {
	switch {
	case a <= 0:
		return max(b, 0)
	case b <= 0 || a < b:
		return a
	}
	return b
}

// Refused is the ListenOptions.Refused of the server's listener. A process whose own program was replaced
// refuses every peer: while a handover is under way it answers replaced, so the client finds the
// successor; otherwise nothing will take the vault over, and it locks itself.
func (s *Server) Refused(conn net.Conn, _ error) {
	var h hello
	_ = readMessage(conn, &h)
	code := codeRefused
	switch s.programState() {
	case replacedHandover:
		code = codeReplaced
	case replacedLocked:
		code = codeLocked
	}
	_ = writeMessage(conn, response{V: ProtocolVersion, App: s.cfg.Version, Error: code})
}

// Serve answers connections on l, a listener of Config.Endpoint opened with the server's Refused, until the
// server locks: on OpLock, when the idle period or the maximum unlock time ends, when the socket is no
// longer its own, when its program was replaced without a handover, after a handover, or through Close. It
// then closes l, waits for the requests in flight, overwrites what it holds and returns nil. It returns an
// error only if l fails otherwise.
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	s.listener = l
	s.timer = time.AfterFunc(time.Until(s.locksAt), s.stop)
	s.mu.Unlock()
	if s.stopping() {
		_ = l.Close()
	}
	defer func() {
		s.stop()
		s.handlers.Wait()
		s.wipe()
	}()
	go s.watch()
	for {
		conn, err := l.Accept()
		if err != nil {
			if s.stopping() {
				return nil
			}
			var peer *localipc.PeerError
			if errors.As(err, &peer) {
				continue // a refused peer; the listener closed its connection
			}
			return err
		}
		s.handlers.Add(1)
		go func() {
			defer s.handlers.Done()
			s.serveConn(conn)
		}()
	}
}

// Close locks the server as OpLock would.
func (s *Server) Close() error {
	s.stop()
	return nil
}

func (s *Server) stopping() bool {
	select {
	case <-s.stopped:
		return true
	default:
		return false
	}
}

// stop ends Serve once: no further connection is accepted and a prepared handover is ended.
func (s *Server) stop() {
	s.stopOnce.Do(func() {
		close(s.stopped)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.timer != nil {
			s.timer.Stop()
		}
		if s.listener != nil {
			_ = s.listener.Close()
		}
		if s.pending != nil {
			_ = s.pending.SetReadDeadline(time.Unix(1, 0))
		}
	})
}

// wipe overwrites the document and drops the key. The key's own bytes belong to the age library and can
// only be dropped.
func (s *Server) wipe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc.release()
	s.doc, s.key, s.locked = nil, nil, true
}

// watch locks the server once its socket is gone or another file, or its program was replaced while no
// handover is under way.
func (s *Server) watch() {
	path, err := s.cfg.Endpoint.Path()
	if err != nil {
		s.stop()
		return
	}
	var own os.FileInfo
	ownErr := errors.ErrUnsupported
	if socketFile {
		own, ownErr = os.Lstat(path)
		if ownErr != nil {
			s.stop()
			return
		}
	}
	ticker := time.NewTicker(s.watchEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopped:
			return
		case <-ticker.C:
		}
		if s.programState() == replacedLocked {
			return
		}
		if ownErr == nil {
			if now, err := os.Lstat(path); err != nil || !os.SameFile(own, now) {
				s.mu.Lock()
				handedOver := s.handingOver
				s.mu.Unlock()
				if !handedOver {
					s.stop()
				}
				return
			}
		}
	}
}

// The outcomes of programState.
const (
	replacedNot      = iota // the program is in place
	replacedHandover        // replaced while a handover is prepared or committed
	replacedLocked          // replaced with no handover: the server locked itself
)

// programState tells whether this process's program was replaced and locks the server if no handover
// would take the vault over. A program that cannot be told counts as replaced.
func (s *Server) programState() int {
	_, replaced, err := s.program()
	if err == nil && !replaced {
		return replacedNot
	}
	s.mu.Lock()
	handover := s.handingOver || s.pending != nil
	s.mu.Unlock()
	if handover {
		return replacedHandover
	}
	s.stop()
	return replacedLocked
}

func (s *Server) requestTimeout() time.Duration {
	if s.cfg.RequestTimeout > 0 {
		return s.cfg.RequestTimeout
	}
	return DefaultRequestTimeout
}

func (s *Server) answer(code string, result any) response {
	resp := response{V: ProtocolVersion, App: s.cfg.Version, Error: code}
	if code == "" && result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			resp.Error = codeFailed
		} else {
			resp.Result = data
		}
	}
	return resp
}

// serveConn answers the challenge and then the one request of a connection, within one deadline. The
// listener checked the peer before.
func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(s.requestTimeout())); err != nil {
		return
	}
	var h hello
	if err := readMessage(conn, &h); err != nil {
		if errors.Is(err, ErrTooLarge) {
			_ = writeMessage(conn, s.answer(codeTooLarge, nil))
		}
		return
	}
	if h.V != ProtocolVersion || h.App != s.cfg.Version {
		_ = writeMessage(conn, s.answer(codeVersion, nil))
		return
	}
	if resp := s.solve(h.Challenge); writeMessage(conn, resp) != nil || resp.Error != "" {
		return
	}

	var req request
	if err := readMessage(conn, &req); err != nil {
		code := codeBadRequest
		if errors.Is(err, ErrTooLarge) {
			code = codeTooLarge
		}
		_ = writeMessage(conn, s.answer(code, nil))
		return
	}
	if req.V != ProtocolVersion || req.App != s.cfg.Version {
		_ = writeMessage(conn, s.answer(codeVersion, nil))
		return
	}
	if req.Op == OpHandover {
		s.serveHandover(conn, req.Body)
		return
	}
	if !s.begin() {
		_ = writeMessage(conn, s.answer(codeReplaced, nil))
		return
	}
	defer s.end()
	resp := s.dispatch(req)
	// A lock is answered after the server stopped, so a client told it is locked finds no listener.
	if req.Op == OpLock && resp.Error == "" {
		s.stop()
	}
	_ = writeMessage(conn, resp)
}

// solve answers a client's challenge with the proof that this server holds the vault's key.
func (s *Server) solve(challenge []byte) response {
	s.mu.Lock()
	key := s.key
	handingOver, locked := s.handingOver, s.locked || s.stopping()
	s.mu.Unlock()
	switch {
	case handingOver:
		return s.answer(codeReplaced, nil)
	case locked || key == nil:
		return s.answer(codeLocked, nil)
	}
	proof, ok := solveChallenge(key, challenge)
	if !ok {
		return s.answer(codeChallenge, nil)
	}
	resp := s.answer("", nil)
	resp.Proof = proof
	return resp
}

// begin counts a request as in flight unless a handover was committed: its answer would be lost to the
// successor. end counts it as answered.
func (s *Server) begin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handingOver {
		return false
	}
	s.inFlight++
	return true
}

func (s *Server) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlight--
}

// dispatch carries out one checked request under the server's lock.
func (s *Server) dispatch(req request) (resp response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked || s.stopping() {
		return s.answer(codeLocked, nil)
	}
	switch req.Op {
	case OpStatus:
		st := statusResult{PID: os.Getpid(), LocksAt: s.locksAt, MemoryLocked: s.doc != nil && s.doc.locked}
		if !s.hardAt.IsZero() {
			at := s.hardAt
			st.UnlockEndsAt = &at
		}
		return s.answer("", st)
	case OpLock:
		return s.answer("", nil)
	}
	handler, ok := s.cfg.Handlers[req.Op]
	if !ok {
		return s.answer(codeUnknownOp, nil)
	}
	defer func() {
		if recover() != nil {
			resp = s.answer(codeFailed, nil)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout())
	defer cancel()
	result, err := handler(ctx, &Held{s: s}, req.Body)
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) && wordPattern.MatchString(appErr.Code) && !isKitCode(appErr.Code) {
			return s.answer(appErr.Code, nil)
		}
		return s.answer(codeFailed, nil)
	}
	return s.answer("", result)
}

// touch restarts the idle period, never past the hard deadline. The caller holds s.mu.
func (s *Server) touch() {
	s.locksAt = s.nextLocksAt()
	if s.timer != nil {
		s.timer.Reset(time.Until(s.locksAt))
	}
}

// nextLocksAt is the idle deadline from now, held back to the hard deadline. The caller holds s.mu.
func (s *Server) nextLocksAt() time.Time {
	at := time.Now().Add(s.cfg.IdleTimeout)
	if !s.hardAt.IsZero() && s.hardAt.Before(at) {
		return s.hardAt
	}
	return at
}
