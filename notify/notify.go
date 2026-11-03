package notify

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/castrowithcee/qatlas-kit/httpx"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Limits of one message, in UTF-16 code units as the chat services count them. MaxText also bounds a whole
// chat message and stays below the smallest limit of the chat services, so a message is cut here, after
// redaction, and never by the service.
const (
	MaxSubject = 200
	MaxText    = 4000
	MaxJSON    = 1 << 20
)

const (
	defaultAttempts = 3
	defaultBackoff  = 2 * time.Second
	defaultTimeout  = 15 * time.Second
	defaultInterval = time.Second
	// maxHold is the longest wait a remote side may ask for before a retry; a longer one ends the send.
	maxHold = time.Minute
	// maxAnswer bounds how much of a successful answer is read.
	maxAnswer = 64 << 10
)

// Message is what one notification says. The application writes every text; the kit only cleans,
// redacts and bounds it.
type Message struct {
	// Event names what happened, for the event header of a webhook. It is optional.
	Event string
	// Subject is the subject of an email and the first line of a chat message.
	Subject string
	// Text is the body of an email and the rest of a chat message.
	Text string
	// JSON is the body a webhook posts. A webhook refuses a message without it.
	JSON []byte
}

// Options configure a Sender. The zero value is ready to use.
type Options struct {
	// Attempts is the number of tries per Send; zero is three. Only a failure that may pass is repeated.
	Attempts int
	// Backoff is the wait before the second try, doubled for each further one; zero is two seconds.
	Backoff time.Duration
	// Timeout bounds one try; zero is 15 seconds.
	Timeout time.Duration
	// Interval is the least time between two requests with the same credential; zero is one second.
	Interval time.Duration
	// TLS is the base of every TLS connection, for example with the roots of a private authority. Its
	// server name is set per connection, its minimum version is at least TLS 1.2, and a configuration that
	// skips verification is refused.
	TLS *tls.Config
	// UserAgent is sent with every HTTP request; empty leaves the default of Go.
	UserAgent string
	// Redactor removes the secrets the application knows from every text a Sender sends or reports, in
	// addition to the credentials of the channel in use.
	Redactor *redact.Redactor
	// Now is the clock of webhook timestamps and email dates; nil is time.Now.
	Now func() time.Time
	// OnFailure is told about every failed try, already redacted, for example to log it.
	OnFailure func(attempt int, err *Error)
}

// Sender delivers messages over channels. It is safe for concurrent use.
type Sender struct {
	opts   Options
	tls    *tls.Config
	client *http.Client
	limits *ratelimit.Registry
}

// New returns a Sender. It refuses a TLS configuration that skips verification.
func New(opts Options) (*Sender, error) {
	if opts.Attempts <= 0 {
		opts.Attempts = defaultAttempts
	}
	if opts.Backoff <= 0 {
		opts.Backoff = defaultBackoff
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.Interval <= 0 {
		opts.Interval = defaultInterval
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Redactor == nil {
		opts.Redactor = &redact.Redactor{}
	}
	base := &tls.Config{}
	if opts.TLS != nil {
		if opts.TLS.InsecureSkipVerify {
			return nil, &Error{Code: output.CodeConfigInvalid, Message: "a TLS configuration that skips verification is refused"}
		}
		base = opts.TLS.Clone()
	}
	// The name of each connection's own host is the one to verify.
	base.ServerName = ""
	if base.MinVersion < tls.VersionTLS12 {
		base.MinVersion = tls.VersionTLS12
	}
	transport := &http.Transport{}
	if standard, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = standard.Clone()
	}
	transport.TLSClientConfig = base
	return &Sender{
		opts: opts, tls: base,
		client: httpx.NoRedirectClient(0, transport),
		limits: ratelimit.NewRegistry(opts.Interval),
	}, nil
}

// Channel is one way to deliver a message: Webhook, Slack, Telegram or SMTP.
type Channel interface {
	kind() string
	check() error
	secrets() []string
	limitKey() string
	deliver(ctx context.Context, s *Sender, m prepared) error
}

// Error is a failed send. Its message and Original are redacted and never carry a credential or an
// address that holds one.
type Error struct {
	Code    output.Code
	Message string
	// Original is what the remote side answered, redacted; text a third party wrote.
	Original *httpx.Original
	retry    bool
	hold     time.Duration
	cause    error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns only a context error, so errors.Is tells a cancelled send; no other cause is kept,
// because its text may carry an address or a credential.
func (e *Error) Unwrap() error { return e.cause }

// prepared is a message after cleaning and redaction.
type prepared struct {
	event, subject, text string
	json                 []byte
}

// Send delivers m over ch, repeating a failure that may pass with growing waits. It returns nil or an
// *Error.
func (s *Sender) Send(ctx context.Context, ch Channel, m Message) error {
	if ch == nil {
		return &Error{Code: output.CodeInvalidRequest, Message: "no channel was given"}
	}
	var local redact.Redactor
	local.Add(ch.secrets()...)
	redactText := func(text string) string { return local.Apply(s.opts.Redactor.Apply(text)) }
	if err := ch.check(); err != nil {
		return redacted(err, redactText)
	}
	p, err := prepare(m, redactText)
	if err != nil {
		return err
	}
	limiter := s.limits.For(ch.kind() + "\x00" + ch.limitKey())
	wait := s.opts.Backoff
	var last *Error
	for attempt := 1; attempt <= s.opts.Attempts; attempt++ {
		if attempt > 1 {
			if err := ratelimit.Sleep(ctx, wait); err != nil {
				return contextError(err)
			}
			wait *= 2
		}
		if err := limiter.Wait(ctx); err != nil {
			var beyond *ratelimit.BeyondDeadlineError
			if errors.As(err, &beyond) {
				return &Error{Code: output.CodeRateLimited, Message: "the next free slot of this channel is due after the send has to end"}
			}
			return contextError(err)
		}
		try, cancel := context.WithTimeout(ctx, s.opts.Timeout)
		err := ch.deliver(try, s, p)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return contextError(ctx.Err())
		}
		last = redacted(err, redactText)
		if s.opts.OnFailure != nil {
			s.opts.OnFailure(attempt, last)
		}
		if !last.retry || last.hold > maxHold {
			break
		}
		limiter.HoldFor(last.hold)
	}
	return last
}

// redacted turns any failure into an *Error whose texts have passed redactText.
func redacted(err error, redactText func(string) string) *Error {
	var failure *Error
	if !errors.As(err, &failure) {
		failure = &Error{Code: output.CodeRuntime, Message: err.Error(), retry: true}
	}
	out := *failure
	out.Message = redactText(failure.Message)
	if failure.Original != nil {
		original := *failure.Original
		original.Documentation = redactText(original.Documentation)
		original.Entries = make([]httpx.OriginalEntry, len(failure.Original.Entries))
		for i, entry := range failure.Original.Entries {
			original.Entries[i] = httpx.OriginalEntry{
				Type: redactText(entry.Type), Code: redactText(entry.Code),
				Path: redactText(entry.Path), Message: redactText(entry.Message),
			}
		}
		out.Original = &original
	}
	return &out
}

func contextError(err error) *Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: output.CodeTimeout, Message: "the send did not finish in time", cause: err}
	}
	return &Error{Code: output.CodeRuntime, Message: "the send was cancelled", cause: err}
}

func prepare(m Message, redactText func(string) string) (prepared, error) {
	// Redact before cutting, so a cut can never leave part of a secret behind.
	p := prepared{
		event:   clean(redactText(m.Event), MaxSubject, false),
		subject: clean(redactText(m.Subject), MaxSubject, false),
		text:    clean(redactText(m.Text), MaxText, true),
	}
	if p.subject == "" && p.text == "" {
		return prepared{}, &Error{Code: output.CodeInvalidRequest, Message: "the message has neither a subject nor a text"}
	}
	if m.JSON != nil {
		if len(m.JSON) > MaxJSON {
			return prepared{}, &Error{Code: output.CodeInvalidRequest, Message: "the JSON body of the message is too large"}
		}
		p.json = []byte(redactText(string(m.JSON)))
		// A secret that contains JSON syntax can break the document when it is replaced.
		if !json.Valid(p.json) {
			return prepared{}, &Error{Code: output.CodeInvalidRequest, Message: "the JSON body of the message is not valid JSON"}
		}
	}
	return p, nil
}

// clean replaces control characters, keeping line breaks and tabs only in multiline text, and cuts s to
// at most limit runes.
func clean(s string, limit int, multiline bool) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, "?"), "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if multiline && (r == '\n' || r == '\t') {
			return r
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	if !multiline {
		s = strings.Join(strings.Fields(s), " ")
	}
	return cut(strings.TrimSpace(s), limit)
}

// cut shortens s to at most limit UTF-16 code units, marking a cut with an ellipsis.
func cut(s string, limit int) string {
	units := 0
	for _, r := range s {
		units += utf16.RuneLen(r)
	}
	if units <= limit {
		return s
	}
	units = 0
	for i, r := range s {
		units += utf16.RuneLen(r)
		if units > limit-3 {
			return s[:i] + "..."
		}
	}
	return s
}

// chatText is the message as one plain text: the subject, then the text.
func (p prepared) chatText() string {
	switch {
	case p.subject == "":
		return p.text
	case p.text == "":
		return p.subject
	}
	return cut(p.subject+"\n"+p.text, MaxText)
}
