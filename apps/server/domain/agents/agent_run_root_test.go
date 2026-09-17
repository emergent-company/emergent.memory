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

	"github.com/emergent-company/emergent.memory/domain/provider"
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
	mu        sync.Mutex
	queries   []string
	args      [][]driver.NamedValue
	lastQuery string
	lastArgs  []driver.NamedValue
}

func (d *rootCaptureDriver) reset() {
	d.mu.Lock()
	d.queries = nil
	d.args = nil
	d.lastQuery = ""
	d.lastArgs = nil
	d.mu.Unlock()
}

func (d *rootCaptureDriver) queriesCopy() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.queries))
	copy(out, d.queries)
	return out
}

// insertFor returns the most recent INSERT statement targeting the given table.
func (d *rootCaptureDriver) insertFor(table string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	needle := `INSERT INTO "kb"."` + table + `"`
	for i := len(d.queries) - 1; i >= 0; i-- {
		if strings.Contains(d.queries[i], needle) {
			return d.queries[i]
		}
	}
	return ""
}

func (d *rootCaptureDriver) record(query string, args []driver.NamedValue) {
	d.mu.Lock()
	d.queries = append(d.queries, query)
	d.args = append(d.args, args)
	d.lastQuery = query
	d.lastArgs = args
	d.mu.Unlock()
}

// splitTopLevel splits a SQL value list on commas that are not inside string
// literals or nested brackets.
func splitTopLevel(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\'':
			if inQuote && i+1 < len(s) && s[i+1] == '\'' {
				cur.WriteString("''")
				i++
				continue
			}
			inQuote = !inQuote
			cur.WriteByte(ch)
		case inQuote:
			cur.WriteByte(ch)
		case ch == '(' || ch == '[':
			depth++
			cur.WriteByte(ch)
		case ch == ')' || ch == ']':
			depth--
			cur.WriteByte(ch)
		case ch == ',' && depth == 0:
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	return append(out, strings.TrimSpace(cur.String()))
}

// rootColumnValue returns the literal written for the root_run_id column of the
// last INSERT (e.g. "'root-1'", "DEFAULT", "NULL", "”"), and whether the column
// was present at all. Bun inlines literals for this dialect, so the SQL text is
// the source of truth for what the insert actually sends.
func rootColumnValue(t *testing.T, query string) (string, bool) {
	t.Helper()
	up := strings.ToUpper(query)
	insertAt := strings.Index(up, "INSERT INTO")
	require.GreaterOrEqual(t, insertAt, 0, "expected an INSERT statement, got: %s", query)

	openRel := strings.Index(query[insertAt:], "(")
	require.GreaterOrEqual(t, openRel, 0, "expected a column list, got: %s", query)
	open := insertAt + openRel
	closeRel := strings.Index(query[open:], ")")
	require.GreaterOrEqual(t, closeRel, 0, "expected a closed column list, got: %s", query)
	columns := splitTopLevel(query[open+1 : open+closeRel])

	valuesAt := strings.Index(up[open:], "VALUES")
	require.GreaterOrEqual(t, valuesAt, 0, "expected a VALUES clause, got: %s", query)
	valuesAt += open
	valuesOpenRel := strings.Index(query[valuesAt:], "(")
	require.GreaterOrEqual(t, valuesOpenRel, 0, "expected a values tuple, got: %s", query)
	valuesOpen := valuesAt + valuesOpenRel
	// Walk to the matching close paren, honouring string literals.
	depth, inQuote, valuesClose := 0, false, -1
	for i := valuesOpen; i < len(query); i++ {
		switch query[i] {
		case '\'':
			inQuote = !inQuote
		case '(':
			if !inQuote {
				depth++
			}
		case ')':
			if !inQuote {
				depth--
				if depth == 0 {
					valuesClose = i
				}
			}
		}
		if valuesClose >= 0 {
			break
		}
	}
	require.GreaterOrEqual(t, valuesClose, 0, "expected a closed values tuple, got: %s", query)
	values := splitTopLevel(query[valuesOpen+1 : valuesClose])

	for i, col := range columns {
		if strings.Trim(strings.TrimSpace(col), `"`) != "root_run_id" {
			continue
		}
		if i >= len(values) {
			return "", false
		}
		return values[i], true
	}
	return "", false
}

func (d *rootCaptureDriver) Open(string) (driver.Conn, error) { return &rootCaptureConn{d: d}, nil }

type rootCaptureConn struct{ d *rootCaptureDriver }

func (c *rootCaptureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *rootCaptureConn) Close() error              { return nil }
func (c *rootCaptureConn) Begin() (driver.Tx, error) { return rootCaptureTx{}, nil }

func (c *rootCaptureConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.d.record(query, args)
	return driver.RowsAffected(1), nil
}

func (c *rootCaptureConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.d.record(query, args)
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

// A non-nil empty root means "absent". If it were written to the uuid column the
// insert would fail, so it must be normalized to NULL at the create boundary.
func TestCreateRunWithOptions_EmptyRootBecomesNull(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)

	_, _ = repo.CreateRunWithOptions(context.Background(), CreateRunOptions{
		AgentID:   "agent-1",
		RootRunID: strPtr(""),
	})

	query := rootDriver.insertFor("agent_runs")
	value, present := rootColumnValue(t, query)
	require.True(t, present, "root_run_id column must be present in the INSERT: %s", query)
	assert.NotEqual(t, "''", value, "an empty root must never be written as an empty string literal")
	assert.Contains(t, []string{"DEFAULT", "NULL"}, value, "an empty root must be written as NULL/default, got %s", value)
}

func TestCreateRunQueued_EmptyRootBecomesNull(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)

	_, _ = repo.CreateRunQueued(context.Background(), "agent-1", 1, CreateRunQueuedOptions{
		RootRunID: strPtr(""),
	})

	query := rootDriver.insertFor("agent_runs")
	value, present := rootColumnValue(t, query)
	require.True(t, present, "root_run_id column must be present in the INSERT: %s", query)
	assert.NotEqual(t, "''", value, "an empty root must never be written as an empty string literal")
	assert.Contains(t, []string{"DEFAULT", "NULL"}, value, "an empty root must be written as NULL/default, got %s", value)
}

// The value really does reach the column when it is a real root: this pins the
// column/argument alignment the empty-root tests rely on.
func TestCreateRunWithOptions_RootValueBoundToColumn(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)

	_, _ = repo.CreateRunWithOptions(context.Background(), CreateRunOptions{
		AgentID:   "agent-1",
		RootRunID: strPtr("root-1"),
	})

	query := rootDriver.insertFor("agent_runs")
	value, present := rootColumnValue(t, query)
	require.True(t, present, "root_run_id column must be present in the INSERT: %s", query)
	assert.Equal(t, "'root-1'", value)
}

// The delegation tool reads the delegator's root from context once and uses it
// for both dispatch modes; empty means "no override", never a pointer to "".
func TestRootOverrideFromContext(t *testing.T) {
	assert.Nil(t, rootOverrideFromContext(context.Background()))

	ctx := provider.ContextWithRootRunID(context.Background(), "root-1")
	override := rootOverrideFromContext(ctx)
	require.NotNil(t, override)
	assert.Equal(t, "root-1", *override)

	emptyCtx := provider.ContextWithRootRunID(context.Background(), "")
	assert.Nil(t, rootOverrideFromContext(emptyCtx))
}
