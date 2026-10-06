package mcpregistry

import (
	"fmt"
	"sync"
	"time"
)

// registryCacheTTL bounds how long a successful registry browse result is
// reused before it is re-fetched from the (slow, degraded) upstream. 60s keeps
// repeated searches and pagination snappy while still picking up upstream
// changes reasonably quickly.
const registryCacheTTL = 60 * time.Second

// registryCache is a thread-safe, short-TTL, in-memory cache for successful
// registry browse results. It is keyed by the full request identity — search:
// query+limit+cursor; lookup: name — so identical or paginated queries are
// served without re-hitting the origin. Only successful results are cached;
// errors are never stored, so a transient upstream failure cannot become
// sticky. Cached values are pointers to freshly built DTOs and are treated as
// immutable by callers.
type registryCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	items map[string]registryCacheEntry
}

// registryCacheEntry is a cached value together with its expiry time.
type registryCacheEntry struct {
	value   any
	expires time.Time
}

// newRegistryCache builds a cache with the given TTL and clock. A TTL <= 0
// falls back to registryCacheTTL; a nil clock falls back to time.Now. The
// clock injection exists so tests can force expiry deterministically.
func newRegistryCache(ttl time.Duration, now func() time.Time) *registryCache {
	if ttl <= 0 {
		ttl = registryCacheTTL
	}
	if now == nil {
		now = time.Now
	}
	return &registryCache{
		ttl:   ttl,
		now:   now,
		items: make(map[string]registryCacheEntry),
	}
}

// get returns the cached value for key if present and unexpired. Expired
// entries are evicted on read.
func (c *registryCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if c.now().After(e.expires) {
		delete(c.items, key)
		return nil, false
	}
	return e.value, true
}

// set stores value for key until now+ttl.
func (c *registryCache) set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = registryCacheEntry{
		value:   value,
		expires: c.now().Add(c.ttl),
	}
}

// registrySearchKey builds the cache key for a search request. %q quoting keeps
// the identity unambiguous even when query/cursor contain separators.
func registrySearchKey(query string, limit int, cursor string) string {
	return fmt.Sprintf("search:%q:%d:%q", query, limit, cursor)
}

// registryLookupKey builds the cache key for a single-server lookup.
func registryLookupKey(name string) string {
	return fmt.Sprintf("lookup:%q", name)
}
