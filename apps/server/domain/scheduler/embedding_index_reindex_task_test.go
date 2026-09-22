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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// testTargetIndex is the index name exercised by the fake-driver tests. It is
// injected into the task so the reindex/recovery machinery can be tested even
// though the production list is now empty (all ivfflat embedding indexes were
// migrated to HNSW: graph objects 00164, chunks/skills 00170, graph
// relationships 00171).
const testTargetIndex = "idx_graph_relationships_embedding_ivfflat"

func TestEmbeddingIndexTargetsQualified(t *testing.T) {
	// Every ivfflat embedding index has been migrated to HNSW, so the scheduled
	// reindex list must be empty: a nightly REINDEX against a dropped index would
	// fail and be surfaced as an aggregate task error every night.
	assert.Empty(t, embeddingIndexTargets)

	// The qualification machinery still matters for any future target.
	target := embeddingIndexTarget{schema: "kb", name: testTargetIndex}
	assert.Equal(t, `"kb"."`+testTargetIndex+`"`, target.qualified())
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct{ in, want string }{
		{"kb", `"kb"`},
		{"plain", `"plain"`},
		{`weird"name`, `"weird""name"`},
	}
	for _, tt := range tests {
		if got := quoteIdent(tt.in); got != tt.want {
			t.Errorf("quoteIdent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// =============================================================================
// DB-free fake driver
//
// embeddingReindexFakeDB is in-memory state used by embeddingReindexFakeDriver
// to exercise EmbeddingIndexReindexTask.Run without a live PostgreSQL. The
// validity SELECT returns whether each index is marked invalid; each REINDEX
// Exec either succeeds or fails per index name.
// =============================================================================

type embeddingReindexFakeDB struct {
	mu        sync.Mutex
	invalid   map[string]bool  // index name -> report invalid (validity SELECT)
	failNames map[string]bool  // index name -> REINDEX fails
	checkErr  map[string]error // index name -> validity SELECT errors

	validityChecks []string // index names whose validity was queried, in order
	reindexSQL     []string // full REINDEX statements, in order
}

var (
	embeddingReindexRegistryMu sync.Mutex
	embeddingReindexRegistry   = map[string]*embeddingReindexFakeDB{}
	embeddingReindexRegister   sync.Once
)

func newEmbeddingIndexReindexTask(t *testing.T, state *embeddingReindexFakeDB) *EmbeddingIndexReindexTask {
	t.Helper()
	dsn := t.Name()
	embeddingReindexRegister.Do(func() { sql.Register("fakereindex", embeddingReindexFakeDriver{}) })

	embeddingReindexRegistryMu.Lock()
	embeddingReindexRegistry[dsn] = state
	embeddingReindexRegistryMu.Unlock()

	sqldb, err := sql.Open("fakereindex", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	task := NewEmbeddingIndexReindexTask(bun.NewDB(sqldb, pgdialect.New()), slog.Default())
	// Production targets are empty post-HNSW; inject one so the fake-driver tests
	// still exercise the validity-check + reindex/recovery paths.
	task.targets = []embeddingIndexTarget{{schema: "kb", name: testTargetIndex}}
	return task
}

type embeddingReindexFakeDriver struct{}

func (embeddingReindexFakeDriver) Open(dsn string) (driver.Conn, error) {
	embeddingReindexRegistryMu.Lock()
	state := embeddingReindexRegistry[dsn]
	embeddingReindexRegistryMu.Unlock()
	if state == nil {
		return nil, errors.New("no fake reindex state for dsn " + dsn)
	}
	return &embeddingReindexFakeConn{state: state}, nil
}

type embeddingReindexFakeConn struct{ state *embeddingReindexFakeDB }

func (c *embeddingReindexFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *embeddingReindexFakeConn) Close() error { return nil }
func (c *embeddingReindexFakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("tx unsupported")
}

func (c *embeddingReindexFakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()

	name := validityIndexName(query, args)
	s.validityChecks = append(s.validityChecks, name)
	if err := s.checkErr[name]; err != nil {
		return nil, err
	}
	return &singleBoolRows{val: s.invalid[name]}, nil
}

func (c *embeddingReindexFakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reindexSQL = append(s.reindexSQL, query)
	name := reindexIndexName(query)
	if s.failNames[name] {
		return nil, errors.New("reindex failed")
	}
	return driver.RowsAffected(1), nil
}

type singleBoolRows struct {
	val  bool
	done bool
}

func (r *singleBoolRows) Columns() []string { return []string{"indisvalid"} }
func (r *singleBoolRows) Close() error      { return nil }
func (r *singleBoolRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.val
	return nil
}

// validityIndexName extracts the index name from the validity SELECT. bun
// inlines the string args as literals (e.g. `c.relname = '<index>'`);
// when that is absent we fall back to the last string argument.
func validityIndexName(query string, args []driver.NamedValue) string {
	const marker = "c.relname = '"
	if i := strings.Index(query, marker); i >= 0 {
		rest := query[i+len(marker):]
		if j := strings.Index(rest, "'"); j >= 0 {
			return rest[:j]
		}
	}
	name := ""
	for _, a := range args {
		if s, ok := a.Value.(string); ok {
			name = s
		}
	}
	return name
}

// reindexIndexName extracts the index name from a REINDEX statement, which is
// always the final quoted identifier (e.g. `REINDEX INDEX CONCURRENTLY
// "kb"."<index>"`).
func reindexIndexName(query string) string {
	q := strings.TrimSpace(query)
	i := strings.Index(q, `"`)
	if i < 0 {
		return ""
	}
	rest := q[i:]
	j := strings.LastIndex(rest, `"`)
	if j < 0 {
		return ""
	}
	k := strings.LastIndex(rest[:j], `"`)
	if k < 0 {
		return ""
	}
	return rest[k+1 : j]
}

// =============================================================================
// EmbeddingIndexReindexTask.Run
// =============================================================================

func TestEmbeddingIndexReindexTask_Run_ValidIndexes(t *testing.T) {
	state := &embeddingReindexFakeDB{invalid: map[string]bool{}}
	task := newEmbeddingIndexReindexTask(t, state)

	require.NoError(t, task.Run(context.Background()))

	// Every target is validity-checked then concurrently reindexed, no recovery.
	assert.Equal(t, len(task.targets), len(state.validityChecks))
	assert.Len(t, state.reindexSQL, len(task.targets))
	for _, q := range state.reindexSQL {
		assert.Contains(t, q, "REINDEX INDEX CONCURRENTLY", "valid index uses concurrent reindex only")
		assert.NotContains(t, q, "REINDEX INDEX CONCURRENTLY CONCURRENTLY")
	}
}

func TestEmbeddingIndexReindexTask_Run_InvalidIndexRecovered(t *testing.T) {
	state := &embeddingReindexFakeDB{
		invalid: map[string]bool{testTargetIndex: true},
	}
	task := newEmbeddingIndexReindexTask(t, state)

	require.NoError(t, task.Run(context.Background()))

	// The invalid index gets a plain REINDEX recovery first, then a concurrent
	// rebuild; valid indexes are only concurrently rebuilt.
	var recovery, concurrent int
	for _, q := range state.reindexSQL {
		switch {
		case strings.Contains(q, "CONCURRENTLY"):
			concurrent++
		default:
			recovery++
		}
	}
	assert.Equal(t, 1, recovery, "exactly one invalid index needs a plain recovery")
	assert.Equal(t, len(task.targets), concurrent, "every target is concurrently rebuilt")
	assert.Contains(t, strings.Join(state.reindexSQL, "\n"), `REINDEX INDEX "kb"."`+testTargetIndex+`"`)
}

func TestEmbeddingIndexReindexTask_Run_FailureReturnsAggregateError(t *testing.T) {
	state := &embeddingReindexFakeDB{
		invalid:   map[string]bool{},
		failNames: map[string]bool{testTargetIndex: true},
	}
	task := newEmbeddingIndexReindexTask(t, state)

	err := task.Run(context.Background())

	require.Error(t, err, "per-index failure must surface as an aggregate error")
	assert.Len(t, state.reindexSQL, len(task.targets),
		"a poisoned index must not abort the remaining targets")
	assert.Contains(t, err.Error(), testTargetIndex)
}

func TestEmbeddingIndexReindexTask_Run_ValidityCheckErrorReturnsAggregateError(t *testing.T) {
	state := &embeddingReindexFakeDB{
		invalid:  map[string]bool{},
		checkErr: map[string]error{testTargetIndex: errors.New("db down")},
	}
	task := newEmbeddingIndexReindexTask(t, state)

	err := task.Run(context.Background())

	require.Error(t, err, "validity-check failure must surface as an aggregate error")
	assert.Contains(t, err.Error(), testTargetIndex)
	// The failing target is skipped and never reindexed.
	assert.Len(t, state.reindexSQL, len(task.targets)-1)
	for _, q := range state.reindexSQL {
		assert.NotContains(t, q, testTargetIndex)
	}
}
