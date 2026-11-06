//go:build linux || darwin

package vaultproc

import (
	"context"
	"os"
	"testing"
	"time"

	"filippo.io/age"
	"golang.org/x/crypto/ssh"

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
