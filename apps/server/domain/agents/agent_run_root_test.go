package agents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// =============================================================================
// root_run_id linkage tests (fix/agent-spawn-root-run-id)
//
// The regression: root_run_id was only persisted when the OTel span context was
// valid, and tracing is off by default, so every run (including spawned children
// and re-enqueued runs) ended up with root_run_id = NULL. These tests pin the
// fixed behavior without a live DB.
// =============================================================================

func TestResolveRootRunID_TopLevelSelfRoots(t *testing.T) {
	// A top-level run has no caller override and no persisted root; it must root
	// to its own (newly created) run ID.
	run := &AgentRun{ID: "run-1"}
	assert.Equal(t, "run-1", resolveRootRunID(run, nil))
}

func TestResolveRootRunID_ChildCarriesParentRoot(t *testing.T) {
	// A spawned child receives the parent's root as an override; it must carry it
	// unchanged and not fall back to its own ID even though the child row has its
	// own (different) ID.
	parentRoot := "root-parent"
	run := &AgentRun{ID: "child-1"}
	assert.Equal(t, parentRoot, resolveRootRunID(run, &parentRoot))
}

func TestResolveRootRunID_OverrideWinsOverStoredRoot(t *testing.T) {
	// If both an override and a persisted root are present, the override wins.
	override := "root-override"
	run := &AgentRun{ID: "run-1", RootRunID: strPtr("root-stored")}
	assert.Equal(t, override, resolveRootRunID(run, &override))
}

func TestResolveRootRunID_ReenqueuedKeepsStoredRoot(t *testing.T) {
	// A re-enqueued run has no caller override (the worker path), but its row was
	// created with the orchestration root. It must keep that root instead of
	// self-rooting to its new run ID.
	run := &AgentRun{ID: "requeue-1", RootRunID: strPtr("root-parent")}
	assert.Equal(t, "root-parent", resolveRootRunID(run, nil))
}

func TestResolveRootRunID_EmptyFieldsSelfRoot(t *testing.T) {
	// Empty-string override and empty-string persisted root are both treated as
	// absent, falling back to the run's own ID.
	assert.Equal(t, "run-1", resolveRootRunID(&AgentRun{ID: "run-1"}, strPtr("")))
	assert.Equal(t, "run-1", resolveRootRunID(&AgentRun{ID: "run-1", RootRunID: strPtr("")}, nil))
}

// =============================================================================
// Capture driver: records SQL statements so tests can assert on the generated
// UPDATE/INSERT without a live DB. Supports ExecContext, QueryContext, and
// transactions (needed by CreateRunQueued's RunInTx).
// =============================================================================

type rootCaptureDriver struct {
	mu      sync.Mutex
	queries []string
}

func (d *rootCaptureDriver) reset() {
	d.mu.Lock()
	d.queries = nil
	d.mu.Unlock()
}

func (d *rootCaptureDriver) queriesCopy() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.queries))
	copy(out, d.queries)
	return out
}

func (d *rootCaptureDriver) Open(string) (driver.Conn, error) { return &rootCaptureConn{d: d}, nil }

type rootCaptureConn struct{ d *rootCaptureDriver }

func (c *rootCaptureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *rootCaptureConn) Close() error              { return nil }
func (c *rootCaptureConn) Begin() (driver.Tx, error) { return rootCaptureTx{}, nil }

func (c *rootCaptureConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.d.mu.Lock()
	c.d.queries = append(c.d.queries, query)
	c.d.mu.Unlock()
	return driver.RowsAffected(1), nil
}

func (c *rootCaptureConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.d.mu.Lock()
	c.d.queries = append(c.d.queries, query)
	c.d.mu.Unlock()
	return rootEmptyRows{}, nil
}

type rootCaptureTx struct{}

func (rootCaptureTx) Commit() error   { return nil }
func (rootCaptureTx) Rollback() error { return nil }

type rootEmptyRows struct{}

func (rootEmptyRows) Columns() []string         { return nil }
func (rootEmptyRows) Close() error              { return nil }
func (rootEmptyRows) Next([]driver.Value) error { return io.EOF }

var (
	rootCaptureOnce sync.Once
	rootDriver      = &rootCaptureDriver{}
)

func newRootCaptureRepository(t *testing.T) *Repository {
	t.Helper()
	rootCaptureOnce.Do(func() { sql.Register("agents_root_capture", rootDriver) })
	sqldb, err := sql.Open("agents_root_capture", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return NewRepository(bun.NewDB(sqldb, pgdialect.New()))
}

func rootTestExecutor(repo *Repository) *AgentExecutor {
	return &AgentExecutor{
		repo: repo,
		log:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

// =============================================================================
// persistRunLinkage tests: root_run_id is persisted unconditionally; trace_id
// only when the span context is valid.
// =============================================================================

func TestPersistRunLinkage_InvalidSpanStillWritesRoot(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	ae := rootTestExecutor(repo)

	// Zero-value SpanContext is invalid (tracing off → noop provider).
	ae.persistRunLinkage(context.Background(), "run-1", "root-1", trace.SpanContext{})

	query := strings.Join(rootDriver.queriesCopy(), "\n")
	require.NotEmpty(t, query, "persistRunLinkage must issue an UPDATE")
	assert.Contains(t, query, "root_run_id", "root_run_id must be persisted even with an invalid span context")
	assert.Contains(t, query, "trace_id = NULL", "trace_id must be NULL when the span context is invalid")
}

func TestPersistRunLinkage_ValidSpanWritesTrace(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	ae := rootTestExecutor(repo)

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	require.True(t, sc.IsValid(), "test span context must be valid")

	ae.persistRunLinkage(context.Background(), "run-1", "root-1", sc)

	query := strings.Join(rootDriver.queriesCopy(), "\n")
	require.NotEmpty(t, query, "persistRunLinkage must issue an UPDATE")
	assert.Contains(t, query, "root_run_id", "root_run_id must be persisted")
	assert.Contains(t, query, "trace_id", "trace_id must be persisted when the span context is valid")
	assert.NotContains(t, query, "trace_id = NULL", "trace_id must not be NULL for a valid span context")
}

// =============================================================================
// CreateRunWithOptions / CreateRunQueued tests: root is persisted at insert.
// =============================================================================

func TestCreateRunWithOptions_PersistsRootAtInsert(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)

	_, _ = repo.CreateRunWithOptions(context.Background(), CreateRunOptions{
		AgentID:   "agent-1",
		RootRunID: strPtr("root-1"),
	})

	query := strings.Join(rootDriver.queriesCopy(), "\n")
	require.NotEmpty(t, query, "CreateRunWithOptions must issue an INSERT")
	assert.Contains(t, query, "root_run_id", "INSERT must include the root_run_id column, got: %s", query)
}

func TestCreateRunQueued_PersistsRootAtInsert(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)

	_, _ = repo.CreateRunQueued(context.Background(), "agent-1", 1, CreateRunQueuedOptions{
		RootRunID: strPtr("root-1"),
	})

	query := strings.Join(rootDriver.queriesCopy(), "\n")
	require.NotEmpty(t, query, "CreateRunQueued must issue an INSERT")
	assert.Contains(t, query, "root_run_id", "queued INSERT must include the root_run_id column, got: %s", query)
}
