package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

// SMTPSecurity is how an SMTP connection is encrypted. Every way encrypts before the login; there is no
// unencrypted one.
type SMTPSecurity int

const (
	// StartTLS upgrades the connection with STARTTLS and refuses a server that does not offer it.
	StartTLS SMTPSecurity = iota
	// ImplicitTLS speaks TLS from the start.
	ImplicitTLS
)

// SMTP sends the message as a plain text email. User and Password are the credential; without a user no
// login is sent. The login uses PLAIN or, where the server offers only that, LOGIN.
type SMTP struct {
	Host string
	// Port is zero for 587 with StartTLS and 465 with ImplicitTLS.
	Port     int
	Security SMTPSecurity
	User     string
	Password string
	From     string
	To       []string
}

func (c SMTP) kind() string      { return "smtp" }
func (c SMTP) secrets() []string { return []string{c.Password, c.User} }
func (c SMTP) limitKey() string  { return c.Host + "\x00" + c.User }

func (c SMTP) check() error {
	if c.Host == "" || strings.ContainsAny(c.Host, " /\r\n") {
		return configError("the SMTP host is not valid")
	}
	if c.Security != StartTLS && c.Security != ImplicitTLS {
		return configError("the SMTP security is unknown")
	}
	if c.Port < 0 || c.Port > 65535 {
		return configError("the SMTP port is not valid")
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return configError("the sender address is not valid")
	}
	if len(c.To) == 0 {
		return configError("the email has no recipient")
	}
	for _, to := range c.To {
		if _, err := mail.ParseAddress(to); err != nil {
			return configError("a recipient address is not valid")
		}
	}
	return nil
}

func (c SMTP) deliver(ctx context.Context, s *Sender, m prepared) error {
	return smtpError(c.send(ctx, s, m))
}

func (c SMTP) send(ctx context.Context, s *Sender, m prepared) error {
	from, _ := mail.ParseAddress(c.From)
	to := make([]*mail.Address, len(c.To))
	for i, raw := range c.To {
		to[i], _ = mail.ParseAddress(raw)
	}
	port := c.Port
	if port == 0 {
		port = 587
		if c.Security == ImplicitTLS {
			port = 465
		}
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	// Closing the connection is the only way to end a running SMTP exchange when the context ends.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	config := s.tls.Clone()
	config.ServerName = c.Host
	if c.Security == ImplicitTLS {
		secured := tls.Client(conn, config)
		if err := secured.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return err
		}
		conn = secured
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = client.Close() }()
	if err := client.Hello("localhost"); err != nil {
		return err
	}
	if c.Security == StartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return &Error{Code: output.CodeTLS, Message: "the SMTP server does not offer STARTTLS"}
		}
		if err := client.StartTLS(config); err != nil {
			return err
		}
	}
	if c.User != "" {
		auth, err := c.auth(client)
		if err != nil {
			return err
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt.Address); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(emailOf(from, to, m, s.opts.Now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// auth prefers PLAIN and falls back to LOGIN. Both run only on the encrypted connection send sets up.
func (c SMTP) auth(client *smtp.Client) (smtp.Auth, error) {
	_, params := client.Extension("AUTH")
	mechanisms := strings.Fields(strings.ToUpper(params))
	for _, mechanism := range mechanisms {
		if mechanism == "PLAIN" {
			return smtp.PlainAuth("", c.User, c.Password, c.Host), nil
		}
	}
	for _, mechanism := range mechanisms {
		if mechanism == "LOGIN" {
			return &loginAuth{user: c.User, password: c.Password}, nil
		}
	}
	return nil, &Error{Code: output.CodeAuth, Message: "the SMTP server offers no supported login (PLAIN, LOGIN)"}
}

type loginAuth struct{ user, password string }

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, &Error{Code: output.CodeTLS, Message: "a login over an unencrypted connection is refused"}
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(prompt []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(prompt))) {
	case "username:":
		return []byte(a.user), nil
	case "password:":
		return []byte(a.password), nil
	}
	return nil, &Error{Code: output.CodeAuth, Message: "the SMTP server sent an unexpected login prompt"}
}

// smtpError classifies a failure of the SMTP exchange. A permanent refusal (5xx) and a TLS failure are not
// repeated.
func smtpError(err error) error {
	if err == nil {
		return nil
	}
	var failure *Error
	if errors.As(err, &failure) {
		return failure
	}
	var reply *textproto.Error
	if errors.As(err, &reply) {
		out := &Error{Code: output.CodeRuntime, Message: "the SMTP server refused the email: " + reply.Error(),
			retry: reply.Code < 500}
		if reply.Code == 535 || reply.Code == 534 || reply.Code == 530 {
			out.Code = output.CodeAuth
		}
		return out
	}
	return transportError(err, "the SMTP server")
}

func emailOf(from *mail.Address, to []*mail.Address, m prepared, now time.Time) []byte {
	var b bytes.Buffer
	header := func(name, value string) { b.WriteString(name + ": " + value + "\r\n") }
	recipients := make([]string, len(to))
	for i, address := range to {
		recipients[i] = address.String()
	}
	header("From", from.String())
	header("To", strings.Join(recipients, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", m.subject))
	header("Date", now.Format(time.RFC1123Z))
	header("MIME-Version", "1.0")
	header("Content-Type", `text/plain; charset="utf-8"`)
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	body := quotedprintable.NewWriter(&b)
	_, _ = body.Write([]byte(strings.ReplaceAll(m.text, "\n", "\r\n")))
	_ = body.Close()
	return b.Bytes()
}
