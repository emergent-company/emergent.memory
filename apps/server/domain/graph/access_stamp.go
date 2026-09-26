package graph

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// accessStampWindow is how long a graph object's last_accessed_at write is
// suppressed after a prior write. Ranking consumes last_accessed_at at
// day-granularity (recency half-life / days-since-access), so collapsing the
// repeated search hits on one object into a single write per window is
// imperceptible to relevance while removing the wide-row rewrite that made a
// 15-row UPDATE take 8.5s (issue #1070).
const accessStampWindow = time.Minute

// accessStampDebouncer coalesces last_accessed_at writes so any single object is
// written at most once per accessStampWindow, regardless of how many searches
// return it in that window. Each UPDATE rewrites the full ~14 KB graph_objects
// tuple (MVCC), so deduplicating writes also bounds the bloat on the read path.
//
// It is intentionally best-effort and lossy: it reserves a write slot before the
// (already-async) UPDATE runs, so a failed write simply delays the next retry by
// one window — acceptable for access telemetry.
type accessStampDebouncer struct {
	mu     sync.Mutex
	now    func() time.Time
	window time.Duration
	last   map[uuid.UUID]time.Time
}

func newAccessStampDebouncer() *accessStampDebouncer {
	return newAccessStampDebouncerWithClock(time.Now, accessStampWindow)
}

// newAccessStampDebouncerWithClock is the injectable-clock constructor used by
// tests to exercise window expiry deterministically.
func newAccessStampDebouncerWithClock(now func() time.Time, window time.Duration) *accessStampDebouncer {
	return &accessStampDebouncer{now: now, window: window, last: make(map[uuid.UUID]time.Time)}
}

// filter returns the ids that have not been written within the window and
// reserves their slot now. It never mutates the input slice and never reorders.
func (d *accessStampDebouncer) filter(ids []uuid.UUID) []uuid.UUID {
	now := d.now()

	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if last, ok := d.last[id]; ok && now.Sub(last) < d.window {
			continue
		}
		d.last[id] = now
		out = append(out, id)
	}

	// Opportunistically prune expired entries to bound memory; the map holds one
	// entry per object seen recently, which is at most the search result fan-out.
	if len(d.last) > 1024 {
		for id, last := range d.last {
			if now.Sub(last) >= d.window {
				delete(d.last, id)
			}
		}
	}

	return out
}
