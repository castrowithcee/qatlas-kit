package localipc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func testSecret(fill byte) []byte { return bytes.Repeat([]byte{fill}, MinSecretSize) }

// exchange runs the listener on one end of a pipe and the dialer on the other.
func exchange(t *testing.T, listener, dialer func(net.Conn) error) (listenErr, dialErr error) {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan error, 1)
	go func() {
		err := listener(a)
		_ = a.Close()
		done <- err
	}()
	dialErr = dialer(b)
	_ = b.Close()
	select {
	case listenErr = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not finish")
	}
	return listenErr, dialErr
}

func asListener(secret []byte) func(net.Conn) error {
	return func(c net.Conn) error { return proveAsListener(c, secret) }
}

func asDialer(secret []byte) func(net.Conn) error {
	return func(c net.Conn) error { return proveAsDialer(context.Background(), c, secret) }
}

func wantCrossVersionRefusal(t *testing.T, side string, err error) {
	t.Helper()
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "cross-version check failed") {
		t.Fatalf("%s = %v; want a cross-version refusal", side, err)
	}
}

func TestCrossVersionSecret(t *testing.T) {
	var off *CrossVersion
	if secret, err := off.secret(); secret != nil || err != nil {
		t.Fatalf("unset option = %v, %v", secret, err)
	}
	for name, secret := range map[string][]byte{"nil": nil, "empty": {}, "short": testSecret(1)[1:]} {
		if _, err := (&CrossVersion{Secret: secret}).secret(); !errors.Is(err, ErrShortSecret) {
			t.Errorf("%s secret = %v", name, err)
		}
		if _, err := (ListenOptions{CrossVersion: &CrossVersion{Secret: secret}}).peerCheck(); !errors.Is(err, ErrShortSecret) {
			t.Errorf("%s secret for a listener = %v", name, err)
		}
	}
	given := testSecret(1)
	secret, err := (&CrossVersion{Secret: given}).secret()
	if err != nil {
		t.Fatal(err)
	}
	given[0] = 2
	if secret[0] != 1 {
		t.Fatal("the option shares the caller's secret")
	}
}

func TestCrossVersionHandshake(t *testing.T) {
	listenErr, dialErr := exchange(t, func(c net.Conn) error {
		if err := proveAsListener(c, testSecret(1)); err != nil {
			return err
		}
		_, err := c.Write([]byte("data"))
		return err
	}, func(c net.Conn) error {
		if err := proveAsDialer(context.Background(), c, testSecret(1)); err != nil {
			return err
		}
		var b [4]byte
		if _, err := io.ReadFull(c, b[:]); err != nil || string(b[:]) != "data" {
			return errors.New("no data after the check")
		}
		return nil
	})
	if listenErr != nil || dialErr != nil {
		t.Fatalf("matching secrets refused: listener %v, dialer %v", listenErr, dialErr)
	}
}

func TestCrossVersionRejectsWrongSecret(t *testing.T) {
	// The dialer sees the wrong listener proof first and proves nothing.
	writes := 0
	listenErr, dialErr := exchange(t, asListener(testSecret(1)), func(c net.Conn) error {
		return proveAsDialer(context.Background(), &watchedConn{Conn: c, writes: &writes}, testSecret(2))
	})
	wantCrossVersionRefusal(t, "dialer", dialErr)
	if !strings.Contains(dialErr.Error(), "did not prove") || writes != 1 {
		t.Fatalf("dialer = %v after %d writes; want only its nonce sent", dialErr, writes)
	}
	wantCrossVersionRefusal(t, "listener", listenErr)

	// A dialer that does not check the listener still fails with a wrong proof.
	var reply []byte
	listenErr, _ = exchange(t, asListener(testSecret(1)), func(c net.Conn) error {
		dialerNonce := testSecret(7)
		listenerNonce := receiveAnswer(t, c, dialerNonce)
		_, _ = c.Write(crossVersionProof(testSecret(2), dialerRole, dialerNonce, listenerNonce))
		reply, _ = io.ReadAll(c)
		return nil
	})
	wantCrossVersionRefusal(t, "listener", listenErr)
	if !strings.Contains(listenErr.Error(), "did not prove") || len(reply) != 0 {
		t.Fatalf("listener = %v and replied %d bytes", listenErr, len(reply))
	}
}

func TestCrossVersionRejectsReflectedProof(t *testing.T) {
	// A dialer that returns the listener's own proof.
	listenErr, _ := exchange(t, asListener(testSecret(1)), func(c net.Conn) error {
		if _, err := c.Write(testSecret(7)); err != nil {
			return err
		}
		answer := make([]byte, nonceSize+proofSize)
		if _, err := io.ReadFull(c, answer); err != nil {
			return err
		}
		_, _ = c.Write(answer[nonceSize:])
		_, _ = io.ReadAll(c)
		return nil
	})
	wantCrossVersionRefusal(t, "listener", listenErr)

	// A listener that answers with a valid proof of the dialer's role, as a
	// peer would that reflected the dialer's proof.
	_, dialErr := exchange(t, func(c net.Conn) error {
		dialerNonce := make([]byte, nonceSize)
		if _, err := io.ReadFull(c, dialerNonce); err != nil {
			return err
		}
		listenerNonce := testSecret(9)
		answer := append(listenerNonce, crossVersionProof(testSecret(1), dialerRole, dialerNonce, listenerNonce)...)
		_, _ = c.Write(answer)
		_, _ = io.ReadAll(c)
		return nil
	}, asDialer(testSecret(1)))
	wantCrossVersionRefusal(t, "dialer", dialErr)
}

func TestCrossVersionDialerNeedsConfirmation(t *testing.T) {
	_, dialErr := exchange(t, func(c net.Conn) error {
		dialerNonce := make([]byte, nonceSize)
		if _, err := io.ReadFull(c, dialerNonce); err != nil {
			return err
		}
		listenerNonce := testSecret(9)
		_, _ = c.Write(append(listenerNonce, crossVersionProof(testSecret(1), listenerRole, dialerNonce, listenerNonce)...))
		proof := make([]byte, proofSize)
		_, _ = io.ReadFull(c, proof)
		return nil
	}, asDialer(testSecret(1)))
	wantCrossVersionRefusal(t, "dialer", dialErr)
	if !strings.Contains(dialErr.Error(), "refused this process's proof") {
		t.Fatalf("dialer = %v", dialErr)
	}
}

func TestCrossVersionSilentPeer(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	wantCrossVersionRefusal(t, "dialer", proveAsDialer(ctx, a, testSecret(1)))
	if time.Since(started) > 2*time.Second {
		t.Fatalf("a silent listener held the dialer for %v", time.Since(started))
	}

	c, d := net.Pipe()
	defer c.Close()
	defer d.Close()
	started = time.Now()
	wantCrossVersionRefusal(t, "listener", proveAsListener(c, testSecret(1)))
	if waited := time.Since(started); waited < crossVersionWait-100*time.Millisecond || waited > crossVersionWait+2*time.Second {
		t.Fatalf("a silent dialer held the listener for %v; want about %v", waited, crossVersionWait)
	}
}

func receiveAnswer(t *testing.T, c net.Conn, dialerNonce []byte) []byte {
	t.Helper()
	if _, err := c.Write(dialerNonce); err != nil {
		t.Error(err)
		return nil
	}
	answer := make([]byte, nonceSize+proofSize)
	if _, err := io.ReadFull(c, answer); err != nil {
		t.Error(err)
		return nil
	}
	return answer[:nonceSize]
}

type watchedConn struct {
	net.Conn
	writes *int
}

func (c *watchedConn) Write(b []byte) (int, error) {
	*c.writes++
	return c.Conn.Write(b)
}
