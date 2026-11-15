//go:build linux || darwin || windows

package vaultproc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/vault"
)

// helperArgs run this test binary as the vault process of e in mode.
func helperArgs(mode string, e localipc.Endpoint) []string {
	return []string{"-test.run=^TestVaultProcessHelper$", "--", mode, e.SocketBase, e.FallbackDir}
}

// earlierEndpoint is e as an earlier version of the application named it.
func earlierEndpoint(e localipc.Endpoint) localipc.Endpoint {
	e.RuntimeDirName = "earlier-session"
	return e
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
	opts := RunOptions{Successor: strings.HasPrefix(args[0], "successor"),
		Config: Config{Endpoint: e, Version: 1, ProofLabel: testLabel, IdleTimeout: time.Hour, Handlers: testHandlers()}}
	if args[0] == "successor-earlier" {
		earlier := earlierEndpoint(e)
		opts.Earlier = &earlier
	}
	switch args[0] {
	case "successor-foreign", "successor-foreign-earlier", "serve-earlier":
		opts.Config = Config{Endpoint: e}
		opts.DecodeEarlier = decodeForeign
		if args[0] == "serve-earlier" {
			opts.Successor, opts.DecodeEarlier = false, nil
		}
		withEarlier := args[0] != "successor-foreign"
		opts.Configure = func(key *age.X25519Identity, base Config, _ *localipc.Endpoint) (Config, *localipc.Endpoint, error) {
			if !processHardened() {
				return Config{}, nil, errors.New("not hardened")
			}
			base.Version, base.ProofLabel, base.IdleTimeout = 1, testLabel, 90*time.Minute
			base.Handlers = testHandlers()
			base.Handlers["recipient"] = func(context.Context, *Held, json.RawMessage) (any, error) {
				return key.Recipient().String(), nil
			}
			if !withEarlier {
				return base, nil, nil
			}
			earlier := earlierEndpoint(e)
			return base, &earlier, nil
		}
	case "successor-bad-config":
		opts.Config = Config{Endpoint: e}
		opts.DecodeEarlier = decodeForeign
		opts.Configure = func(*age.X25519Identity, Config, *localipc.Endpoint) (Config, *localipc.Endpoint, error) {
			return Config{}, nil, errors.New("example-secret settings failed")
		}
	}
	err := Run(context.Background(), opts)
	if err != nil && !errors.Is(err, ErrRunning) {
		os.Exit(1)
	}
	os.Exit(0)
}

// foreignState is a state in the format of an earlier application version: another shape than the kit's.
type foreignState struct {
	Identity   string    `json:"identity"`
	Document   string    `json:"document"`
	LocksAt    time.Time `json:"locks_at"`
	UnlockedAt time.Time `json:"unlocked_at"`
	MaxUnlock  int64     `json:"max_unlock"`
}

func encodeForeign(t *testing.T, f foreignState) []byte {
	t.Helper()
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// decodeForeign is the DecodeEarlier of the helper modes. It checks that Run hardened the process first.
func decodeForeign(data []byte) (State, error) {
	if !processHardened() {
		return State{}, errors.New("not hardened")
	}
	return parseForeign(data)
}

func parseForeign(data []byte) (State, error) {
	var f foreignState
	if err := json.Unmarshal(data, &f); err != nil {
		return State{}, errors.New("example-secret cannot be read")
	}
	id, err := age.ParseX25519Identity(f.Identity)
	if err != nil {
		return State{}, errors.New("example-secret key cannot be read")
	}
	return State{Identity: id, Document: []byte(f.Document), UnlockedAt: f.UnlockedAt, LocksAt: f.LocksAt,
		MaxUnlock: time.Duration(f.MaxUnlock)}, nil
}

func TestStartRunAndLock(t *testing.T) {
	e := testEndpoint(t)
	id := newIdentity(t)
	client := &Client{Endpoint: e, Recipient: id.Recipient().String(), Version: 1, ProofLabel: testLabel}
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
	other := &Client{Endpoint: e, Recipient: newIdentity(t).Recipient().String(), Version: 1, ProofLabel: testLabel}
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
