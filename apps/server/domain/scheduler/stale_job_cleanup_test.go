package scheduler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// =============================================================================
// DB-free fake driver for StaleJobCleanupTask.cleanupTable.
//
// staleCleanupFakeDB records the UPDATE statement (and its arg count) that
// cleanupTable builds, so the stale-cleanup WHERE clause can be asserted
// without a live PostgreSQL. The fake never mutates rows; behavior is verified
// by inspecting the generated SQL.
// =============================================================================

type staleCleanupFakeDB struct {
	mu      sync.Mutex
	queries []string
	argCnts []int
}

var (
	staleCleanupRegistryMu sync.Mutex
	staleCleanupRegistry   = map[string]*staleCleanupFakeDB{}
	staleCleanupRegister   sync.Once
)

func newStaleJobCleanupTask(t *testing.T, state *staleCleanupFakeDB) *StaleJobCleanupTask {
	t.Helper()
	dsn := t.Name()
	staleCleanupRegister.Do(func() { sql.Register("fakestalecleanup", staleCleanupFakeDriver{}) })

	staleCleanupRegistryMu.Lock()
	staleCleanupRegistry[dsn] = state
	staleCleanupRegistryMu.Unlock()

	sqldb, err := sql.Open("fakestalecleanup", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	return NewStaleJobCleanupTask(bun.NewDB(sqldb, pgdialect.New()), slog.Default(), 30, 0)
}

type staleCleanupFakeDriver struct{}

func (staleCleanupFakeDriver) Open(dsn string) (driver.Conn, error) {
	staleCleanupRegistryMu.Lock()
	state := staleCleanupRegistry[dsn]
	staleCleanupRegistryMu.Unlock()
	if state == nil {
		return nil, errors.New("no fake stale cleanup state for dsn " + dsn)
	}
	return &staleCleanupFakeConn{state: state}, nil
}

type staleCleanupFakeConn struct{ state *staleCleanupFakeDB }

func (c *staleCleanupFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *staleCleanupFakeConn) Close() error { return nil }
func (c *staleCleanupFakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("tx unsupported")
}

func (c *staleCleanupFakeConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &singleColumnRows{}, nil
}

func (c *staleCleanupFakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, query)
	s.argCnts = append(s.argCnts, len(args))
	return driver.RowsAffected(0), nil
}

// =============================================================================
// StaleJobCleanupTask.cleanupTable
// =============================================================================

func TestStaleJobCleanupTable_SkipsPendingJobs(t *testing.T) {
	state := &staleCleanupFakeDB{}
	task := newStaleJobCleanupTask(t, state)

	cfg := jobTableConfig{
		table:          "kb.graph_embedding_jobs",
		hasStartedAt:   true,
		hasCompletedAt: true,
		errorColumn:    "last_error",
	}
	_, err := task.cleanupTable(context.Background(), cfg, 30)
	require.NoError(t, err)

	require.Len(t, state.queries, 1)
	q := state.queries[0]

	// Only started (processing/running) jobs are candidates for stale-fail.
	assert.Contains(t, q, "status IN ('processing', 'running')")
	// 'pending' must be excluded: never-started jobs are queued, not stale.
	assert.NotContains(t, q, "'pending'")
	assert.Contains(t, q, "started_at IS NOT NULL")
	assert.Contains(t, q, "started_at <")
	// The old created_at OR-clause that failed pending jobs must be gone.
	assert.NotContains(t, q, "created_at <")
	// One cutoff arg (started_at) only, not the old two-arg (started_at, created_at).
	assert.Len(t, state.argCnts, 1)
	assert.LessOrEqual(t, state.argCnts[0], 1)
}

func TestStaleJobCleanupTable_EmailJobsBranchUnchanged(t *testing.T) {
	state := &staleCleanupFakeDB{}
	task := newStaleJobCleanupTask(t, state)

	cfg := jobTableConfig{
		table:          "kb.email_jobs",
		hasStartedAt:   false,
		hasCompletedAt: false,
		errorColumn:    "last_error",
	}
	_, err := task.cleanupTable(context.Background(), cfg, 30)
	require.NoError(t, err)

	require.Len(t, state.queries, 1)
	q := state.queries[0]

	// email_jobs has no started_at: it still uses created_at and includes pending.
	assert.Contains(t, q, "status IN ('pending', 'processing', 'running')")
	assert.Contains(t, q, "created_at <")
}
