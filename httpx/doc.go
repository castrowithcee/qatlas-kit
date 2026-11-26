// Package httpx holds the HTTP mechanics a client of a remote API needs regardless of the API: a client
// that never follows a redirect, the Retry-After wait, cursors bound to the request that issued them, the
// mapping of a failed status to a neutral Error, bounded reading of JSON answers, and the bounded,
// selective reading of an error answer into an Original kept apart from the message.
//
// The caller names the remote side and supplies the messages that differ between APIs.
//
// # Always enforced
//
// NoRedirectClient never follows a redirect, so a credential in a request can only reach the origin the
// caller configured, and bounds every request by the timeout it is given. RetryAfter reads only a positive
// whole number of seconds and does not cap the wait; a caller that bounds it does so itself.
//
// A cursor made by EncodeCursor carries a checksum over its binding and the remote part. DecodeCursor
// refuses a cursor that is malformed, altered, cut short, longer than the limit the caller gives or issued
// for another binding.
//
// An Error never carries the answer body, a header or a credential in its message: ClassifyStatus and
// ClassifyStatusBody build the message from the status and the caller's texts only. ReadJSON reads at most
// the limit it is given and turns anything unreadable or invalid into a ClassInvalidResponse error.
//
// The answer itself is untrusted text that a caller redacts and bounds before it shows it. ReadErrorBody
// reads at most ErrorBodyLimit bytes of it. ErrorOriginal keeps from a JSON answer only what OriginalFields
// names and keeps nothing of one that is not an object or names nothing, because unnamed content may be
// nested objects or an echo of the request.
//
// # Options
//
// The package has no options.
package httpx
