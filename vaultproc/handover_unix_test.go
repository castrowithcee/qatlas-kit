//go:build linux || darwin

package vaultproc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
)

const (
	testNamespace      = "example-release"
	testSignatureAsset = "checksums.txt.example.sig"
	testArchive        = "tool-test.tar.gz"
	newProgramBody     = "new program"
)

// testRelease writes a signed release whose program is body and returns its files and signing key.
func testRelease(t *testing.T, body string) (selfupdate.ReleaseFiles, ssh.PublicKey) {
	t.Helper()
	signer := releasetest.NewSigner(t)
	archive := releasetest.Archive(t, testArchive, releasetest.File{Name: "bin/tool", Body: []byte(body)})
	assets := releasetest.Release(t, signer, testNamespace, testSignatureAsset, map[string][]byte{testArchive: archive})
	dir := t.TempDir()
	for name, data := range assets {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return selfupdate.ReleaseFiles{Checksums: filepath.Join(dir, "checksums.txt"),
		Signature: filepath.Join(dir, testSignatureAsset), Archive: filepath.Join(dir, testArchive),
		ArchiveName: testArchive}, signer.PublicKey()
}

// acceptGate is the beforeAccept of a server. Once armed, the accept loop reports that it arrived and waits
// until the gate is opened, so connections made meanwhile stay in the listener's queue.
type acceptGate struct {
	armed   atomic.Bool
	loops   atomic.Int32
	entered chan struct{}
	opened  chan struct{}
	once    sync.Once
}

func (g *acceptGate) open() { g.once.Do(func() { close(g.opened) }) }

func (g *acceptGate) wait() {
	g.loops.Add(1)
	if g.armed.Load() {
		select {
		case g.entered <- struct{}{}:
		default:
		}
		<-g.opened
	}
}

type handoverSetup struct {
	running
	gate      *acceptGate
	hold      chan struct{} // if set, a successor starts only once it is closed
	cfg       Config
	files     selfupdate.ReleaseFiles
	program   string
	replaced  atomic.Bool
	successor chan *Server
}

// replace installs body as the program, as an update does.
func (h *handoverSetup) replace(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(h.program, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	h.replaced.Store(true)
}

// newHandover runs a server that may hand over; its successor runs in this process too.
func newHandover(t *testing.T, change func(*handoverSetup)) *handoverSetup {
	t.Helper()
	files, key := testRelease(t, newProgramBody)
	h := &handoverSetup{cfg: testConfig(t), files: files, program: filepath.Join(t.TempDir(), "tool"),
		successor: make(chan *Server, 1)}
	h.cfg.MaxUnlock = 2 * time.Hour
	h.cfg.Handover = &HandoverConfig{Namespace: testNamespace, TrustedKeys: []ssh.PublicKey{key}, ProgramName: "tool",
		HandsOver: func(*age.X25519Identity) bool { return true }}
	if err := os.WriteFile(h.program, []byte("old program"), 0o700); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(h)
	}
	id := newIdentity(t)
	server, err := NewServer(h.cfg, State{Identity: id, Document: []byte(testDocument)}, false)
	if err != nil {
		t.Fatal(err)
	}
	h.gate = &acceptGate{entered: make(chan struct{}, 1), opened: make(chan struct{})}
	t.Cleanup(h.gate.open)
	server.beforeAccept = h.gate.wait
	server.program = func() (string, bool, error) { return h.program, h.replaced.Load(), nil }
	server.start = func(ctx context.Context, program string, data []byte) (int, func(), error) {
		if h.hold != nil {
			select {
			case <-h.hold:
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			}
		}
		if program != h.program {
			return 0, nil, errors.New("the successor was not started from the program's path")
		}
		state, err := decodeState(data)
		if err != nil {
			return 0, nil, err
		}
		next, err := NewServer(h.cfg, state, true)
		if err != nil {
			return 0, nil, err
		}
		l, err := h.cfg.Endpoint.ListenWith(localipc.ListenOptions{Successor: true, Refused: next.Refused})
		if err != nil {
			return 0, nil, err
		}
		done := make(chan error, 1)
		go func() {
			wait, cancel := context.WithTimeout(context.Background(), HandoverTimeout)
			defer cancel()
			if localipc.AwaitHandover(wait, l) != nil {
				_ = next.Close()
			}
			done <- next.Serve(l)
		}()
		t.Cleanup(func() {
			_ = next.Close()
			<-done
		})
		h.successor <- next
		return os.Getpid(), func() { _ = next.Close() }, nil
	}
	h.running = serveWith(t, server, h.cfg, id, false)
	return h
}

func TestHandoverToSuccessor(t *testing.T) {
	h := newHandover(t, nil)
	ctx := context.Background()
	handover, err := h.client.PrepareHandover(ctx, h.files)
	if err != nil || handover == nil {
		t.Fatalf("PrepareHandover = %v, %v", handover, err)
	}
	// A prepared handover does not hold up other requests, and a second one does not disturb it.
	if err := h.client.Call(ctx, "get", nil, nil); err != nil {
		t.Fatalf("get while a handover is prepared = %v", err)
	}
	if second, err := h.client.PrepareHandover(ctx, h.files); second != nil || !errors.Is(err, ErrHandover) {
		t.Fatalf("second PrepareHandover = %v, %v", second, err)
	}
	before, err := h.client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.replace(t, newProgramBody)
	pid, err := handover.Commit(ctx)
	if err != nil || pid != os.Getpid() {
		t.Fatalf("Commit = %d, %v", pid, err)
	}
	awaitStopped(t, h.running)

	var got string
	if err := h.client.Call(ctx, "peek", nil, &got); err != nil || got != testDocument {
		t.Fatalf("peek at the successor = %q, %v", got, err)
	}
	after, err := h.client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !after.LocksAt.Equal(before.LocksAt) || !after.UnlockEndsAt.Equal(before.UnlockEndsAt) {
		t.Fatalf("the successor's times %+v differ from its predecessor's %+v", after, before)
	}
}

// refusalReasons is the reason a client reads for each refusal that the process answers with an error.
func refusalReasons() map[string]HandoverReason {
	return map[string]HandoverReason{
		"untrusted key": HandoverNotVerified, "other namespace": HandoverNotVerified, "other program": HandoverNotVerified,
		"tampered archive": HandoverNotVerified, "archive by link": HandoverNotVerified, "relative path": HandoverNotVerified,
		"archive path": HandoverNotVerified, "rollback": HandoverRollback, "no handover": HandoverUnsupported,
		"program unchanged": HandoverProgramUnchanged, "program mismatch": HandoverProgramMismatch,
		"successor fails": HandoverNoSuccessor,
	}
}

func TestHandoverRefusalsLock(t *testing.T) {
	ctx := context.Background()
	prepare := func(t *testing.T, h *handoverSetup) *Handover {
		t.Helper()
		handover, err := h.client.PrepareHandover(ctx, h.files)
		if err != nil || handover == nil {
			t.Fatalf("PrepareHandover = %v, %v", handover, err)
		}
		return handover
	}
	for name, test := range map[string]struct {
		change func(*handoverSetup)
		run    func(*testing.T, *handoverSetup) error
	}{
		"untrusted key": {
			change: func(h *handoverSetup) {
				_, other := testRelease(t, "x")
				h.cfg.Handover.TrustedKeys = []ssh.PublicKey{other}
			},
		},
		"other namespace": {change: func(h *handoverSetup) { h.cfg.Handover.Namespace = "example-other" }},
		"other program":   {change: func(h *handoverSetup) { h.cfg.Handover.ProgramName = "other" }},
		"tampered archive": {change: func(h *handoverSetup) {
			if err := os.WriteFile(h.files.Archive, []byte("tampered"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		"archive by link": {change: func(h *handoverSetup) {
			link := filepath.Join(t.TempDir(), testArchive)
			if err := os.Symlink(h.files.Archive, link); err != nil {
				t.Fatal(err)
			}
			h.files.Archive = link
		}},
		"relative path": {change: func(h *handoverSetup) { h.files.Checksums = "checksums.txt" }},
		"archive path":  {change: func(h *handoverSetup) { h.files.ArchiveName = "../" + testArchive }},
		"rollback":      {change: func(h *handoverSetup) { h.files.Rollback = true }},
		"no handover":   {change: func(h *handoverSetup) { h.cfg.Handover = nil }},
		"setting locks": {change: func(h *handoverSetup) { h.cfg.Handover.HandsOver = func(*age.X25519Identity) bool { return false } }, run: func(t *testing.T, h *handoverSetup) error {
			handover, err := h.client.PrepareHandover(ctx, h.files)
			if handover != nil || err != nil {
				t.Fatalf("PrepareHandover = %v, %v; want the lock the setting asks for", handover, err)
			}
			return ErrHandover
		}},
		"program unchanged": {run: func(t *testing.T, h *handoverSetup) error {
			_, err := prepare(t, h).Commit(ctx)
			return err
		}},
		"program mismatch": {run: func(t *testing.T, h *handoverSetup) error {
			handover := prepare(t, h)
			h.replace(t, "another program")
			_, err := handover.Commit(ctx)
			return err
		}},
		"closed before commit": {run: func(t *testing.T, h *handoverSetup) error {
			_ = prepare(t, h).Close()
			return ErrHandover
		}},
		"successor fails": {run: func(t *testing.T, h *handoverSetup) error {
			handover := prepare(t, h)
			h.server.start = func(context.Context, string, []byte) (int, func(), error) {
				return 0, nil, errors.New("example failure")
			}
			h.replace(t, newProgramBody)
			_, err := handover.Commit(ctx)
			return err
		}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHandover(t, test.change)
			run := test.run
			if run == nil {
				run = func(t *testing.T, h *handoverSetup) error {
					handover, err := h.client.PrepareHandover(ctx, h.files)
					if handover != nil {
						t.Fatal("the handover was prepared")
					}
					return err
				}
			}
			err := run(t, h)
			if !errors.Is(err, ErrHandover) || strings.Contains(err.Error(), h.program) {
				t.Fatalf("handover = %v, want ErrHandover without a path", err)
			}
			if reason, ok := refusalReasons()[name]; ok {
				var he *HandoverError
				if !errors.As(err, &he) || he.Reason != reason || !strings.HasPrefix(err.Error(), handoverPrefix) {
					t.Fatalf("handover = %v, want reason %q with the unchanged text", err, reason)
				}
			}
			awaitStopped(t, h.running)
			if _, err := h.client.Status(ctx); !errors.Is(err, ErrNotRunning) {
				t.Fatalf("status after the refusal = %v, want ErrNotRunning", err)
			}
		})
	}
}

func TestReplacementHandsOver(t *testing.T) {
	h := newHandover(t, nil)
	ctx := context.Background()
	replacement := NewReplacement(h.client)
	if err := replacement.BeforeReplace(ctx, h.files); err != nil {
		t.Fatal(err)
	}
	h.replace(t, newProgramBody)
	replacement.AfterReplace(ctx, h.files)
	if result := replacement.Finish(); result.Outcome != OutcomeHandedOver || result.PID != os.Getpid() || result.Err != nil {
		t.Fatalf("replacement = %+v", result)
	}
	if err := h.client.Call(ctx, "get", nil, nil); err != nil {
		t.Fatalf("get at the successor = %v", err)
	}
}

func TestReplacementLocksWhenTheUpdateFails(t *testing.T) {
	h := newHandover(t, nil)
	replacement := NewReplacement(h.client)
	if err := replacement.BeforeReplace(context.Background(), h.files); err != nil {
		t.Fatal(err)
	}
	if result := replacement.Finish(); result.Outcome != OutcomeLocked || !errors.Is(result.Err, ErrHandover) {
		t.Fatalf("replacement = %+v", result)
	}
	awaitStopped(t, h.running)
}

func TestWatchLocksWhenTheSocketIsGone(t *testing.T) {
	cfg := testConfig(t)
	server, err := NewServer(cfg, State{Identity: newIdentity(t)}, false)
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
}

func TestReleaseFilesAreOpenedSafely(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "data")
	if err := os.WriteFile(file, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readReleaseFile(file, 4); err == nil {
		t.Error("a file above the limit was read")
	}
	if _, err := readReleaseFile(dir, 1<<20); err == nil {
		t.Error("a directory was read")
	}
	if data, err := readReleaseFile(file, 5); err != nil || string(data) != "12345" {
		t.Errorf("readReleaseFile = %q, %v", data, err)
	}
	if _, err := programHash(filepath.Join(dir, "missing"), 1<<20); err == nil {
		t.Error("a missing program was hashed")
	}
}

func waitUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !done(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// commitHeld prepares a handover, replaces the program and commits it in the background. The successor does
// not start until release is called, so the server answers replaced meanwhile. wait returns the commit's result.
func (h *handoverSetup) commitHeld(t *testing.T) (release func(), wait func() error) {
	t.Helper()
	ctx := context.Background()
	handover, err := h.client.PrepareHandover(ctx, h.files)
	if err != nil {
		t.Fatal(err)
	}
	h.replace(t, newProgramBody)
	h.hold = make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := handover.Commit(ctx)
		done <- err
	}()
	waitUntil(t, "the server answers replaced", func() bool {
		h.server.mu.Lock()
		defer h.server.mu.Unlock()
		return h.server.handingOver
	})
	var once sync.Once
	release = func() { once.Do(func() { close(h.hold) }) }
	t.Cleanup(release)
	return release, func() error { return <-done }
}

// A client that was answered replaced and has its next request waiting in the old listener's queue when the
// handover commits is reset as the old listener closes. It is asked again and lands at the successor.
func TestHandoverAnswersAClientQueuedAtTheOldListener(t *testing.T) {
	h := newHandover(t, nil)
	release, wait := h.commitHeld(t)
	base := h.gate.loops.Load()
	result := make(chan error, 1)
	go func() {
		var got string
		err := h.client.Call(context.Background(), "peek", nil, &got)
		if err == nil && got != testDocument {
			err = fmt.Errorf("peek = %q", got)
		}
		result <- err
	}()
	waitUntil(t, "the request was answered replaced and asked again", func() bool { return h.gate.loops.Load() >= base+2 })

	// The accept loop stops before its next Accept, so the client's following request waits in the queue.
	h.gate.armed.Store(true)
	<-h.gate.entered
	// The queued request cannot be seen from here: give the client time to ask again, many pauses long.
	time.Sleep(6 * replacedPause)
	release()
	// The gate stays shut until the answer is in: the queued request is reset by the close of the old
	// listener, not accepted by the old server.
	err := <-result
	h.gate.open()
	if err != nil {
		t.Fatalf("Call() queued across the handover = %v, want the successor's answer", err)
	}
	if err := wait(); err != nil {
		t.Fatalf("Commit() = %v", err)
	}
	awaitStopped(t, h.running)
}

// Without a replaced answer before it, ErrNotRunning is final at once.
func TestNotRunningWithoutReplacedIsFinal(t *testing.T) {
	c := &Client{Endpoint: testEndpoint(t), Recipient: newIdentity(t).Recipient().String(), Version: 1, ProofLabel: testLabel}
	start := time.Now()
	if err := c.Call(context.Background(), "peek", nil, nil); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Call() = %v, want ErrNotRunning", err)
	}
	if d := time.Since(start); d >= replacedLimit/2 {
		t.Fatalf("Call() took %v, want it to give up at once", d)
	}
}

// After a replaced answer, ErrNotRunning is asked again until replacedLimit or the end of the context.
func TestNotRunningAfterReplacedEndsAtTheLimitAndTheContext(t *testing.T) {
	for name, tc := range map[string]struct{ timeout, min, max time.Duration }{
		"limit":   {time.Minute, replacedLimit - replacedPause, replacedLimit + time.Second},
		"context": {replacedLimit / 4, replacedLimit / 4, replacedLimit / 2},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHandover(t, nil)
			_, wait := h.commitHeld(t)
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			result := make(chan error, 1)
			base := h.gate.loops.Load()
			start := time.Now()
			go func() { result <- h.client.Call(ctx, "peek", nil, nil) }()
			// The client asks again only after an answer: a second accepted request means the first was replaced.
			waitUntil(t, "the request was answered replaced and asked again", func() bool { return h.gate.loops.Load() >= base+2 })
			_ = h.server.Close()
			err := <-result
			if d := time.Since(start); err == nil || d < tc.min || d > tc.max {
				t.Fatalf("Call() = %v after %v, want a failure after %v to %v", err, d, tc.min, tc.max)
			}
			if name == "limit" && !errors.Is(err, ErrNotRunning) {
				t.Fatalf("Call() = %v, want ErrNotRunning", err)
			}
			_ = wait()
		})
	}
}
