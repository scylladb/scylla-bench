package rate_limiter

import "time"

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
	Period              time.Duration
	StartTime           time.Time
	CompletedOperations int64
}

func (mxrl *MaximumRateLimiter) Wait() {
	mxrl.CompletedOperations++
	nextRequest := mxrl.StartTime.Add(mxrl.Period * time.Duration(mxrl.CompletedOperations))
	now := time.Now()
	if now.Before(nextRequest) {
		time.Sleep(nextRequest.Sub(now))
	}
}

func (mxrl *MaximumRateLimiter) Expected() time.Time {
	return mxrl.StartTime.Add(mxrl.Period * time.Duration(mxrl.CompletedOperations))
}

func NewRateLimiter(maximumRate int, timeOffset time.Duration) RateLimiter {
	if maximumRate == 0 {
		return &UnlimitedRateLimiter{}
	}
	period := time.Duration(int64(time.Second) / int64(maximumRate))
	return &MaximumRateLimiter{period, time.Now(), 0}
}
