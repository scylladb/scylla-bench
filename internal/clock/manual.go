package clock

import (
	"sync"
	"time"
)

// Manual is a deterministic Clock for use in tests. Time does not advance
// automatically; call Advance to move it forward. Sleep records the requested
// duration and advances the clock instead of blocking, so tests that exercise
// rate-limiter or retry logic complete instantly.
type Manual struct {
	mu      sync.Mutex
	current time.Time
	slept   time.Duration // total duration passed to Sleep across all calls
}

// NewManual returns a Manual clock set to t. If t is zero, it is set to the
// Unix epoch in UTC so that NowUnixNano always returns a non-negative value.
func NewManual(t time.Time) *Manual {
	if t.IsZero() {
		t = time.Unix(0, 0).UTC()
	}
	return &Manual{current: t.UTC()}
}

// Advance moves the clock forward by d.
func (m *Manual) Advance(d time.Duration) {
	m.mu.Lock()
	m.current = m.current.Add(d)
	m.mu.Unlock()
}

// Set moves the clock to t (converted to UTC).
func (m *Manual) Set(t time.Time) {
	m.mu.Lock()
	m.current = t.UTC()
	m.mu.Unlock()
}

// Slept returns the total duration that has been passed to Sleep.
func (m *Manual) Slept() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.slept
}

// Now implements Clock.
func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// Since implements Clock.
func (m *Manual) Since(t time.Time) time.Duration {
	return m.Now().Sub(t)
}

// NowUnixNano implements Clock.
func (m *Manual) NowUnixNano() int64 {
	return m.Now().UnixNano()
}

// Sleep implements Clock. Instead of blocking, it advances the clock by d and
// records the duration so tests can assert how long operations "slept".
func (m *Manual) Sleep(d time.Duration) {
	m.mu.Lock()
	m.current = m.current.Add(d)
	m.slept += d
	m.mu.Unlock()
}
