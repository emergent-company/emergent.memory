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
// DB-free fake driver for EmbeddingJobPurgeTask.Run.
//
// embeddingPurgeFakeDB records the DELETE statements the task issues so the
// table scope and WHERE conditions can be asserted without a live PostgreSQL.
// =============================================================================

type embeddingPurgeFakeDB struct {
	mu      sync.Mutex
	queries []string
	rows    int64
	execErr error
}

var (
	embeddingPurgeRegistryMu sync.Mutex
	embeddingPurgeRegistry   = map[string]*embeddingPurgeFakeDB{}
	embeddingPurgeRegister   sync.Once
)

func newEmbeddingJobPurgeTask(t *testing.T, state *embeddingPurgeFakeDB, retentionDays int) *EmbeddingJobPurgeTask {
	t.Helper()
	dsn := t.Name()
	embeddingPurgeRegister.Do(func() { sql.Register("fakeembeddingpurge", embeddingPurgeFakeDriver{}) })

	embeddingPurgeRegistryMu.Lock()
	embeddingPurgeRegistry[dsn] = state
	embeddingPurgeRegistryMu.Unlock()

	sqldb, err := sql.Open("fakeembeddingpurge", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	return NewEmbeddingJobPurgeTask(bun.NewDB(sqldb, pgdialect.New()), slog.Default(), retentionDays)
}

type embeddingPurgeFakeDriver struct{}

func (embeddingPurgeFakeDriver) Open(dsn string) (driver.Conn, error) {
	embeddingPurgeRegistryMu.Lock()
	state := embeddingPurgeRegistry[dsn]
	embeddingPurgeRegistryMu.Unlock()
	if state == nil {
		return nil, errors.New("no fake embedding purge state for dsn " + dsn)
	}
	return &embeddingPurgeFakeConn{state: state}, nil
}

type embeddingPurgeFakeConn struct{ state *embeddingPurgeFakeDB }

func (c *embeddingPurgeFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *embeddingPurgeFakeConn) Close() error { return nil }
func (c *embeddingPurgeFakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("tx unsupported")
}
func (c *embeddingPurgeFakeConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &embeddingPurgeRows{}, nil
}
func (c *embeddingPurgeFakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, query)
	if s.execErr != nil {
		return nil, s.execErr
	}
	return driver.RowsAffected(s.rows), nil
}

type embeddingPurgeRows struct{}

func (r *embeddingPurgeRows) Columns() []string { return nil }
func (r *embeddingPurgeRows) Close() error      { return nil }
func (r *embeddingPurgeRows) Next([]driver.Value) error {
	return io.EOF
}

// =============================================================================
// EmbeddingJobPurgeTask.Run
// =============================================================================

func TestEmbeddingJobPurgeTask_Run(t *testing.T) {
	state := &embeddingPurgeFakeDB{rows: 5}
	task := newEmbeddingJobPurgeTask(t, state, 7)

	require.NoError(t, task.Run(context.Background()))

	require.Len(t, state.queries, 6, "one DELETE per terminal job table")

	type want struct {
		table    string
		statusIn string
		ageExpr  string
	}
	wants := []want{
		{"kb.graph_embedding_jobs", "status IN ('completed', 'failed', 'dead_letter')", "updated_at <"},
		{"kb.graph_relationship_embedding_jobs", "status IN ('completed', 'failed', 'dead_letter')", "updated_at <"},
		{"kb.chunk_embedding_jobs", "status IN ('completed', 'failed', 'dead_letter')", "updated_at <"},
		{"kb.document_parsing_jobs", "status IN ('completed', 'failed')", "updated_at <"},
		{"kb.object_extraction_jobs", "status IN ('completed', 'failed')", "updated_at <"},
		{"kb.email_jobs", "status IN ('sent', 'failed', 'dead_letter')", "COALESCE(processed_at, created_at) <"},
	}
	for i, w := range wants {
		q := state.queries[i]
		assert.Contains(t, q, "DELETE FROM "+w.table)
		assert.Contains(t, q, w.statusIn, "terminal status predicate for %s", w.table)
		assert.Contains(t, q, w.ageExpr, "retention age predicate for %s", w.table)
	}
	// email_jobs has no updated_at column — must never reference it.
	emailQ := state.queries[5]
	assert.NotContains(t, emailQ, "updated_at")
}

func TestEmbeddingJobPurgeTask_Run_ErrorDoesNotStopOthers(t *testing.T) {
	state := &embeddingPurgeFakeDB{rows: 1, execErr: errors.New("db down")}
	task := newEmbeddingJobPurgeTask(t, state, 7)

	require.NoError(t, task.Run(context.Background()), "best-effort: a failure on a table must not abort the task")

	// Every table is attempted even though the first fails.
	assert.Len(t, state.queries, 6)
}
