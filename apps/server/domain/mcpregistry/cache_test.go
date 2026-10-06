package mcpregistry

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const cacheSearchBody = `{"servers":[{"server":{"name":"io.github.github/github-mcp-server","description":"x","version":"1.0.0"}}],"metadata":{"count":1}}`

const cacheServerBody = `{"server":{"name":"io.github.github/github-mcp-server","description":"x","version":"1.0.0"}}`

func cacheSearchHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(cacheSearchBody))
}

func cacheServerHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(cacheServerBody))
}

// newCacheTestService builds a Service backed by an httptest server whose
// handler increments *calls before delegating to handler. The repository and
// MCP service are nil — the registry browse paths don't touch them.
func newCacheTestService(t *testing.T, handler http.HandlerFunc) (*Service, *int32) {
	t.Helper()
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(ts.Close)
	svc := NewService(nil, nil, NewRegistryClientWithBaseURL(ts.URL), nil, slog.Default())
	return svc, &calls
}

func TestRegistryCache_TTLExpiry(t *testing.T) {
	now := time.Now()
	c := newRegistryCache(time.Minute, func() time.Time { return now })

	c.set("k", "v")
	if got, ok := c.get("k"); !ok || got != "v" {
		t.Fatalf("get before expiry = (%v, %v), want (v, true)", got, ok)
	}

	now = now.Add(time.Minute + time.Second)
	if _, ok := c.get("k"); ok {
		t.Fatalf("get after expiry = true, want false")
	}
}

func TestRegistryCache_NilTTLFallsBack(t *testing.T) {
	c := newRegistryCache(0, nil)
	if c.ttl != registryCacheTTL {
		t.Fatalf("ttl = %v, want %v", c.ttl, registryCacheTTL)
	}
}

func TestRegistrySearchAndLookupKeysDistinct(t *testing.T) {
	// Search and lookup must never collide, even for identical raw strings.
	if registrySearchKey("x", 1, "") == registryLookupKey("x") {
		t.Fatalf("search and lookup keys must be distinct")
	}
	// Distinct request identities must produce distinct search keys.
	if registrySearchKey("a", 1, "") == registrySearchKey("a", 2, "") {
		t.Fatalf("different limits must produce distinct search keys")
	}
	if registrySearchKey("a", 1, "") == registrySearchKey("a", 1, "c1") {
		t.Fatalf("different cursors must produce distinct search keys")
	}
}

func TestService_SearchRegistry_CacheHit(t *testing.T) {
	svc, calls := newCacheTestService(t, cacheSearchHandler)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		res, err := svc.SearchRegistry(ctx, "github", 20, "")
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if len(res.Servers) != 1 {
			t.Fatalf("call %d: len(Servers) = %d, want 1", i, len(res.Servers))
		}
	}

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (second call should be a cache hit)", got)
	}
}

func TestService_SearchRegistry_DistinctRequestsCallUpstream(t *testing.T) {
	svc, calls := newCacheTestService(t, cacheSearchHandler)
	ctx := context.Background()

	if _, err := svc.SearchRegistry(ctx, "github", 20, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SearchRegistry(ctx, "other", 20, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SearchRegistry(ctx, "github", 20, "cursor1"); err != nil {
		t.Fatal(err)
	}

	if got := atomic.LoadInt32(calls); got != 3 {
		t.Fatalf("upstream calls = %d, want 3 (distinct identities must not share a cache entry)", got)
	}
}

func TestService_SearchRegistry_ErrorNotCached(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()
	svc := NewService(nil, nil, NewRegistryClientWithBaseURL(ts.URL), nil, slog.Default())
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := svc.SearchRegistry(ctx, "github", 20, ""); err == nil {
			t.Fatalf("call %d: expected error, got nil", i)
		}
	}

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 (errors must never be cached)", got)
	}
}

func TestService_SearchRegistry_ErrorNotSticky(t *testing.T) {
	var calls int32
	var failing int32 = 1
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if atomic.LoadInt32(&failing) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		cacheSearchHandler(w, r)
	}))
	defer ts.Close()
	svc := NewService(nil, nil, NewRegistryClientWithBaseURL(ts.URL), nil, slog.Default())
	ctx := context.Background()

	if _, err := svc.SearchRegistry(ctx, "github", 20, ""); err == nil {
		t.Fatal("first call: expected error, got nil")
	}

	atomic.StoreInt32(&failing, 0)
	res, err := svc.SearchRegistry(ctx, "github", 20, "")
	if err != nil {
		t.Fatalf("second call should succeed after upstream recovers: %v", err)
	}
	if len(res.Servers) != 1 {
		t.Fatalf("len(Servers) = %d, want 1", len(res.Servers))
	}

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 (transient error must not stick)", got)
	}
}

func TestService_GetRegistryServer_CacheHit(t *testing.T) {
	svc, calls := newCacheTestService(t, cacheServerHandler)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := svc.GetRegistryServer(ctx, "io.github.github/github-mcp-server"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (second call should be a cache hit)", got)
	}
}

func TestService_GetRegistryServer_NotFoundNotCached(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()
	svc := NewService(nil, nil, NewRegistryClientWithBaseURL(ts.URL), nil, slog.Default())
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := svc.GetRegistryServer(ctx, "io.github.github/does-not-exist"); err == nil {
			t.Fatalf("call %d: expected error, got nil", i)
		}
	}

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 (not-found must not be cached)", got)
	}
}
