//go:build linux || darwin || windows

package vaultproc

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/vault"
)

// helperArgs run this test binary as the vault process of e in mode.
func helperArgs(mode string, e localipc.Endpoint) []string {
	return []string{"-test.run=^TestVaultProcessHelper$", "--", mode, e.SocketBase, e.FallbackDir}
}

func helperCommand(mode string, e localipc.Endpoint) background.Command {
	return background.Command{Args: helperArgs(mode, e), Env: append(os.Environ(), "VAULT_PROCESS_HELPER=1")}
}

// TestVaultProcessHelper is the vault process the tests start.
func TestVaultProcessHelper(t *testing.T) {
	if os.Getenv("VAULT_PROCESS_HELPER") != "1" {
		return
	}
	time.AfterFunc(2*time.Minute, func() { os.Exit(3) }) // never outlives a test run that lost track of it
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	e := localipc.Endpoint{RuntimeDirName: "session", PipePrefix: "example-", ErrorName: "example",
		SocketBase: args[1], FallbackDir: args[2]}
	err := Run(context.Background(), RunOptions{Successor: args[0] == "successor",
		Config: Config{Endpoint: e, Version: 1, IdleTimeout: time.Hour, Handlers: testHandlers()}})
	if err != nil && !errors.Is(err, ErrRunning) {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestStartRunAndLock(t *testing.T) {
	e := testEndpoint(t)
	id := newIdentity(t)
	client := &Client{Endpoint: e, Recipient: id.Recipient().String(), Version: 1}
	ctx := context.Background()
	snap := vault.Snapshot{Identity: id, Document: []byte(testDocument)}

	status, err := Start(ctx, client, helperCommand("serve", e), snap, background.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Lock(context.Background()) })
	if status.PID <= 0 || status.PID == os.Getpid() {
		t.Fatalf("status = %+v, want another process", status)
	}
	var got string
	if err := client.Call(ctx, "get", nil, &got); err != nil || got != testDocument {
		t.Fatalf("get = %q, %v", got, err)
	}

	// A second start finds the first process and leaves it alone.
	again, err := Start(ctx, client, helperCommand("serve", e), vault.Snapshot{Identity: id}, background.Options{})
	if err != nil || again.PID != status.PID {
		t.Fatalf("second Start = %+v, %v; want process %d", again, err, status.PID)
	}
	if err := client.Call(ctx, "peek", nil, &got); err != nil || got != testDocument {
		t.Fatalf("peek after the second start = %q, %v", got, err)
	}

	// A process of another vault does not pass the challenge.
	other := &Client{Endpoint: e, Recipient: newIdentity(t).Recipient().String(), Version: 1}
	if _, err := Start(ctx, other, helperCommand("serve", e), vault.Snapshot{Identity: newIdentity(t)},
		background.Options{}); !errors.Is(err, ErrUnproven) {
		t.Fatalf("Start for another vault = %v, want ErrUnproven", err)
	}

	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Status(ctx); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("status after lock = %v", err)
	}
	if _, err := Start(ctx, client, helperCommand("serve", e), vault.Snapshot{}, background.Options{}); !errors.Is(err, vault.ErrLocked) {
		t.Fatalf("Start without a key = %v", err)
	}
}
