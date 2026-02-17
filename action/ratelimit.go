package action

import (
	"sync"
	"time"
)

// RateLimiter implements a token-bucket rate limiter.
type RateLimiter struct {
	tokens    float64
	maxTokens float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a rate limiter that allows ratePerMinute operations per minute.
func NewRateLimiter(ratePerMinute int) *RateLimiter {
	max := float64(ratePerMinute)
	return &RateLimiter{
		tokens:     max,
		maxTokens:  max,
		refillRate: max / 60.0,
		lastRefill: time.Now(),
	}
}

// Allow returns true if a token is available, consuming one.
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()
	r.tokens += elapsed * r.refillRate
	if r.tokens > r.maxTokens {
		r.tokens = r.maxTokens
	}
	r.lastRefill = now

	if r.tokens < 1.0 {
		return false
	}
	r.tokens--
	return true
}
