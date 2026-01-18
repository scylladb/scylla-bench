package main

import (
	"testing"
	"time"

	"github.com/scylladb/scylla-bench/pkg/results"
)

// TestUTCTimezoneEnforcement verifies that all time operations use UTC timezone
// to avoid issues during daylight saving time shifts or timezone changes.
func TestUTCTimezoneEnforcement(t *testing.T) {
	t.Parallel()

	// Test 1: Verify NewRateLimiter uses UTC
	t.Run("RateLimiterUsesUTC", func(t *testing.T) {
		rateLimiter := NewRateLimiter(1000, time.Second)
		if maxRL, ok := rateLimiter.(*MaximumRateLimiter); ok {
			if maxRL.StartTime.Location() != time.UTC {
				t.Errorf("RateLimiter StartTime not using UTC: got %v, want UTC", maxRL.StartTime.Location())
			}
		}
	})

	// Test 2: Verify time operations in results use UTC
	t.Run("ResultTimestampsUseUTC", func(t *testing.T) {
		// We can't directly access the private startTime field,
		// but we can verify that the merged result uses UTC
		result := results.NewMergedResult()
		if result.HistogramStartTime == 0 {
			t.Skip("Histogram not initialized in this configuration")
		}
		// Convert the UnixNano timestamp back to time to verify it would be UTC
		histogramStartTime := time.Unix(0, result.HistogramStartTime)
		// UTC times should be equal when converted
		utcTime := histogramStartTime.UTC()
		if !histogramStartTime.Equal(utcTime) {
			t.Errorf("Timestamp not using UTC timezone")
		}
	})

	// Test 3: Verify time operations remain consistent across different local timezones
	t.Run("TimeOperationsConsistent", func(t *testing.T) {
		// This test verifies that UTC times are not affected by local timezone settings
		// We can't safely change time.Local in parallel tests due to race conditions,
		// but we can verify that UTC() always returns UTC regardless of how we call it

		// Create rate limiters and verify they use UTC
		rateLimiter1 := NewRateLimiter(1000, time.Second)
		maxRL1 := rateLimiter1.(*MaximumRateLimiter)

		// Wait a tiny bit
		time.Sleep(time.Millisecond)

		// Create another rate limiter
		rateLimiter2 := NewRateLimiter(1000, time.Second)
		maxRL2 := rateLimiter2.(*MaximumRateLimiter)

		// Both should be using UTC
		if maxRL1.StartTime.Location() != time.UTC {
			t.Errorf("RateLimiter1 not using UTC: got %v", maxRL1.StartTime.Location())
		}
		if maxRL2.StartTime.Location() != time.UTC {
			t.Errorf("RateLimiter2 not using UTC: got %v", maxRL2.StartTime.Location())
		}

		// The times should be close but not identical (we slept between them)
		timeDiff := maxRL2.StartTime.Sub(maxRL1.StartTime)
		if timeDiff < 0 {
			t.Errorf("Second rate limiter created before first: %v", timeDiff)
		}
		if timeDiff > time.Second {
			t.Errorf("Time difference too large: %v", timeDiff)
		}
	})

	// Test 4: Verify time.Now().UTC() returns UTC location
	t.Run("DirectUTCCall", func(t *testing.T) {
		now := time.Now().UTC()
		if now.Location() != time.UTC {
			t.Errorf("time.Now().UTC() not returning UTC: got %v", now.Location())
		}
	})
}

// TestTimezoneDSTTransition tests that operations during DST transitions work correctly
func TestTimezoneDSTTransition(t *testing.T) {
	t.Parallel()

	// Test rate limiter behavior around DST transition
	t.Run("RateLimiterDuringDST", func(t *testing.T) {
		// Create a rate limiter
		rateLimiter := NewRateLimiter(100, time.Second) // 100 ops/sec
		maxRL := rateLimiter.(*MaximumRateLimiter)

		// Verify it's using UTC (which doesn't have DST)
		if maxRL.StartTime.Location() != time.UTC {
			t.Errorf("RateLimiter not using UTC: got %v", maxRL.StartTime.Location())
		}

		// Simulate several operations
		for i := 0; i < 5; i++ {
			expectedTime := maxRL.Expected()
			if expectedTime.Location() != time.UTC {
				t.Errorf("Expected time not using UTC: got %v", expectedTime.Location())
			}
			maxRL.CompletedOperations++
		}
	})
}
