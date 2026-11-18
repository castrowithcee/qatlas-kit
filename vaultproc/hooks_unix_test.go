//go:build linux || darwin

package vaultproc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"filippo.io/age"
)

func TestOnLockSocketGone(t *testing.T) {
	cfg := testConfig(t)
	log := attachHooks(&cfg)
	server, err := NewServer(cfg, State{Identity: newIdentity(t), Document: []byte(testDocument)}, false)
	if err != nil {
		t.Fatal(err)
	}
	server.watchEvery = 20 * time.Millisecond
	r := serveWith(t, server, cfg, server.key, false)
	if _, err := r.client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, err := cfg.Endpoint.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	awaitStopped(t, r)
	if event := log.lockEvent(t); event != (LockEvent{Reason: LockSocketGone}) {
		t.Fatalf("OnLock = %+v", event)
	}
}

func TestHooksOnHandoverToSuccessor(t *testing.T) {
	var log *hookLog
	h := newHandover(t, func(h *handoverSetup) { log = attachHooks(&h.cfg) })
	ctx := context.Background()
	handover, err := h.client.PrepareHandover(ctx, h.files)
	if err != nil || handover == nil {
		t.Fatalf("PrepareHandover = %v, %v", handover, err)
	}
	// A second request while one is under way is refused and reports nothing.
	if second, err := h.client.PrepareHandover(ctx, h.files); second != nil || !errors.Is(err, ErrHandover) {
		t.Fatalf("second PrepareHandover = %v, %v", second, err)
	}
	if len(log.handovers) != 0 {
		t.Fatal("OnHandover ran before the handover happened")
	}
	h.replace(t, newProgramBody)
	if _, err := handover.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// The result is known by the time the client has its answer, and after the successor started.
	select {
	case result := <-log.handovers:
		if result != (HandoverResult{HandedOver: true}) {
			t.Fatalf("OnHandover = %+v", result)
		}
	default:
		t.Fatal("OnHandover did not run before the client was answered")
	}
	if len(h.successor) != 1 {
		t.Fatal("OnHandover ran before the successor started")
	}
	awaitStopped(t, h.running)
	if event := log.lockEvent(t); event != (LockEvent{Reason: LockHandedOver}) {
		t.Fatalf("OnLock = %+v", event)
	}
	if len(log.handovers) != 0 {
		t.Fatal("OnHandover reported more than one result")
	}
}

func TestHooksOnFailedHandover(t *testing.T) {
	ctx := context.Background()
	prepare := func(t *testing.T, h *handoverSetup) *Handover {
		t.Helper()
		handover, err := h.client.PrepareHandover(ctx, h.files)
		if err != nil || handover == nil {
			t.Fatalf("PrepareHandover = %v, %v", handover, err)
		}
		return handover
	}
	prepareFails := func(t *testing.T, h *handoverSetup) error {
		t.Helper()
		handover, err := h.client.PrepareHandover(ctx, h.files)
		if handover != nil {
			t.Fatal("the handover was prepared")
		}
		return err
	}
	for name, test := range map[string]struct {
		change func(*handoverSetup)
		run    func(*testing.T, *handoverSetup) error
		reason HandoverReason
		lock   LockReason
	}{
		"not verified": {change: func(h *handoverSetup) { h.cfg.Handover.Namespace = "example-other" },
			reason: HandoverNotVerified},
		"rollback":          {change: func(h *handoverSetup) { h.files.Rollback = true }, reason: HandoverRollback},
		"unsupported":       {change: func(h *handoverSetup) { h.cfg.Handover = nil }, reason: HandoverUnsupported},
		"archive too large": {change: func(h *handoverSetup) { h.cfg.Handover.MaxFileSize = 16 }, reason: HandoverTooLarge},
		"declined": {change: func(h *handoverSetup) {
			h.cfg.Handover.HandsOver = func(*age.X25519Identity) bool { return false }
		}, lock: LockHandoverDeclined, reason: HandoverDeclined},
		"program unchanged": {run: func(t *testing.T, h *handoverSetup) error {
			_, err := prepare(t, h).Commit(ctx)
			return err
		}, reason: HandoverProgramUnchanged},
		"program mismatch": {run: func(t *testing.T, h *handoverSetup) error {
			handover := prepare(t, h)
			h.replace(t, "another program")
			_, err := handover.Commit(ctx)
			return err
		}, reason: HandoverProgramMismatch},
		"program too large": {change: func(h *handoverSetup) { h.cfg.Handover.MaxFileSize = 4096 },
			run: func(t *testing.T, h *handoverSetup) error {
				handover := prepare(t, h)
				h.replace(t, string(make([]byte, 8192)))
				_, err := handover.Commit(ctx)
				return err
			}, reason: HandoverTooLarge},
		"no successor": {run: func(t *testing.T, h *handoverSetup) error {
			handover := prepare(t, h)
			h.server.start = func(context.Context, string, []byte) (int, func(), error) {
				return 0, nil, errors.New("example failure")
			}
			h.replace(t, newProgramBody)
			_, err := handover.Commit(ctx)
			return err
		}, reason: HandoverNoSuccessor},
		"bad request at prepare": {run: func(t *testing.T, h *handoverSetup) error {
			// A request that names the wrong phase first.
			return exchangeHandover(t, h, nil, phaseCommit)
		}, reason: HandoverBadRequest},
		"bad request at commit": {run: func(t *testing.T, h *handoverSetup) error {
			handover := prepare(t, h)
			return exchangeHandover(t, h, handover, phasePrepare)
		}, reason: HandoverBadRequest},
		"connection closed before commit": {run: func(t *testing.T, h *handoverSetup) error {
			_ = prepare(t, h).Close()
			return ErrHandover
		}, reason: HandoverNoSuccessor},
	} {
		t.Run(name, func(t *testing.T) {
			var log *hookLog
			h := newHandover(t, func(h *handoverSetup) {
				if test.change != nil {
					test.change(h)
				}
				log = attachHooks(&h.cfg)
			})
			run := test.run
			if run == nil {
				run = prepareFails
			}
			err := run(t, h)
			awaitStopped(t, h.running)
			// Declined asks for the lock and reports no error to the client.
			if test.reason != HandoverDeclined && !errors.Is(err, ErrHandover) {
				t.Fatalf("handover = %v, want ErrHandover", err)
			}
			// The client reads the reason the process reports. A closed connection is no answer.
			var he *HandoverError
			switch {
			case errors.As(err, &he):
				if he.Reason != test.reason {
					t.Fatalf("the client reads %q, want %q", he.Reason, test.reason)
				}
			case test.reason != HandoverDeclined && name != "connection closed before commit":
				t.Fatalf("handover = %v, want a *HandoverError", err)
			}
			select {
			case result := <-log.handovers:
				if result != (HandoverResult{Reason: test.reason}) {
					t.Fatalf("OnHandover = %+v, want reason %q", result, test.reason)
				}
			default:
				t.Fatal("OnHandover did not run")
			}
			want := LockEvent{Reason: LockHandoverFailed, Handover: test.reason}
			if test.lock != "" {
				want = LockEvent{Reason: test.lock}
			}
			if event := log.lockEvent(t); event != want {
				t.Fatalf("OnLock = %+v, want %+v", event, want)
			}
			if len(log.handovers) != 0 {
				t.Fatal("OnHandover reported more than one result")
			}
		})
	}
}

// exchangeHandover sends a handover request for phase on the connection of handover, or on a new one when
// handover is nil, and returns the error the client makes of the answer.
func exchangeHandover(t *testing.T, h *handoverSetup, handover *Handover, phase string) error {
	t.Helper()
	ctx := context.Background()
	var conn net.Conn
	if handover != nil {
		conn = handover.conn
	} else {
		var err error
		if conn, err = h.client.dial(ctx); err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
	}
	body, err := json.Marshal(handoverRequest{Phase: phase})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.client.exchange(ctx, conn, request{Op: OpHandover, Body: body})
	return err
}

func TestHandoverBusyLeavesTheFirstHandoverAlone(t *testing.T) {
	var log *hookLog
	h := newHandover(t, func(h *handoverSetup) { log = attachHooks(&h.cfg) })
	ctx := context.Background()
	// PrepareHandover returns once the process answered, which is after it claimed the handover, so the
	// second request meets a handover under way without any waiting.
	first, err := h.client.PrepareHandover(ctx, h.files)
	if err != nil || first == nil {
		t.Fatalf("PrepareHandover = %v, %v", first, err)
	}
	second, err := h.client.PrepareHandover(ctx, h.files)
	var he *HandoverError
	if second != nil || !errors.As(err, &he) || he.Reason != HandoverBusy || !errors.Is(err, ErrHandover) ||
		errors.Is(err, ErrBadRequest) || errors.Is(err, ErrNotRunning) {
		t.Fatalf("second PrepareHandover = %v, %v; want ErrHandover with reason %q", second, err, HandoverBusy)
	}
	if h.server.stopping() {
		t.Fatal("the busy answer locked the process")
	}
	if len(log.handovers) != 0 {
		t.Fatal("OnHandover ran for the busy answer")
	}
	// The first handover still completes.
	h.replace(t, newProgramBody)
	if _, err := first.Commit(ctx); err != nil {
		t.Fatalf("Commit = %v, want the first handover to happen", err)
	}
	select {
	case result := <-log.handovers:
		if result != (HandoverResult{HandedOver: true}) {
			t.Fatalf("OnHandover = %+v", result)
		}
	default:
		t.Fatal("OnHandover did not run for the first handover")
	}
	awaitStopped(t, h.running)
	if event := log.lockEvent(t); event != (LockEvent{Reason: LockHandedOver}) {
		t.Fatalf("OnLock = %+v", event)
	}
}

func TestHandoverHookFailureDoesNotChangeTheHandover(t *testing.T) {
	h := newHandover(t, func(h *handoverSetup) {
		h.cfg.OnHandover = func(*Held, HandoverResult) error { panic("example") }
	})
	ctx := context.Background()
	handover, err := h.client.PrepareHandover(ctx, h.files)
	if err != nil || handover == nil {
		t.Fatalf("PrepareHandover = %v, %v", handover, err)
	}
	h.replace(t, newProgramBody)
	if _, err := handover.Commit(ctx); err != nil {
		t.Fatalf("Commit = %v, want the handover to happen", err)
	}
	if err := <-h.done; !errors.Is(err, ErrHook) {
		t.Fatalf("Serve() = %v, want ErrHook", err)
	}
	h.done <- nil
}
