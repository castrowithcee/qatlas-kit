package vaultproc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/release"
)

// The largest release files a prepare reads, and the largest program a commit hashes.
const (
	maxMetadataFile = 1 << 20
	maxArchiveFile  = 256 << 20
	maxProgramFile  = 256 << 20
)

// HandoverConfig is what a vault process needs to hand the vault over to a successor. It comes from the
// program and the configuration the process was started with, never from a request.
type HandoverConfig struct {
	// Namespace and TrustedKeys verify the release signature, as selfupdate.Client does.
	Namespace   string
	TrustedKeys []ssh.PublicKey
	// ProgramName is the program's file name; the release archive holds it as "bin/<ProgramName>".
	ProgramName string
	// SuccessorArgs start the successor from the replaced program, typically the hidden subcommand that
	// calls Run with RunOptions.Successor set.
	SuccessorArgs []string
	// HandsOver reads, with the vault's key, whether the user chose that an update hands the vault over.
	// It is asked at every prepare; false, or a setting it cannot trust, locks the process. Required.
	HandsOver func(key *age.X25519Identity) bool
}

func (h *HandoverConfig) validate() error {
	switch {
	case h.Namespace == "" || len(h.TrustedKeys) == 0:
		return errors.New("vaultproc: a handover needs a signature namespace and trusted keys")
	case h.ProgramName == "" || h.ProgramName != filepath.Base(h.ProgramName) || strings.ContainsAny(h.ProgramName, `/\`):
		return errors.New("vaultproc: a handover needs the program's file name")
	case h.HandsOver == nil:
		return errors.New("vaultproc: a handover needs the user's update setting")
	}
	return nil
}

// serveHandover carries out a handover request, which takes over its connection: the prepare, then the
// commit on the same connection. Every way it ends other than a completed handover locks the server. A
// second handover while one is under way is refused without disturbing the first.
func (s *Server) serveHandover(conn net.Conn, body json.RawMessage) {
	if code := s.claimHandover(conn); code != "" {
		_ = writeMessage(conn, s.answer(code, nil))
		return
	}
	defer s.releaseHandover()
	var req handoverRequest
	if json.Unmarshal(body, &req) != nil || req.Phase != phasePrepare || req.Release == nil {
		s.failHandover(conn, codeBadRequest)
		return
	}
	_ = conn.SetDeadline(time.Now().Add(HandoverTimeout))
	hash, handsOver, code := s.prepare(*req.Release)
	switch {
	case code != "":
		s.failHandover(conn, code)
		return
	case !handsOver:
		// The user's setting asks an update to lock the vault.
		s.stop()
		_ = writeMessage(conn, s.answer("", handoverResult{}))
		return
	}
	if writeMessage(conn, s.answer("", handoverResult{HandOver: true})) != nil {
		s.failHandover(conn, codeHandoverFailed)
		return
	}

	// The client replaces the program now and commits on this connection, which was checked while it
	// still ran the program this process runs.
	_ = conn.SetDeadline(time.Now().Add(HandoverTimeout))
	var commit request
	if err := readMessage(conn, &commit); err != nil {
		s.failHandover(conn, codeHandoverFailed)
		return
	}
	var phase handoverRequest
	if commit.V != ProtocolVersion || commit.App != s.cfg.Version || commit.Op != OpHandover ||
		json.Unmarshal(commit.Body, &phase) != nil || phase.Phase != phaseCommit {
		s.failHandover(conn, codeBadRequest)
		return
	}
	_ = conn.SetDeadline(time.Now().Add(HandoverTimeout))
	pid, code := s.commit(hash)
	if code != "" {
		s.failHandover(conn, code)
		return
	}
	_ = writeMessage(conn, s.answer("", handoverResult{PID: pid}))
	s.stop()
}

func (s *Server) claimHandover(conn net.Conn) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.handingOver:
		return codeReplaced
	case s.locked || s.stopping():
		return codeLocked
	case s.pending != nil:
		return codeHandoverFailed
	}
	s.pending = conn
	return ""
}

func (s *Server) releaseHandover() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = nil
}

// failHandover locks the server and then answers with code, so no client hears of the failure while the
// server still accepts connections.
func (s *Server) failHandover(conn net.Conn, code string) {
	s.mu.Lock()
	s.handingOver = false
	s.mu.Unlock()
	s.stop()
	_ = conn.SetWriteDeadline(time.Now().Add(s.requestTimeout()))
	_ = writeMessage(conn, s.answer(code, nil))
}

// prepare verifies the release files and reads the user's update setting, both by the server itself. It
// returns the SHA-256 of the program in the release.
func (s *Server) prepare(files releaseFiles) (hash string, handsOver bool, code string) {
	s.mu.Lock()
	key, locked := s.key, s.locked || s.stopping()
	s.mu.Unlock()
	config := s.cfg.Handover
	switch {
	case locked:
		return "", false, codeLocked
	case !handoverSupported || config == nil:
		return "", false, codeNoHandover
	case files.Rollback:
		// An older program cannot be trusted with the vault.
		return "", false, codeRollback
	}
	hash, err := verifyRelease(files, config)
	if err != nil {
		return "", false, codeUnverified
	}
	return hash, config.HandsOver(key), ""
}

// commit hands the vault over to a successor started from the program that replaced this one, which must
// be the program of the verified release. From its start, new requests are answered replaced; those already
// read are answered first, so the successor's state holds their changes. Any failure before the successor
// took the socket over ends the successor.
func (s *Server) commit(hash string) (int, string) {
	s.mu.Lock()
	if s.locked || s.stopping() {
		s.mu.Unlock()
		return 0, codeLocked
	}
	s.handingOver = true
	// The successor takes the lock time over; no timer is needed here until then.
	if s.timer != nil {
		s.timer.Stop()
	}
	l := s.listener
	s.mu.Unlock()
	if !s.drain() {
		return 0, codeHandoverFailed
	}

	program, replaced, err := s.program()
	if err != nil || !replaced || !filepath.IsAbs(program) {
		return 0, codeUnchanged
	}
	if got, err := programHash(program); err != nil || got != hash {
		return 0, codeMismatch
	}
	state, ok := s.snapshot()
	if !ok {
		return 0, codeHandoverFailed
	}
	defer clear(state)

	ctx, cancel := context.WithTimeout(context.Background(), HandoverTimeout)
	defer cancel()
	go func() {
		select {
		case <-s.stopped:
			cancel()
		case <-ctx.Done():
		}
	}()
	pid, end, err := s.start(ctx, program, state)
	if err != nil {
		return 0, codeHandoverFailed
	}
	// The socket changes hands under s.mu, which stop takes as well: a lock that comes first ends the
	// successor, and one that comes after finds the socket handed over.
	s.mu.Lock()
	if s.stopping() {
		err = errors.New("the server is locking")
	} else {
		err = localipc.HandOver(l)
	}
	s.mu.Unlock()
	if err != nil {
		end()
		return 0, codeHandoverFailed
	}
	return pid, ""
}

// startSuccessor starts program as the successor and hands it state; it returns once the successor
// listens beside the channel.
func (s *Server) startSuccessor(ctx context.Context, program string, state []byte) (int, func(), error) {
	result, err := background.Start(ctx, background.Command{Program: program, Args: s.cfg.Handover.SuccessorArgs},
		state, background.Options{Timeout: HandoverTimeout})
	if err != nil {
		return 0, nil, err
	}
	end := func() {
		_ = result.Process.Kill()
		select {
		case <-result.Process.Done():
		case <-time.After(5 * time.Second):
		}
	}
	if result.Report != background.ReportReady {
		// Only a process this one started listens beside the channel, never one that found another running.
		end()
		return 0, nil, errors.New("the successor did not start")
	}
	return result.PID, end, nil
}

// drain waits until every request read before the commit is answered, within one request's time.
func (s *Server) drain() bool {
	deadline := time.Now().Add(s.requestTimeout())
	for {
		s.mu.Lock()
		n := s.inFlight
		s.mu.Unlock()
		switch {
		case n == 0:
			return true
		case s.stopping() || time.Now().After(deadline):
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// snapshot returns the handover bytes of what the server holds, with its lock time and unlock time.
func (s *Server) snapshot() ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key == nil || s.locked || s.stopping() {
		return nil, false
	}
	state := State{Identity: s.key, Document: s.doc.bytes(), UnlockedAt: s.unlocked, LocksAt: s.locksAt}
	if !s.hardAt.IsZero() {
		state.MaxUnlock = s.hardAt.Sub(s.unlocked)
	}
	data, err := encodeState(state)
	return data, err == nil
}

// verifyRelease checks the release files itself, as selfupdate does: the signature over the checksums, the
// archive's checksum, and then the program in the archive, whose hash it returns.
func verifyRelease(files releaseFiles, config *HandoverConfig) (string, error) {
	name := files.ArchiveName
	if name == "" || len(name) > 255 || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return "", errors.New("the archive name is not a file name")
	}
	checksums, err := readReleaseFile(files.Checksums, maxMetadataFile)
	if err != nil {
		return "", err
	}
	signature, err := readReleaseFile(files.Signature, maxMetadataFile)
	if err != nil {
		return "", err
	}
	archive, err := readReleaseFile(files.Archive, maxArchiveFile)
	if err != nil {
		return "", err
	}
	if err := release.VerifyArchive(checksums, signature, archive, config.Namespace, name, config.TrustedKeys); err != nil {
		return "", err
	}
	return release.ProgramSHA256(name, archive, "bin/"+config.ProgramName)
}

// readReleaseFile reads a regular file at an absolute path, of at most limit bytes. A path from a client
// names data to verify, never a program to run.
func readReleaseFile(path string, limit int64) ([]byte, error) {
	f, err := openRegular(path, limit)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, errors.New("the file cannot be read")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("the file is too large")
	}
	return data, nil
}

func programHash(path string) (string, error) {
	f, err := openRegular(path, maxProgramFile)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maxProgramFile+1))
	if err != nil {
		return "", errors.New("the program cannot be read")
	}
	if n > maxProgramFile {
		return "", errors.New("the program is too large")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// openRegular opens path, which must be absolute and name a regular file of at most limit bytes itself,
// not through a symbolic link, and be the same file once open.
func openRegular(path string, limit int64) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("the path is not absolute")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("the file cannot be read")
	}
	if !before.Mode().IsRegular() || before.Size() > limit {
		return nil, fmt.Errorf("the file is not a regular file of at most %d bytes", limit)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("the file cannot be read")
	}
	if after, err := f.Stat(); err != nil || !os.SameFile(before, after) {
		_ = f.Close()
		return nil, errors.New("the file changed while it was opened")
	}
	return f, nil
}
