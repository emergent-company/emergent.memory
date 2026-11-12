package scheduler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// =============================================================================
// DB-backed behavior: only embedding-cache rows past the retention window go.
//
// The regression this guards (issue #684) is that kb.embedding_cache had no
// delete path at all, so old rows — the ones a model switch or the format-in-key
// change orphans — stayed forever. The task must prune exactly the aged rows and
// leave the fresh ones, while keeping the existing auth introspection behavior.
// =============================================================================

func TestCacheCleanupTask_PrunesOnlyOldEmbeddingRows(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "scheduler_cache_cleanup")
	defer tdb.Close()

	db := tdb.DB

	seedEmbedding := func(age time.Duration) string {
		t.Helper()
		key := "embed-" + uuid.New().String()
		_, err := db.NewRaw(
			`INSERT INTO kb.embedding_cache (cache_key, model_id, embedding, created_at)
			 VALUES (?, 'test-model', '[]'::jsonb, NOW() - ?::interval)`,
			key, age.String(),
		).Exec(ctx)
		require.NoError(t, err)
		return key
	}

	// The cleanup cutoff is "now - 30 days". 31 days is unambiguously stale;
	// 29 days and "just now" are unambiguously fresh, so clock skew inside the
	// run cannot flip a row across the boundary.
	stale := seedEmbedding(31 * 24 * time.Hour)
	fresh := seedEmbedding(29 * 24 * time.Hour)
	justWritten := seedEmbedding(0)

	expiredToken := "expired-" + uuid.New().String()[:8]
	_, err := db.NewRaw(`
		INSERT INTO kb.auth_introspection_cache (token_hash, introspection_data, expires_at)
		VALUES (?, '{}', NOW() - INTERVAL '1 hour')
	`, expiredToken).Exec(ctx)
	require.NoError(t, err)

	validToken := "valid-" + uuid.New().String()[:8]
	_, err = db.NewRaw(`
		INSERT INTO kb.auth_introspection_cache (token_hash, introspection_data, expires_at)
		VALUES (?, '{}', NOW() + INTERVAL '1 hour')
	`, validToken).Exec(ctx)
	require.NoError(t, err)

	task := NewCacheCleanupTask(db, slog.New(slog.NewTextHandler(io.Discard, nil)), 30)
	require.NoError(t, task.Run(ctx))

	countEmbedding := func(key string) int {
		t.Helper()
		var n int
		require.NoError(t, db.NewRaw(
			`SELECT COUNT(*) FROM kb.embedding_cache WHERE cache_key = ?`, key,
		).Scan(ctx, &n))
		return n
	}
	assert.Equal(t, 0, countEmbedding(stale), "a row older than the retention window must be pruned")
	assert.Equal(t, 1, countEmbedding(fresh), "a row inside the retention window must survive")
	assert.Equal(t, 1, countEmbedding(justWritten), "a just-written row must survive")

	countAuth := func(token string) int {
		t.Helper()
		var n int
		require.NoError(t, db.NewRaw(
			`SELECT COUNT(*) FROM kb.auth_introspection_cache WHERE token_hash = ?`, token,
		).Scan(ctx, &n))
		return n
	}
	assert.Equal(t, 0, countAuth(expiredToken), "expired auth cache entries are still deleted")
	assert.Equal(t, 1, countAuth(validToken), "live auth cache entries are still kept")
}

// =============================================================================
// DB-free driver: a failure in one prune must not skip the other.
//
// The two deletes are deliberately independent so one broken table is visible
// and cannot silently stop the other from being cleaned.
// =============================================================================

type cacheCleanupFakeDB struct {
	mu      sync.Mutex
	queries []string
	authErr bool
}

var (
	cacheCleanupRegistryMu sync.Mutex
	cacheCleanupRegistry   = map[string]*cacheCleanupFakeDB{}
	cacheCleanupRegister   sync.Once
)

func newCacheCleanupTask(t *testing.T, state *cacheCleanupFakeDB, retentionDays int) *CacheCleanupTask {
	t.Helper()
	dsn := t.Name()
	cacheCleanupRegister.Do(func() { sql.Register("fakecachecleanup", cacheCleanupFakeDriver{}) })

	cacheCleanupRegistryMu.Lock()
	cacheCleanupRegistry[dsn] = state
	cacheCleanupRegistryMu.Unlock()

	sqldb, err := sql.Open("fakecachecleanup", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	return NewCacheCleanupTask(bun.NewDB(sqldb, pgdialect.New()), slog.Default(), retentionDays)
}

type cacheCleanupFakeDriver struct{}

func (cacheCleanupFakeDriver) Open(dsn string) (driver.Conn, error) {
	cacheCleanupRegistryMu.Lock()
	state := cacheCleanupRegistry[dsn]
	cacheCleanupRegistryMu.Unlock()
	if state == nil {
		return nil, errors.New("no fake cache cleanup state for dsn " + dsn)
	}
	return &cacheCleanupFakeConn{state: state}, nil
}

type cacheCleanupFakeConn struct{ state *cacheCleanupFakeDB }

func (c *cacheCleanupFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *cacheCleanupFakeConn) Close() error { return nil }
func (c *cacheCleanupFakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("tx unsupported")
}
func (c *cacheCleanupFakeConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &cacheCleanupRows{}, nil
}
func (c *cacheCleanupFakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, query)
	if s.authErr && strings.Contains(query, "auth_introspection_cache") {
		return nil, errors.New("auth cache unavailable")
	}
	return driver.RowsAffected(0), nil
}

type cacheCleanupRows struct{}

func (r *cacheCleanupRows) Columns() []string { return nil }
func (r *cacheCleanupRows) Close() error      { return nil }
func (r *cacheCleanupRows) Next([]driver.Value) error {
	return io.EOF
}

func TestCacheCleanupTask_AuthFailureDoesNotSkipEmbeddingPrune(t *testing.T) {
	state := &cacheCleanupFakeDB{authErr: true}
	task := newCacheCleanupTask(t, state, 30)

	err := task.Run(context.Background())
	require.Error(t, err, "a failed prune must be reported, not swallowed")

	state.mu.Lock()
	defer state.mu.Unlock()
	require.Len(t, state.queries, 2, "both caches are attempted even when the first fails")
	assert.Contains(t, state.queries[0], "auth_introspection_cache")
	assert.Contains(t, state.queries[1], "embedding_cache")
	assert.Contains(t, state.queries[1], "created_at <", "the embedding prune filters on age")
}

func TestNewCacheCleanupTask_NonPositiveRetentionDefaults(t *testing.T) {
	for _, days := range []int{0, -1} {
		task := NewCacheCleanupTask(nil, slog.Default(), days)
		assert.Equal(t, defaultEmbeddingCacheRetentionDays, task.retentionDays,
			"retention %d must fall back to the default instead of disabling pruning", days)
	}
}
