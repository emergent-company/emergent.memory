package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTracker() *tokenUsageTracker {
	return &tokenUsageTracker{
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		throttle:  lastUsedThrottle,
		lastFlush: make(map[string]time.Time),
		now:       time.Now,
	}
}

// The throttle decision is deterministic and independent of the async flush: a
// token's first use is due, an immediate second use is throttled, and a use
// after the interval elapses is due again.
func TestTokenUsageTrackerThrottleDecision(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	tr := newTestTracker()
	tr.now = func() time.Time { return now }

	require.True(t, tr.shouldFlush("tok-1"), "first use must be due")
	assert.False(t, tr.shouldFlush("tok-1"), "immediate second use must be throttled")

	now = now.Add(lastUsedThrottle - time.Second)
	assert.False(t, tr.shouldFlush("tok-1"), "use just inside the interval is still throttled")

	now = now.Add(time.Second) // exactly the interval
	require.True(t, tr.shouldFlush("tok-1"), "use at/after the interval must be due again")

	// The throttle is per-token: a different token is unaffected.
	assert.True(t, tr.shouldFlush("tok-2"), "a different token's first use is due")
}

// Two rapid touches must produce at most one flush; a touch after the interval
// must produce a second. flushFn records the writes into a channel so the
// asynchronous flush can be observed deterministically.
func TestTokenUsageTouchThrottlesFlushes(t *testing.T) {
	now := time.Now()
	tr := newTestTracker()
	tr.now = func() time.Time { return now }

	flushed := make(chan string, 4)
	tr.flushFn = func(_ context.Context, tokenID string) error {
		flushed <- tokenID
		return nil
	}

	tr.touch("tok-1")
	tr.touch("tok-1") // throttled

	waitFlush(t, flushed, "tok-1")
	assertNoFlush(t, flushed)

	// After the throttle elapses, a further touch flushes again.
	now = now.Add(lastUsedThrottle + time.Second)
	tr.touch("tok-1")
	waitFlush(t, flushed, "tok-1")
}

// A failure in the flush path is swallowed: touch never returns an error, never
// panics, and the recorded error is not surfaced to the caller.
func TestTokenUsageTouchFailureIsNonFatal(t *testing.T) {
	tr := newTestTracker()
	done := make(chan struct{})
	tr.flushFn = func(_ context.Context, _ string) error {
		close(done)
		return errors.New("injected store failure")
	}

	tr.touch("tok-1")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("flush did not run")
	}
}

// touch is a no-op for an empty token id and for a nil tracker.
func TestTokenUsageTouchGuards(t *testing.T) {
	tr := newTestTracker()
	calls := 0
	tr.flushFn = func(_ context.Context, _ string) error { calls++; return nil }

	tr.touch("") // empty id: no flush
	var nilTr *tokenUsageTracker
	nilTr.touch("tok-1") // nil tracker: no panic

	assert.Equal(t, 0, calls, "no flush should have been enqueued")
}

func waitFlush(t *testing.T, ch chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		assert.Equal(t, want, got)
	case <-time.After(3 * time.Second):
		t.Fatalf("expected a flush for %q", want)
	}
}

func assertNoFlush(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("unexpected extra flush for %q", got)
	case <-time.After(150 * time.Millisecond):
	}
}
