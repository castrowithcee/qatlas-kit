// Package ratelimit spaces and counts the requests that share one credential.
//
// A remote API usually counts its limit per key, across every endpoint that key can reach, so the budget
// belongs to the key and not to a single client or operation. A Registry keeps one Limiter per key for
// its own lifetime and stores only a digest of each key; a Limiter reserves the next slot and blocks until
// it is due. The budget is per key and per Registry: processes that share a key do not share a budget.
//
// A Counter counts units in a window, either sliding or daily in a time zone, and rejects a reservation
// over the limit before the request is sent. A CounterRegistry keeps one Counter per key. The count lives
// in the process and covers only its own reservations. The server stays authoritative: a server refusal
// is never overridden, and the counter is only a check beforehand. Release the units of a reservation
// only while the request provably was not sent.
package ratelimit
