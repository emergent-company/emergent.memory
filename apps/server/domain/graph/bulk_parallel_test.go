package graph

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// =============================================================================
// Bounded worker pool: the concurrency bound is a hard invariant
// =============================================================================

// TestRunBulkItems_BoundedConcurrency pins the two properties the bulk
// endpoints rely on: never more than `concurrency` items in flight, and the
// pool actually runs that many concurrently (i.e. it is not secretly serial).
// If the bound is removed (workers spawned per item), the "extra item arrived"
// select fires and fails the test.
func TestRunBulkItems_BoundedConcurrency(t *testing.T) {
	const (
		n       = 12
		workers = 3
	)

	arrived := make(chan int, n)
	release := make(chan struct{})
	done := make(chan struct{})

	var inFlight, maxInFlight, visited int32
	go func() {
		runBulkItems(n, workers, func(i int) {
			cur := atomic.AddInt32(&inFlight, 1)
			for {
				m := atomic.LoadInt32(&maxInFlight)
				if cur <= m || atomic.CompareAndSwapInt32(&maxInFlight, m, cur) {
					break
				}
			}
			atomic.AddInt32(&visited, 1)
			arrived <- i // announce this item's presence
			<-release    // hold the slot until the test releases it
			atomic.AddInt32(&inFlight, -1)
		})
		close(done)
	}()

	// Exactly `workers` items must be able to sit in flight simultaneously.
	for k := 0; k < workers; k++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("pool did not reach its configured concurrency of %d", workers)
		}
	}
	// ...and a further item must NOT have started while those were blocked.
	select {
	case extra := <-arrived:
		t.Fatalf("item %d started while %d items were in flight; bound of %d violated", extra, workers, workers)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runBulkItems did not return after release")
	}

	assert.Equal(t, int32(workers), maxInFlight, "pool must reach, but never exceed, its bound")
	assert.Equal(t, int32(n), visited, "every index must be visited exactly once")
}

// TestRunBulkItems_ClampsToItemCount covers the small-batch edge: concurrency
// larger than n must not spawn idle workers or drop/duplicate an index.
func TestRunBulkItems_ClampsToItemCount(t *testing.T) {
	var visited int32
	runBulkItems(2, 64, func(i int) { atomic.AddInt32(&visited, 1) })
	assert.Equal(t, int32(2), visited)
}

// TestEffectiveBulkConcurrency_Clamped pins the DB_MAX_OPEN_CONNS clamp
// (issue #1482; spec scenario "Concurrency never exceeds the pool"): a
// configured worker count above the connection pool is lowered to leave one
// connection of headroom, a non-positive value falls back to the default, and
// the effective count is never below one. Mutating the clamp out (removing the
// `c > limit` branch) makes the "above pool" case fail.
func TestEffectiveBulkConcurrency_Clamped(t *testing.T) {
	cases := []struct {
		name         string
		configured   int
		maxOpenConns int
		want         int
	}{
		{"default when unset", 0, 100, defaultBulkConcurrency},
		{"below pool keeps configured", 4, 100, 4},
		{"above pool clamps to pool minus headroom", 200, 100, 99},
		{"default clamps to small pool minus headroom", 0, 3, 2},
		{"pool of one still allows one", 8, 1, 1},
		{"negative falls back to default", -5, 100, defaultBulkConcurrency},
		{"unbounded pool keeps default", 0, 0, defaultBulkConcurrency},
		{"unbounded pool keeps configured", 12, 0, 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, effectiveBulkConcurrency(tc.configured, tc.maxOpenConns))
		})
	}
}

// =============================================================================
// DB-backed parallel bulk tests
// =============================================================================

func openBulkTestDBTB(tb testing.TB) *bun.DB {
	tb.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(tb, "integration test requires database")
	}
	tdb := testdb.SetupTestDBOrFail(tb, context.Background(), "graph_bulk_parallel")
	tb.Cleanup(tdb.Close)
	return tdb.DB
}

func newBulkTestRepoConc(tb testing.TB, db *bun.DB, concurrency int) *Repository {
	tb.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	cfg.Graph.BulkConcurrency = concurrency
	return NewRepository(db, log, cfg)
}

func seedProjectTB(tb testing.TB, db *bun.DB, projectID uuid.UUID) {
	tb.Helper()
	orgID := uuid.New()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'test-org', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, orgID)
	require.NoError(tb, err)
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO kb.projects (id, organization_id, name, created_at, updated_at)
		VALUES (?, ?, 'test-project', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, projectID, orgID)
	require.NoError(tb, err)
}

func insertRawObjectTB(tb testing.TB, db *bun.DB, projectID uuid.UUID, objType, key string) uuid.UUID {
	tb.Helper()
	seedProjectTB(tb, db, projectID)
	id := uuid.New()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, content_hash, created_at, updated_at)
		VALUES
			(?, ?, NULL, ?, NULL, 1, ?, ?, 'active',
			 '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())
	`, id, projectID, id, objType, key, "hash-"+id.String())
	require.NoError(tb, err)
	return id
}

// objectVersionChain is the persisted shape of one object's version history,
// used to assert the supersedes chain and single-HEAD invariant.
type objectVersionChain struct {
	HeadID          uuid.UUID  `bun:"head_id"`
	HeadVersion     int        `bun:"head_version"`
	HeadContentHash []byte     `bun:"head_content_hash"`
	HeadCount       int        `bun:"head_count"`
	SupersededByID  *uuid.UUID `bun:"superseded_by_id"`
}

func loadVersionChain(t *testing.T, db *bun.DB, canonicalID uuid.UUID) objectVersionChain {
	t.Helper()
	var chain objectVersionChain
	err := db.NewRaw(`
		SELECT
			(SELECT id FROM kb.graph_objects WHERE canonical_id = ? AND supersedes_id IS NULL) AS head_id,
			(SELECT version FROM kb.graph_objects WHERE canonical_id = ? AND supersedes_id IS NULL) AS head_version,
			(SELECT content_hash FROM kb.graph_objects WHERE canonical_id = ? AND supersedes_id IS NULL) AS head_content_hash,
			(SELECT count(*) FROM kb.graph_objects WHERE canonical_id = ? AND supersedes_id IS NULL) AS head_count,
			(SELECT supersedes_id FROM kb.graph_objects WHERE canonical_id = ? AND supersedes_id IS NOT NULL ORDER BY version DESC LIMIT 1) AS superseded_by_id
	`, canonicalID, canonicalID, canonicalID, canonicalID, canonicalID).Scan(context.Background(), &chain)
	require.NoError(t, err)
	return chain
}

// TestBulkUpdateObjects_Parallel_OneHeadPerObjectAndChain is the correctness
// invariant for the parallel path: after a many-item batch, every object has
// exactly one HEAD, the supersede link points old → new, ContentHash is
// populated, and results keep request order and index→id pairing.
func TestBulkUpdateObjects_Parallel_OneHeadPerObjectAndChain(t *testing.T) {
	db := openBulkTestDBTB(t)
	repo := newBulkTestRepoConc(t, db, 8)
	svc := NewService(repo, repo.log, nil, nil, nil, nil, nil, NoopEventSink{}, nil, nil)
	ctx := context.Background()
	projectID := uuid.New()
	seedProjectTB(t, db, projectID)

	const n = 25
	ids := make([]uuid.UUID, n)
	items := make([]BulkUpdateObjectItem, n)
	for i := range ids {
		ids[i] = insertRawObjectTB(t, db, projectID, "Thing", fmt.Sprintf("par-%d", i))
		items[i] = BulkUpdateObjectItem{
			ID:         ids[i].String(),
			Properties: map[string]any{"section_label": fmt.Sprintf("L%d", i)},
		}
	}

	resp, err := svc.BulkUpdateObjects(ctx, projectID, &BulkUpdateObjectsRequest{Items: items}, nil)
	require.NoError(t, err)
	require.Equal(t, n, resp.Success)
	require.Equal(t, 0, resp.Failed)
	require.Len(t, resp.Results, n)

	for i := range ids {
		res := resp.Results[i]
		assert.Equal(t, i, res.Index, "result order must follow request order")
		assert.Equal(t, ids[i].String(), res.ID, "result id must match request position")
		require.NotNil(t, res.Object)
		assert.Equal(t, ids[i], res.Object.CanonicalID)
		assert.Equal(t, 2, res.Object.Version, "each object must advance to version 2")

		chain := loadVersionChain(t, db, ids[i])
		assert.Equal(t, 1, chain.HeadCount, "object %d must have exactly one HEAD", i)
		assert.Equal(t, 2, chain.HeadVersion)
		assert.NotEmpty(t, chain.HeadContentHash, "new HEAD must carry a ContentHash")
		require.NotNil(t, chain.SupersededByID, "old version must be superseded")
		assert.Equal(t, chain.HeadID, *chain.SupersededByID, "supersede link must point at the new HEAD")
	}
}

// TestBulkUpdateObjects_Parallel_TwoItemsSameObjectSerializes proves that two
// items touching the same object from different workers still produce a linear
// version chain (the per-object advisory lock serializes them) and never two
// HEADs.
func TestBulkUpdateObjects_Parallel_TwoItemsSameObjectSerializes(t *testing.T) {
	db := openBulkTestDBTB(t)
	repo := newBulkTestRepoConc(t, db, 8)
	svc := NewService(repo, repo.log, nil, nil, nil, nil, nil, NoopEventSink{}, nil, nil)
	ctx := context.Background()
	projectID := uuid.New()
	seedProjectTB(t, db, projectID)

	id := insertRawObjectTB(t, db, projectID, "Thing", "dup-1")
	resp, err := svc.BulkUpdateObjects(ctx, projectID, &BulkUpdateObjectsRequest{Items: []BulkUpdateObjectItem{
		{ID: id.String(), Properties: map[string]any{"a": "1"}},
		{ID: id.String(), Properties: map[string]any{"b": "2"}},
	}}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, resp.Success)
	require.Equal(t, 0, resp.Failed)

	chain := loadVersionChain(t, db, id)
	assert.Equal(t, 1, chain.HeadCount, "duplicate-target items must not create two HEADs")
	assert.Equal(t, 3, chain.HeadVersion, "duplicate-target items must serialize into a linear chain")

	var rows int
	err = db.NewRaw(`SELECT count(*) FROM kb.graph_objects WHERE canonical_id = ?`, id).Scan(ctx, &rows)
	require.NoError(t, err)
	assert.Equal(t, 3, rows, "three versions: v1 + two updates")
}

// TestBulkUpdateObjects_Parallel_PerItemIsolation keeps the partial-success
// contract under the pool: an invalid id fails only its own item.
func TestBulkUpdateObjects_Parallel_PerItemIsolation(t *testing.T) {
	db := openBulkTestDBTB(t)
	repo := newBulkTestRepoConc(t, db, 8)
	svc := NewService(repo, repo.log, nil, nil, nil, nil, nil, NoopEventSink{}, nil, nil)
	ctx := context.Background()
	projectID := uuid.New()
	seedProjectTB(t, db, projectID)

	ok := insertRawObjectTB(t, db, projectID, "Thing", "iso-1")
	resp, err := svc.BulkUpdateObjects(ctx, projectID, &BulkUpdateObjectsRequest{Items: []BulkUpdateObjectItem{
		{ID: ok.String(), Properties: map[string]any{"x": "1"}},
		{ID: "not-a-uuid", Properties: map[string]any{"x": "2"}},
		{ID: uuid.NewString(), Properties: map[string]any{"x": "3"}}, // valid uuid, missing object
	}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, resp.Success)
	require.Equal(t, 2, resp.Failed)
	require.Len(t, resp.Results, 3)
	assert.True(t, resp.Results[0].Success)
	assert.False(t, resp.Results[1].Success)
	require.NotNil(t, resp.Results[1].Error)
	assert.False(t, resp.Results[2].Success)
	require.NotNil(t, resp.Results[2].Error)
	for i := range resp.Results {
		assert.Equal(t, i, resp.Results[i].Index)
	}
}

// TestBulkUpsertObjects_Parallel_OneHeadPerKey exercises the same bounded pool
// through CreateOrUpdate: N create-then-update upserts keep one HEAD per key.
func TestBulkUpsertObjects_Parallel_OneHeadPerKey(t *testing.T) {
	db := openBulkTestDBTB(t)
	repo := newBulkTestRepoConc(t, db, 8)
	svc := NewService(repo, repo.log, nil, nil, nil, nil, nil, NoopEventSink{}, nil, nil)
	ctx := context.Background()
	projectID := uuid.New()
	seedProjectTB(t, db, projectID)

	const n = 20
	keys := make([]string, n)
	create := make([]CreateGraphObjectRequest, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("up-%d", i)
		create[i] = CreateGraphObjectRequest{Type: "Thing", Key: &keys[i], Properties: map[string]any{"name": "one"}}
	}

	first, err := svc.BulkUpsertObjects(ctx, projectID, &BulkUpsertObjectsRequest{Items: create}, nil)
	require.NoError(t, err)
	require.Equal(t, n, first.Success)
	require.Equal(t, 0, first.Failed)

	update := make([]CreateGraphObjectRequest, n)
	for i := range keys {
		update[i] = CreateGraphObjectRequest{Type: "Thing", Key: &keys[i], Properties: map[string]any{"name": "two"}}
	}
	second, err := svc.BulkUpsertObjects(ctx, projectID, &BulkUpsertObjectsRequest{Items: update}, nil)
	require.NoError(t, err)
	require.Equal(t, n, second.Success)
	require.Equal(t, 0, second.Failed)

	for i := range keys {
		require.NotNil(t, second.Results[i].Object)
		assert.Equal(t, 2, second.Results[i].Object.Version, "key %s must advance to version 2", keys[i])
		assert.False(t, second.Results[i].Created)

		var heads int
		err = db.NewRaw(`
			SELECT count(*) FROM kb.graph_objects
			WHERE project_id = ? AND type = 'Thing' AND key = ? AND supersedes_id IS NULL`,
			projectID, keys[i]).Scan(ctx, &heads)
		require.NoError(t, err)
		assert.Equal(t, 1, heads, "key %s must have exactly one HEAD", keys[i])
	}
}

// =============================================================================
// Benchmark: serial vs bounded-parallel bulk update
// =============================================================================

// BenchmarkBulkUpdateObjects_SerialVsParallel runs the same 32-item bulk update
// at concurrency 1 and 8 against a real database. The per-item path is I/O
// bound (identity read + version INSERT + supersedes UPDATE), so the parallel
// sub-benchmark should be substantially faster in wall time while producing the
// same result set.
func BenchmarkBulkUpdateObjects_SerialVsParallel(b *testing.B) {
	db := openBulkTestDBTB(b)
	for _, concurrency := range []int{1, 8} {
		b.Run(fmt.Sprintf("concurrency=%d", concurrency), func(b *testing.B) {
			repo := newBulkTestRepoConc(b, db, concurrency)
			svc := NewService(repo, repo.log, nil, nil, nil, nil, nil, NoopEventSink{}, nil, nil)
			projectID := uuid.New()
			seedProjectTB(b, db, projectID)

			const n = 32
			ids := make([]uuid.UUID, n)
			for i := range ids {
				ids[i] = insertRawObjectTB(b, db, projectID, "Thing", fmt.Sprintf("bench-%d", i))
			}

			b.ReportAllocs()
			b.ResetTimer()
			for it := 0; it < b.N; it++ {
				items := make([]BulkUpdateObjectItem, n)
				for i := range ids {
					items[i] = BulkUpdateObjectItem{
						ID:         ids[i].String(),
						Properties: map[string]any{"section_label": fmt.Sprintf("L-%d-%d", it, i)},
					}
				}
				resp, err := svc.BulkUpdateObjects(context.Background(), projectID, &BulkUpdateObjectsRequest{Items: items}, nil)
				if err != nil {
					b.Fatal(err)
				}
				if resp.Success != n {
					b.Fatalf("expected %d successes, got %d (failed=%d)", n, resp.Success, resp.Failed)
				}
			}
		})
	}
}
