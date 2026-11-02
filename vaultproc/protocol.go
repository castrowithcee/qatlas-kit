package vaultproc

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"filippo.io/age"
)

// ProtocolVersion is the version of the kit's part of the protocol. Every message carries it, together with
// the application's own Config.Version, and either side refuses a message of another version.
const ProtocolVersion = 1

// MaxMessage bounds one request or answer, newline included.
const MaxMessage = 64 << 10

// DefaultRequestTimeout bounds one connection on either side unless Config.RequestTimeout or
// Client.Timeout set another limit.
const DefaultRequestTimeout = 5 * time.Second

// HandoverTimeout bounds each phase of a handover: the prepare, the wait for the commit while the program
// is replaced, and the commit, which starts the successor.
const HandoverTimeout = 30 * time.Second

// The operations the kit answers itself. An application cannot register them.
const (
	OpStatus   = "status"
	OpLock     = "lock"
	OpHandover = "handover"
)

// The codes of the kit. They are fixed words, never built from a request, so an answer can neither carry a
// secret nor echo what a peer sent.
const (
	codeRefused        = "refused"
	codeBadRequest     = "bad-request"
	codeTooLarge       = "too-large"
	codeVersion        = "version"
	codeLocked         = "locked"
	codeChallenge      = "challenge"
	codeReplaced       = "replaced"
	codeUnknownOp      = "unknown-operation"
	codeFailed         = "failed"
	codeUnverified     = "release-unverified"
	codeRollback       = "rollback"
	codeUnchanged      = "program-unchanged"
	codeMismatch       = "program-mismatch"
	codeHandoverFailed = "handover-failed"
	codeNoHandover     = "handover-unsupported"
)

func isKitCode(code string) bool {
	switch code {
	case codeRefused, codeBadRequest, codeTooLarge, codeVersion, codeLocked, codeChallenge, codeReplaced,
		codeUnknownOp, codeFailed, codeUnverified, codeRollback, codeUnchanged, codeMismatch, codeHandoverFailed,
		codeNoHandover:
		return true
	}
	return false
}

// wordPattern is the form of an operation name and of an application's error code.
var wordPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// The phases of a handover request.
const (
	phasePrepare = "prepare"
	phaseCommit  = "commit"
)

// nonceSize is the number of random bytes a challenge holds.
const nonceSize = 32

// proofLabel separates the proof from any other use of the nonce's hash.
const proofLabel = "vault process proof v1\x00"

// hello is the first line of every connection: a fresh nonce encrypted to the vault's recipient.
type hello struct {
	V         int    `json:"v"`
	App       int    `json:"app"`
	Challenge []byte `json:"challenge,omitempty"`
}

type request struct {
	V    int             `json:"v"`
	App  int             `json:"app"`
	Op   string          `json:"op"`
	Body json.RawMessage `json:"body,omitempty"`
}

type response struct {
	V      int             `json:"v"`
	App    int             `json:"app"`
	Error  string          `json:"error,omitempty"`
	Proof  []byte          `json:"proof,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// statusResult is the result of OpStatus.
type statusResult struct {
	PID          int        `json:"pid"`
	LocksAt      time.Time  `json:"locks_at"`
	UnlockEndsAt *time.Time `json:"unlock_ends_at,omitempty"`
	MemoryLocked bool       `json:"memory_locked,omitempty"`
}

// handoverRequest is the body of OpHandover.
type handoverRequest struct {
	Phase   string        `json:"phase"`
	Release *releaseFiles `json:"release,omitempty"`
}

type releaseFiles struct {
	Checksums   string `json:"checksums"`
	Signature   string `json:"signature"`
	Archive     string `json:"archive"`
	ArchiveName string `json:"archive_name"`
	Rollback    bool   `json:"rollback,omitempty"`
}

// handoverResult answers a prepare with whether the vault hands over, and a commit with the successor.
type handoverResult struct {
	HandOver bool `json:"hand_over,omitempty"`
	PID      int  `json:"pid,omitempty"`
}

// writeMessage sends v as one line of JSON and overwrites the encoded bytes, which may hold a secret.
func writeMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return errors.New("cannot encode a vault process message")
	}
	defer clear(data)
	if len(data)+1 > MaxMessage {
		return ErrTooLarge
	}
	line := append(data, '\n')
	defer clear(line)
	_, err = w.Write(line)
	return err
}

// readMessage reads one line of JSON into v, never more than MaxMessage bytes, and overwrites what it read.
func readMessage(r io.Reader, v any) error {
	reader := bufio.NewReaderSize(io.LimitReader(r, MaxMessage+1), 4096)
	line, err := reader.ReadBytes('\n')
	defer clear(line)
	switch {
	case len(line) > MaxMessage:
		return ErrTooLarge
	case errors.Is(err, io.EOF):
		if len(line) == 0 {
			return io.EOF
		}
		return io.ErrUnexpectedEOF
	case err != nil:
		return err
	}
	if err := json.Unmarshal(line, v); err != nil {
		// The decoder's message may quote the input, which can be a secret.
		return errMalformed
	}
	return nil
}

var errMalformed = errors.New("a vault process message is not valid JSON")

// newChallenge returns a fresh nonce and the challenge that carries it, encrypted to recipient. The caller
// clears the nonce.
func newChallenge(recipient age.Recipient) (nonce, challenge []byte, err error) {
	nonce = make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("cannot read random bytes for the challenge: %w", err)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err == nil {
		_, err = w.Write(nonce)
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		clear(nonce)
		return nil, nil, errors.New("cannot encrypt the challenge")
	}
	return nonce, buf.Bytes(), nil
}

// solveChallenge decrypts a challenge with key and returns its proof. It refuses anything that does not
// decrypt to exactly one nonce, and returns only a hash: a server that answered with the plaintext would
// decrypt any message encrypted to the vault for whoever asked.
func solveChallenge(key age.Identity, challenge []byte) ([]byte, bool) {
	if key == nil || len(challenge) == 0 {
		return nil, false
	}
	r, err := age.Decrypt(bytes.NewReader(challenge), key)
	if err != nil {
		return nil, false
	}
	nonce, err := io.ReadAll(io.LimitReader(r, nonceSize+1))
	defer clear(nonce)
	if err != nil || len(nonce) != nonceSize {
		return nil, false
	}
	return proofOf(nonce), true
}

func proofOf(nonce []byte) []byte {
	h := sha256.New()
	h.Write([]byte(proofLabel))
	h.Write(nonce)
	return h.Sum(nil)
}

func checkProof(nonce, proof []byte) bool {
	return subtle.ConstantTimeCompare(proofOf(nonce), proof) == 1
}
