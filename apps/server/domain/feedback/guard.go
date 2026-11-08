package feedback

import (
	"sync"
	"time"
)

// submissionGuard applies the per-user rate limit and duplicate suppression.
//
// The state is in memory. That is deliberate: a feedback submission is a rare,
// interactive action, and a per-instance budget is enough to stop an agent
// retry loop from filing hundreds of issues. A multi-replica deployment can
// therefore admit up to limit*replicas submissions per user per hour, which is
// acceptable for this feature (the GitHub token itself is the hard backstop).
type submissionGuard struct {
	mu           sync.Mutex
	limit        int
	window       time.Duration
	dedupeWindow time.Duration
	now          func() time.Time
	entries      map[string]*guardEntry
	// maxEntries bounds the map on a hostile workload. Sweeping expired
	// entries is the first response; eviction of the least recently seen
	// identity is the fallback.
	maxEntries int
}

type guardEntry struct {
	hits     []time.Time
	results  map[string]guardResult
	lastSeen time.Time
}

type guardResult struct {
	result *Result
	at     time.Time
}

func newSubmissionGuard(limit int, dedupeWindow time.Duration, now func() time.Time) *submissionGuard {
	if limit <= 0 {
		limit = DefaultRateLimitPerHour
	}
	if now == nil {
		now = time.Now
	}
	return &submissionGuard{
		limit:        limit,
		window:       time.Hour,
		dedupeWindow: dedupeWindow,
		now:          now,
		entries:      make(map[string]*guardEntry),
		maxEntries:   10_000,
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

	// Drop hits that left the rolling window.
	keep := e.hits[:0]
	for _, hit := range e.hits {
		if now.Sub(hit) < g.window {
			keep = append(keep, hit)
		}
	}
	e.hits = keep

	if len(e.hits) >= g.limit {
		return g.window - now.Sub(e.hits[0])
	}
	e.hits = append(e.hits, now)
	return 0
}

// lookupDuplicate returns the issue created by an identical submission within
// the dedupe window, or nil.
func (g *submissionGuard) lookupDuplicate(identity, fingerprint string) *Result {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	e, ok := g.entries[identity]
	if !ok {
		return nil
	}
	e.lastSeen = now
	prior, ok := e.results[fingerprint]
	if !ok {
		return nil
	}
	if now.Sub(prior.at) >= g.dedupeWindow {
		delete(e.results, fingerprint)
		return nil
	}
	copied := *prior.result
	return &copied
}

// rememberResult records a created issue so an immediate retry is answered with
// the same issue instead of a duplicate.
func (g *submissionGuard) rememberResult(identity, fingerprint string, result *Result) {
	if result == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	e := g.entryLocked(identity, now)
	if e.results == nil {
		e.results = make(map[string]guardResult)
	}
	for key, prior := range e.results {
		if now.Sub(prior.at) >= g.dedupeWindow {
			delete(e.results, key)
		}
	}
	copied := *result
	e.results[fingerprint] = guardResult{result: &copied, at: now}
}

func (g *submissionGuard) entryLocked(identity string, now time.Time) *guardEntry {
	if e, ok := g.entries[identity]; ok {
		e.lastSeen = now
		return e
	}
	if len(g.entries) >= g.maxEntries {
		g.sweepLocked(now)
	}
	e := &guardEntry{lastSeen: now}
	g.entries[identity] = e
	return e
}

// sweepLocked discards identities with no live hits or dedupe entries. If the
// map is still at capacity, the least recently seen identity is evicted so the
// memory bound holds even under a flood of distinct identities.
func (g *submissionGuard) sweepLocked(now time.Time) {
	for identity, e := range g.entries {
		if len(e.results) == 0 && now.Sub(e.lastSeen) >= g.window {
			delete(g.entries, identity)
		}
	}
	if len(g.entries) < g.maxEntries {
		return
	}
	var oldest string
	var oldestSeen time.Time
	for identity, e := range g.entries {
		if oldest == "" || e.lastSeen.Before(oldestSeen) {
			oldest, oldestSeen = identity, e.lastSeen
		}
	}
	if oldest != "" {
		delete(g.entries, oldest)
	}
}
