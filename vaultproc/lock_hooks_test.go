//go:build linux || darwin || windows

package vaultproc

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"filippo.io/age"
)

// hookLog records what the hooks of a server are told. The channels are buffered, so a hook never waits.
type hookLog struct {
	locks     chan LockEvent
	handovers chan HandoverResult
	// held is whether OnLock saw the key and the document.
	held atomic.Bool
}

func attachHooks(cfg *Config) *hookLog {
	log := &hookLog{locks: make(chan LockEvent, 8), handovers: make(chan HandoverResult, 8)}
	cfg.OnLock = func(h *Held, event LockEvent) error {
		log.held.Store(h.Identity() != nil && len(h.Document()) > 0)
		log.locks <- event
		return nil
	}
	cfg.OnHandover = func(_ *Held, result HandoverResult) error {
		log.handovers <- result
		return nil
	}
	return log
}

// lockEvent returns the one event Serve produced; it fails if there is none or a second one.
func (l *hookLog) lockEvent(t *testing.T) LockEvent {
	t.Helper()
	select {
	case event := <-l.locks:
		select {
		case second := <-l.locks:
			t.Fatalf("OnLock ran twice: %+v then %+v", event, second)
		default:
		}
		return event
	default:
		t.Fatal("OnLock did not run before Serve returned")
		return LockEvent{}
	}
}

func TestOnLockReasons(t *testing.T) {
	for name, test := range map[string]struct {
		want  LockReason
		setup func(cfg *Config)
		run   func(t *testing.T, r running)
	}{
		"idle":       {want: LockIdle, setup: func(cfg *Config) { cfg.IdleTimeout = 30 * time.Millisecond }},
		"max unlock": {want: LockMaxUnlock, setup: func(cfg *Config) { cfg.MaxUnlock = 30 * time.Millisecond }},
		"command": {want: LockCommand, run: func(t *testing.T, r running) {
			if err := r.client.Lock(context.Background()); err != nil {
				t.Fatal(err)
			}
		}},
		"close": {want: LockClosed, run: func(t *testing.T, r running) { _ = r.server.Close() }},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t)
			log := attachHooks(&cfg)
			if test.setup != nil {
				test.setup(&cfg)
			}
			r := serve(t, cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)})
			if test.run != nil {
				test.run(t, r)
			}
			awaitStopped(t, r)
			if event := log.lockEvent(t); event != (LockEvent{Reason: test.want}) {
				t.Fatalf("OnLock = %+v, want %q", event, test.want)
			}
			if !log.held.Load() {
				t.Error("OnLock did not reach the key and the document through Held")
			}
		})
	}
}

func TestOnLockProgramReplaced(t *testing.T) {
	cfg := testConfig(t)
	log := attachHooks(&cfg)
	server, err := NewServer(cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)}, false)
	if err != nil {
		t.Fatal(err)
	}
	server.watchEvery = 20 * time.Millisecond
	var replaced atomic.Bool
	server.program = func() (string, bool, error) { return "/example/tool", replaced.Load(), nil }
	r := serveWith(t, server, cfg, server.key, false)
	if _, err := r.client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	replaced.Store(true)
	awaitStopped(t, r)
	if event := log.lockEvent(t); event != (LockEvent{Reason: LockProgramReplaced, Trigger: TriggerWatch}) {
		t.Fatalf("OnLock = %+v", event)
	}
}

func TestOnLockProgramReplacedByRefusedClient(t *testing.T) {
	cfg := testConfig(t)
	cfg.ReplacedProgram = ReplacedLocksOnClient
	log := attachHooks(&cfg)
	server, err := NewServer(cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)}, false)
	if err != nil {
		t.Fatal(err)
	}
	server.watchEvery = 20 * time.Millisecond
	var replaced atomic.Bool
	server.program = func() (string, bool, error) { return "/example/tool", replaced.Load(), nil }
	r := serveWith(t, server, cfg, server.key, false)
	if _, err := r.client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	replaced.Store(true)
	time.Sleep(200 * time.Millisecond) // many watch ticks: none may lock
	if server.stopping() {
		t.Fatal("the watch locked although the program check is off")
	}
	refuse(t, server)
	awaitStopped(t, r)
	if event := log.lockEvent(t); event != (LockEvent{Reason: LockProgramReplaced, Trigger: TriggerRefusedClient}) {
		t.Fatalf("OnLock = %+v", event)
	}
}

func TestReplacedProgramRejectsUnknownValue(t *testing.T) {
	cfg := testConfig(t)
	cfg.ReplacedProgram = ReplacedProgramLock(99)
	if _, err := NewServer(cfg, State{Identity: newIdentity(t)}, false); err == nil {
		t.Fatal("NewServer accepted an unknown ReplacedProgram")
	}
}

func TestOnLockNotCalledWithoutServe(t *testing.T) {
	cfg := testConfig(t)
	log := attachHooks(&cfg)
	server, err := NewServer(cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)}, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = server.Close()
	server.wipe()
	if len(log.locks) != 0 || len(log.handovers) != 0 {
		t.Fatal("a hook ran although Serve never did")
	}
}

func TestOnLockRunsWhenClosedBeforeServe(t *testing.T) {
	cfg := testConfig(t)
	log := attachHooks(&cfg)
	server, err := NewServer(cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)}, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = server.Close()
	r := serveWith(t, server, cfg, server.key, false)
	awaitStopped(t, r)
	if event := log.lockEvent(t); event.Reason != LockClosed {
		t.Fatalf("OnLock = %+v", event)
	}
}

func TestOnLockIsBeforeWipeAndWipeAlwaysRuns(t *testing.T) {
	for name, hook := range map[string]func(*Held, LockEvent) error{
		"fails":  func(*Held, LockEvent) error { return errors.New("example failure") },
		"panics": func(*Held, LockEvent) error { panic("example") },
		"replaces the document": func(h *Held, _ LockEvent) error {
			h.Replace([]byte("example replacement"))
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t)
			var ran atomic.Int32
			cfg.OnLock = func(h *Held, event LockEvent) error {
				ran.Add(1)
				if h.Identity() == nil || string(h.Document()) != testDocument {
					t.Error("the hook found the vault already overwritten")
				}
				return hook(h, event)
			}
			r := serve(t, cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)})
			if err := r.client.Lock(context.Background()); err != nil {
				t.Fatal(err)
			}
			err := <-r.done
			r.done <- err
			wantErr := name != "replaces the document"
			if errors.Is(err, ErrHook) != wantErr || (!wantErr && err != nil) {
				t.Fatalf("Serve() = %v, want ErrHook = %v", err, wantErr)
			}
			if ran.Load() != 1 {
				t.Fatalf("OnLock ran %d times", ran.Load())
			}
			if r.server.doc != nil || r.server.key != nil || !r.server.locked {
				t.Fatal("the server did not overwrite what it held")
			}
		})
	}
}

func TestHandoverFileLimitCanOnlyTighten(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		limit int64
		ok    bool
	}{
		{0, true}, {1, true}, {maxReleaseFile, true}, {maxReleaseFile + 1, false}, {-1, false}, {-maxReleaseFile, false},
	} {
		cfg := testConfig(t)
		cfg.Handover = &HandoverConfig{Namespace: "example-release", TrustedKeys: []ssh.PublicKey{key}, ProgramName: "tool",
			HandsOver: func(*age.X25519Identity) bool { return true }, MaxFileSize: test.limit}
		if err := cfg.validate(); (err == nil) != test.ok {
			t.Errorf("MaxFileSize %d: validate() = %v, want ok = %v", test.limit, err, test.ok)
		}
	}
	if (&HandoverConfig{}).fileLimit() != maxReleaseFile || (&HandoverConfig{MaxFileSize: 5}).fileLimit() != 5 {
		t.Error("fileLimit does not follow MaxFileSize")
	}
}
