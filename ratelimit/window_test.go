package ratelimit

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	_ "time/tzdata" // zone data for the DST tests on platforms without a system database
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func (c *fakeClock) Advance(d time.Duration) { c.Set(c.Now().Add(d)) }

func mustReserve(t *testing.T, c *Counter, n int) *Reservation {
	t.Helper()
	r, err := c.Reserve(n)
	if err != nil {
		t.Fatalf("Reserve(%d) = %v", n, err)
	}
	return r
}

func wantExhausted(t *testing.T, c *Counter, n int, wait time.Duration) {
	t.Helper()
	r, err := c.Reserve(n)
	var ex *ExhaustedError
	if !errors.As(err, &ex) || r != nil {
		t.Fatalf("Reserve(%d) = %v, %v, want *ExhaustedError", n, r, err)
	}
	if ex.Wait != wait {
		t.Fatalf("Wait = %v, want %v", ex.Wait, wait)
	}
}

func TestSlidingRejectsTheFourthUntilTheOldestExpires(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1000, 0)}
	c := NewCounter(3, Sliding(time.Minute), clk.Now)
	for i := 0; i < 3; i++ {
		mustReserve(t, c, 1)
		clk.Advance(10 * time.Second)
	}
	wantExhausted(t, c, 1, 30*time.Second)
	clk.Advance(30 * time.Second)
	mustReserve(t, c, 1)
}

func TestSlidingRejectsAllOrNothingAndWaitsAcrossEntries(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1000, 0)}
	c := NewCounter(3, Sliding(time.Minute), clk.Now)
	mustReserve(t, c, 1)
	clk.Advance(10 * time.Second)
	mustReserve(t, c, 1)
	clk.Advance(10 * time.Second)

	wantExhausted(t, c, 2, 40*time.Second) // needs the first entry to expire
	mustReserve(t, c, 1)                   // state unchanged, one unit is still free
	wantExhausted(t, c, 3, 40*time.Second+20*time.Second)
}

func TestRejectsOversizedAndInvalidUnitsWithoutChange(t *testing.T) {
	for _, w := range []Window{Sliding(time.Minute), Daily(time.UTC)} {
		clk := &fakeClock{now: time.Unix(1000, 0)}
		c := NewCounter(3, w, clk.Now)

		var exceeds *ExceedsLimitError
		if _, err := c.Reserve(4); !errors.As(err, &exceeds) || exceeds.Requested != 4 || exceeds.Limit != 3 {
			t.Fatalf("Reserve(4) = %v, want *ExceedsLimitError", err)
		}
		for _, n := range []int{0, -1} {
			var invalid *InvalidUnitsError
			if _, err := c.Reserve(n); !errors.As(err, &invalid) || invalid.Units != n {
				t.Fatalf("Reserve(%d) = %v, want *InvalidUnitsError", n, err)
			}
		}
		mustReserve(t, c, 3) // nothing was consumed by the rejections
	}
}

func TestDailyResetsAtMidnightAndWaitsUntilThen(t *testing.T) {
	zone := time.FixedZone("test", 2*3600)
	clk := &fakeClock{now: time.Date(2026, 3, 10, 22, 0, 0, 0, zone)}
	c := NewCounter(2, Daily(zone), clk.Now)
	mustReserve(t, c, 2)
	wantExhausted(t, c, 1, 2*time.Hour)
	clk.Set(time.Date(2026, 3, 10, 23, 59, 59, 0, zone))
	wantExhausted(t, c, 1, time.Second)
	clk.Set(time.Date(2026, 3, 11, 0, 0, 0, 0, zone))
	mustReserve(t, c, 2)
}

func TestDailyFollowsTheDSTDayLength(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		start time.Time
		wait  time.Duration
	}{
		{"23 hour day", time.Date(2026, 3, 8, 0, 0, 0, 0, la), 23 * time.Hour},
		{"25 hour day", time.Date(2026, 11, 1, 0, 0, 0, 0, la), 25 * time.Hour},
	}
	for _, tc := range cases {
		clk := &fakeClock{now: tc.start}
		c := NewCounter(1, Daily(la), clk.Now)
		mustReserve(t, c, 1)
		wantExhausted(t, c, 1, tc.wait)
		clk.Advance(tc.wait - time.Nanosecond)
		wantExhausted(t, c, 1, time.Nanosecond)
		clk.Advance(time.Nanosecond)
		mustReserve(t, c, 1)
	}
}

func TestReleaseReturnsOwnUnitsOnceAndKeepsOthers(t *testing.T) {
	for name, w := range map[string]Window{"sliding": Sliding(time.Hour), "daily": Daily(time.UTC)} {
		t.Run(name, func(t *testing.T) {
			clk := &fakeClock{now: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)}
			c := NewCounter(3, w, clk.Now)
			other := mustReserve(t, c, 1)
			mine := mustReserve(t, c, 2)
			mine.Release()
			mine.Release() // a second release must not return more
			mustReserve(t, c, 2)
			if _, err := c.Reserve(1); err == nil {
				t.Fatal("double release freed foreign units")
			}
			_ = other
		})
	}
}

func TestReleaseAfterExpiryHasNoEffect(t *testing.T) {
	for name, w := range map[string]Window{"sliding": Sliding(time.Hour), "daily": Daily(time.UTC)} {
		t.Run(name, func(t *testing.T) {
			clk := &fakeClock{now: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)}
			c := NewCounter(2, w, clk.Now)
			old := mustReserve(t, c, 2)
			clk.Advance(24 * time.Hour)
			mustReserve(t, c, 2)
			old.Release()
			if _, err := c.Reserve(1); err == nil {
				t.Fatal("a release after expiry returned units of the new window")
			}
		})
	}
}

func TestSlidingLogDoesNotGrowOverCycles(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1000, 0)}
	c := NewCounter(5, Sliding(time.Hour), clk.Now)
	keep := mustReserve(t, c, 1)
	for i := 0; i < 1000; i++ {
		mustReserve(t, c, 2).Release()
	}
	if len(c.log) != 1 || c.used != 1 {
		t.Fatalf("log = %d entries, used = %d, want 1 and 1", len(c.log), c.used)
	}
	keep.Release()
	if len(c.log) != 0 || c.used != 0 {
		t.Fatalf("log = %d entries, used = %d, want empty", len(c.log), c.used)
	}
}

func TestConcurrentReservationsNeverExceedTheLimit(t *testing.T) {
	for name, w := range map[string]Window{"sliding": Sliding(time.Hour), "daily": Daily(time.UTC)} {
		t.Run(name, func(t *testing.T) {
			const limit, goroutines = 10, 100
			clk := &fakeClock{now: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)}
			c := NewCounter(limit, w, clk.Now)
			var ok atomic.Int64
			var wg sync.WaitGroup
			for i := 0; i < goroutines; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, err := c.Reserve(1); err == nil {
						ok.Add(1)
					}
				}()
			}
			wg.Wait()
			if ok.Load() != limit {
				t.Fatalf("%d reservations succeeded, want %d", ok.Load(), limit)
			}
		})
	}
}

// With releases mixed in, successes minus releases never pass the limit and the end state is consistent.
func TestConcurrentReservationsMixedWithReleases(t *testing.T) {
	const limit = 5
	clk := &fakeClock{now: time.Unix(1000, 0)}
	c := NewCounter(limit, Sliding(time.Hour), clk.Now)
	var held, peak atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := c.Reserve(1)
			if err != nil {
				return
			}
			cur := held.Add(1)
			for {
				p := peak.Load()
				if cur <= p || peak.CompareAndSwap(p, cur) {
					break
				}
			}
			if i%2 == 0 {
				held.Add(-1)
				r.Release()
			}
		}(i)
	}
	wg.Wait()
	if peak.Load() > limit {
		t.Fatalf("peak %d exceeded the limit %d", peak.Load(), limit)
	}
	if c.used != int(held.Load()) || len(c.log) != c.used {
		t.Fatalf("used = %d, log = %d, held = %d", c.used, len(c.log), held.Load())
	}
}

func TestBackwardsClockKeepsOrderAndNeverWaitsNegative(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1000, 0)}
	c := NewCounter(2, Sliding(time.Minute), clk.Now)
	mustReserve(t, c, 1)
	clk.Set(time.Unix(900, 0))
	mustReserve(t, c, 1)
	wantExhausted(t, c, 1, time.Minute)
	if c.log[0].at.After(c.log[1].at) {
		t.Fatal("entries are out of order")
	}

	d := NewCounter(1, Daily(time.UTC), clk.Now)
	clk.Set(time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	mustReserve(t, d, 1)
	clk.Set(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	wantExhausted(t, d, 1, 12*time.Hour)
}

func TestInvalidConstructionPanics(t *testing.T) {
	cases := map[string]func(){
		"limit zero":      func() { NewCounter(0, Sliding(time.Second), time.Now) },
		"length zero":     func() { NewCounter(1, Sliding(0), time.Now) },
		"length negative": func() { NewCounter(1, Sliding(-time.Second), time.Now) },
		"nil zone":        func() { NewCounter(1, Daily(nil), time.Now) },
		"zero window":     func() { NewCounter(1, Window{}, time.Now) },
		"nil clock":       func() { NewCounter(1, Sliding(time.Second), nil) },
		"registry limit":  func() { NewCounterRegistry(0, Sliding(time.Second)) },
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			f()
		})
	}
}

func TestErrorsNeverContainTheKey(t *testing.T) {
	const key = "canary-counter-error-key-7e21"
	registry := NewCounterRegistry(1, Sliding(time.Hour))
	c := registry.For(key)
	mustReserve(t, c, 1)
	for _, n := range []int{1, 2, 0} {
		_, err := c.Reserve(n)
		if err == nil || strings.Contains(err.Error(), key) {
			t.Errorf("Reserve(%d) error %v", n, err)
		}
	}
}

func TestCounterRegistryKeepsOneBudgetPerKeyAndOnlyDigests(t *testing.T) {
	registry := NewCounterRegistry(1, Sliding(time.Hour))
	const first = "canary-ratelimit-counter-one-3f81"
	const second = "canary-ratelimit-counter-two-5c02"
	if registry.For(first) != registry.For(first) {
		t.Error("one key does not keep one counter")
	}
	if registry.For(first) == registry.For(second) {
		t.Error("two keys share one counter")
	}
	mustReserve(t, registry.For(first), 1)
	mustReserve(t, registry.For(second), 1)
	for digest := range registry.counters {
		if string(digest[:]) == first || string(digest[:]) == second {
			t.Error("the registry stored a key instead of its digest")
		}
	}
}

func TestCounterRegistryReplaceRestoresThePreviousState(t *testing.T) {
	registry := NewCounterRegistry(1, Sliding(time.Hour))
	const key = "canary-ratelimit-counter-replace-9a4d"
	replacement := NewCounter(9, Sliding(time.Hour), time.Now)

	restore := registry.Replace(key, replacement)
	if registry.For(key) != replacement {
		t.Fatal("Replace() did not install the counter")
	}
	restore()
	if _, ok := registry.counters[digestOf(key)]; ok {
		t.Error("restore() left an unknown key behind")
	}
	original := registry.For(key)
	restore = registry.Replace(key, replacement)
	restore()
	if registry.For(key) != original {
		t.Error("restore() did not put the original counter back")
	}
}
