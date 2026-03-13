// Package clock provides time utilities that enforce UTC throughout the codebase.
//
// All time operations in scylla-bench must go through a Clock instead of
// calling time.Now() or time.Sleep() directly. This ensures:
//   - All timestamps are timezone-neutral (UTC), unaffected by DST transitions.
//   - Tests can inject a [Manual] clock to control and inspect time without sleeping.
//
// Usage: construct a [UTC] once at program start via [New] and pass it
// everywhere through dependency injection.
package clock

import "time"

// Clock is the interface for all time operations in scylla-bench.
//
// Production code receives a [UTC]; tests can supply a [Manual] to
// control time deterministically without real sleeps.
type Clock interface {
	// Now returns the current time. Implementations must always return a UTC time.
	Now() time.Time

	// Since returns the elapsed time since t. The [UTC] implementation
	// delegates to time.Since to preserve the monotonic clock reading.
	Since(t time.Time) time.Duration

	// NowUnixNano returns the current time as nanoseconds since the Unix epoch.
	// Use this when only a Unix nanosecond timestamp is needed, to make the
	// intent explicit and avoid an intermediate time.Time allocation.
	NowUnixNano() int64

	// Sleep pauses the current goroutine for at least d. On a [Manual] clock this
	// advances the clock by d instead of blocking, making rate-limiter and retry
	// logic deterministically testable.
	Sleep(d time.Duration)
}

// UTC is the real-clock implementation. It is stateless, so multiple
// instances are equivalent, but prefer injecting a single instance for
// consistency.
type UTC struct{}

// New returns the production Clock.
func New() Clock {
	return UTC{}
}

func (UTC) Now() time.Time {
	return time.Now().UTC()
}

func (UTC) Since(t time.Time) time.Duration {
	return time.Since(t)
}

func (UTC) NowUnixNano() int64 {
	return time.Now().UnixNano()
}

func (UTC) Sleep(d time.Duration) {
	time.Sleep(d)
}
