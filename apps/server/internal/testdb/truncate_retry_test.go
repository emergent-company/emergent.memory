package testdb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// pgErr builds a *pgconn.PgError the way pgx surfaces a server-side SQLSTATE.
func pgErr(code, message string) *pgconn.PgError {
	return &pgconn.PgError{Code: code, Message: message}
}

// noSleep records the backoff it was asked to wait for without waiting.
func noSleep(delays *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*delays = append(*delays, d)
		return nil
	}
}

// TestRetryOnTransientLockRetriesDeadlockThenSucceeds is the regression guard
// for issue #2410: a reset that hits a transient 40P01 (a background worker
// holding a conflicting table lock) must be retried and succeed instead of
// failing the suite.
func TestRetryOnTransientLockRetriesDeadlockThenSucceeds(t *testing.T) {
	calls := 0
	var delays []time.Duration
	err := retryOnTransientLock(context.Background(), noSleep(&delays), func() error {
		calls++
		if calls < 3 {
			return pgErr("40P01", "deadlock detected")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("retry should have succeeded on the third attempt, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("op calls = %d, want 3", calls)
	}
	if len(delays) != 2 {
		t.Fatalf("backoff sleeps = %d, want 2", len(delays))
	}
	for i, d := range delays {
		if d < truncateRetryMinDelay || d > truncateRetryMaxDelay {
			t.Errorf("delay[%d] = %v, want within [%v, %v]", i, d, truncateRetryMinDelay, truncateRetryMaxDelay)
		}
	}
}

// TestRetryOnTransientLockRetriesBothSQLStates covers 55P03 (lock_not_available)
// as well as 40P01: both are transient lock conflicts.
func TestRetryOnTransientLockRetriesBothSQLStates(t *testing.T) {
	for _, code := range []string{"40P01", "55P03"} {
		t.Run(code, func(t *testing.T) {
			calls := 0
			var delays []time.Duration
			err := retryOnTransientLock(context.Background(), noSleep(&delays), func() error {
				calls++
				if calls == 1 {
					return pgErr(code, "transient lock conflict")
				}
				return nil
			})
			if err != nil {
				t.Fatalf("retry should have succeeded, got %v", err)
			}
			if calls != 2 {
				t.Fatalf("op calls = %d, want 2", calls)
			}
		})
	}
}

// TestRetryOnTransientLockSurfacesPersistentFailure verifies a real hang stays
// visible: after the bounded attempts the original 40P01 is returned unchanged
// (wrapped) and the message records how many attempts were made.
func TestRetryOnTransientLockSurfacesPersistentFailure(t *testing.T) {
	original := pgErr("40P01", "deadlock detected")
	calls := 0
	var delays []time.Duration
	err := retryOnTransientLock(context.Background(), noSleep(&delays), func() error {
		calls++
		return original
	})

	if err == nil {
		t.Fatal("a persistently failing op must return an error")
	}
	if calls != truncateAttempts {
		t.Fatalf("op calls = %d, want %d", calls, truncateAttempts)
	}
	if len(delays) != truncateAttempts-1 {
		t.Fatalf("backoff sleeps = %d, want %d", len(delays), truncateAttempts-1)
	}
	if !errors.Is(err, original) {
		t.Fatalf("error %v does not wrap the original transient error %v", err, original)
	}
	var pgErrOut *pgconn.PgError
	if !errors.As(err, &pgErrOut) || pgErrOut.Code != "40P01" {
		t.Fatalf("error %v does not preserve SQLSTATE 40P01", err)
	}
	if !strings.Contains(err.Error(), "(after 5 attempts)") {
		t.Fatalf("error %q does not record the attempt count", err.Error())
	}
}

// TestRetryOnTransientLockDoesNotRetryPermanentError verifies non-lock errors
// (e.g. undefined table) surface on the first attempt instead of being
// swallowed by the retry loop.
func TestRetryOnTransientLockDoesNotRetryPermanentError(t *testing.T) {
	original := pgErr("42P01", `relation "kb.missing" does not exist`)
	calls := 0
	var delays []time.Duration
	err := retryOnTransientLock(context.Background(), noSleep(&delays), func() error {
		calls++
		return original
	})

	if !errors.Is(err, original) {
		t.Fatalf("error = %v, want the original permanent error", err)
	}
	if calls != 1 {
		t.Fatalf("op calls = %d, want 1 (no retries for permanent errors)", calls)
	}
	if len(delays) != 0 {
		t.Fatalf("backoff sleeps = %d, want 0", len(delays))
	}
}

// TestRetryOnTransientLockStopsOnCancellation verifies a cancelled context
// interrupts the backoff instead of sleeping out every attempt.
func TestRetryOnTransientLockStopsOnCancellation(t *testing.T) {
	original := pgErr("40P01", "deadlock detected")
	calls := 0
	err := retryOnTransientLock(context.Background(),
		func(context.Context, time.Duration) error { return context.Canceled },
		func() error {
			calls++
			return original
		})

	if calls != 1 {
		t.Fatalf("op calls = %d, want 1", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !errors.Is(err, original) {
		t.Fatalf("error = %v, want the original transient error preserved", err)
	}
}

// TestTruncateRetryDelayBounds pins the backoff envelope: every delay is in
// [50ms, 500ms] and the window grows exponentially until it is capped.
func TestTruncateRetryDelayBounds(t *testing.T) {
	for attempt := 0; attempt < truncateAttempts; attempt++ {
		window := truncateRetryWindow(attempt)
		for i := 0; i < 100; i++ {
			d := truncateRetryDelay(attempt)
			if d < truncateRetryMinDelay {
				t.Fatalf("attempt %d: delay %v below minimum %v", attempt, d, truncateRetryMinDelay)
			}
			if d > window {
				t.Fatalf("attempt %d: delay %v above window %v", attempt, d, window)
			}
			if d > truncateRetryMaxDelay {
				t.Fatalf("attempt %d: delay %v above hard maximum %v", attempt, d, truncateRetryMaxDelay)
			}
		}
	}
}

// TestTruncateRetryDelayWindows pins the exponential schedule itself: the first
// attempt already jitters over 50–100 ms (it used to be a fixed 50 ms despite
// the "50–100 ms" comment), and the 500 ms cap is reached by the fourth of the
// five attempts. Without this the cap was unreachable and therefore untested.
func TestTruncateRetryDelayWindows(t *testing.T) {
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
	}
	for attempt, window := range want {
		if got := truncateRetryWindow(attempt); got != window {
			t.Errorf("truncateRetryWindow(%d) = %v, want %v", attempt, got, window)
		}
	}
	if last := truncateRetryWindow(truncateAttempts - 2); last != truncateRetryMaxDelay {
		t.Fatalf("last backoff window at %d attempts = %v, want the %v cap to be reached",
			truncateAttempts, last, truncateRetryMaxDelay)
	}
}

// TestTruncateRetryDelayJittersFirstAttempt proves the first retry is jittered
// rather than a constant 50 ms, so parallel resets do not retry in lockstep.
func TestTruncateRetryDelayJittersFirstAttempt(t *testing.T) {
	const samples = 1000
	minSeen, maxSeen := truncateRetryMaxDelay, time.Duration(0)
	for i := 0; i < samples; i++ {
		d := truncateRetryDelay(0)
		if d < minSeen {
			minSeen = d
		}
		if d > maxSeen {
			maxSeen = d
		}
	}
	if minSeen < truncateRetryMinDelay || maxSeen > truncateRetryWindow(0) {
		t.Fatalf("first-attempt delays span [%v, %v], want within [%v, %v]",
			minSeen, maxSeen, truncateRetryMinDelay, truncateRetryWindow(0))
	}
	if minSeen == maxSeen {
		t.Fatalf("truncateRetryDelay(0) returned the constant %v over %d samples; the first attempt has no jitter",
			minSeen, samples)
	}
}

// TestSleepWithContextCancellation exercises the real sleeper (no stub): a
// cancelled context must cut the wait short instead of sleeping out the full
// delay. Replacing sleepWithContext's select with a plain time.Sleep is exactly
// the regression this test exists to catch, and it makes this test block for the
// whole 5 s delay and return a nil error.
func TestSleepWithContextCancellation(t *testing.T) {
	const delay = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := sleepWithContext(ctx, delay)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepWithContext() error = %v, want context.Canceled", err)
	}
	if elapsed >= delay/2 {
		t.Fatalf("sleepWithContext returned after %v; cancellation should cut the %v delay short", elapsed, delay)
	}
}

// TestSleepWithContextHonoursAlreadyCancelledContext covers the early return:
// a context that is already done must not start a timer at all.
func TestSleepWithContextHonoursAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := sleepWithContext(ctx, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepWithContext() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("already-cancelled sleepWithContext took %v, want an immediate return", elapsed)
	}
}
