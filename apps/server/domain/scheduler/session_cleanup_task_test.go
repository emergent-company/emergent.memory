package scheduler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// =============================================================================
// DB-free fake driver for SessionCleanupTask.Run.
//
// sessionCleanupFakeDB records the SELECT and DELETE statements the task issues
// so the terminal-status predicate can be asserted without a live PostgreSQL.
// =============================================================================

type sessionCleanupFakeDB struct {
	mu      sync.Mutex
	queries []string
}

var (
	sessionCleanupRegistryMu sync.Mutex
	sessionCleanupRegistry   = map[string]*sessionCleanupFakeDB{}
	sessionCleanupRegister   sync.Once
)

func newSessionCleanupTask(t *testing.T, state *sessionCleanupFakeDB, retentionDays int) *SessionCleanupTask {
	t.Helper()
	dsn := t.Name()
	sessionCleanupRegister.Do(func() { sql.Register("fakesessioncleanup", sessionCleanupFakeDriver{}) })

	sessionCleanupRegistryMu.Lock()
	sessionCleanupRegistry[dsn] = state
	sessionCleanupRegistryMu.Unlock()

	sqldb, err := sql.Open("fakesessioncleanup", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	return NewSessionCleanupTask(bun.NewDB(sqldb, pgdialect.New()), slog.Default(), retentionDays)
}

type sessionCleanupFakeDriver struct{}

func (sessionCleanupFakeDriver) Open(dsn string) (driver.Conn, error) {
	sessionCleanupRegistryMu.Lock()
	state := sessionCleanupRegistry[dsn]
	sessionCleanupRegistryMu.Unlock()
	if state == nil {
		return nil, errors.New("no fake session cleanup state for dsn " + dsn)
	}
	return &sessionCleanupFakeConn{state: state}, nil
}

type sessionCleanupFakeConn struct{ state *sessionCleanupFakeDB }

func (c *sessionCleanupFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *sessionCleanupFakeConn) Close() error { return nil }
func (c *sessionCleanupFakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("tx unsupported")
}
func (c *sessionCleanupFakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	c.state.queries = append(c.state.queries, query)
	c.state.mu.Unlock()
	return &sessionCleanupRows{}, nil
}
func (c *sessionCleanupFakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	c.state.queries = append(c.state.queries, query)
	c.state.mu.Unlock()
	return driver.RowsAffected(0), nil
}

type sessionCleanupRows struct{}

func (r *sessionCleanupRows) Columns() []string { return nil }
func (r *sessionCleanupRows) Close() error      { return nil }
func (r *sessionCleanupRows) Next([]driver.Value) error {
	return io.EOF
}

// =============================================================================
// SessionCleanupTask.Run — terminal status predicate
// =============================================================================

// TestSessionCleanupSelectsCompletedAndFailedRuns is the fail-first regression
// test for #1110: the cleanup query used the literals 'success'/'error', which
// do not match the real statuses 'completed'/'failed', so completed and failed
// runs were never selected for cleanup. It asserts the SELECT predicate selects
// the real terminal statuses and none of the phantom ones.
func TestSessionCleanupSelectsCompletedAndFailedRuns(t *testing.T) {
	state := &sessionCleanupFakeDB{}
	task := newSessionCleanupTask(t, state, 90)

	require.NoError(t, task.Run(context.Background()))

	require.NotEmpty(t, state.queries, "Run must issue the eligibility SELECT")
	selectQuery := state.queries[0]

	assert.Contains(t, selectQuery, "FROM kb.agent_runs")
	assert.Contains(t, selectQuery, "status IN (", "predicate must filter by status")

	// The real terminal statuses must be selected.
	for _, want := range []string{"'completed'", "'failed'", "'skipped'", "'cancelled'"} {
		assert.Contains(t, selectQuery, want, "status predicate must include %s", want)
	}

	// The phantom statuses must be gone.
	for _, gone := range []string{"'success'", "'error'"} {
		assert.NotContains(t, selectQuery, gone, "phantom status %s must not be selected", gone)
	}
}
