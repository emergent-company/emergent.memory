package scheduler

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// =============================================================================
// DB-free fake driver for StaleJobCleanupTask.
//
// staleCleanupFakeDB records the UPDATE statements (and their arg counts) that
// cleanupTable builds, so the stale-cleanup WHERE clause can be asserted without
// a live PostgreSQL. The fake never mutates rows; reaping behavior is verified by
// inspecting the generated SQL, and the mass-reap alert is verified by capturing
// the task's structured logs.
// =============================================================================

type staleCleanupFakeDB struct {
	mu      sync.Mutex
	queries []string
	argCnts []int
	rows    int64
}

var (
	staleCleanupRegistryMu sync.Mutex
	staleCleanupRegistry   = map[string]*staleCleanupFakeDB{}
	staleCleanupRegister   sync.Once
)

func newStaleJobCleanupTask(t *testing.T, state *staleCleanupFakeDB) *StaleJobCleanupTask {
	t.Helper()
	return newStaleJobCleanupTaskWithLogger(t, state, slog.Default())
}

func newStaleJobCleanupTaskWithLogger(t *testing.T, state *staleCleanupFakeDB, log *slog.Logger) *StaleJobCleanupTask {
	t.Helper()
	dsn := t.Name()
	staleCleanupRegister.Do(func() { sql.Register("fakestalecleanup", staleCleanupFakeDriver{}) })

	staleCleanupRegistryMu.Lock()
	staleCleanupRegistry[dsn] = state
	staleCleanupRegistryMu.Unlock()

	sqldb, err := sql.Open("fakestalecleanup", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	return NewStaleJobCleanupTask(bun.NewDB(sqldb, pgdialect.New()), log, 30, 0)
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
	return driver.RowsAffected(s.rows), nil
}

// =============================================================================
// cleanupStaleJobsQuery — predicate selection (no DB required)
// =============================================================================

func TestCleanupStaleJobsQuery_StartedAtTables(t *testing.T) {
	tables := []struct {
		name       string
		table      string
		errorCol   string
		hasStartAt bool
	}{
		{name: "graph_embedding", table: "kb.graph_embedding_jobs", errorCol: "last_error", hasStartAt: true},
		{name: "chunk_embedding", table: "kb.chunk_embedding_jobs", errorCol: "last_error", hasStartAt: true},
		{name: "document_parsing", table: "kb.document_parsing_jobs", errorCol: "error_message", hasStartAt: true},
		{name: "object_extraction", table: "kb.object_extraction_jobs", errorCol: "error_message", hasStartAt: true},
	}

	cutoff := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	for _, tt := range tables {
		t.Run(tt.name, func(t *testing.T) {
			cfg := jobTableConfig{
				table:          tt.table,
				hasStartedAt:   tt.hasStartAt,
				hasCompletedAt: true,
				errorColumn:    tt.errorCol,
			}
			query, args := cleanupStaleJobsQuery(cfg, cutoff)

			// Only started in-flight jobs are reaped.
			assert.Contains(t, query, "status IN ('processing', 'running')")
			assert.Contains(t, query, "started_at IS NOT NULL")
			assert.Contains(t, query, "started_at < ?")
			// Never-started pending jobs must be excluded: they are queued, not stale.
			assert.NotContains(t, query, "'pending'", "never-started pending jobs must not be terminal-failed")
			// The old created_at OR-clause that failed pending jobs must be gone.
			assert.NotContains(t, query, "created_at <")
			// Terminal-fail bookkeeping is preserved.
			assert.Contains(t, query, "SET status = 'failed'")
			assert.Contains(t, query, "completed_at = NOW()")
			assert.Contains(t, query, tt.errorCol+" = '"+staleJobMessage+"'")

			require.Len(t, args, 1)
			assert.Equal(t, cutoff, args[0])
		})
	}
}

func TestCleanupStaleJobsQuery_EmailJobsExcludesPending(t *testing.T) {
	cfg := jobTableConfig{
		table:          "kb.email_jobs",
		hasStartedAt:   false,
		hasCompletedAt: false,
		errorColumn:    "last_error",
	}
	cutoff := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	query, args := cleanupStaleJobsQuery(cfg, cutoff)

	// email_jobs has no started_at, so it falls back to created_at — but pending
	// rows (never attempted) must still be excluded, exactly like the started_at
	// tables. This is the table the original sweep still mass-failed.
	assert.Contains(t, query, "status IN ('processing', 'running')")
	assert.Contains(t, query, "created_at < ?")
	assert.NotContains(t, query, "'pending'", "email_jobs pending rows must not be terminal-failed")
	assert.NotContains(t, query, "started_at")
	assert.NotContains(t, query, "completed_at")

	require.Len(t, args, 1)
	assert.Equal(t, cutoff, args[0])
}

func TestJobTableConfig_EffectiveStaleMinutes(t *testing.T) {
	assert.Equal(t, 30, jobTableConfig{}.effectiveStaleMinutes(30))
	assert.Equal(t, 480, jobTableConfig{staleMinutesOverride: 480}.effectiveStaleMinutes(30))
	// A zero/negative override must not shadow the global threshold.
	assert.Equal(t, 30, jobTableConfig{staleMinutesOverride: -5}.effectiveStaleMinutes(30))
}

// =============================================================================
// cleanupTable — generated statement via the fake driver
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

	assert.Contains(t, q, "status IN ('processing', 'running')")
	assert.NotContains(t, q, "'pending'")
	assert.Contains(t, q, "started_at IS NOT NULL")
	assert.Contains(t, q, "started_at <")
	assert.NotContains(t, q, "created_at <")
	// The statement is issued once per table (args are bound/inlined by bun).
	assert.Len(t, state.argCnts, 1)
}

func TestStaleJobCleanupTable_EmailJobsExcludesPending(t *testing.T) {
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

	assert.Contains(t, q, "status IN ('processing', 'running')")
	assert.Contains(t, q, "created_at <")
	assert.NotContains(t, q, "'pending'")
	assert.Len(t, state.argCnts, 1)
}

func TestStaleJobCleanupTable_PerTableThresholdOverride(t *testing.T) {
	state := &staleCleanupFakeDB{}
	task := newStaleJobCleanupTask(t, state)

	cfg := jobTableConfig{
		table:                "kb.document_parsing_jobs",
		hasStartedAt:         true,
		hasCompletedAt:       true,
		errorColumn:          "error_message",
		staleMinutesOverride: 480,
	}
	_, err := task.cleanupTable(context.Background(), cfg, 30)
	require.NoError(t, err)

	// The generated statement is issued exactly once for the table.
	require.Len(t, state.queries, 1)
	require.Len(t, state.argCnts, 1)
}

// =============================================================================
// Mass-reap visibility
// =============================================================================

func TestStaleJobCleanupTask_MassReapAlert(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	state := &staleCleanupFakeDB{rows: 2000}
	task := newStaleJobCleanupTaskWithLogger(t, state, log)
	task.SetMassReapThreshold(1000)

	require.NoError(t, task.Run(context.Background()))

	out := buf.String()
	assert.Contains(t, out, `"alert":"`+staleJobMassReapAlert+`"`, "a sweep reaping > threshold must emit the mass-reap alert")
	assert.Contains(t, out, `"level":"ERROR"`)
}

func TestStaleJobCleanupTask_NoMassReapAlertBelowThreshold(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	state := &staleCleanupFakeDB{rows: 3}
	task := newStaleJobCleanupTaskWithLogger(t, state, log)
	task.SetMassReapThreshold(1000)

	require.NoError(t, task.Run(context.Background()))

	assert.NotContains(t, strings.ToLower(buf.String()), "mass stale-job reap")
	assert.NotContains(t, buf.String(), `"alert":"`+staleJobMassReapAlert+`"`)
}

func TestStaleJobCleanupTask_SetMassReapThreshold(t *testing.T) {
	task := newStaleJobCleanupTask(t, &staleCleanupFakeDB{})

	assert.Equal(t, defaultStaleJobMassReapThreshold, task.GetMassReapThreshold())

	task.SetMassReapThreshold(5)
	assert.Equal(t, 5, task.GetMassReapThreshold())

	// Values <= 0 fall back to the default rather than disabling silently.
	task.SetMassReapThreshold(0)
	assert.Equal(t, defaultStaleJobMassReapThreshold, task.GetMassReapThreshold())

	task.SetMassReapThreshold(-10)
	assert.Equal(t, defaultStaleJobMassReapThreshold, task.GetMassReapThreshold())
}
