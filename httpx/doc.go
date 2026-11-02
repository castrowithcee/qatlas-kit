// Package httpx holds the HTTP mechanics a client of a remote API needs regardless of the API: a client
// that never follows a redirect, the Retry-After wait, cursors bound to the request that issued them, the
// mapping of a failed status to a neutral Error, bounded reading of JSON answers, and the bounded,
// selective reading of an error answer into an Original kept apart from the message.
//
// The caller names the remote side and supplies the messages that differ between APIs. An Error never
// carries the answer body, a header or a credential in its message; the answer itself is untrusted text
// that a caller redacts and bounds before it shows it.
package httpx
