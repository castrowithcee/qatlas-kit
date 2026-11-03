package fileretry

import "time"

// Options tune Do. The zero value retries nothing.
type Options struct {
	// Transient reports the errors that waiting cures; nil means no failure is retried.
	Transient func(error) bool
	// Delays are the waits between the attempts; empty means DefaultDelays.
	Delays []time.Duration
	// Sleep waits between the attempts; nil means time.Sleep.
	Sleep func(time.Duration)
}

// DefaultDelays returns the waits Do uses unless Options.Delays is set, 2.775 s in all at most.
func DefaultDelays() []time.Duration {
	return []time.Duration{25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond,
		200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1200 * time.Millisecond}
}

// Do runs op, and repeats an op that failed with a transient error after each of the delays in turn. op
// must change nothing when it fails, so repeating it is safe; any other error is returned at once.
func Do(op func() error, opts Options) error {
	delays := opts.Delays
	if len(delays) == 0 {
		delays = DefaultDelays()
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	err := op()
	for _, delay := range delays {
		if err == nil || opts.Transient == nil || !opts.Transient(err) {
			return err
		}
		sleep(delay)
		err = op()
	}
	return err
}
