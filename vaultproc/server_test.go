//go:build linux || darwin || windows

package vaultproc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

const testDocument = `{"schema":1,"note":"example-value"}`

func newIdentity(t *testing.T) *age.X25519Identity {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// testHandlers read and replace the document; only get touches.
func testHandlers() map[string]HandlerFunc {
	return map[string]HandlerFunc{
		"get": func(_ context.Context, held *Held, _ json.RawMessage) (any, error) {
			held.Touch()
			return string(held.Document()), nil
		},
		"peek": func(_ context.Context, held *Held, _ json.RawMessage) (any, error) {
			return string(held.Document()), nil
		},
		"set": func(_ context.Context, held *Held, body json.RawMessage) (any, error) {
			var doc string
			if err := json.Unmarshal(body, &doc); err != nil {
				return nil, &Error{Code: "invalid-document"}
			}
			held.Replace([]byte(doc))
			return nil, nil
		},
		"kit-code": func(context.Context, *Held, json.RawMessage) (any, error) {
			return nil, &Error{Code: codeLocked}
		},
		"plain-error": func(context.Context, *Held, json.RawMessage) (any, error) {
			return nil, errors.New("disk path /example/secret failed")
		},
		"panics": func(context.Context, *Held, json.RawMessage) (any, error) { panic("example") },
	}
}

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{Endpoint: testEndpoint(t), Version: 1, IdleTimeout: time.Hour, Handlers: testHandlers()}
}

type running struct {
	server *Server
	client *Client
	done   chan error
}

// serve runs a server for state on cfg's endpoint in this process.
func serve(t *testing.T, cfg Config, state State) running {
	t.Helper()
	server, err := NewServer(cfg, state, false)
	if err != nil {
		t.Fatal(err)
	}
	return serveWith(t, server, cfg, state.Identity, false)
}

func serveWith(t *testing.T, server *Server, cfg Config, id *age.X25519Identity, successor bool) running {
	t.Helper()
	l, err := cfg.Endpoint.ListenWith(localipc.ListenOptions{Successor: successor, Refused: server.Refused})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(l) }()
	t.Cleanup(func() {
		_ = server.Close()
		<-done
	})
	return running{server: server, done: done,
		client: &Client{Endpoint: cfg.Endpoint, Recipient: id.Recipient().String(), Version: cfg.Version}}
}

func awaitStopped(t *testing.T, r running) {
	t.Helper()
	select {
	case err := <-r.done:
		r.done <- err
		if err != nil {
			t.Fatalf("Serve() = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not lock")
	}
}

func TestCallStatusAndLock(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxUnlock = time.Hour
	r := serve(t, cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)})
	ctx := context.Background()

	var got string
	if err := r.client.Call(ctx, "get", nil, &got); err != nil || got != testDocument {
		t.Fatalf("get = %q, %v", got, err)
	}
	if err := r.client.Call(ctx, "set", `{"schema":1}`, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.client.Call(ctx, "peek", nil, &got); err != nil || got != `{"schema":1}` {
		t.Fatalf("peek after set = %q, %v", got, err)
	}
	status, err := r.client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.PID != os.Getpid() || status.LocksAt.IsZero() || status.UnlockEndsAt.IsZero() ||
		status.LocksAt.After(status.UnlockEndsAt) {
		t.Fatalf("status = %+v", status)
	}
	if err := r.client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	awaitStopped(t, r)
	if _, err := r.client.Status(ctx); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("status after lock = %v, want ErrNotRunning", err)
	}
	if r.server.doc != nil || r.server.key != nil {
		t.Fatal("the server still holds the vault after it locked")
	}
}

func TestErrorsCarryFixedCodesOnly(t *testing.T) {
	r := serve(t, testConfig(t), State{Identity: newIdentity(t), Document: []byte(testDocument)})
	ctx := context.Background()
	var appErr *Error
	if err := r.client.Call(ctx, "set", 7, nil); !errors.As(err, &appErr) || appErr.Code != "invalid-document" {
		t.Fatalf("set with a wrong body = %v", err)
	}
	for _, op := range []string{"kit-code", "plain-error", "panics"} {
		err := r.client.Call(ctx, op, nil, nil)
		if !errors.Is(err, ErrFailed) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s = %v, want ErrFailed without the handler's text", op, err)
		}
	}
	if err := r.client.Call(ctx, "missing", nil, nil); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("unknown operation = %v", err)
	}
	if err := r.client.Call(ctx, OpHandover, nil, nil); err == nil {
		t.Fatal("Call accepted the handover operation")
	}
	if err := r.client.Call(ctx, "get", strings.Repeat("x", MaxMessage), nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized request = %v", err)
	}
	var got string
	if err := r.client.Call(ctx, "get", nil, &got); err != nil || got != testDocument {
		t.Fatalf("the server did not survive the refusals: %q, %v", got, err)
	}
}

func TestConfigIsChecked(t *testing.T) {
	id := newIdentity(t)
	for name, change := range map[string]func(*Config){
		"no version":       func(c *Config) { c.Version = 0 },
		"no idle timeout":  func(c *Config) { c.IdleTimeout = 0 },
		"negative maximum": func(c *Config) { c.MaxUnlock = -time.Second },
		"reserved name":    func(c *Config) { c.Handlers[OpStatus] = c.Handlers["get"] },
		"bad name":         func(c *Config) { c.Handlers["Get"] = c.Handlers["get"] },
		"nil handler":      func(c *Config) { c.Handlers["none"] = nil },
		"handover without keys": func(c *Config) {
			c.Handover = &HandoverConfig{Namespace: "example", ProgramName: "tool", HandsOver: func(*age.X25519Identity) bool { return true }}
		},
	} {
		cfg := testConfig(t)
		change(&cfg)
		if _, err := NewServer(cfg, State{Identity: id}, false); err == nil {
			t.Errorf("%s: NewServer accepted the config", name)
		}
	}
	if _, err := NewServer(testConfig(t), State{}, false); err == nil {
		t.Error("NewServer accepted a state without a key")
	}
	if _, err := NewServer(testConfig(t), State{Identity: id}, true); err == nil {
		t.Error("NewServer accepted a successor state without a lock time")
	}
}

func TestClientRefusesWhatCannotProveTheKey(t *testing.T) {
	cfg := testConfig(t)
	r := serve(t, cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)})
	ctx := context.Background()

	other := *r.client
	other.Recipient = newIdentity(t).Recipient().String()
	_, err := other.Status(ctx)
	var peer *localipc.PeerError
	if !errors.Is(err, ErrUnproven) || !errors.Is(err, localipc.ErrRefused) || !errors.As(err, &peer) || peer.PID != os.Getpid() {
		t.Fatalf("status with another vault's recipient = %v, want ErrUnproven naming process %d", err, os.Getpid())
	}

	newer := *r.client
	newer.Version = 2
	if _, err := newer.Status(ctx); !errors.Is(err, ErrVersion) || !errors.As(err, &peer) {
		t.Fatalf("status of another version = %v, want ErrVersion with the process", err)
	}

	none := *r.client
	none.Recipient = ""
	if _, err := none.Status(ctx); !errors.Is(err, ErrNoRecipient) {
		t.Fatalf("status without a recipient = %v", err)
	}
	none.Recipient = "not a recipient"
	if _, err := none.Status(ctx); err == nil {
		t.Fatal("status with an unreadable recipient succeeded")
	}
}

// fakeServer answers every connection on e with answer, which gets the hello the client sent.
func fakeServer(t *testing.T, e localipc.Endpoint, answer func(net.Conn)) {
	t.Helper()
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			go func() {
				defer conn.Close()
				answer(conn)
			}()
		}
	}()
}

func TestClientRefusesFakeServers(t *testing.T) {
	id := newIdentity(t)
	for name, answer := range map[string]func(net.Conn){
		"silent": func(conn net.Conn) { time.Sleep(2 * time.Second) },
		"wrong proof": func(conn net.Conn) {
			var h hello
			_ = readMessage(conn, &h)
			_ = writeMessage(conn, response{V: ProtocolVersion, App: 1, Proof: bytes.Repeat([]byte{1}, 32)})
		},
		"echo of the challenge": func(conn net.Conn) {
			var h hello
			_ = readMessage(conn, &h)
			_ = writeMessage(conn, response{V: ProtocolVersion, App: 1, Proof: h.Challenge})
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := testEndpoint(t)
			fakeServer(t, e, answer)
			client := &Client{Endpoint: e, Recipient: id.Recipient().String(), Version: 1, Timeout: 300 * time.Millisecond}
			start := time.Now()
			_, err := client.Status(context.Background())
			var peer *localipc.PeerError
			if !errors.Is(err, localipc.ErrRefused) || !errors.As(err, &peer) || peer.PID != os.Getpid() {
				t.Fatalf("status = %v, want a refusal naming process %d", err, os.Getpid())
			}
			if time.Since(start) > 3*time.Second {
				t.Fatalf("the refusal took %v", time.Since(start))
			}
		})
	}
}

func TestNotRunning(t *testing.T) {
	client := &Client{Endpoint: testEndpoint(t), Recipient: newIdentity(t).Recipient().String(), Version: 1}
	if _, err := client.Status(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("status without a process = %v", err)
	}
	if err := client.Lock(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("lock without a process = %v", err)
	}
}

func TestIdleTimeoutLocksAndTouchDefers(t *testing.T) {
	cfg := testConfig(t)
	cfg.IdleTimeout = 600 * time.Millisecond
	r := serve(t, cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)})
	ctx := context.Background()
	for range 3 {
		time.Sleep(300 * time.Millisecond)
		if err := r.client.Call(ctx, "get", nil, nil); err != nil {
			t.Fatalf("get within the idle period = %v", err)
		}
	}
	// Status and peek do not touch.
	for range 2 {
		time.Sleep(200 * time.Millisecond)
		_ = r.client.Call(ctx, "peek", nil, nil)
	}
	awaitStopped(t, r)
	if err := r.client.Call(ctx, "get", nil, nil); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("get after the idle period = %v", err)
	}
}

func TestMaximumUnlockIsNeverExtended(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxUnlock = time.Hour
	unlocked := time.Now().Add(-time.Hour + 700*time.Millisecond)
	r := serve(t, cfg, State{Identity: newIdentity(t), UnlockedAt: unlocked, MaxUnlock: 2 * time.Hour})
	ctx := context.Background()
	status, err := r.client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := unlocked.Add(time.Hour); !status.UnlockEndsAt.Equal(want) || !status.LocksAt.Equal(want) {
		t.Fatalf("status = %+v, want the shorter maximum from the unlock, %v", status, want)
	}
	for range 3 {
		_ = r.client.Call(ctx, "get", nil, nil)
		time.Sleep(100 * time.Millisecond)
	}
	awaitStopped(t, r)
}

func TestInheritedLockTimeIsKept(t *testing.T) {
	cfg := testConfig(t)
	id := newIdentity(t)
	server, err := NewServer(cfg, State{Identity: id, LocksAt: time.Now().Add(300 * time.Millisecond)}, true)
	if err != nil {
		t.Fatal(err)
	}
	r := serveWith(t, server, cfg, id, false)
	awaitStopped(t, r)
}

func TestRefusedPeersAndReplacedProgram(t *testing.T) {
	cfg := testConfig(t)
	server, err := NewServer(cfg, State{Identity: newIdentity(t)}, false)
	if err != nil {
		t.Fatal(err)
	}
	replaced := false
	server.program = func() (string, bool, error) { return "/example/tool", replaced, nil }
	refusal := func() string {
		a, b := net.Pipe()
		defer a.Close()
		go func() {
			defer b.Close()
			server.Refused(b, localipc.ErrRefused)
		}()
		_ = writeMessage(a, hello{V: ProtocolVersion, App: 1})
		var resp response
		if err := readMessage(a, &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Error
	}
	if code := refusal(); code != codeRefused || server.stopping() {
		t.Fatalf("refusal = %q, stopping %v", code, server.stopping())
	}
	replaced = true
	server.pending = &net.TCPConn{}
	if code := refusal(); code != codeReplaced || server.stopping() {
		t.Fatalf("refusal during a handover = %q, stopping %v", code, server.stopping())
	}
	server.pending = nil
	if code := refusal(); code != codeLocked || !server.stopping() {
		t.Fatalf("refusal after a replacement = %q, stopping %v", code, server.stopping())
	}
	server.program = func() (string, bool, error) { return "", false, errors.New("cannot be told") }
	if programState := server.programState(); programState != replacedLocked {
		t.Fatalf("an unknown program state = %d, want locked", programState)
	}
}

func TestWatchLocksWhenTheProgramIsReplaced(t *testing.T) {
	cfg := testConfig(t)
	server, err := NewServer(cfg, State{Identity: newIdentity(t)}, false)
	if err != nil {
		t.Fatal(err)
	}
	server.watchEvery = 20 * time.Millisecond
	replaced := make(chan struct{})
	server.program = func() (string, bool, error) {
		select {
		case <-replaced:
			return "/example/tool", true, nil
		default:
			return "/example/tool", false, nil
		}
	}
	r := serveWith(t, server, cfg, server.key, false)
	if _, err := r.client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(replaced)
	awaitStopped(t, r)
}

func TestMessagesNeverEchoInput(t *testing.T) {
	var v map[string]any
	err := readMessage(strings.NewReader(`{"note":"example-value"`+"\n"), &v)
	if err == nil || strings.Contains(err.Error(), "example-value") {
		t.Fatalf("readMessage = %v", err)
	}
	if err := readMessage(strings.NewReader(strings.Repeat("x", MaxMessage+1)), &v); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized line = %v", err)
	}
	if err := readMessage(strings.NewReader(`{}`), &v); err == nil {
		t.Fatal("a line without its end was accepted")
	}
}

func TestChallengeIsSolvedOnlyWithTheKey(t *testing.T) {
	id := newIdentity(t)
	nonce, challenge, err := newChallenge(id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	proof, ok := solveChallenge(id, challenge)
	if !ok || !checkProof(nonce, proof) || bytes.Contains(proof, nonce) {
		t.Fatal("the key holder cannot prove itself")
	}
	if _, ok := solveChallenge(newIdentity(t), challenge); ok {
		t.Fatal("another key solved the challenge")
	}
	if _, ok := solveChallenge(id, nil); ok {
		t.Fatal("an empty challenge was solved")
	}
	var long bytes.Buffer
	w, _ := age.Encrypt(&long, id.Recipient())
	_, _ = w.Write(make([]byte, nonceSize+1))
	_ = w.Close()
	if _, ok := solveChallenge(id, long.Bytes()); ok {
		t.Fatal("a challenge of the wrong size was solved, which would decrypt any message")
	}
	if checkProof(nonce, proofOf(make([]byte, nonceSize))) {
		t.Fatal("a proof of another nonce passed")
	}
}

func TestStateRoundTripAndRefusals(t *testing.T) {
	id := newIdentity(t)
	at := time.Now().Add(time.Minute).Round(0)
	data, err := encodeState(State{Identity: id, Document: []byte(testDocument), UnlockedAt: at.Add(-time.Hour),
		LocksAt: at, MaxUnlock: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeState(data)
	if err != nil {
		t.Fatal(err)
	}
	if state.Identity.String() != id.String() || string(state.Document) != testDocument || !state.LocksAt.Equal(at) ||
		state.MaxUnlock != 2*time.Hour {
		t.Fatalf("decoded state differs: %+v", state)
	}
	for name, data := range map[string]string{
		"not json":         `{"identity":"example-secret"`,
		"other version":    `{"v":99,"identity":"QUJD"}`,
		"no key":           `{"v":1}`,
		"bad key":          `{"v":1,"identity":"ZXhhbXBsZS1zZWNyZXQ="}`,
		"negative maximum": `{"v":1,"identity":"QUJD","max_unlock":-1}`,
	} {
		if _, err := decodeState([]byte(data)); err == nil || strings.Contains(err.Error(), "example-secret") {
			t.Errorf("%s: decodeState = %v", name, err)
		}
	}
	if _, err := encodeState(State{}); err == nil {
		t.Error("encodeState accepted a state without a key")
	}
}

func TestLockedBytes(t *testing.T) {
	for _, size := range []int{0, 1, os.Getpagesize() + 1} {
		data := bytes.Repeat([]byte{7}, size)
		l := newLocked(data)
		if got := l.bytes(); !bytes.Equal(got, data) || cap(got) != size {
			t.Fatalf("size %d: bytes() = %d bytes, cap %d", size, len(got), cap(got))
		}
		l.release()
		if l.bytes() != nil || l.mem != nil {
			t.Fatalf("size %d: released bytes remain", size)
		}
		l.release()
	}
	var none *lockedBytes
	if none.bytes() != nil {
		t.Fatal("nil holds bytes")
	}
	none.release()
}

func TestSocketBase(t *testing.T) {
	dir := t.TempDir()
	a, err := SocketBase("vault-", dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := SocketBase("vault-", dir+string(os.PathSeparator)+".")
	c, _ := SocketBase("vault-", t.TempDir())
	if a != b || a == c || !strings.HasPrefix(a, "vault-") || len(a) != len("vault-")+16 {
		t.Fatalf("SocketBase = %q, %q, %q", a, b, c)
	}
}
