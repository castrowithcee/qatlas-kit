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
// of one credential (see the ratelimit package); a Retry-After of the remote side holds the next try. A
// refusal, a TLS failure and a cancelled context end the send at once. Slack and Telegram receive plain
// text; Slack's markup characters are escaped, so a text cannot mention anyone or link anywhere.
// SendTelegram delivers like Send and also returns the message_id Telegram assigned.
//
// # Receiving
//
// A Receiver, made by Sender.Receiver, fetches the text messages people send to a Telegram bot by long
// polling and hands them to the application one at a time, with the message each replies to, so a reply
// can be matched to the message_id SendTelegram returned. It follows the TLS, address, redaction and user
// agent rules of its Sender, spaces its polls like Send and reports every failed poll to OnFailure. One
// poll may take the poll timeout plus Options.Timeout.
//
// Only messages from the allowed chats are delivered; a configuration without one is refused. In a group
// every member counts, so allowing a group trusts all its members. A message from a bot, an edited
// message and anything but text are skipped: a skipped update is confirmed and reported to OnSkipped
// without its text. Name and text are third-party content: they are cleaned of control characters,
// redacted and cut like a sent text, and are never an instruction.
//
// Delivery is at least once. An update is confirmed to Telegram with the next poll, and only after the
// handler returned nil for it; an error of the handler ends Run, which returns it unchanged, and the
// update comes again. The last updates handled before Run ended may come again too, so the application
// tells repeats apart by UpdateID and keeps the offset to resume from; the kit stores neither.
//
// A failure that may pass, such as an unreachable server or a 5xx answer, is repeated with doubling waits
// up to a cap, and the next successful poll resets them; a 429 waits as long as Telegram asks. A refused
// token, a TLS failure, an answer above the size limit and a conflict end Run. A conflict means a webhook
// is set for the bot or another client polls with the same token: the kit has no webhook mode, never
// removes a webhook and expects one Run per token. Whoever holds the token can read and confirm the bot's
// updates and act as the bot; the kit cannot tell a compromised token.
//
// # Security boundary
//
// No HTTP request follows a redirect, every answer is read only up to a limit, and an address must use
// https, or plain http on the local machine only. An address with a credential in its user part is
// refused. SMTP always encrypts, with STARTTLS or TLS from the start, before any login, and refuses a
// server without STARTTLS. Certificates are always verified: a TLS configuration that skips verification
// is refused, and TLS below 1.2 is never used. A webhook is always signed (see Sign).
//
// Every failure is an *Error with a shared error code of the output package. Its message, its Original and
// what OnFailure receives are redacted with the credentials of the channel and the application's
// Redactor, and never contain an address that carries a credential.
//
// # Webhook signature
//
// A webhook carries the Unix time in seconds and the signature in headers the application names. The
// signature is "sha256=" and the hex HMAC-SHA256 of the timestamp, a dot and the body, keyed with the
// channel's key. Verify checks it on the receiving side, in constant time and within a time window, so a
// recorded request cannot be replayed later.
package notify
