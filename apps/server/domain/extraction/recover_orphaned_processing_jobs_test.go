package extraction

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
// DB-free fake driver for RecoverOrphanedProcessingJobs.
//
// orphanRecoveryFakeDB records the UPDATE statement each service builds so the
// WHERE clause can be asserted without a live PostgreSQL. The restart-resistance
// behavior is verified by the generated SQL: it must target ALL 'processing'
// jobs (no started_at age gate), so a RECENTLY-started (30s-old) processing job
// is reset to 'pending' — the thing the old 10-minute threshold failed to do.
// =============================================================================

type orphanRecoveryFakeDB struct {
	mu      sync.Mutex
	queries []string
	rows    int64
	execErr error
}

var (
	orphanRecoveryRegistryMu sync.Mutex
	orphanRecoveryRegistry   = map[string]*orphanRecoveryFakeDB{}
	orphanRecoveryRegister   sync.Once
)

func newOrphanRecoveryDB(t *testing.T, state *orphanRecoveryFakeDB) bun.IDB {
	t.Helper()
	dsn := t.Name()
	orphanRecoveryRegister.Do(func() { sql.Register("fakeorphanrecovery", orphanRecoveryFakeDriver{}) })

	orphanRecoveryRegistryMu.Lock()
	orphanRecoveryRegistry[dsn] = state
	orphanRecoveryRegistryMu.Unlock()

	sqldb, err := sql.Open("fakeorphanrecovery", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	return bun.NewDB(sqldb, pgdialect.New())
}

type orphanRecoveryFakeDriver struct{}

func (orphanRecoveryFakeDriver) Open(dsn string) (driver.Conn, error) {
	orphanRecoveryRegistryMu.Lock()
	state := orphanRecoveryRegistry[dsn]
	orphanRecoveryRegistryMu.Unlock()
	if state == nil {
		return nil, errors.New("no fake orphan recovery state for dsn " + dsn)
	}
	return &orphanRecoveryFakeConn{state: state}, nil
}

type orphanRecoveryFakeConn struct{ state *orphanRecoveryFakeDB }

func (c *orphanRecoveryFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *orphanRecoveryFakeConn) Close() error { return nil }
func (c *orphanRecoveryFakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("tx unsupported")
}
func (c *orphanRecoveryFakeConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &orphanRecoveryRows{}, nil
}
func (c *orphanRecoveryFakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, query)
	if s.execErr != nil {
		return nil, s.execErr
	}
	return driver.RowsAffected(s.rows), nil
}

type orphanRecoveryRows struct{}

func (r *orphanRecoveryRows) Columns() []string { return nil }
func (r *orphanRecoveryRows) Close() error      { return nil }
func (r *orphanRecoveryRows) Next([]driver.Value) error {
	return io.EOF
}

// =============================================================================
// RecoverOrphanedProcessingJobs
// =============================================================================

func TestRecoverOrphanedProcessingJobs_NoAgeGate(t *testing.T) {
	tests := []struct {
		name  string
		table string
		run   func(t *testing.T, db bun.IDB) (int, error)
	}{
		{
			name:  "graph",
			table: "kb.graph_embedding_jobs",
			run: func(t *testing.T, db bun.IDB) (int, error) {
				return NewGraphEmbeddingJobsService(db, slog.Default(), nil).RecoverOrphanedProcessingJobs(context.Background())
			},
		},
		{
			name:  "relationship",
			table: "kb.graph_relationship_embedding_jobs",
			run: func(t *testing.T, db bun.IDB) (int, error) {
				return NewGraphRelationshipEmbeddingJobsService(db, slog.Default(), nil).RecoverOrphanedProcessingJobs(context.Background())
			},
		},
		{
			name:  "chunk",
			table: "kb.chunk_embedding_jobs",
			run: func(t *testing.T, db bun.IDB) (int, error) {
				return NewChunkEmbeddingJobsService(db, slog.Default(), nil).RecoverOrphanedProcessingJobs(context.Background())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &orphanRecoveryFakeDB{rows: 7}
			db := newOrphanRecoveryDB(t, state)

			count, err := tt.run(t, db)
			require.NoError(t, err)
			assert.Equal(t, 7, count)

			require.Len(t, state.queries, 1)
			q := state.queries[0]

			// Resets ALL processing jobs: resets status and clears started_at.
			assert.Contains(t, q, "UPDATE "+tt.table)
			assert.Contains(t, q, "SET status = 'pending'")
			assert.Contains(t, q, "started_at = NULL")
			assert.Contains(t, q, "WHERE status = 'processing'")
			// The restart-resistance fix: no started_at age gate, so a
			// RECENTLY-started (e.g. 30s-old) processing job is also recovered.
			assert.NotContains(t, q, "started_at <", "must not age-gate: a recent orphan is still an orphan")
			assert.NotContains(t, q, "interval", "must not reference a time interval")
		})
	}
}

func TestRecoverOrphanedProcessingJobs_Error(t *testing.T) {
	state := &orphanRecoveryFakeDB{execErr: errors.New("db down")}
	db := newOrphanRecoveryDB(t, state)

	svc := NewGraphEmbeddingJobsService(db, slog.Default(), nil)
	_, err := svc.RecoverOrphanedProcessingJobs(context.Background())
	require.Error(t, err)
}
