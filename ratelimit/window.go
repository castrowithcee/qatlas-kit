package ratelimit

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// Window says how long a reserved unit counts against the limit of a Counter. Build one with Sliding or
// Daily. The zero value is invalid and makes NewCounter panic.
type Window struct {
	length time.Duration
	zone   *time.Location
}

// Sliding returns a window in which a unit counts for d from the moment it is reserved. d must be positive.
func Sliding(d time.Duration) Window { return Window{length: d} }

// Daily returns a window in which a unit counts until the next midnight in zone, so a day is 23 or 25
// hours long when the zone changes its clock. zone must not be nil.
func Daily(zone *time.Location) Window { return Window{zone: zone} }

func (w Window) valid() bool {
	return (w.length > 0) != (w.zone != nil)
}

// ExhaustedError reports that the units do not fit now. Wait is how long it takes until they fit,
// never negative. Nothing was reserved.
type ExhaustedError struct{ Wait time.Duration }

func (e *ExhaustedError) Error() string {
	return fmt.Sprintf("the request limit is used up, the units fit again in %s", e.Wait)
}

// ExceedsLimitError reports a request for more units than the limit allows at all, so waiting never helps.
type ExceedsLimitError struct{ Requested, Limit int }

func (e *ExceedsLimitError) Error() string {
	return fmt.Sprintf("%d units exceed the limit of %d and never fit", e.Requested, e.Limit)
}

// InvalidUnitsError reports a request for fewer than one unit.
type InvalidUnitsError struct{ Units int }

func (e *InvalidUnitsError) Error() string {
	return fmt.Sprintf("a request needs at least one unit, got %d", e.Units)
}

// Counter counts units in a window and rejects a reservation that would exceed the limit. It lives in
// one process and only counts its own reservations: the server stays authoritative, the counter is a
// check before sending.
type Counter struct {
	mu     sync.Mutex
	limit  int
	window Window
	now    func() time.Time

	// last is the latest time seen. A clock that runs backwards is held at it, so the order of the
	// reservations never changes and no wait turns negative.
	last   time.Time
	nextID uint64

	// Sliding window: one entry per reservation in order of time, so at most one entry per unit.
	log  []entry
	used int

	// Daily window: constant state, the day ends at dayEnd.
	dayEnd time.Time
}

type entry struct {
	id uint64
	at time.Time
	n  int
}

// NewCounter returns a counter that admits at most limit units per window. now is the test seam,
// production passes time.Now. It panics when limit is below 1, the window is not built by Sliding or
// Daily, its length is not positive or its zone is nil.
func NewCounter(limit int, window Window, now func() time.Time) *Counter {
	if limit < 1 {
		panic("ratelimit: the limit must be at least 1")
	}
	if !window.valid() {
		panic("ratelimit: the window needs a positive length or a time zone")
	}
	if now == nil {
		panic("ratelimit: the clock must not be nil")
	}
	return &Counter{limit: limit, window: window, now: now}
}

// Reservation is the claim on units of a Counter.
type Reservation struct {
	counter *Counter
	id      uint64
	n       int
	dayEnd  time.Time
	done    bool
}

// Reserve claims n units all at once or not at all. It returns *InvalidUnitsError for n below 1,
// *ExceedsLimitError for n above the limit and *ExhaustedError with the wait when the units do not fit
// now. A rejection changes nothing and the errors never contain the key.
func (c *Counter) Reserve(n int) (*Reservation, error) {
	if n < 1 {
		return nil, &InvalidUnitsError{Units: n}
	}
	if n > c.limit {
		return nil, &ExceedsLimitError{Requested: n, Limit: c.limit}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.tick()
	if c.window.zone != nil {
		return c.reserveDaily(now, n)
	}
	return c.reserveSliding(now, n)
}

// tick returns the current time, never earlier than a time returned before.
func (c *Counter) tick() time.Time {
	now := c.now()
	if now.Before(c.last) {
		return c.last
	}
	c.last = now
	return now
}

func (c *Counter) reserveSliding(now time.Time, n int) (*Reservation, error) {
	c.expire(now)
	if c.used+n > c.limit {
		free := c.limit - c.used
		for _, e := range c.log {
			free += e.n
			if free >= n {
				return nil, &ExhaustedError{Wait: nonNegative(e.at.Add(c.window.length).Sub(now))}
			}
		}
	}
	c.nextID++
	c.log = append(c.log, entry{id: c.nextID, at: now, n: n})
	c.used += n
	return &Reservation{counter: c, id: c.nextID, n: n}, nil
}

// expire drops the entries whose window has passed.
func (c *Counter) expire(now time.Time) {
	drop := 0
	for drop < len(c.log) && !c.log[drop].at.Add(c.window.length).After(now) {
		c.used -= c.log[drop].n
		drop++
	}
	if drop > 0 {
		c.log = append(c.log[:0], c.log[drop:]...)
	}
}

func (c *Counter) reserveDaily(now time.Time, n int) (*Reservation, error) {
	c.rollDay(now)
	if c.used+n > c.limit {
		return nil, &ExhaustedError{Wait: nonNegative(c.dayEnd.Sub(now))}
	}
	c.nextID++
	c.used += n
	return &Reservation{counter: c, id: c.nextID, n: n, dayEnd: c.dayEnd}, nil
}

// rollDay starts a new day once the previous one has ended. The next midnight comes from the calendar
// date, not from adding 24 hours, so a change of the zone's clock gives a 23 or 25 hour day.
func (c *Counter) rollDay(now time.Time) {
	if !c.dayEnd.IsZero() && now.Before(c.dayEnd) {
		return
	}
	year, month, day := now.In(c.window.zone).Date()
	c.dayEnd = time.Date(year, month, day+1, 0, 0, 0, 0, c.window.zone)
	c.used = 0
}

// Release returns the units of the reservation. It is idempotent and does nothing once the window of
// the reservation has passed. Release only when the request was provably not sent.
func (r *Reservation) Release() {
	c := r.counter
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	now := c.tick()
	if c.window.zone != nil {
		if now.Before(r.dayEnd) && c.dayEnd.Equal(r.dayEnd) {
			c.used -= r.n
		}
		return
	}
	c.expire(now)
	for i, e := range c.log {
		if e.id == r.id {
			c.used -= e.n
			c.log = append(c.log[:i], c.log[i+1:]...)
			return
		}
	}
}

func nonNegative(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

// CounterRegistry holds the counter of every credential one client has seen.
type CounterRegistry struct {
	mu       sync.Mutex
	limit    int
	window   Window
	counters map[[sha256.Size]byte]*Counter
}

// NewCounterRegistry returns a registry whose counters share one limit and window per key. It panics
// for the same invalid values as NewCounter.
func NewCounterRegistry(limit int, window Window) *CounterRegistry {
	NewCounter(limit, window, time.Now)
	return &CounterRegistry{limit: limit, window: window, counters: map[[sha256.Size]byte]*Counter{}}
}

// For returns the counter of one credential value, creating it on first use. Only a digest of the key
// is kept.
func (r *CounterRegistry) For(key string) *Counter {
	digest := digestOf(key)
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.counters[digest]; ok {
		return existing
	}
	created := NewCounter(r.limit, r.window, time.Now)
	r.counters[digest] = created
	return created
}

// Replace installs counter for key and returns the function that restores the previous state. It is
// the seam a client test uses; production code only calls For.
func (r *CounterRegistry) Replace(key string, counter *Counter) (restore func()) {
	digest := digestOf(key)
	r.mu.Lock()
	previous, existed := r.counters[digest]
	r.counters[digest] = counter
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if existed {
			r.counters[digest] = previous
			return
		}
		delete(r.counters, digest)
	}
}
