package adk

import (
	"context"
	"fmt"
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
	// providerRetryMax is the longest wait one model call accepts. A provider
	// asking for longer is not retried inside the call at all (see
	// providerRetryWait): the worker is released and the job's own retry
	// handles it, rather than waiting the cap and re-asking for the same hint.
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

// providerRetryWait reports whether a retryable response earns another attempt,
// and how long to wait first. It is the single decision point shared by every
// model client, so the policy cannot drift between wire protocols (#254).
//
// It answers false when the status is not retryable, when the call's attempts
// are spent, or when the provider's Retry-After is longer than providerRetryMax.
// The last case deliberately gives up at once instead of waiting the cap and
// re-asking: a hint that long is not a delay one call should absorb, so the
// worker is released and the job's own retry covers it (#254).
func providerRetryWait(attempt, statusCode int, retryAfter string) (time.Duration, bool) {
	if !isRetryableProviderStatus(statusCode) || attempt >= maxProviderRetries {
		return 0, false
	}
	if d, ok := parseRetryAfter(retryAfter, time.Now()); ok && d > providerRetryMax {
		return 0, false
	}
	return providerRetryDelay(attempt, retryAfter), true
}

// retryContextError reports the end of the context as the reason the call
// stopped. Without it a cancelled run would surface the last 429/5xx response
// and blame the provider for something the caller did (#254). The context error
// is wrapped, so errors.Is(err, context.Canceled) and DeadlineExceeded work.
func retryContextError(client string, ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		// sleepWithContext reports false only when the context ended, but a
		// nil Err here would otherwise produce an error that matches neither
		// sentinel.
		err = context.Canceled
	}
	return fmt.Errorf("%s: cancelled during provider retry: %w", client, err)
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
