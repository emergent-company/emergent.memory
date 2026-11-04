package database

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

// testLogger discards log output so retry tests stay quiet.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestPingWithRetry_SucceedsAfterFailures proves the retry loop actually
// re-pings after a transient failure instead of hard-failing on the first
// error (issue #1481). The attempt count assertion is the mutation guard: if
// the retry loop is removed, calls would be 1 and this test fails.
func TestPingWithRetry_SucceedsAfterFailures(t *testing.T) {
	var calls int32
	failures := int32(3)
	ping := func(context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		if n <= failures {
			return errors.New("connection refused")
		}
		return nil
	}

	err := pingWithRetry(context.Background(), ping, 5, time.Millisecond, 5*time.Millisecond, testLogger())
	if err != nil {
		t.Fatalf("pingWithRetry() error = %v, want nil", err)
	}
	if got := atomic.LoadInt32(&calls); got != failures+1 {
		t.Fatalf("ping calls = %d, want %d (retry loop did not run)", got, failures+1)
	}
}

// TestPingWithRetry_ExhaustsAttempts asserts a permanently-down DB still fails,
// and that it fails only after the full attempt budget is spent.
func TestPingWithRetry_ExhaustsAttempts(t *testing.T) {
	var calls int32
	sentinel := errors.New("still down")
	ping := func(context.Context) error {
		atomic.AddInt32(&calls, 1)
		return sentinel
	}

	err := pingWithRetry(context.Background(), ping, 3, time.Millisecond, 2*time.Millisecond, testLogger())
	if err == nil {
		t.Fatal("pingWithRetry() error = nil, want non-nil after exhausting attempts")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("pingWithRetry() error = %v, want wrapped sentinel", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("ping calls = %d, want 3", got)
	}
}

// TestPingWithRetry_ClampsAttempts guards the attempts < 1 edge: it must still
// try once rather than skipping the ping entirely.
func TestPingWithRetry_ClampsAttempts(t *testing.T) {
	var calls int32
	ping := func(context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("down")
	}

	_ = pingWithRetry(context.Background(), ping, 0, 0, 0, testLogger())
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("ping calls = %d, want 1", got)
	}
}

// TestPingWithRetry_ContextCancelledDuringBackoff verifies a cancelled context
// aborts promptly instead of sleeping through the remaining backoff.
func TestPingWithRetry_ContextCancelledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ping := func(context.Context) error {
		cancel()
		return errors.New("down")
	}

	start := time.Now()
	err := pingWithRetry(ctx, ping, 10, time.Hour, time.Hour, testLogger())
	if err == nil {
		t.Fatal("pingWithRetry() error = nil, want context error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("pingWithRetry() blocked %s after cancel, want prompt abort", elapsed)
	}
}
