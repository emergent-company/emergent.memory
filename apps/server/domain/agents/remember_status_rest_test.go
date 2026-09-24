package agents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// errNoRowsDriver is a database/sql driver whose connections fail every query
// with sql.ErrNoRows, so Repository lookups resolve as "no rows" (nil, nil)
// without a live database. Used to exercise the remember-status not-found path.
type errNoRowsDriver struct{}

func (errNoRowsDriver) Open(string) (driver.Conn, error) { return errNoRowsConn{}, nil }

type errNoRowsConn struct{}

func (errNoRowsConn) Prepare(string) (driver.Stmt, error) { return nil, sql.ErrNoRows }
func (errNoRowsConn) Close() error                        { return nil }
func (errNoRowsConn) Begin() (driver.Tx, error)           { return nil, sql.ErrNoRows }
func (errNoRowsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, sql.ErrNoRows
}
func (errNoRowsConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, sql.ErrNoRows
}

var registerErrNoRowsOnce sync.Once

// newNoRowsRepository returns a Repository backed by a driver that yields no
// rows for every query, so FindRunByIDForProject returns (nil, nil).
func newNoRowsRepository(t *testing.T) *Repository {
	t.Helper()
	registerErrNoRowsOnce.Do(func() { sql.Register("errnorows", errNoRowsDriver{}) })
	sqldb, err := sql.Open("errnorows", "")
	if err != nil {
		t.Fatalf("open errnorows db: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	return NewRepository(bun.NewDB(sqldb, pgdialect.New()))
}

// newRememberStatusMCPHandler builds an MCPToolHandler over a no-rows repo.
func newRememberStatusMCPHandler(t *testing.T) *MCPToolHandler {
	t.Helper()
	return NewMCPToolHandler(newNoRowsRepository(t), nil, slog.Default(), nil, nil)
}

// TestBuildRememberStatus_UnknownRunReturnsErrRunNotFound verifies the not-found
// sentinel: buildRememberStatus returns errRunNotFound (wrapped with the run id)
// for an unknown run, detectable via errors.Is.
func TestBuildRememberStatus_UnknownRunReturnsErrRunNotFound(t *testing.T) {
	h := newRememberStatusMCPHandler(t)

	result, err := h.buildRememberStatus(context.Background(), "proj-1", "run-unknown")
	if result != nil {
		t.Errorf("result = %v, want nil for not-found run", result)
	}
	if err == nil {
		t.Fatal("expected error for unknown run")
	}
	if !errors.Is(err, errRunNotFound) {
		t.Errorf("errors.Is(err, errRunNotFound) = false, err = %v", err)
	}
	want := "agent run not found: run-unknown"
	if err.Error() != want {
		t.Errorf("err.Error() = %q, want %q", err.Error(), want)
	}
}

// TestExecuteRememberStatus_MissingRunID verifies the MCP wrapper keeps the
// "run_id is required" error message (no DB touched).
func TestExecuteRememberStatus_MissingRunID(t *testing.T) {
	h := newRememberStatusMCPHandler(t)

	res, err := h.ExecuteRememberStatus(context.Background(), "proj-1", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "run_id is required") {
		t.Errorf("result text = %q, want run_id is required", res.Content[0].Text)
	}
}

// TestExecuteRememberStatus_UnknownRunReturnsNotFoundMessage verifies the MCP
// wrapper surfaces the not-found message (agent run not found: <id>).
func TestExecuteRememberStatus_UnknownRunReturnsNotFoundMessage(t *testing.T) {
	h := newRememberStatusMCPHandler(t)

	res, err := h.ExecuteRememberStatus(context.Background(), "proj-1", map[string]any{"run_id": "run-unknown"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "agent run not found: run-unknown") {
		t.Errorf("result text = %q, want not-found message", res.Content[0].Text)
	}
}

// TestGetRunRememberStatus_NotFoundMapsTo404 verifies the REST handler maps the
// errRunNotFound sentinel to HTTP 404.
func TestGetRunRememberStatus_NotFoundMapsTo404(t *testing.T) {
	h := &Handler{mcpTools: newRememberStatusMCPHandler(t)}
	c, rec := newRememberStatusEchoContext("proj-1", "run-unknown")

	err := h.GetRunRememberStatus(c)

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T: %v", err, err)
	}
	if appErr.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", appErr.HTTPStatus)
	}
	_ = rec
}

// TestGetRunRememberStatus_MissingParamsReturn400 verifies the REST handler
// rejects missing projectId/runId with 400 before any aggregation runs.
func TestGetRunRememberStatus_MissingParamsReturn400(t *testing.T) {
	h := &Handler{}
	tests := []struct {
		name    string
		project string
		runID   string
	}{
		{"missing runId", "proj-1", ""},
		{"missing projectId", "", "run-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newRememberStatusEchoContext(tt.project, tt.runID)

			err := h.GetRunRememberStatus(c)

			var appErr *apperror.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("expected *apperror.Error, got %T: %v", err, err)
			}
			if appErr.HTTPStatus != http.StatusBadRequest {
				t.Errorf("HTTPStatus = %d, want 400", appErr.HTTPStatus)
			}
		})
	}
}

// newRememberStatusEchoContext builds an authenticated Echo context with
// projectId/runId path params set, simulating RequireAuth + RequireProjectTokenScope.
func newRememberStatusEchoContext(projectID, runID string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/agent-runs/"+runID+"/remember-status", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("projectId", "runId")
	c.SetParamValues(projectID, runID)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-test-id", ProjectID: projectID})
	return c, rec
}
