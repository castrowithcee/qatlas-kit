package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-kit/httpx"
	"github.com/castrowithcee/qatlas-kit/output"
)

// WebhookHeaders are the header names of a webhook request, chosen by the application.
type WebhookHeaders struct {
	// Event carries Message.Event; empty sends no event header.
	Event string
	// Timestamp carries the Unix time of the signature in decimal seconds. It is required.
	Timestamp string
	// Signature carries the signature (see Sign). It is required.
	Signature string
}

// Webhook posts the JSON of a message to an https address, signed with Key over the timestamp and the
// body (see Sign). The address may use plain http only on the local machine.
type Webhook struct {
	URL     string
	Key     string
	Headers WebhookHeaders
}

func (w Webhook) kind() string      { return "webhook" }
func (w Webhook) secrets() []string { return []string{w.Key} }
func (w Webhook) limitKey() string  { return w.URL }

func (w Webhook) check() error {
	if w.Key == "" {
		return configError("the webhook has no signing key")
	}
	for _, name := range []string{w.Headers.Timestamp, w.Headers.Signature} {
		if !validHeaderName(name) {
			return configError("the webhook needs valid names for its timestamp and signature headers")
		}
	}
	if w.Headers.Event != "" && !validHeaderName(w.Headers.Event) {
		return configError("the event header name of the webhook is not valid")
	}
	return requireSecure(w.URL, "webhook")
}

func (w Webhook) deliver(ctx context.Context, s *Sender, m prepared) error {
	if m.json == nil {
		return &Error{Code: output.CodeInvalidRequest, Message: "a webhook message needs a JSON body"}
	}
	timestamp := strconv.FormatInt(s.opts.Now().Unix(), 10)
	headers := map[string]string{
		w.Headers.Timestamp: timestamp,
		w.Headers.Signature: Sign([]byte(w.Key), timestamp, m.json),
	}
	if w.Headers.Event != "" && m.event != "" {
		headers[w.Headers.Event] = m.event
	}
	return s.post(ctx, w.URL, m.json, headers, "the webhook receiver", httpx.OriginalFields{})
}

// Slack posts the message as plain text to an incoming webhook. URL is the credential.
type Slack struct {
	URL string
}

func (c Slack) kind() string { return "slack" }

// secrets covers the parts of the address a remote side may echo on their own; the last path segment is
// the token of an incoming webhook.
func (c Slack) secrets() []string {
	values := []string{c.URL}
	if u, err := url.Parse(c.URL); err == nil && strings.Trim(u.Path, "/") != "" {
		path := strings.Trim(u.Path, "/")
		values = append(values, path, strings.Trim(u.EscapedPath(), "/"), path[strings.LastIndex(path, "/")+1:])
	}
	return values
}
func (c Slack) limitKey() string { return c.URL }
func (c Slack) check() error     { return requireSecure(c.URL, "Slack webhook") }

func (c Slack) deliver(ctx context.Context, s *Sender, m prepared) error {
	// Escaping keeps a third party's text from becoming a mention or a link.
	text := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(m.chatText())
	body, _ := json.Marshal(map[string]string{"text": text})
	return s.post(ctx, c.URL, body, nil, "Slack", httpx.OriginalFields{})
}

// telegramAPI is the address of the Bot API when Telegram.BaseURL is empty.
const telegramAPI = "https://api.telegram.org"

// Telegram sends the message as plain text to a chat through the Bot API. Token is the credential.
type Telegram struct {
	Token  string
	ChatID string
	// BaseURL replaces the address of the Bot API, for example for a local Bot API server.
	BaseURL string
}

func (c Telegram) kind() string { return "telegram" }

func (c Telegram) secrets() []string {
	return []string{c.Token, url.PathEscape(c.Token), url.QueryEscape(c.Token)}
}

func (c Telegram) limitKey() string { return c.Token + "\x00" + c.ChatID }

func (c Telegram) check() error {
	if c.Token == "" || c.ChatID == "" {
		return configError("the Telegram channel needs a token and a chat")
	}
	return requireSecure(c.base(), "Telegram API")
}

func (c Telegram) base() string {
	if c.BaseURL == "" {
		return telegramAPI
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c Telegram) deliver(ctx context.Context, s *Sender, m prepared) error {
	body, _ := json.Marshal(map[string]string{"chat_id": c.ChatID, "text": m.chatText()})
	target := c.base() + "/bot" + url.PathEscape(c.Token) + "/sendMessage"
	return s.post(ctx, target, body, nil, "Telegram", httpx.OriginalFields{Message: "description", Code: "error_code"})
}

func configError(message string) *Error {
	return &Error{Code: output.CodeConfigInvalid, Message: message}
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune(`"(),/:;<=>?@[\]{}`, r) {
			return false
		}
	}
	return true
}

// requireSecure refuses an address unless it uses https, or plain http on the local machine. The
// message never names the address, which may carry a credential.
func requireSecure(raw, subject string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return configError("the " + subject + " address is not a valid URL")
	}
	if u.Scheme == "https" || (u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil
	}
	return configError("the " + subject + " address must use https")
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// post sends one JSON request. No text of a transport error reaches the result unchecked: an *url.Error
// names the address, which may carry a credential.
func (s *Sender) post(ctx context.Context, target string, body []byte, headers map[string]string,
	subject string, fields httpx.OriginalFields) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return configError("the " + subject + " address is not a valid URL")
	}
	req.Header.Set("Content-Type", "application/json")
	if s.opts.UserAgent != "" {
		req.Header.Set("User-Agent", s.opts.UserAgent)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return transportError(err, subject)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswer+1))
		if err != nil {
			return transportError(err, subject)
		}
		if n > maxAnswer {
			return &Error{Code: output.CodeRuntime, Message: subject + " answered with more than the size limit"}
		}
		return nil
	}
	failure := httpx.ClassifyStatusBody("send", resp.StatusCode, httpx.StatusTexts{
		Subject:    subject,
		Auth:       subject + " refused the credential",
		Permission: subject + " does not allow this message",
		NotFound:   subject + " does not know the address or the chat",
	}, resp.Body, fields)
	out := &Error{Code: codeOf(failure.Class), Message: failure.Message, Original: failure.Original}
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		out.retry, out.hold = true, httpx.RetryAfter(resp.Header)
	case http.StatusRequestTimeout:
		out.retry = true
	default:
		out.retry = resp.StatusCode >= 500
	}
	return out
}

func codeOf(class httpx.Class) output.Code {
	switch class {
	case httpx.ClassAuth:
		return output.CodeAuth
	case httpx.ClassPermission:
		return output.CodePermission
	case httpx.ClassNotFound:
		return output.CodeNotFound
	case httpx.ClassRateLimited:
		return output.CodeRateLimited
	case httpx.ClassUnreachable:
		return output.CodeUnreachable
	case httpx.ClassTimeout:
		return output.CodeTimeout
	}
	return output.CodeRuntime
}

// transportError classifies a failure below HTTP. A TLS failure is not repeated: it does not pass by
// itself and repeating it only tells an attacker more.
func transportError(err error, subject string) *Error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if tlsFailure(err) {
		return &Error{Code: output.CodeTLS, Message: "the TLS connection to " + subject + " failed: " + err.Error()}
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: output.CodeTimeout, Message: subject + " did not answer in time", retry: true}
	}
	return &Error{Code: output.CodeUnreachable, Message: subject + " could not be reached: " + err.Error(), retry: true}
}

func tlsFailure(err error) bool {
	var (
		verification *tls.CertificateVerificationError
		authority    x509.UnknownAuthorityError
		hostname     x509.HostnameError
		invalid      x509.CertificateInvalidError
		record       tls.RecordHeaderError
		alert        tls.AlertError
	)
	return errors.As(err, &verification) || errors.As(err, &authority) || errors.As(err, &hostname) ||
		errors.As(err, &invalid) || errors.As(err, &record) || errors.As(err, &alert)
}
