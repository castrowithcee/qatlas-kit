// Package ratelimit spaces and counts the requests that share one credential.
//
// A remote API usually counts its limit per key, across every endpoint that key can reach, so the budget
// belongs to the key and not to a single client or operation. A Registry keeps one Limiter per key for
// its own lifetime; a Limiter reserves the next slot and blocks until it is due.
//
// A Counter counts units in a window, either sliding or daily in a time zone, and rejects a reservation
// over the limit before the request is sent. A CounterRegistry keeps one Counter per key. Release the units
// of a reservation only while the request provably was not sent.
//
// # Always enforced
//
// A Registry and a CounterRegistry store only a digest of each key, never the key itself, and no error
// contains it. The budget is per key and per Registry: processes that share a key do not share a budget.
//
// A Limiter keeps at least its interval between two reserved slots. Wait returns the context error when the
// request ends while it waits, so a cancelled request is never sent, and a *BeyondDeadlineError, without
// reserving anything, when the slot is due only after the request's deadline.
//
// A Counter admits a reservation all at once or not at all and changes nothing when it rejects one:
// *InvalidUnitsError for fewer than one unit, *ExceedsLimitError for more units than the limit allows at
// all and *ExhaustedError with the wait when the units do not fit now. The count lives in the process and
// covers only its own reservations. The server stays authoritative: a server refusal is never overridden,
// and the counter is only a check beforehand. A clock that runs backwards is held at the latest time seen,
// so no wait turns negative. NewCounter and NewCounterRegistry panic for a limit below 1, a window not
// built by Sliding or Daily, a window without a positive length or a time zone, and, for NewCounter, a nil
// clock.
//
// # Options
//
// The package has no options.
package ratelimit
