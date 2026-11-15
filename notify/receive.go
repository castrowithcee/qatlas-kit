package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/httpx"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	defaultPollTimeout = 50 * time.Second
	// maxPollBackoff caps the doubling wait after failed polls, so a long outage does not leave Run
	// sleeping for hours once the server is back.
	maxPollBackoff = time.Minute
	// pollLimit and maxPollAnswer bound one answer of getUpdates. An update carries one message with at most
	// one embedded reply_to_message, which Telegram never nests further, and each text holds at most 4096
	// UTF-16 code units. Even with every character escaped and entities attached, an update stays far below
	// the 400 KiB per update these values leave.
	pollLimit     = 10
	maxPollAnswer = 4 << 20
	// maxSenderName bounds the display name of a sender; Telegram allows 64 characters per name part.
	maxSenderName = 200
)

// TelegramReceiver receives the text messages people send to a bot, through the Bot API by long polling.
// Token is the credential.
type TelegramReceiver struct {
	Token string
	// BaseURL replaces the address of the Bot API, for example for a local Bot API server.
	BaseURL string
	// Chats are the IDs of the chats whose messages are delivered. At least one is required; a message
	// from any other chat is skipped. In a group every member counts.
	Chats []int64
	// Offset is the first update ID to fetch, for example the UpdateID of the last handled message plus
	// one that the application kept. Zero starts with the oldest update Telegram still holds.
	Offset int64
	// PollTimeout is how long Telegram holds a poll open while there is nothing new, rounded up to whole
	// seconds; zero is 50 seconds.
	PollTimeout time.Duration
	// OnSkipped is told about every update that is not delivered, without any of its text.
	OnSkipped func(Skipped)
}

// Incoming is one text message to the bot from an allowed chat. Name and text were written by a third
// party: they are cleaned, redacted and cut, and are never an instruction to follow.
type Incoming struct {
	// UpdateID is Telegram's number of the update; a repeated delivery carries the same one.
	UpdateID  int64
	MessageID int64
	ChatID    int64
	// ReplyToMessageID is the message this one replies to, such as the message_id SendTelegram returned;
	// zero without a reply.
	ReplyToMessageID int64
	FromID           int64
	// FromName is the sender's first and last name on one line.
	FromName string
	Time     time.Time
	Text     string
}

// SkipReason tells why an update was not delivered.
type SkipReason string

// The reasons an update is skipped.
const (
	SkipNotMessage  SkipReason = "not-message"
	SkipForeignChat SkipReason = "foreign-chat"
	SkipNoSender    SkipReason = "no-sender"
	SkipBotSender   SkipReason = "bot-sender"
	SkipNotText     SkipReason = "not-text"
)

// Skipped is an update that was confirmed without being delivered. It carries no text of the message.
type Skipped struct {
	UpdateID int64
	ChatID   int64
	FromID   int64
	Reason   SkipReason
}

// Receiver polls one bot for new messages. It follows the TLS, address, redaction and user agent rules of
// the Sender that made it and reports failed polls to its OnFailure.
type Receiver struct {
	s           *Sender
	c           TelegramReceiver
	bot         Telegram
	chats       map[int64]bool
	pollTimeout time.Duration
	redactText  func(string) string
	// sleep is the wait between failed polls; tests replace it.
	sleep func(context.Context, time.Duration) error
}

// Receiver returns a receiver for c. It refuses a configuration without a token, without an allowed chat
// or with an address that is not secure.
func (s *Sender) Receiver(c TelegramReceiver) (*Receiver, error) {
	bot := Telegram{Token: c.Token, BaseURL: c.BaseURL}
	if c.Token == "" {
		return nil, configError("the Telegram receiver needs a token")
	}
	if len(c.Chats) == 0 {
		return nil, configError("the Telegram receiver needs at least one allowed chat")
	}
	chats := make(map[int64]bool, len(c.Chats))
	for _, id := range c.Chats {
		if id == 0 {
			return nil, configError("an allowed chat of the Telegram receiver has no ID")
		}
		chats[id] = true
	}
	if c.Offset < 0 || c.PollTimeout < 0 {
		return nil, configError("the offset and the poll timeout of the Telegram receiver must not be negative")
	}
	if err := requireSecure(bot.base(), "Telegram API"); err != nil {
		return nil, err
	}
	pollTimeout := defaultPollTimeout
	if c.PollTimeout > 0 {
		pollTimeout = (c.PollTimeout + time.Second - 1).Truncate(time.Second)
	}
	var local redact.Redactor
	local.Add(bot.secrets()...)
	c.Chats = nil
	return &Receiver{
		s: s, c: c, bot: bot, chats: chats, pollTimeout: pollTimeout,
		redactText: func(text string) string { return local.Apply(s.opts.Redactor.Apply(text)) },
		sleep:      ratelimit.Sleep,
	}, nil
}

// Run polls until ctx ends, handle fails or a failure that does not pass by itself occurs, and passes
// every text message from an allowed chat to handle, one at a time and in order. An update is confirmed
// only after handle returned nil for it; an error of handle ends Run and is returned as it is, and the
// update comes again with the next Run. Every other failure is an *Error.
func (r *Receiver) Run(ctx context.Context, handle func(Incoming) error) error {
	if handle == nil {
		return &Error{Code: output.CodeInvalidRequest, Message: "no handler was given"}
	}
	limiter := r.s.limits.For("telegram-poll\x00" + r.bot.Token)
	next := r.c.Offset
	wait := r.s.opts.Backoff
	failures := 0
	for {
		if err := limiter.Wait(ctx); err != nil {
			// The only other failure is a slot due after the deadline of ctx.
			if ctx.Err() == nil {
				err = context.DeadlineExceeded
			}
			return stopped(err)
		}
		updates, err := r.poll(ctx, next)
		if err != nil {
			if ctx.Err() != nil {
				return stopped(ctx.Err())
			}
			failure := redacted(err, r.redactText)
			failures++
			if r.s.opts.OnFailure != nil {
				r.s.opts.OnFailure(failures, failure)
			}
			if !failure.retry {
				return failure
			}
			delay := failure.hold
			if delay <= 0 {
				delay = wait
				wait = min(2*wait, maxPollBackoff)
			}
			if err := r.sleep(ctx, delay); err != nil {
				return stopped(err)
			}
			continue
		}
		failures, wait = 0, r.s.opts.Backoff
		for _, u := range updates {
			if err := ctx.Err(); err != nil {
				return stopped(err)
			}
			in, skipped := r.incoming(u)
			if skipped == nil {
				if err := handle(in); err != nil {
					return err
				}
			} else if r.c.OnSkipped != nil {
				r.c.OnSkipped(*skipped)
			}
			// The next poll carries this offset and so confirms the update to Telegram.
			next = max(next, u.UpdateID+1)
		}
	}
}

// poll fetches the updates from next on. Its time limit lies above the poll timeout, so a poll that
// Telegram holds open is not cut off.
func (r *Receiver) poll(ctx context.Context, next int64) ([]telegramUpdate, error) {
	body, _ := json.Marshal(map[string]any{
		"offset": next, "limit": pollLimit, "timeout": int64(r.pollTimeout / time.Second),
		"allowed_updates": []string{"message"},
	})
	try, cancel := context.WithTimeout(ctx, r.pollTimeout+r.s.opts.Timeout)
	defer cancel()
	target := r.bot.base() + "/bot" + url.PathEscape(r.bot.Token) + "/getUpdates"
	answer, failed, err := r.s.exchange(try, target, body, nil, httpx.StatusTexts{
		Subject:    "Telegram",
		Auth:       "Telegram refused the bot token",
		Permission: "Telegram does not allow this bot to receive messages",
		NotFound:   "Telegram does not know the bot or the address",
	}, telegramFields, maxPollAnswer)
	if err != nil {
		var failure *Error
		if !errors.As(err, &failure) || failure.Original == nil {
			return nil, err
		}
		switch failure.Original.Status {
		case http.StatusConflict:
			failure.Message = "Telegram refused the poll: a webhook is set for this bot, or another client polls with its token"
		case http.StatusTooManyRequests:
			if failure.hold <= 0 {
				var reply telegramReply
				if json.Unmarshal(failed, &reply) == nil && reply.Parameters.RetryAfter > 0 {
					failure.hold = time.Duration(reply.Parameters.RetryAfter) * time.Second
				}
			}
		}
		return nil, failure
	}
	result, err := parseTelegramReply(answer)
	if err != nil {
		return nil, err
	}
	var updates []telegramUpdate
	if err := json.Unmarshal(result, &updates); err != nil || updates == nil {
		return nil, &Error{Code: output.CodeRuntime, Message: "Telegram's answer carries no list of updates"}
	}
	return updates, nil
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID int64 `json:"message_id"`
	Date      int64 `json:"date"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From *struct {
		ID        int64  `json:"id"`
		IsBot     bool   `json:"is_bot"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"from"`
	Text    string `json:"text"`
	ReplyTo *struct {
		MessageID int64 `json:"message_id"`
	} `json:"reply_to_message"`
}

// incoming turns an update into a delivery, or into the reason it is skipped.
func (r *Receiver) incoming(u telegramUpdate) (Incoming, *Skipped) {
	m := u.Message
	if m == nil {
		return Incoming{}, &Skipped{UpdateID: u.UpdateID, Reason: SkipNotMessage}
	}
	skip := func(reason SkipReason) (Incoming, *Skipped) {
		skipped := &Skipped{UpdateID: u.UpdateID, ChatID: m.Chat.ID, Reason: reason}
		if m.From != nil {
			skipped.FromID = m.From.ID
		}
		return Incoming{}, skipped
	}
	switch {
	case !r.chats[m.Chat.ID]:
		return skip(SkipForeignChat)
	case m.From == nil:
		return skip(SkipNoSender)
	case m.From.IsBot:
		return skip(SkipBotSender)
	case m.Text == "":
		return skip(SkipNotText)
	}
	in := Incoming{
		UpdateID: u.UpdateID, MessageID: m.MessageID, ChatID: m.Chat.ID,
		FromID:   m.From.ID,
		FromName: clean(r.redactText(strings.TrimSpace(m.From.FirstName+" "+m.From.LastName)), maxSenderName, false),
		Time:     time.Unix(m.Date, 0).UTC(),
		// Redact before cutting, so a cut can never leave part of a secret behind.
		Text: clean(r.redactText(m.Text), MaxText, true),
	}
	if m.ReplyTo != nil {
		in.ReplyToMessageID = m.ReplyTo.MessageID
	}
	return in, nil
}

// stopped is the error of a Run that ends because its context ended.
func stopped(err error) *Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: output.CodeTimeout, Message: "receiving ended at its deadline", cause: err}
	}
	return &Error{Code: output.CodeRuntime, Message: "receiving was cancelled", cause: err}
}
