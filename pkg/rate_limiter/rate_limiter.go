package rate_limiter

import (
	"time"

	"github.com/scylladb/scylla-bench/internal/clock"
)

type RateLimiter interface {
	Wait()
	Expected() time.Time
}

type UnlimitedRateLimiter struct{}

func (*UnlimitedRateLimiter) Wait() {}

func (*UnlimitedRateLimiter) Expected() time.Time {
	return time.Time{}
}

type MaximumRateLimiter struct {
	clk                 clock.Clock
	StartTime           time.Time
	Period              time.Duration
	CompletedOperations int64
}

func (mxrl *MaximumRateLimiter) Wait() {
	mxrl.CompletedOperations++
	nextRequest := mxrl.StartTime.Add(mxrl.Period * time.Duration(mxrl.CompletedOperations))
	now := mxrl.clk.Now()
	if now.Before(nextRequest) {
		mxrl.clk.Sleep(nextRequest.Sub(now))
	}
}

func (mxrl *MaximumRateLimiter) Expected() time.Time {
	return mxrl.StartTime.Add(mxrl.Period * time.Duration(mxrl.CompletedOperations))
}

func NewRateLimiter(clk clock.Clock, maximumRate int, _ time.Duration) RateLimiter {
	if maximumRate == 0 {
		return &UnlimitedRateLimiter{}
	}
	period := time.Duration(int64(time.Second) / int64(maximumRate))
	return &MaximumRateLimiter{
		clk:                 clk,
		Period:              period,
		StartTime:           clk.Now(),
		CompletedOperations: 0,
	}
}
