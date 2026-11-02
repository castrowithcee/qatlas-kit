// Package ratelimit spaces the requests that share one credential.
//
// A remote API usually counts its limit per key, across every endpoint that key can reach, so the spacing
// belongs to the key and not to a single client or operation. A Registry keeps one Limiter per key for
// its own lifetime and stores only a digest of each key; a Limiter reserves the next slot and blocks until
// it is due. The budget is per key and per Registry: processes that share a key do not share a budget.
package ratelimit
