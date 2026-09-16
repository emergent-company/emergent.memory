package agents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// errUUIDSyntax mimics the Postgres error raised when a non-UUID string is
// compared against a uuid column (SQLSTATE 22P02). It is deliberately NOT
// sql.ErrNoRows, matching production behaviour for GET .../agent-definitions/chat.
var errUUIDSyntax = errors.New(`ERROR: invalid input syntax for type uuid: "chat" (SQLSTATE 22P02)`)

// uuidSyntaxErrorDriver is a database/sql driver whose connections fail every
// query with a Postgres 22P02-style error, so an unguarded repository lookup
// returns a raw DB error (which callers wrap as HTTP 500).
type uuidSyntaxErrorDriver struct{}

func (uuidSyntaxErrorDriver) Open(string) (driver.Conn, error) { return uuidSyntaxErrorConn{}, nil }

type uuidSyntaxErrorConn struct{}

func (uuidSyntaxErrorConn) Prepare(string) (driver.Stmt, error) { return nil, errUUIDSyntax }
func (uuidSyntaxErrorConn) Close() error                        { return nil }
func (uuidSyntaxErrorConn) Begin() (driver.Tx, error)           { return nil, errUUIDSyntax }
func (uuidSyntaxErrorConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, errUUIDSyntax
}
func (uuidSyntaxErrorConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errUUIDSyntax
}

// singleRowDriver returns exactly one row (id, project_id, name) for any query.
// The DSN is used as the returned id, so each test can choose the row identity.
type singleRowDriver struct{}

func (singleRowDriver) Open(dsn string) (driver.Conn, error) { return &singleRowConn{id: dsn}, nil }

type singleRowConn struct{ id string }

func (*singleRowConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (*singleRowConn) Close() error                        { return nil }
func (*singleRowConn) Begin() (driver.Tx, error)           { return nil, errors.New("not implemented") }
func (c *singleRowConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &singleRowRows{id: c.id}, nil
}
func (*singleRowConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("not implemented")
}

type singleRowRows struct {
	id   string
	done bool
}

func (*singleRowRows) Columns() []string { return []string{"id", "project_id", "name"} }
func (*singleRowRows) Close() error      { return nil }
func (r *singleRowRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.id
	dest[1] = "proj-test-id"
	dest[2] = "resolved-name"
	return nil
}

var (
	registerUUIDSyntaxOnce sync.Once
	registerSingleRowOnce  sync.Once
)

func newUUIDSyntaxRepository(t *testing.T) *Repository {
	t.Helper()
	registerUUIDSyntaxOnce.Do(func() { sql.Register("agents_uuid_syntax", uuidSyntaxErrorDriver{}) })
	sqldb, err := sql.Open("agents_uuid_syntax", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return NewRepository(bun.NewDB(sqldb, pgdialect.New()))
}

func newSingleRowRepository(t *testing.T, id string) *Repository {
	t.Helper()
	registerSingleRowOnce.Do(func() { sql.Register("agents_single_row", singleRowDriver{}) })
	sqldb, err := sql.Open("agents_single_row", id)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return NewRepository(bun.NewDB(sqldb, pgdialect.New()))
}

// TestFindDefinitionByID_NonUUIDReturnsNilNil is the regression test for
// MEMORY-UI-1Z / MEMORY-UI-21: a non-UUID id ("chat" — an agent name passed as
// an id) must resolve as not-found without touching the uuid column.
func TestFindDefinitionByID_NonUUIDReturnsNilNil(t *testing.T) {
	repo := newUUIDSyntaxRepository(t)
	projectID := "proj-test-id"

	def, err := repo.FindDefinitionByID(context.Background(), "chat", &projectID)

	require.NoError(t, err, "non-UUID id must not surface a raw DB error")
	assert.Nil(t, def)
}

// TestFindDefinitionByID_ValidUUIDStillHitsDBAndPropagatesError guards against
// over-broad validation: a valid UUID id is still passed to the database, and
// genuine DB errors are propagated (not swallowed as not-found).
func TestFindDefinitionByID_ValidUUIDStillHitsDBAndPropagatesError(t *testing.T) {
	repo := newUUIDSyntaxRepository(t)
	projectID := "proj-test-id"

	def, err := repo.FindDefinitionByID(context.Background(), uuid.NewString(), &projectID)

	require.Error(t, err, "valid UUID ids must still query the DB; DB errors must propagate")
	assert.Nil(t, def)
}

// TestFindDefinitionByID_ValidUUIDPresentReturnsRow verifies the happy path is
// unaffected: a valid UUID that exists returns the scanned definition.
func TestFindDefinitionByID_ValidUUIDPresentReturnsRow(t *testing.T) {
	id := uuid.NewString()
	repo := newSingleRowRepository(t, id)

	def, err := repo.FindDefinitionByID(context.Background(), id, nil)

	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Equal(t, id, def.ID)
	assert.Equal(t, "proj-test-id", def.ProjectID)
	assert.Equal(t, "resolved-name", def.Name)
}

// TestFindDefinitionByID_ValidUUIDAbsentReturnsNilNil verifies a valid UUID that
// does not exist still resolves as (nil, nil).
func TestFindDefinitionByID_ValidUUIDAbsentReturnsNilNil(t *testing.T) {
	repo := newNoRowsRepository(t)

	def, err := repo.FindDefinitionByID(context.Background(), uuid.NewString(), nil)

	require.NoError(t, err)
	assert.Nil(t, def)
}

// newDefinitionEchoContext builds an authenticated Echo context with the :id
// path param set, simulating a request through RequireAuth.
func newDefinitionEchoContext(projectID, id string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/agent-definitions/"+id, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("projectId", "id")
	c.SetParamValues(projectID, id)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-test-id", ProjectID: projectID})
	return c, rec
}

// TestGetDefinition_NonUUIDIDReturns404 pins the user-visible symptom:
// GET /api/projects/{pid}/agent-definitions/chat must return 404, not 500.
func TestGetDefinition_NonUUIDIDReturns404(t *testing.T) {
	h := &Handler{repo: newUUIDSyntaxRepository(t)}
	c, _ := newDefinitionEchoContext("proj-test-id", "chat")

	err := h.GetDefinition(c)

	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T: %v", err, err)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}
