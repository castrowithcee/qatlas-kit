//go:build linux || darwin

package vaultproc

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/localipc"
)

// TestSuccessorProcessTakesOver starts a real successor process from this test binary and hands it the
// channel, as a commit does once the program checks out.
func TestSuccessorProcessTakesOver(t *testing.T) {
	cfg := testConfig(t)
	_, key := testRelease(t, newProgramBody)
	cfg.Handover = &HandoverConfig{Namespace: testNamespace, TrustedKeys: []ssh.PublicKey{key}, ProgramName: "tool",
		SuccessorArgs: helperArgs("successor", cfg.Endpoint), HandsOver: func(*age.X25519Identity) bool { return true }}
	t.Setenv("VAULT_PROCESS_HELPER", "1")
	id := newIdentity(t)
	server, err := NewServer(cfg, State{Identity: id, Document: []byte(testDocument)}, false)
	if err != nil {
		t.Fatal(err)
	}
	l, err := cfg.Endpoint.ListenWith(localipc.ListenOptions{Refused: server.Refused})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	state, ok := server.snapshot()
	if !ok {
		t.Fatal("no snapshot")
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), HandoverTimeout)
	defer cancel()
	pid, end, err := server.startSuccessor(ctx, program, state)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	if err := localipc.HandOver(l); err != nil {
		t.Fatal(err)
	}
	client := &Client{Endpoint: cfg.Endpoint, Recipient: id.Recipient().String(), Version: 1, ProofLabel: testLabel}
	var status Status
	for deadline := time.Now().Add(5 * time.Second); ; {
		if status, err = client.Status(ctx); err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || status.PID != pid {
		t.Fatalf("status = %+v, %v; want successor %d", status, err, pid)
	}
	var got string
	if err := client.Call(ctx, "peek", nil, &got); err != nil || got != testDocument {
		t.Fatalf("peek at the successor = %q, %v", got, err)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestSuccessorProcessTakesOverFromEarlierEndpoint starts a real successor process with RunOptions.Earlier
// while a listener of the earlier endpoint holds the channel, and lets that listener hand over.
func TestSuccessorProcessTakesOverFromEarlierEndpoint(t *testing.T) {
	cfg := testConfig(t)
	_, key := testRelease(t, newProgramBody)
	earlierCfg := cfg
	earlierCfg.Endpoint = earlierEndpoint(cfg.Endpoint)
	earlierCfg.Handover = &HandoverConfig{Namespace: testNamespace, TrustedKeys: []ssh.PublicKey{key},
		ProgramName: "tool", SuccessorArgs: helperArgs("successor-earlier", cfg.Endpoint),
		HandsOver: func(*age.X25519Identity) bool { return true }}
	t.Setenv("VAULT_PROCESS_HELPER", "1")
	id := newIdentity(t)
	server, err := NewServer(earlierCfg, State{Identity: id, Document: []byte(testDocument)}, false)
	if err != nil {
		t.Fatal(err)
	}
	earlier, err := earlierCfg.Endpoint.ListenWith(localipc.ListenOptions{Refused: server.Refused})
	if err != nil {
		t.Fatal(err)
	}
	defer earlier.Close()
	state, ok := server.snapshot()
	if !ok {
		t.Fatal("no snapshot")
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), HandoverTimeout)
	defer cancel()
	pid, end, err := server.startSuccessor(ctx, program, state)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	client := &Client{Endpoint: cfg.Endpoint, Recipient: id.Recipient().String(), Version: 1, ProofLabel: testLabel}
	early, cancelEarly := context.WithTimeout(ctx, 300*time.Millisecond)
	_, err = client.Status(early)
	cancelEarly()
	if err == nil {
		t.Fatal("the successor answered before the handover")
	}
	if err := localipc.HandOver(earlier); err != nil {
		t.Fatal(err)
	}
	var status Status
	for deadline := time.Now().Add(5 * time.Second); ; {
		if status, err = client.Status(ctx); err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || status.PID != pid {
		t.Fatalf("status = %+v, %v; want successor %d", status, err, pid)
	}
	var got string
	if err := client.Call(ctx, "peek", nil, &got); err != nil || got != testDocument {
		t.Fatalf("peek at the successor = %q, %v", got, err)
	}
	if bases, err := earlierCfg.Endpoint.Running(); err != nil || len(bases) != 0 {
		t.Fatalf("the earlier endpoint still lists %v, %v", bases, err)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestSuccessorProcessReadsEarlierFormat starts a real successor process with a state in an earlier
// format, with DecodeEarlier and Configure, and lets the process holding the channel hand over.
func TestSuccessorProcessReadsEarlierFormat(t *testing.T) {
	e := testEndpoint(t)
	l, err := e.ListenWith(localipc.ListenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	id := newIdentity(t)
	locksAt := time.Now().Add(10 * time.Minute).Round(time.Second)
	data := encodeForeign(t, foreignState{Identity: id.String(), Document: testDocument, LocksAt: locksAt,
		UnlockedAt: time.Now().Add(-time.Minute), MaxUnlock: int64(time.Hour)})
	ctx, cancel := context.WithTimeout(context.Background(), HandoverTimeout)
	defer cancel()
	res, err := background.Start(ctx, helperCommand("successor-foreign", e), data, background.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := localipc.HandOver(l); err != nil {
		t.Fatal(err)
	}
	client := &Client{Endpoint: e, Recipient: id.Recipient().String(), Version: 1, ProofLabel: testLabel}
	var status Status
	for deadline := time.Now().Add(5 * time.Second); ; {
		if status, err = client.Status(ctx); err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || status.PID != res.PID {
		t.Fatalf("status = %+v, %v; want successor %d", status, err, res.PID)
	}
	if status.LocksAt.After(locksAt) || status.UnlockEndsAt.IsZero() {
		t.Errorf("status = %+v, want the predecessor's lock time %v and a maximum", status, locksAt)
	}
	var got string
	if err := client.Call(ctx, "peek", nil, &got); err != nil || got != testDocument {
		t.Fatalf("peek = %q, %v", got, err)
	}
	if err := client.Call(ctx, "recipient", nil, &got); err != nil || got != id.Recipient().String() {
		t.Fatalf("recipient = %q, %v; Configure did not run with the key", got, err)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestSuccessorProcessRefusesBadEarlierState checks that a state the hooks reject starts nothing, reports
// no content and leaves the channel unserved.
func TestSuccessorProcessRefusesBadEarlierState(t *testing.T) {
	id := newIdentity(t)
	later := time.Now().Add(10 * time.Minute)
	for name, c := range map[string]struct {
		mode string
		data []byte
	}{
		"not a state":      {"successor-foreign", []byte("example-secret")},
		"no lock time":     {"successor-foreign", encodeForeign(t, foreignState{Identity: id.String(), Document: "example-secret"})},
		"negative maximum": {"successor-foreign", encodeForeign(t, foreignState{Identity: id.String(), LocksAt: later, MaxUnlock: -1})},
		"no key":           {"successor-foreign", encodeForeign(t, foreignState{LocksAt: later, Document: "example-secret"})},
		"settings fail":    {"successor-bad-config", encodeForeign(t, foreignState{Identity: id.String(), LocksAt: later})},
	} {
		e := testEndpoint(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := background.Start(ctx, helperCommand(c.mode, e), c.data, background.Options{})
		cancel()
		if err == nil || strings.Contains(err.Error(), "example-secret") {
			t.Errorf("%s: Start = %v", name, err)
		}
		client := &Client{Endpoint: e, Recipient: id.Recipient().String(), Version: 1, ProofLabel: testLabel}
		if _, err := client.Status(context.Background()); !errors.Is(err, ErrNotRunning) {
			t.Errorf("%s: status = %v, want ErrNotRunning", name, err)
		}
		if bases, err := e.Running(); err != nil || len(bases) != 0 {
			t.Errorf("%s: the endpoint lists %v, %v", name, bases, err)
		}
	}
}

// TestSuccessorProcessTakesEarlierFromConfigure starts a successor whose Configure names the earlier
// endpoint, which only the hook knows, and lets a listener of that endpoint hand over.
func TestSuccessorProcessTakesEarlierFromConfigure(t *testing.T) {
	e := testEndpoint(t)
	earlier, err := earlierEndpoint(e).ListenWith(localipc.ListenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer earlier.Close()
	id := newIdentity(t)
	data := encodeForeign(t, foreignState{Identity: id.String(), Document: testDocument,
		LocksAt: time.Now().Add(10 * time.Minute)})
	ctx, cancel := context.WithTimeout(context.Background(), HandoverTimeout)
	defer cancel()
	res, err := background.Start(ctx, helperCommand("successor-foreign-earlier", e), data, background.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := localipc.HandOver(earlier); err != nil {
		t.Fatal(err)
	}
	client := &Client{Endpoint: e, Recipient: id.Recipient().String(), Version: 1, ProofLabel: testLabel}
	var status Status
	for deadline := time.Now().Add(5 * time.Second); ; {
		if status, err = client.Status(ctx); err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || status.PID != res.PID {
		t.Fatalf("status = %+v, %v; want successor %d", status, err, res.PID)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestEarlierFromConfigureNeedsSuccessor checks that an Earlier endpoint from Configure without Successor
// is refused by the listener check and nothing listens.
func TestEarlierFromConfigureNeedsSuccessor(t *testing.T) {
	e := testEndpoint(t)
	id := newIdentity(t)
	data := kitStateBytes(t, id)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := background.Start(ctx, helperCommand("serve-earlier", e), data, background.Options{}); err == nil {
		t.Fatal("a process with Earlier but without Successor started")
	}
	client := &Client{Endpoint: e, Recipient: id.Recipient().String(), Version: 1, ProofLabel: testLabel}
	if _, err := client.Status(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("status = %v, want ErrNotRunning", err)
	}
}

func kitStateBytes(t *testing.T, id *age.X25519Identity) []byte {
	t.Helper()
	data, err := encodeState(State{Identity: id, Document: []byte(testDocument)})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
