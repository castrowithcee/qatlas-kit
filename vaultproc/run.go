package vaultproc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/vault"
)

// SocketBase returns the socket base of the vault process for the vault in dir: prefix, then a hash of the
// vault's absolute path, so the processes of two vaults never share a channel. On Windows the path is
// compared without regard to case, as the file system does. prefix comes from the application.
func SocketBase(prefix, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("vaultproc: cannot resolve the vault directory: %w", err)
	}
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	sum := sha256.Sum256([]byte(abs))
	return prefix + hex.EncodeToString(sum[:8]), nil
}

// Start starts cmd, the program's hidden subcommand that calls Run, as the vault process of the unlocked
// vault snap describes, detached from this process and its terminal. The key and the document reach the
// process only through an inherited pipe, never through arguments, the environment or a file. It returns
// once client reaches the process, which must prove that it holds the vault's key. If a process already
// answers client, Start returns its status and starts none. opts bounds the start.
func Start(ctx context.Context, client *Client, cmd background.Command, snap vault.Snapshot,
	opts background.Options) (Status, error) {
	if snap.Identity == nil {
		return Status{}, vault.ErrLocked
	}
	// A running process keeps the vault; no second one is handed the key.
	if status, err := client.Status(ctx); !errors.Is(err, ErrNotRunning) {
		return status, err
	}
	data, err := encodeState(State{Identity: snap.Identity, Document: snap.Document, UnlockedAt: time.Now()})
	if err != nil {
		return Status{}, err
	}
	defer clear(data)
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = background.DefaultTimeout
	}
	deadline := time.Now().Add(timeout)
	if _, err := background.Start(ctx, cmd, data, opts); err != nil {
		return Status{}, err
	}
	clear(data)
	for {
		status, err := client.Status(ctx)
		if err == nil {
			return status, nil
		}
		if !errors.Is(err, ErrNotRunning) || time.Now().After(deadline) || ctx.Err() != nil {
			return Status{}, fmt.Errorf("the vault process does not answer: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// RunOptions configure Run.
type RunOptions struct {
	Config
	// Successor starts the process as the successor of a running vault process, as HandoverConfig's
	// SuccessorArgs ask: it listens beside the channel until the running process hands it over.
	Successor bool
	// Earlier, with Successor, is the channel of a vault process of an earlier version of the application
	// that served the vault under another endpoint; see localipc.ListenOptions.Earlier.
	Earlier *localipc.Endpoint
}

// Run is the vault process: the hidden subcommand Start runs. It hardens the process before it reads
// anything secret (localipc.Harden), reads its state from the inherited pipe and overwrites what it read,
// listens on the channel and reports ready, and serves until it locks. A second process for the same
// channel reports running and returns ErrRunning. A process not started by Start returns ErrNotStarted
// before it reads anything. The stop signals of the platform lock it, as does the end of ctx.
func Run(ctx context.Context, opts RunOptions) error {
	child, ok := background.Inherit()
	if !ok {
		return ErrNotStarted
	}
	defer child.Close()
	fail := func(err error) error {
		child.Fail(err.Error())
		return err
	}
	if err := opts.validate(); err != nil {
		return fail(err)
	}
	if err := localipc.Harden(); err != nil {
		return fail(err)
	}
	data, err := child.ReadHandover(MaxState)
	if err != nil {
		return fail(err)
	}
	state, err := decodeState(data)
	clear(data)
	if err != nil {
		return fail(err)
	}
	server, err := NewServer(opts.Config, state, opts.Successor)
	if err != nil {
		return fail(err)
	}
	listener, err := opts.Endpoint.ListenWith(localipc.ListenOptions{Successor: opts.Successor, Earlier: opts.Earlier,
		Refused: server.Refused})
	if err != nil {
		server.wipe()
		if errors.Is(err, localipc.ErrRunning) && !opts.Successor {
			child.Running()
			return ErrRunning
		}
		return fail(err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, stopSignals()...)
	defer signal.Stop(signals)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-signals:
		case <-ctx.Done():
		}
		_ = server.Close()
	}()

	child.Ready()
	if opts.Successor {
		// A successor serves only once the channel is its own; one that never gets it locks at once.
		wait, cancelWait := context.WithTimeout(ctx, HandoverTimeout)
		err := localipc.AwaitHandover(wait, listener)
		cancelWait()
		if err != nil {
			_ = server.Close()
		}
	}
	return server.Serve(listener)
}
