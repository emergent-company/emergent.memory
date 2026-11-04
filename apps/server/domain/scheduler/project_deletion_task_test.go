package scheduler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
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
// DB-free fake driver
//
// projectDeletionFakeDB is an in-memory state used by projectDeletionFakeDriver
// to exercise ProjectDeletionTask.Run without a live PostgreSQL. The SELECT
// returns the current due ID set; each Exec removes (or fails to remove) one ID.
// =============================================================================

type projectDeletionFakeDB struct {
	mu          sync.Mutex
	due         []string
	failIDs     map[string]bool
	zeroRowIDs  map[string]bool
	selectErr   error
	selectCalls int
	deleteSQL   []string
	deletedIDs  []string
}

var (
	projectDeletionRegistryMu sync.Mutex
	projectDeletionRegistry   = map[string]*projectDeletionFakeDB{}
	projectDeletionRegister   sync.Once
)

func newProjectDeletionTask(t *testing.T, state *projectDeletionFakeDB) *ProjectDeletionTask {
	t.Helper()
	dsn := t.Name()
	projectDeletionRegister.Do(func() { sql.Register("fakedeletion", projectDeletionFakeDriver{}) })

	projectDeletionRegistryMu.Lock()
	projectDeletionRegistry[dsn] = state
	projectDeletionRegistryMu.Unlock()

	sqldb, err := sql.Open("fakedeletion", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	return NewProjectDeletionTask(bun.NewDB(sqldb, pgdialect.New()), slog.Default())
}

type projectDeletionFakeDriver struct{}

func (projectDeletionFakeDriver) Open(dsn string) (driver.Conn, error) {
	projectDeletionRegistryMu.Lock()
	state := projectDeletionRegistry[dsn]
	projectDeletionRegistryMu.Unlock()
	if state == nil {
		return nil, errors.New("no fake deletion state for dsn " + dsn)
	}
	return &projectDeletionFakeConn{state: state}, nil
}

type projectDeletionFakeConn struct{ state *projectDeletionFakeDB }

func (c *projectDeletionFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *projectDeletionFakeConn) Close() error { return nil }
func (c *projectDeletionFakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("tx unsupported")
}

func (c *projectDeletionFakeConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selectCalls++
	if s.selectErr != nil {
		return nil, s.selectErr
	}
	return &singleColumnRows{ids: append([]string(nil), s.due...)}, nil
}

func (c *projectDeletionFakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteSQL = append(s.deleteSQL, query)

	var id string
	if len(args) > 0 {
		if v, ok := args[0].Value.(string); ok {
			id = v
		}
	}
	// bun renders string args as inlined SQL literals rather than placeholders
	// (e.g. `id = 'p1'`), so fall back to parsing the id out of the statement.
	if id == "" {
		const marker = "id = '"
		if i := strings.Index(query, marker); i >= 0 {
			rest := query[i+len(marker):]
			if j := strings.Index(rest, "'"); j >= 0 {
				id = rest[:j]
			}
		}
	}
	if s.failIDs[id] {
		return nil, errors.New("cascade failed")
	}

	idx := -1
	for i, v := range s.due {
		if v == id {
			idx = i
			break
		}
	}
	if idx < 0 || s.zeroRowIDs[id] {
		return driver.RowsAffected(0), nil
	}
	s.due = append(s.due[:idx], s.due[idx+1:]...)
	s.deletedIDs = append(s.deletedIDs, id)
	return driver.RowsAffected(1), nil
}

type singleColumnRows struct {
	ids []string
	pos int
}

func (r *singleColumnRows) Columns() []string { return []string{"id"} }
func (r *singleColumnRows) Close() error      { return nil }
func (r *singleColumnRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.ids) {
		return io.EOF
	}
	dest[0] = r.ids[r.pos]
	r.pos++
	return nil
}

// =============================================================================
// ProjectDeletionTask.Run
// =============================================================================

func TestProjectDeletionTask_Run_IsolatesFailures(t *testing.T) {
	state := &projectDeletionFakeDB{
		due:     []string{"p1", "p2", "p3"},
		failIDs: map[string]bool{"p2": true},
	}
	task := newProjectDeletionTask(t, state)

	require.NoError(t, task.Run(context.Background()))

	assert.Equal(t, []string{"p1", "p3"}, state.deletedIDs, "poison pill must not block others")
	assert.Contains(t, state.due, "p2", "failed project stays due for a later run")
	assert.Len(t, state.deleteSQL, 3, "each due project is attempted with its own statement")
}

func TestProjectDeletionTask_Run_UsesGuardedDelete(t *testing.T) {
	state := &projectDeletionFakeDB{due: []string{"p1"}}
	task := newProjectDeletionTask(t, state)

	require.NoError(t, task.Run(context.Background()))

	require.Len(t, state.deleteSQL, 1)
	q := state.deleteSQL[0]
	assert.Contains(t, q, "DELETE FROM kb.projects")
	assert.Contains(t, q, "deletion_scheduled_for IS NOT NULL",
		"guarded delete must re-check the schedule to close the restore TOCTOU window")
	assert.Contains(t, q, "deletion_scheduled_for <= now()")
}

func TestProjectDeletionTask_Run_ZeroDueIsSuccess(t *testing.T) {
	state := &projectDeletionFakeDB{}
	task := newProjectDeletionTask(t, state)

	require.NoError(t, task.Run(context.Background()))

	assert.Equal(t, 1, state.selectCalls)
	assert.Empty(t, state.deleteSQL)
}

func TestProjectDeletionTask_Run_AllPoisonStopsAfterOneBatch(t *testing.T) {
	state := &projectDeletionFakeDB{failIDs: map[string]bool{}}
	for i := 0; i < projectDeletionBatchSize; i++ {
		id := fmt.Sprintf("p%d", i)
		state.due = append(state.due, id)
		state.failIDs[id] = true
	}
	task := newProjectDeletionTask(t, state)

	require.NoError(t, task.Run(context.Background()))

	assert.Equal(t, 1, state.selectCalls, "a full batch of poison pills must not loop forever")
	assert.Len(t, state.deleteSQL, projectDeletionBatchSize)
	assert.Empty(t, state.deletedIDs)
}

func TestProjectDeletionTask_Run_GuardedZeroRowsNotCounted(t *testing.T) {
	// Simulates a concurrent restore: the row no longer matches the guard, so the
	// DELETE affects 0 rows and the project is not counted as purged.
	state := &projectDeletionFakeDB{
		due:        []string{"p1"},
		zeroRowIDs: map[string]bool{"p1": true},
	}
	task := newProjectDeletionTask(t, state)

	require.NoError(t, task.Run(context.Background()))

	assert.Empty(t, state.deletedIDs)
	assert.Len(t, state.deleteSQL, 1)
}

func TestProjectDeletionTask_Run_SelectErrorReturned(t *testing.T) {
	state := &projectDeletionFakeDB{selectErr: errors.New("db down")}
	task := newProjectDeletionTask(t, state)

	require.Error(t, task.Run(context.Background()))
}

// =============================================================================
// Duration-string env parsing (PROJECT_DELETION_SWEEP_INTERVAL)
// =============================================================================

func TestGetEnvDurationString(t *testing.T) {
	t.Setenv("TEST_DURATION_STRING", "2m30s")
	assert.Equal(t, 2*time.Minute+30*time.Second, getEnvDurationString("TEST_DURATION_STRING", time.Minute))

	t.Setenv("TEST_DURATION_STRING", "not-a-duration")
	assert.Equal(t, time.Minute, getEnvDurationString("TEST_DURATION_STRING", time.Minute))

	assert.Equal(t, time.Minute, getEnvDurationString("DEFINITELY_UNSET_DURATION_STRING", time.Minute))
}

func TestNewConfig_ProjectDeletionSweepIntervalIsDurationString(t *testing.T) {
	t.Setenv("PROJECT_DELETION_SWEEP_INTERVAL", "90s")
	cfg := NewConfig()
	assert.Equal(t, 90*time.Second, cfg.ProjectDeletionSweepInterval)

	t.Setenv("PROJECT_DELETION_SWEEP_INTERVAL", "garbage")
	cfg = NewConfig()
	assert.Equal(t, time.Minute, cfg.ProjectDeletionSweepInterval)
}
