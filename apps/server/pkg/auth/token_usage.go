package auth

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// lastUsedThrottle is the minimum interval between last_used_at writes for a
// single token. It bounds the write frequency on the hot auth path: at most one
// UPDATE per token per lastUsedThrottle, regardless of how often the token is
// used. The stored timestamp is therefore coarse (minute-grained) — readers of
// core.api_tokens.last_used_at MUST treat it as such, not as an exact per-request
// timestamp.
const lastUsedThrottle = time.Minute

// tokenUsageTracker records that an emt_* API token was used by writing
// core.api_tokens.last_used_at, coalesced by a per-token throttle so the hot
// auth path never performs — or blocks on — a synchronous UPDATE.
//
// Semantics (documented in the api-token-audit capability):
//   - "last used" means "last seen on a successful validation" (the token row
//     was live, non-revoked, non-expired and within any scope ceiling), not
//     "last authorized action".
//   - Resolution is coarse: the stored timestamp is the time of a flush, which
//     is within lastUsedThrottle of the true last use.
//   - Write-frequency bound: at most one write per token per lastUsedThrottle.
//     Writes are fire-and-forget on a detached goroutine with a short timeout;
//     a failed write is logged at debug and never surfaced to the caller.
type tokenUsageTracker struct {
	db       bun.IDB
	log      *slog.Logger
	throttle time.Duration

	mu        sync.Mutex
	lastFlush map[string]time.Time // tokenID -> time the last flush was enqueued

	// now and flushFn are test seams. flushFn defaults to the real UPDATE.
	now     func() time.Time
	flushFn func(ctx context.Context, tokenID string) error
}

// newTokenUsageTracker builds a tracker backed by db. It is used by
// NewMiddleware; a nil db tracker simply never enqueues a flush (unit-test and
// standalone postures).
func newTokenUsageTracker(db bun.IDB, log *slog.Logger) *tokenUsageTracker {
	return &tokenUsageTracker{
		db:        db,
		log:       log,
		throttle:  lastUsedThrottle,
		lastFlush: make(map[string]time.Time),
		now:       time.Now,
	}
}

// touch records that tokenID was used, throttled to at most one flush per token
// per throttle window. It is best-effort and non-blocking: it never returns an
// error and the write happens on a detached goroutine.
func (t *tokenUsageTracker) touch(tokenID string) {
	if t == nil || tokenID == "" {
		return
	}
	if !t.shouldFlush(tokenID) {
		return
	}
	t.flush(tokenID)
}

// shouldFlush reports whether a flush for tokenID is due (first use, or the
// throttle interval has elapsed since the last flush). It records the enqueue
// time as a side effect when it returns true, so the throttle is enforced by
// the decision itself and the async flush cannot be re-entered within the
// window.
func (t *tokenUsageTracker) shouldFlush(tokenID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.lastFlush[tokenID]; ok && t.now().Sub(last) < t.throttle {
		return false
	}
	t.lastFlush[tokenID] = t.now()
	return true
}

// flush writes last_used_at for tokenID on a detached goroutine. A failed write
// is logged at debug and never surfaced; the goroutine is bounded by a short
// timeout so a slow store cannot leak goroutines.
func (t *tokenUsageTracker) flush(tokenID string) {
	if t.db == nil && t.flushFn == nil {
		return
	}
	fn := t.flushFn
	if fn == nil {
		fn = t.defaultFlush
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := fn(ctx, tokenID); err != nil {
			t.log.Debug("last_used_at touch failed", slog.String("token_id", tokenID), logger.Error(err))
		}
	}()
}

// defaultFlush is the real write: a single UPDATE keyed by token id.
func (t *tokenUsageTracker) defaultFlush(ctx context.Context, tokenID string) error {
	_, err := t.db.NewUpdate().
		Table("core.api_tokens").
		Set("last_used_at = NOW()").
		Where("id = ?", tokenID).
		Exec(ctx)
	return err
}
