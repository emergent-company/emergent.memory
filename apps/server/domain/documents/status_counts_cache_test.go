package documents

import (
	"testing"
	"time"
)

func TestStatusCountsCacheTTL(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cache := newStatusCountsCache(30 * time.Second)
	cache.now = func() time.Time { return now }

	params := ListParams{ProjectID: "p1", Query: "report"}
	key := newStatusCountsKey(params)
	want := StatusCounts{All: 10, Ready: 7, Failed: 3}
	cache.put(key, want)

	got, ok := cache.get(key)
	if !ok || got != want {
		t.Fatalf("get within TTL = %+v, %v; want %+v, true", got, ok, want)
	}

	now = now.Add(29 * time.Second)
	if _, ok := cache.get(key); !ok {
		t.Fatal("entry expired before the TTL")
	}

	now = now.Add(2 * time.Second)
	if _, ok := cache.get(key); ok {
		t.Fatal("entry survived past the TTL")
	}
}

func TestStatusCountsKeyScope(t *testing.T) {
	source := "drive"
	parent := "doc-parent"
	base := ListParams{ProjectID: "p1", Query: "report"}
	withSource := base
	withSource.SourceType = &source
	withParent := base
	withParent.ParentDocumentID = &parent
	otherProject := base
	otherProject.ProjectID = "p2"
	otherQuery := base
	otherQuery.Query = "other"
	rootOnly := base
	rootOnly.RootOnly = true
	includeDeleted := base
	includeDeleted.IncludeDeletedFromSource = true
	sourceID := "0190f2b0-0000-7000-8000-000000000001"
	withSourceID := base
	withSourceID.SourceID = &sourceID
	withUnassigned := base
	withUnassigned.SourceUnassigned = true

	keys := []statusCountsKey{
		newStatusCountsKey(base),
		newStatusCountsKey(withSource),
		newStatusCountsKey(withSourceID),
		newStatusCountsKey(withUnassigned),
		newStatusCountsKey(withParent),
		newStatusCountsKey(otherProject),
		newStatusCountsKey(otherQuery),
		newStatusCountsKey(rootOnly),
		newStatusCountsKey(includeDeleted),
	}
	seen := make(map[statusCountsKey]bool, len(keys))
	for i, key := range keys {
		if seen[key] {
			t.Fatalf("key %d collides with an earlier filter combination: %+v", i, key)
		}
		seen[key] = true
	}

	// The status filter is deliberately not part of the key: the breakdown
	// ignores it by contract, so switching tabs must reuse the same entry.
	withStatus := base
	withStatus.Status = "ready"
	if newStatusCountsKey(withStatus) != newStatusCountsKey(base) {
		t.Error("status filter must not affect the breakdown cache key")
	}
	// Sort/order/page size do not affect the breakdown either.
	paged := base
	paged.Sort = SortUpdated
	paged.Order = OrderAsc
	paged.Limit = 10
	paged.Cursor = &Cursor{Sort: SortUpdated, Value: time.Now(), ID: "x"}
	if newStatusCountsKey(paged) != newStatusCountsKey(base) {
		t.Error("page/sort must not affect the breakdown cache key")
	}
}

func TestStatusCountsCacheIsBounded(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cache := newStatusCountsCache(time.Minute)
	cache.now = func() time.Time { return now }
	cache.maxEntries = 2

	cache.put(statusCountsKey{ProjectID: "p", Query: "a"}, StatusCounts{All: 1})
	now = now.Add(time.Second)
	cache.put(statusCountsKey{ProjectID: "p", Query: "b"}, StatusCounts{All: 2})
	now = now.Add(time.Second)
	cache.put(statusCountsKey{ProjectID: "p", Query: "c"}, StatusCounts{All: 3})

	cache.mu.Lock()
	n := len(cache.entries)
	cache.mu.Unlock()
	if n > cache.maxEntries {
		t.Fatalf("cache holds %d entries, want <= %d", n, cache.maxEntries)
	}
	if _, ok := cache.get(statusCountsKey{ProjectID: "p", Query: "c"}); !ok {
		t.Error("the newest entry must survive eviction")
	}
}
