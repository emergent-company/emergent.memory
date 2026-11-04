package auth

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// activityThrottle is the minimum interval between activity flushes for a
// single (user, project) pair. It bounds the write frequency on the hot auth
// path: at most one upsert per pair per interval, regardless of request rate.
// The stored counters are therefore coarse aggregates, not exact request
// counts — readers must treat them as directional, not billing-grade.
const activityThrottle = time.Minute

// activityTracker records that a user was seen on an authenticated request by
// upserting today's bucket in kb.user_activity_daily and touching
// core.user_profiles.last_activity_at. It is the write side of the
// member-activity capability.
//
// Semantics:
//   - "last seen" means "last successful authentication", not "last authorized
//     action" — the middleware has not yet run handler-level authorization.
//   - The project/organization pair is only recorded when the request carried a
//     project that the middleware resolved to an owning organization, so a
//     caller cannot attribute activity to an arbitrary project by sending a
//     forged X-Project-ID header.
//   - Writes are fire-and-forget on a detached goroutine with a short timeout;
//     a failed write is logged at debug and never surfaced to the caller.
type activityTracker struct {
	db       bun.IDB
	log      *slog.Logger
	throttle time.Duration

	mu        sync.Mutex
	lastFlush map[string]time.Time

	// now and flushFn are test seams. flushFn defaults to the real upsert.
	now     func() time.Time
	flushFn func(ctx context.Context, userID, orgID, projectID string) error
}

// newActivityTracker builds a tracker backed by db. A nil db tracker never
// enqueues a flush (unit-test and standalone postures).
func newActivityTracker(db bun.IDB, log *slog.Logger) *activityTracker {
	return &activityTracker{
		db:        db,
		log:       log,
		throttle:  activityThrottle,
		lastFlush: make(map[string]time.Time),
		now:       time.Now,
	}
}

// touch records that userID was seen, throttled to one flush per
// (user, project) per throttle window. Non-blocking and best-effort.
func (t *activityTracker) touch(userID, orgID, projectID string) {
	if t == nil || userID == "" {
		return
	}
	key := userID + "|" + orgID + "|" + projectID
	if !t.shouldFlush(key) {
		return
	}
	t.flush(userID, orgID, projectID)
}

func (t *activityTracker) shouldFlush(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.lastFlush[key]; ok && t.now().Sub(last) < t.throttle {
		return false
	}
	t.lastFlush[key] = t.now()
	return true
}

func (t *activityTracker) flush(userID, orgID, projectID string) {
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
		if err := fn(ctx, userID, orgID, projectID); err != nil {
			t.log.Debug("activity touch failed",
				slog.String("user_id", userID),
				slog.String("project_id", projectID),
				logger.Error(err))
		}
	}()
}

// defaultFlush upserts today's request bucket and the user's global last-seen
// timestamp. The bucket id is deterministic over the key so repeated flushes,
// the nightly rollup, and the backfill all converge on the same row.
func (t *activityTracker) defaultFlush(ctx context.Context, userID, orgID, projectID string) error {
	const upsert = `
INSERT INTO kb.user_activity_daily AS a
    (id, day, user_id, organization_id, project_id, request_count, last_seen_at)
SELECT md5(concat(
           (now() AT TIME ZONE 'utc')::date::text, '|', up.id::text, '|',
           coalesce(NULLIF(?::text, '')::uuid::text, ''), '|',
           coalesce(NULLIF(?::text, '')::uuid::text, '')
       ))::uuid,
       (now() AT TIME ZONE 'utc')::date,
       up.id,
       NULLIF(?::text, '')::uuid,
       NULLIF(?::text, '')::uuid,
       1,
       now()
FROM core.user_profiles AS up
WHERE up.id = ?::uuid
  AND (
      NULLIF(?::text, '') IS NULL
      OR EXISTS (SELECT 1 FROM kb.projects p WHERE p.id = NULLIF(?::text, '')::uuid)
  )
ON CONFLICT (id) DO UPDATE
SET request_count = a.request_count + 1,
    last_seen_at  = GREATEST(a.last_seen_at, now()),
    updated_at    = now()`

	if _, err := t.db.NewRaw(upsert, orgID, projectID, orgID, projectID, userID, projectID, projectID).Exec(ctx); err != nil {
		return err
	}

	_, err := t.db.NewRaw(
		`UPDATE core.user_profiles SET last_activity_at = now() WHERE id = ?::uuid`,
		userID,
	).Exec(ctx)
	return err
}
