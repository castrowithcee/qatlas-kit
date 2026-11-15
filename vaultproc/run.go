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

	"filippo.io/age"

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
	// DecodeEarlier, with Successor, reads a state that the process that started this one handed over in
	// an earlier format of the application. Run tries the kit's own format first and calls it only if
	// that fails. data is the handover as read; the hook must not keep it or part of it, and Run overwrites
	// it afterwards. The hook runs after localipc.Harden. Run rejects a result without a key, without
	// State.LocksAt or with a negative State.MaxUnlock, and reports every failure without its text.
	// Without Successor, RunOptions are invalid with it: a first start always comes in the kit's format.
	DecodeEarlier func(data []byte) (State, error)
	// Configure, when set, forms the Config and the Earlier endpoint the process serves with after the state
	// was read, from the state's key and the values of the options, for settings the application keeps
	// encrypted under that key. The Config it returns replaces the embedded one and the endpoint replaces
	// Earlier (nil removes it); both are checked like any other, so Earlier is still valid only with
	// Successor. The Config is validated in full, including the shortened maximum unlock time and the lock
	// time of a successor's predecessor, which it cannot extend. With Configure the embedded Config is not
	// checked before the state was read, so it may be incomplete. The hook runs after localipc.Harden and
	// its error is reported without its text. It must not keep the key or change it.
	Configure func(key *age.X25519Identity, base Config, earlier *localipc.Endpoint) (Config, *localipc.Endpoint, error)
}

func (o RunOptions) validate() error {
	if o.DecodeEarlier != nil && !o.Successor {
		return errors.New("vaultproc: DecodeEarlier needs Successor")
	}
	if o.Configure != nil {
		return nil
	}
	return o.Config.validate()
}

// open turns the handover bytes into a server and the Config it serves with. It takes the bytes and
// overwrites them before it returns. Every error is fixed text: neither the bytes nor a hook's error leave it.
func (o RunOptions) open(data []byte) (*Server, Config, *localipc.Endpoint, error) {
	defer clear(data)
	state, err := decodeState(data)
	if err != nil && o.DecodeEarlier != nil {
		state, err = o.decodeEarlier(data)
	}
	if err != nil {
		return nil, Config{}, nil, err
	}
	cfg, earlier := o.Config, o.Earlier
	if o.Configure != nil {
		cfg, earlier, err = o.Configure(state.Identity, cfg, earlier)
		if err != nil {
			clear(state.Document)
			return nil, Config{}, nil, errors.New("the vault process settings cannot be read")
		}
	}
	server, err := NewServer(cfg, state, o.Successor)
	if err != nil {
		return nil, Config{}, nil, err
	}
	return server, cfg, earlier, nil
}

func (o RunOptions) decodeEarlier(data []byte) (State, error) {
	state, err := o.DecodeEarlier(data)
	if err != nil || state.Identity == nil || state.LocksAt.IsZero() || state.MaxUnlock < 0 {
		clear(state.Document)
		return State{}, errors.New("the vault process state cannot be read")
	}
	return state, nil
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
	server, cfg, earlier, err := opts.open(data)
	if err != nil {
		return fail(err)
	}
	listener, err := cfg.Endpoint.ListenWith(localipc.ListenOptions{Successor: opts.Successor, Earlier: earlier,
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
