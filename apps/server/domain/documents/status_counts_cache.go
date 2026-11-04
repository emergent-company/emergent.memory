package documents

import (
	"sync"
	"time"
)

// defaultStatusCountsTTL bounds how long the per-status breakdown may be stale.
// The breakdown is a filter chip, not a consistency-critical read: a document
// uploaded a moment ago may be missing from the numbers for up to this window
// (#2262). The listing rows themselves are never cached.
const defaultStatusCountsTTL = 30 * time.Second

// defaultStatusCountsMaxEntries bounds the map so arbitrary user search terms
// cannot grow it without limit in a long-lived process. The working set is one
// entry per active filter combination; 1024 is far above that.
const defaultStatusCountsMaxEntries = 1024

// statusCountsKey is the breakdown's cache identity. Status is deliberately
// absent: the breakdown ignores the status filter by contract, and the cursor,
// sort and page size do not affect it either.
type statusCountsKey struct {
	ProjectID                string
	SourceType               string
	SourceID                 string
	SourceUnassigned         bool
	ParentDocumentID         string
	Query                    string
	RootOnly                 bool
	IncludeDeletedFromSource bool
}

type statusCountsEntry struct {
	counts  StatusCounts
	expires time.Time
}

// statusCountsCache memoises the breakdown for one TTL. It is intentionally a
// plain mutex map: the values are small, the hit rate is one per page render,
// and a single documents service instance serves the whole process.
type statusCountsCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
	entries    map[statusCountsKey]statusCountsEntry
}

func newStatusCountsCache(ttl time.Duration) *statusCountsCache {
	return &statusCountsCache{
		ttl:        ttl,
		maxEntries: defaultStatusCountsMaxEntries,
		now:        time.Now,
		entries:    make(map[statusCountsKey]statusCountsEntry),
	}
}

func newStatusCountsKey(params ListParams) statusCountsKey {
	key := statusCountsKey{
		ProjectID:                params.ProjectID,
		Query:                    params.Query,
		RootOnly:                 params.RootOnly,
		IncludeDeletedFromSource: params.IncludeDeletedFromSource,
	}
	if params.SourceType != nil {
		key.SourceType = *params.SourceType
	}
	if params.SourceID != nil {
		key.SourceID = *params.SourceID
	}
	key.SourceUnassigned = params.SourceUnassigned
	if params.ParentDocumentID != nil {
		key.ParentDocumentID = *params.ParentDocumentID
	}
	return key
}

func (c *statusCountsCache) get(key statusCountsKey) (StatusCounts, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return StatusCounts{}, false
	}
	if !c.now().Before(entry.expires) {
		delete(c.entries, key)
		return StatusCounts{}, false
	}
	return entry.counts, true
}

func (c *statusCountsCache) put(key statusCountsKey, counts StatusCounts) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		c.evict(now)
	}
	c.entries[key] = statusCountsEntry{counts: counts, expires: now.Add(c.ttl)}
}

// evict drops expired entries, then the soonest-to-expire one if the map is
// still at the cap. Caller holds the lock.
func (c *statusCountsCache) evict(now time.Time) {
	for k, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) < c.maxEntries {
		return
	}
	var oldestKey statusCountsKey
	var oldestExpiry time.Time
	first := true
	for k, e := range c.entries {
		if first || e.expires.Before(oldestExpiry) {
			oldestKey, oldestExpiry, first = k, e.expires, false
		}
	}
	if !first {
		delete(c.entries, oldestKey)
	}
}
