// Package notify delivers notifications over a signed webhook, Slack, Telegram and SMTP.
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
