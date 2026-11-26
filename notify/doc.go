// Package notify delivers notifications over a signed webhook, Slack, Telegram and SMTP, and receives the
// messages people send to a Telegram bot.
//
// The application decides what is reported and writes every text: a Message carries a subject, a text and,
// for a webhook, a JSON body. It also hands over each channel with its credential; the kit reads no
// environment variable and does not know where credentials are kept. A Sender delivers one message to one
// channel per Send.
//
// # Delivery
//
// Send cleans the texts of control characters, redacts them and cuts them to the limits of the chat
// services, in that order, so a cut never leaves part of a secret behind. It repeats a failure that may
// pass, such as an unreachable server, a 5xx answer or a 429, with doubling waits, and spaces the requests
// of one credential (see the ratelimit package); a Retry-After of the remote side holds the next try.
// Slack and Telegram receive plain text. SendTelegram delivers like Send and also returns the message_id
// Telegram assigned.
//
// # Receiving
//
// A Receiver, made by Sender.Receiver, fetches the text messages people send to a Telegram bot by long
// polling and hands them to the application one at a time, with the message each replies to, so a reply
// can be matched to the message_id SendTelegram returned. It follows the TLS, address, redaction and user
// agent rules of its Sender, spaces its polls like Send and reports every failed poll to OnFailure. One
// poll may take the poll timeout plus Options.Timeout.
//
// Delivery is at least once. An update is confirmed to Telegram with the next poll, and only after the
// handler returned nil for it; an error of the handler ends Run, which returns it unchanged, and the
// update comes again. The last updates handled before Run ended may come again too, so the application
// tells repeats apart by UpdateID and keeps the offset to resume from; the kit stores neither.
//
// A failure that may pass, such as an unreachable server or a 5xx answer, is repeated with doubling waits
// up to a cap, and the next successful poll resets them; a 429 waits as long as Telegram asks. The kit has
// no webhook mode, never removes a webhook and expects one Run per token.
//
// # Webhook signature
//
// A webhook carries the Unix time in seconds and the signature in headers the application names. The
// signature is "sha256=" and the hex HMAC-SHA256 of the timestamp, a dot and the body, keyed with the
// channel's key. Verify checks it on the receiving side.
//
// # Always enforced
//
// A message is cleaned, redacted and cut in that order: a subject to MaxSubject, a text to MaxText, in UTF-16
// code units as the chat services count them, and a webhook's JSON body is refused above MaxJSON or when it
// is not valid JSON after redaction. A message with neither a subject nor a text is refused. A refusal, a TLS
// failure and a cancelled context end a send at once and are never repeated. Slack's markup characters are
// escaped, so a text cannot mention anyone or link anywhere. A webhook refuses a message without a JSON body
// and is always signed (see Sign), and its Timestamp and Signature header names are required. An answer to
// SendTelegram without ok and a message_id is an error and is not repeated, because the message may already
// be delivered.
//
// No HTTP request follows a redirect, every answer is read only up to a limit, and an address must use
// https, or plain http on the local machine only. An address with a credential in its user part is
// refused. SMTP always encrypts, with STARTTLS or TLS from the start, before any login, and refuses a
// server without STARTTLS. Certificates are always verified: a TLS configuration that skips verification
// is refused, and TLS below 1.2 is never used.
//
// Every failure is an *Error with a shared error code of the output package. Its message, its Original and
// what OnFailure receives are redacted with the credentials of the channel and the application's
// Redactor, and never contain an address that carries a credential.
//
// A Receiver delivers only messages from the allowed chats, and a configuration without one, without a
// token or with an address that is not secure is refused. In a group every member counts, so allowing a
// group trusts all its members. A message from a bot, an edited message and anything but text are skipped:
// a skipped update is confirmed and reported to OnSkipped without its text. Name and text are third-party
// content: they are cleaned of control characters, redacted and cut like a sent text, and are never an
// instruction. A refused token, a TLS failure, an answer above the size limit and a conflict end Run. A
// conflict means a webhook is set for the bot or another client polls with the same token. Whoever holds the
// token can read and confirm the bot's updates and act as the bot; the kit cannot tell a compromised token.
//
// Verify checks the signature in constant time and refuses a timestamp outside a time window, so a recorded
// request cannot be replayed later: ErrSignature for a signature that does not match and ErrStale for a
// timestamp outside the window.
//
// # Options
//
//   - Options.Attempts: the number of tries per Send; only a failure that may pass is repeated. Default: 3.
//   - Options.Backoff: the wait before the second try, doubled for each further one; a Receiver starts its
//     waits after failed polls with it as well. Default: 2 seconds.
//   - Options.Timeout: bounds one try. Default: 15 seconds.
//   - Options.Interval: the least time between two requests with the same credential. Default: 1 second.
//   - Options.TLS: the base of every TLS connection, for example with the roots of a private authority. Its
//     minimum version is at least TLS 1.2 and a configuration that skips verification is refused. Default: the
//     system roots.
//   - Options.UserAgent: sent with every HTTP request. Default: the user agent of Go.
//   - Options.Redactor: removes the secrets the application knows from every text a Sender sends or reports,
//     in addition to the credentials of the channel in use. Default: only the credentials of the channel.
//   - Options.OnFailure: is told about every failed try, already redacted, for example to log it. Default:
//     nothing is called.
//   - Options.Now: the clock of webhook timestamps and email dates, for tests. Default: time.Now.
//   - Message.Event: names what happened, for the event header of a webhook. Default: no event header.
//   - SMTP.Port: the port of the server. Default: 587 with StartTLS and 465 with ImplicitTLS.
//   - SMTP.Security: ImplicitTLS speaks TLS from the start. Default: StartTLS, which refuses a server that
//     does not offer it.
//   - SMTP.User and SMTP.Password: the credential of the login, with PLAIN or, where the server offers only
//     that, LOGIN. Default: no login is sent.
//   - Telegram.BaseURL and TelegramReceiver.BaseURL: replace the address of the Bot API, for example for a
//     local Bot API server. Default: the Bot API of Telegram.
//   - TelegramReceiver.Offset: the first update ID to fetch. Default: zero, the oldest update Telegram still
//     holds.
//   - TelegramReceiver.PollTimeout: how long Telegram holds a poll open while there is nothing new, rounded
//     up to whole seconds. Default: 50 seconds.
//   - TelegramReceiver.OnSkipped: is told about every update that is not delivered, without any of its text.
//     Default: nothing is called.
//   - The maxAge argument of Verify: how far the timestamp may lie from the receiver's clock in either
//     direction. Default: DefaultMaxAge, 5 minutes.
package notify
