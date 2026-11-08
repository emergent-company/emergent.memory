package feedback

import (
	"sync"
	"time"
)

// submissionGuard applies the per-user rate limit.
//
// The state is in memory. That is deliberate: a feedback submission is a rare,
// interactive action, and a per-instance budget is enough to stop an agent
// retry loop from filing hundreds of issues. A multi-replica deployment can
// therefore admit up to limit*replicas submissions per user per hour, which is
// acceptable for this feature (the GitHub token itself is the hard backstop).
//
// Duplicate suppression no longer lives here: it is the database claim in
// claim.go, because an in-memory read-then-write was not atomic across
// concurrent callers (issue #472).
type submissionGuard struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	now     func() time.Time
	entries map[string]*guardEntry
	// maxEntries is the point at which the map is swept and idle identities are
	// evicted. It is a trigger, not a hard bound: an identity that holds a live
	// budget slot is never evicted (see sweepLocked).
	maxEntries int
	// sweepInterval throttles the O(n) sweep while the map is over maxEntries,
	// so a flood of distinct identities cannot turn every insert into a full
	// scan.
	sweepInterval time.Duration
	lastSweep     time.Time
}

type guardEntry struct {
	hits     []time.Time
	lastSeen time.Time
}

func newSubmissionGuard(limit int, now func() time.Time) *submissionGuard {
	if limit <= 0 {
		limit = DefaultRateLimitPerHour
	}
	if now == nil {
		now = time.Now
	}
	return &submissionGuard{
		limit:         limit,
		window:        time.Hour,
		now:           now,
		entries:       make(map[string]*guardEntry),
		maxEntries:    10_000,
		sweepInterval: time.Minute,
	}
}

// reserve records one submission attempt for identity and reports how long the
// caller must wait when the rolling-hour budget is exhausted. A zero duration
// means the attempt is allowed.
func (g *submissionGuard) reserve(identity string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	e := g.entryLocked(identity, now)

	e.hits = liveHits(e.hits, now, g.window)
	if len(e.hits) >= g.limit {
		return g.window - now.Sub(e.hits[0])
	}
	e.hits = append(e.hits, now)
	return 0
}

func liveHits(hits []time.Time, now time.Time, window time.Duration) []time.Time {
	keep := hits[:0]
	for _, hit := range hits {
		if now.Sub(hit) < window {
			keep = append(keep, hit)
		}
	}
	return keep
}

func (g *submissionGuard) entryLocked(identity string, now time.Time) *guardEntry {
	if e, ok := g.entries[identity]; ok {
		e.lastSeen = now
		return e
	}
	if len(g.entries) >= g.maxEntries &&
		(g.lastSweep.IsZero() || now.Sub(g.lastSweep) >= g.sweepInterval) {
		g.sweepLocked(now)
		g.lastSweep = now
	}
	e := &guardEntry{lastSeen: now}
	g.entries[identity] = e
	return e
}

// sweepLocked prunes expired hits and drops identities that are idle for a full
// window. If the map is still at capacity it evicts the least recently seen
// IDLE identity.
//
// It never evicts an identity with a live hit: doing so would refund that
// user's budget and let a rate-limited caller regain it by flooding the map with
// other identities (issue #472). When every remaining identity holds a live
// hit, none is evicted and the map is allowed to exceed maxEntries. Memory is
// then bounded by the number of distinct identities that were accepted for
// submission in the rolling hour: each entry is bought with a real submit (and
// most of those file a GitHub issue), all live entries expire within the hour,
// and the GitHub token rate limit caps that throughput. The hard in-memory bound
// is deliberately traded for the correctness of the rate limit.
func (g *submissionGuard) sweepLocked(now time.Time) {
	for identity, e := range g.entries {
		e.hits = liveHits(e.hits, now, g.window)
		if len(e.hits) == 0 && now.Sub(e.lastSeen) >= g.window {
			delete(g.entries, identity)
		}
	}
	if len(g.entries) < g.maxEntries {
		return
	}

	var oldest string
	var oldestSeen time.Time
	for identity, e := range g.entries {
		if len(e.hits) > 0 {
			continue
		}
		if oldest == "" || e.lastSeen.Before(oldestSeen) {
			oldest, oldestSeen = identity, e.lastSeen
		}
	}
	if oldest != "" {
		delete(g.entries, oldest)
	}
}
