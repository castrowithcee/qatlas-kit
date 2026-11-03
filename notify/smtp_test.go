package notify

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"mime"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

// smtpServer is a minimal local SMTP server that records what it receives.
type smtpServer struct {
	port      int
	roots     *x509.CertPool
	startTLS  bool
	mechs     string
	wantUser  string
	wantPass  string
	mu        sync.Mutex
	authSeen  bool
	authTLS   bool
	plainAuth bool
	from      string
	rcpt      []string
	data      string
}

func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "demo"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// startSMTP serves one connection at a time. implicitTLS wraps every connection in TLS from the start;
// otherwise STARTTLS is offered when startTLS is true.
func startSMTP(t *testing.T, implicitTLS, startTLS bool, mechs string) *smtpServer {
	t.Helper()
	cert, pool := testCertificate(t)
	config := &tls.Config{Certificates: []tls.Certificate{cert}}
	var ln net.Listener
	var err error
	if implicitTLS {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", config)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s := &smtpServer{port: ln.Addr().(*net.TCPAddr).Port, roots: pool, startTLS: startTLS, mechs: mechs,
		wantUser: invented("mail-user"), wantPass: invented("mail-pass")}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.serve(conn, config, implicitTLS)
		}
	}()
	return s
}

func (s *smtpServer) serve(conn net.Conn, config *tls.Config, encrypted bool) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(conn, format+"\r\n", a...) }
	line := func() string {
		l, err := r.ReadString('\n')
		if err != nil {
			return ""
		}
		return strings.TrimRight(l, "\r\n")
	}
	say("220 demo ready")
	for {
		l := line()
		command := strings.ToUpper(l)
		switch {
		case l == "":
			return
		case strings.HasPrefix(command, "EHLO"):
			say("250-demo")
			if s.startTLS && !encrypted {
				say("250-STARTTLS")
			}
			say("250 AUTH %s", s.mechs)
		case command == "STARTTLS":
			say("220 go ahead")
			secured := tls.Server(conn, config)
			if err := secured.Handshake(); err != nil {
				return
			}
			conn, encrypted = secured, true
			r = bufio.NewReader(secured)
		case strings.HasPrefix(command, "AUTH PLAIN"):
			fields := strings.Fields(l)
			var raw []byte
			if len(fields) > 2 {
				raw, _ = base64.StdEncoding.DecodeString(fields[2])
			}
			parts := strings.Split(string(raw), "\x00")
			s.reply(say, encrypted, true, len(parts) == 3 && parts[1] == s.wantUser && parts[2] == s.wantPass)
		case command == "AUTH LOGIN":
			say("334 %s", base64.StdEncoding.EncodeToString([]byte("Username:")))
			user, _ := base64.StdEncoding.DecodeString(line())
			say("334 %s", base64.StdEncoding.EncodeToString([]byte("Password:")))
			pass, _ := base64.StdEncoding.DecodeString(line())
			s.reply(say, encrypted, false, string(user) == s.wantUser && string(pass) == s.wantPass)
		case strings.HasPrefix(command, "MAIL FROM:"):
			s.mu.Lock()
			s.from = l[len("MAIL FROM:"):]
			s.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(command, "RCPT TO:"):
			s.mu.Lock()
			s.rcpt = append(s.rcpt, l[len("RCPT TO:"):])
			s.mu.Unlock()
			say("250 ok")
		case command == "DATA":
			say("354 go")
			var b strings.Builder
			for d := line(); d != "."; d = line() {
				b.WriteString(d + "\n")
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			say("250 queued")
		case command == "QUIT":
			say("221 bye")
			return
		default:
			say("500 unknown")
		}
	}
}

func (s *smtpServer) reply(say func(string, ...any), encrypted, plain, ok bool) {
	s.mu.Lock()
	s.authSeen, s.authTLS, s.plainAuth = true, encrypted, plain
	s.mu.Unlock()
	if ok {
		say("235 welcome")
		return
	}
	say("535 wrong login for %s", s.wantPass)
}

func (s *smtpServer) channel(security SMTPSecurity) SMTP {
	return SMTP{Host: "127.0.0.1", Port: s.port, Security: security, User: invented("mail-user"),
		Password: invented("mail-pass"), From: "Demo Sender <sender@example.com>",
		To: []string{"ops@example.com", "Second Demo <second@example.com>"}}
}

func (s *smtpServer) sender(t *testing.T, attempts *int) *Sender {
	return newSender(t, Options{TLS: &tls.Config{RootCAs: s.roots}, OnFailure: func(int, *Error) {
		if attempts != nil {
			*attempts++
		}
	}})
}

func TestSMTPStartTLSLogsInAndSends(t *testing.T) {
	srv := startSMTP(t, false, true, "PLAIN LOGIN")
	msg := Message{Subject: "Grüße\r\nBcc: other@example.com", Text: "line one\nline two"}
	if err := srv.sender(t, nil).Send(context.Background(), srv.channel(StartTLS), msg); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !srv.authSeen || !srv.authTLS || !srv.plainAuth {
		t.Errorf("auth seen %v, encrypted %v, plain %v", srv.authSeen, srv.authTLS, srv.plainAuth)
	}
	if srv.from != "<sender@example.com>" || strings.Join(srv.rcpt, ",") != "<ops@example.com>,<second@example.com>" {
		t.Errorf("from %s, rcpt %v", srv.from, srv.rcpt)
	}
	var subject string
	for _, l := range strings.Split(srv.data, "\n") {
		if strings.HasPrefix(l, "Bcc:") {
			t.Errorf("a header was injected: %q", srv.data)
		}
		if value, ok := strings.CutPrefix(l, "Subject: "); ok {
			subject, _ = new(mime.WordDecoder).DecodeHeader(value)
		}
	}
	if subject != "Grüße Bcc: other@example.com" {
		t.Errorf("subject = %q", subject)
	}
	if !strings.Contains(srv.data, "line one\nline two") || !strings.Contains(srv.data, "Date: Fri, 02 Jan 2026 03:04:05 +0000") {
		t.Errorf("data = %q", srv.data)
	}
}

func TestSMTPImplicitTLSWithLoginMechanism(t *testing.T) {
	srv := startSMTP(t, true, false, "LOGIN")
	if err := srv.sender(t, nil).Send(context.Background(), srv.channel(ImplicitTLS), testMessage); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !srv.authSeen || !srv.authTLS || srv.plainAuth {
		t.Errorf("auth seen %v, encrypted %v, plain %v", srv.authSeen, srv.authTLS, srv.plainAuth)
	}
}

func TestSMTPWithoutSTARTTLSSendsNoLogin(t *testing.T) {
	srv := startSMTP(t, false, false, "PLAIN LOGIN")
	attempts := 0
	err := srv.sender(t, &attempts).Send(context.Background(), srv.channel(StartTLS), testMessage)
	if code := codeOfErr(t, err); code != output.CodeTLS || attempts != 1 {
		t.Errorf("code = %s, attempts = %d (%v)", code, attempts, err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.authSeen {
		t.Error("a login was sent over an unencrypted connection")
	}
}

func TestSMTPRefusesUntrustedCertificate(t *testing.T) {
	for _, implicit := range []bool{false, true} {
		srv := startSMTP(t, implicit, true, "PLAIN")
		security := StartTLS
		if implicit {
			security = ImplicitTLS
		}
		attempts := 0
		s := newSender(t, Options{OnFailure: func(int, *Error) { attempts++ }})
		err := s.Send(context.Background(), srv.channel(security), testMessage)
		if code := codeOfErr(t, err); code != output.CodeTLS || attempts != 1 {
			t.Errorf("implicit %v: code = %s, attempts = %d (%v)", implicit, code, attempts, err)
		}
		srv.mu.Lock()
		if srv.authSeen {
			t.Errorf("implicit %v: a login was sent", implicit)
		}
		srv.mu.Unlock()
	}
}

func TestSMTPWrongLoginIsNotRetriedAndNotShown(t *testing.T) {
	srv := startSMTP(t, false, true, "PLAIN")
	ch := srv.channel(StartTLS)
	ch.Password = invented("other-pass")
	srv.wantPass = ch.Password + "-changed"
	var log []string
	s := newSender(t, Options{TLS: &tls.Config{RootCAs: srv.roots}, OnFailure: func(attempt int, err *Error) {
		log = append(log, fmt.Sprint(attempt, err))
	}})
	err := s.Send(context.Background(), ch, testMessage)
	if code := codeOfErr(t, err); code != output.CodeAuth || len(log) != 1 {
		t.Fatalf("code = %s, log = %v", code, log)
	}
	if shown := err.Error() + strings.Join(log, ""); strings.Contains(shown, ch.Password) || !strings.Contains(shown, "535") {
		t.Errorf("shown = %q", shown)
	}
}

func TestSMTPCancelledContextEndsTheExchange(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		// The server never greets, so only the end of the context can end the exchange.
		if conn, err := ln.Accept(); err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	defer close(done)
	go func() {
		conn := <-accepted
		cancel()
		<-done
		_ = conn.Close()
	}()
	ch := SMTP{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, From: "sender@example.com", To: []string{"ops@example.com"}}
	start := time.Now()
	err = newSender(t, Options{}).Send(ctx, ch, testMessage)
	if !errors.Is(err, context.Canceled) || time.Since(start) > 3*time.Second {
		t.Errorf("err = %v after %s", err, time.Since(start))
	}
}

func TestSMTPRefusesInvalidSettings(t *testing.T) {
	valid := SMTP{Host: "127.0.0.1", Port: 1, From: "sender@example.com", To: []string{"ops@example.com"}}
	cases := map[string]func(*SMTP){
		"no host":           func(c *SMTP) { c.Host = "" },
		"host with space":   func(c *SMTP) { c.Host = "demo host" },
		"unknown security":  func(c *SMTP) { c.Security = 7 },
		"bad port":          func(c *SMTP) { c.Port = 70000 },
		"bad sender":        func(c *SMTP) { c.From = "not an address" },
		"no recipient":      func(c *SMTP) { c.To = nil },
		"bad recipient":     func(c *SMTP) { c.To = []string{"ops@example.com", "nobody"} },
		"header in address": func(c *SMTP) { c.From = "sender@example.com\r\nBcc: other@example.com" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			ch := valid
			change(&ch)
			if code := codeOfErr(t, newSender(t, Options{}).Send(context.Background(), ch, testMessage)); code != output.CodeConfigInvalid {
				t.Errorf("code = %s", code)
			}
		})
	}
}
