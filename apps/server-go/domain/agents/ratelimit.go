package agents

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// WebhookRateLimiter manages rate limiters for individual webhook hooks
type WebhookRateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
}

// NewWebhookRateLimiter creates a new rate limiter manager
func NewWebhookRateLimiter() *WebhookRateLimiter {
	return &WebhookRateLimiter{
		limiters: make(map[string]*rate.Limiter),
	}
}

// CheckRateLimit checks if a request is allowed for the given hook ID based on its configuration
func (m *WebhookRateLimiter) CheckRateLimit(ctx context.Context, hookID string, config *RateLimitConfig) bool {
	// If no config, default to some reasonable limits or allow all.
	// We'll enforce a default of 60 req/min if config is nil.
	reqPerMin := 60
	burst := 10

	if config != nil {
		if config.RequestsPerMinute > 0 {
			reqPerMin = config.RequestsPerMinute
		}
		if config.BurstSize > 0 {
			burst = config.BurstSize
		}
	}

	limiter := m.getLimiter(hookID, reqPerMin, burst)
	return limiter.Allow()
}

// getLimiter retrieves or creates a limiter for a hook
func (m *WebhookRateLimiter) getLimiter(hookID string, reqPerMin, burst int) *rate.Limiter {
	m.mu.RLock()
	limiter, exists := m.limiters[hookID]
	m.mu.RUnlock()

	// Calculate rate (events per second)
	r := rate.Every(time.Minute / time.Duration(reqPerMin))

	if exists {
		// Update limits dynamically if they changed
		if limiter.Limit() != r || limiter.Burst() != burst {
			limiter.SetLimit(r)
			limiter.SetBurst(burst)
		}
		return limiter
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check to prevent race condition
	limiter, exists = m.limiters[hookID]
	if exists {
		if limiter.Limit() != r || limiter.Burst() != burst {
			limiter.SetLimit(r)
			limiter.SetBurst(burst)
		}
		return limiter
	}

	limiter = rate.NewLimiter(r, burst)
	m.limiters[hookID] = limiter
	return limiter
}

// RemoveLimiter removes a limiter when a hook is deleted or disabled
func (m *WebhookRateLimiter) RemoveLimiter(hookID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.limiters, hookID)
}
