package localipc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// CrossVersion admits a peer started from another program file, as the
// package documentation describes. Secret must be at least MinSecretSize
// bytes; ListenWith and DialWith use a copy of it.
type CrossVersion struct {
	Secret []byte
}

// MinSecretSize is the shortest secret CrossVersion accepts.
const MinSecretSize = 32

// ErrShortSecret means CrossVersion was chosen with a secret shorter than
// MinSecretSize bytes, or with none.
var ErrShortSecret = fmt.Errorf("a cross-version check needs a secret of at least %d bytes", MinSecretSize)

const (
	nonceSize = 32
	proofSize = sha256.Size
	// Distinct role labels make a proof reflected back to its sender fail.
	listenerRole = "localipc cross-version listener v1"
	dialerRole   = "localipc cross-version dialer v1"
	// crossVersionAccepted tells the dialer that the listener took its proof.
	crossVersionAccepted byte = 0x01
	// crossVersionWait bounds the exchange in Accept, which serves one
	// connection at a time: a local exchange takes milliseconds even on a
	// loaded machine, and a silent peer holds other clients no longer than this.
	crossVersionWait = 2 * time.Second
)

// secret returns a copy of the option's secret, or nil when the option is off.
func (c *CrossVersion) secret() ([]byte, error) {
	if c == nil {
		return nil, nil
	}
	if len(c.Secret) < MinSecretSize {
		return nil, ErrShortSecret
	}
	return bytes.Clone(c.Secret), nil
}

// peerCheck returns the check Accept runs on every connection of an Endpoint.
func (o ListenOptions) peerCheck() (func(net.Conn) (int, error), error) {
	secret, err := o.CrossVersion.secret()
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return func(c net.Conn) (int, error) { return checkPeer(c, true) }, nil
	}
	return func(c net.Conn) (int, error) {
		pid, err := checkPeer(c, false)
		if err == nil {
			err = proveAsListener(c, secret)
		}
		return pid, err
	}, nil
}

// proveAsListener answers the dialer's nonce with its own nonce and proof and
// accepts only a dialer that proves the secret for both nonces.
func proveAsListener(conn net.Conn, secret []byte) error {
	if err := conn.SetDeadline(time.Now().Add(crossVersionWait)); err != nil {
		return fmt.Errorf("%w: cannot bound the cross-version check: %v", ErrRefused, err)
	}
	if err := listenerExchange(conn, secret); err != nil {
		return fmt.Errorf("%w: the cross-version check failed: %w", ErrRefused, err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("%w: cannot reset the deadline: %v", ErrRefused, err)
	}
	return nil
}

func listenerExchange(conn net.Conn, secret []byte) error {
	dialerNonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(conn, dialerNonce); err != nil {
		return fmt.Errorf("no nonce from the peer: %w", err)
	}
	ownNonce := make([]byte, nonceSize)
	if _, err := rand.Read(ownNonce); err != nil {
		return err
	}
	answer := append(ownNonce, crossVersionProof(secret, listenerRole, dialerNonce, ownNonce)...)
	if _, err := conn.Write(answer); err != nil {
		return err
	}
	proof := make([]byte, proofSize)
	if _, err := io.ReadFull(conn, proof); err != nil {
		return fmt.Errorf("no proof from the peer: %w", err)
	}
	if !hmac.Equal(proof, crossVersionProof(secret, dialerRole, dialerNonce, ownNonce)) {
		return errors.New("the peer did not prove the secret")
	}
	_, err := conn.Write([]byte{crossVersionAccepted})
	return err
}

// proveAsDialer sends a nonce first and proves the secret only to a listener
// that proved it before. ctx bounds the exchange.
func proveAsDialer(ctx context.Context, conn net.Conn, secret []byte) error {
	return bounded(ctx, conn, "the cross-version check failed", func() error { return dialerExchange(conn, secret) })
}

func dialerExchange(conn net.Conn, secret []byte) error {
	ownNonce := make([]byte, nonceSize)
	if _, err := rand.Read(ownNonce); err != nil {
		return err
	}
	if _, err := conn.Write(ownNonce); err != nil {
		return err
	}
	answer := make([]byte, nonceSize+proofSize)
	if _, err := io.ReadFull(conn, answer); err != nil {
		return fmt.Errorf("no proof from the peer: %w", err)
	}
	listenerNonce, proof := answer[:nonceSize], answer[nonceSize:]
	if !hmac.Equal(proof, crossVersionProof(secret, listenerRole, ownNonce, listenerNonce)) {
		return errors.New("the peer did not prove the secret")
	}
	if _, err := conn.Write(crossVersionProof(secret, dialerRole, ownNonce, listenerNonce)); err != nil {
		return err
	}
	var accepted [1]byte
	if _, err := io.ReadFull(conn, accepted[:]); err != nil || accepted[0] != crossVersionAccepted {
		return errors.New("the peer refused this process's proof")
	}
	return nil
}

func crossVersionProof(secret []byte, role string, dialerNonce, listenerNonce []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(role))
	mac.Write(dialerNonce)
	mac.Write(listenerNonce)
	return mac.Sum(nil)
}
