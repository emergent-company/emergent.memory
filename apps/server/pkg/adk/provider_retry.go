package adk

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Provider rate limiting is transient by definition, but the layer above a
// model call is not: the object-extraction job requeues a failed attempt
// immediately, so three 429s land within seconds and dead-letter a run the
// provider would have accepted a few seconds later (#237). One model call
// therefore answers its own rate limit before surfacing it.
const (
	// maxProviderRetries is how many extra attempts one model call makes after
	// a retryable response. Two is enough to cross a typical rate-limit window
	// without pinning a worker slot for long.
	maxProviderRetries = 2
	// providerRetryBase is the first backoff when the provider sends no
	// Retry-After, doubled for each further attempt.
	providerRetryBase = 5 * time.Second
	// providerRetryMax caps a single wait: a provider asking for longer
	// releases the worker and lets the job's own retry handle it.
	providerRetryMax = 30 * time.Second
)

// isRetryableProviderStatus reports whether a provider response is worth
// another attempt. Rate limiting and transient server/gateway faults are; a 4xx
// the request itself caused is not.
func isRetryableProviderStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// providerRetryDelay returns how long to wait before the next attempt. A
// Retry-After header (seconds or HTTP date) wins; otherwise the backoff doubles
// per attempt. The result is clamped to providerRetryMax.
func providerRetryDelay(attempt int, retryAfter string) time.Duration {
	delay := providerRetryBase << attempt
	if d, ok := parseRetryAfter(retryAfter, time.Now()); ok {
		delay = d
	}
	if delay > providerRetryMax {
		return providerRetryMax
	}
	if delay < 0 {
		return 0
	}
	return delay
}

// parseRetryAfter reads the delay a provider asked for, in either form RFC 9110
// allows. An absent or unparseable value reports false.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			seconds = 0
		}
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := when.Sub(now)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}
	return 0, false
}

// sleepWithContext waits for d and reports whether the wait completed. A
// cancelled context ends it early so a shutdown is never delayed by a backoff.
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
