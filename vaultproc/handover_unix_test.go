//go:build linux || darwin

package vaultproc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	testNamespace  = "example-release"
	testArchive    = "tool-test.tar.gz"
	newProgramBody = "new program"
)

// testRelease writes a signed release whose program is body and returns its files and signing key.
func testRelease(t *testing.T, body string) (selfupdate.ReleaseFiles, ssh.PublicKey) {
	t.Helper()
	signer := releasetest.NewSigner(t)
	archive := releasetest.Archive(t, testArchive, releasetest.File{Name: "bin/tool", Body: []byte(body)})
	assets := releasetest.Release(t, signer, testNamespace, map[string][]byte{testArchive: archive})
	dir := t.TempDir()
	for name, data := range assets {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return selfupdate.ReleaseFiles{Checksums: filepath.Join(dir, "checksums.txt"),
		Signature: filepath.Join(dir, "checksums.txt.sig"), Archive: filepath.Join(dir, testArchive),
		ArchiveName: testArchive}, signer.PublicKey()
}

type handoverSetup struct {
	running
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
	server.program = func() (string, bool, error) { return h.program, h.replaced.Load(), nil }
	server.start = func(_ context.Context, program string, data []byte) (int, func(), error) {
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
			if err := run(t, h); !errors.Is(err, ErrHandover) || strings.Contains(err.Error(), h.program) {
				t.Fatalf("handover = %v, want ErrHandover without a path", err)
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
	if _, err := programHash(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing program was hashed")
	}
}
