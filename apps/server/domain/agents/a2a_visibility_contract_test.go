package agents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// a2aVisibilityDriver is a database/sql driver that serves the two lookups in
// resolveA2AAgentBySkillID without a live Postgres:
//
//   - the external lookup (FindExternalAgentBySlug → FindExternalAgentDefinitions)
//     returns no rows;
//   - the fallback lookup (FindAgentDefinitionBySlug) returns a single
//     internal-visibility definition named "internal-agent".
//
// bun inlines bind values into the SQL string, so the two queries are
// distinguished by their WHERE clause (see QueryContext) rather than arg count.
type a2aVisibilityDriver struct{}

func (a2aVisibilityDriver) Open(string) (driver.Conn, error) { return &a2aVisibilityConn{}, nil }

type a2aVisibilityConn struct{}

func (*a2aVisibilityConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (*a2aVisibilityConn) Close() error              { return nil }
func (*a2aVisibilityConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }
func (*a2aVisibilityConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("not implemented")
}

func (*a2aVisibilityConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	// bun inlines bind values into the SQL string (no separate args), so the two
	// lookups are distinguished by their WHERE clause rather than arg count:
	//   - FindExternalAgentDefinitions renders "... AND (visibility = 'external')"
	//   - FindAgentDefinitionBySlug renders only "WHERE (project_id = ...)"
	switch {
	case strings.Contains(query, "visibility = "):
		// External lookup: no external-visibility definition matches.
		return newA2ADefinitionRows(nil), nil
	case strings.Contains(query, `"kb"."agent_definitions"`):
		// Fallback lookup: a single internal-visibility definition whose ACP slug
		// is "internal-agent" (name "internal-agent").
		return newA2ADefinitionRows([][]driver.Value{
			{"def-internal", "proj-test-id", "internal-agent", string(VisibilityInternal)},
		}), nil
	default:
		// Any other query (e.g. the runtime-agent FindByName) must not be reached
		// on this contract path; yield no rows rather than a mis-shaped result.
		return nil, sql.ErrNoRows
	}
}

// a2aDefinitionRows returns AgentDefinition rows over the columns bun maps from
// kb.agent_definitions. Columns are returned by name so bun scans them into the
// matching struct fields (id, project_id, name, visibility); all other fields
// stay zero-valued.
type a2aDefinitionRows struct {
	columns []string
	rows    [][]driver.Value
	idx     int
}

func newA2ADefinitionRows(rows [][]driver.Value) *a2aDefinitionRows {
	return &a2aDefinitionRows{
		columns: []string{"id", "project_id", "name", "visibility"},
		rows:    rows,
	}
}

func (r *a2aDefinitionRows) Columns() []string { return r.columns }
func (r *a2aDefinitionRows) Close() error      { return nil }
func (r *a2aDefinitionRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

var registerA2AVisibilityOnce sync.Once

// newA2AVisibilityRepository builds a Repository backed by a2aVisibilityDriver.
func newA2AVisibilityRepository(t *testing.T) *Repository {
	t.Helper()
	registerA2AVisibilityOnce.Do(func() { sql.Register("agents_a2a_visibility", a2aVisibilityDriver{}) })
	sqldb, err := sql.Open("agents_a2a_visibility", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return NewRepository(bun.NewDB(sqldb, pgdialect.New()))
}

func newA2AVisibilityHandler(t *testing.T) *A2AHandler {
	t.Helper()
	return &A2AHandler{repo: newA2AVisibilityRepository(t), log: slog.Default()}
}

// TestA2ASendMessage_InternalSlug_Returns400SkillNotFound pins the security
// contract on the send path: a message.metadata["skillId"] that resolves to an
// internal-visibility definition (or is unknown) is a hard HTTP 400
// SKILL_NOT_FOUND. It must never fall back to the project's CLI assistant — a
// fallback would proceed past resolution (creating a run and invoking the
// executor) instead of returning this error envelope.
func TestA2ASendMessage_InternalSlug_Returns400SkillNotFound(t *testing.T) {
	h := newA2AVisibilityHandler(t)
	body := `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hello"}],"metadata":{"skillId":"internal-agent"}}}`
	c, rec := newA2AMessageContext(http.MethodPost, "/message:send", body, true)

	require.NoError(t, h.SendMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "SKILL_NOT_FOUND", mustA2AErr(t, rec).Error.Details[0].Reason)
}

// TestA2AStreamMessage_InternalSlug_Returns400SkillNotFound pins the same
// contract on the stream path, which shares resolveA2AAgent via streamNewTask.
func TestA2AStreamMessage_InternalSlug_Returns400SkillNotFound(t *testing.T) {
	h := newA2AVisibilityHandler(t)
	body := `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hello"}],"metadata":{"skillId":"internal-agent"}}}`
	c, rec := newA2AMessageContext(http.MethodPost, "/message:stream", body, true)

	require.NoError(t, h.StreamMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "SKILL_NOT_FOUND", mustA2AErr(t, rec).Error.Details[0].Reason)
}
