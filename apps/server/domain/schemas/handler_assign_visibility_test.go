package schemas_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// assignNoopSchemaProvider satisfies graph.SchemaProvider for the assign path:
// only InvalidateProjectCache is reached (schema cache invalidation on a real
// assign), never GetProjectSchemas.
type assignNoopSchemaProvider struct{}

func (assignNoopSchemaProvider) GetProjectSchemas(context.Context, string) (*graph.ExtractionSchemas, error) {
	return nil, nil
}

func (assignNoopSchemaProvider) InvalidateProjectCache(string) {}

// seedAssignSchema inserts a kb.graph_schemas row and returns its id. projectID
// may be nil (a global builtin); source is 'custom' for project-owned rows and
// 'builtin' for global builtins.
func seedAssignSchema(t *testing.T, ctx context.Context, db bun.IDB, name string, projectID *string, source string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, project_id, object_type_schemas, source)
		VALUES (?, ?, '1.0.0', ?, '{}'::jsonb, ?)
	`, id, name, projectID, source).Exec(ctx)
	require.NoError(t, err)
	return id
}

// assignTestServer builds an echo router that serves the real AssignPack handler
// with the production error handler and an auth-injecting middleware, so the
// visibility-gated seam is exercised exactly as it is in production (including
// the 404 the error handler emits for a non-visible schema).
func assignTestServer(h *schemas.Handler) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(slog.Default())
	e.POST("/api/schemas/projects/:projectId/assign", h.AssignPack, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: uuid.NewString()})
			return next(c)
		}
	})
	return e
}

func postAssign(t *testing.T, e *echo.Echo, projectID, schemaID string, dryRun bool) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"schema_id": schemaID, "dry_run": dryRun})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/schemas/projects/"+projectID+"/assign", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestAssignPackHandlerVisibility is the REST fail-closed guard for #1129: the
// POST /api/schemas/projects/:projectId/assign handler must route through
// AssignVisiblePack (→ GetAssignablePack) so a foreign project's schema is
// refused 404 in both real and dry-run modes, while an owned or builtin schema
// still succeeds. Reverting the handler to AssignPack makes the foreign case
// pass (red).
func TestAssignPackHandlerVisibility(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "schemasassign")
	defer testDB.Close()
	db := testDB.GetDB()
	log := slog.Default()

	repo := schemas.NewRepository(db, log)
	graphRepo := graph.NewRepository(db, log, testDB.Config)
	graphSvc := graph.NewService(graphRepo, log, assignNoopSchemaProvider{}, nil, nil, nil, nil, nil, nil, nil)
	svc := schemas.NewService(repo, graphSvc, log, testDB.Config)
	h := schemas.NewHandler(svc)
	e := assignTestServer(h)

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	foreignOrgID := uuid.NewString()
	foreignProjectID := uuid.NewString()

	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: "Caller Project",
	}, uuid.NewString()))
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: foreignProjectID, OrgID: foreignOrgID, Name: "Foreign Project",
	}, uuid.NewString()))

	foreignSchema := seedAssignSchema(t, ctx, db, "foreign-secret-schema", &foreignProjectID, "custom")
	ownedSchema := seedAssignSchema(t, ctx, db, "owned-schema", &projectID, "custom")
	builtinSchema := seedAssignSchema(t, ctx, db, "builtin-schema", nil, "builtin")

	assertStatus := func(rec *httptest.ResponseRecorder, want int) {
		t.Helper()
		require.Equal(t, want, rec.Code, "body: %s", rec.Body.String())
	}

	t.Run("foreign schema refused 404 (real)", func(t *testing.T) {
		rec := postAssign(t, e, projectID, foreignSchema, false)
		assertStatus(rec, http.StatusNotFound)
	})

	t.Run("foreign schema refused 404 (dry-run)", func(t *testing.T) {
		rec := postAssign(t, e, projectID, foreignSchema, true)
		assertStatus(rec, http.StatusNotFound)
	})

	t.Run("owned schema succeeds 201", func(t *testing.T) {
		rec := postAssign(t, e, projectID, ownedSchema, false)
		assertStatus(rec, http.StatusCreated)
	})

	t.Run("builtin schema succeeds 201", func(t *testing.T) {
		rec := postAssign(t, e, projectID, builtinSchema, false)
		assertStatus(rec, http.StatusCreated)
	})

	t.Run("owned schema dry-run succeeds 200", func(t *testing.T) {
		rec := postAssign(t, e, projectID, ownedSchema, true)
		assertStatus(rec, http.StatusOK)
	})
}
