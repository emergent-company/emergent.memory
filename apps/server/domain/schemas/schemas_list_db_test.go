package schemas_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// insertCatalogSchema inserts a kb.graph_schemas row with a fixed updated_at
// (used to assert ordering) and returns its id.
func insertCatalogSchema(t *testing.T, ctx context.Context, db bun.IDB, name string, desc *string, projectID *string, updatedAt time.Time) string {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	schema := &schemas.GraphMemorySchema{
		ID:                      uuid.NewString(),
		Name:                    name,
		Version:                 "1.0.0",
		Description:             desc,
		ObjectTypeSchemas:       json.RawMessage(`{}`),
		RelationshipTypeSchemas: json.RawMessage(`[]`),
		UIConfigs:               json.RawMessage(`{}`),
		ExtractionPrompts:       json.RawMessage(`{}`),
		ProjectID:               projectID,
		PublishedAt:             &now,
		CreatedAt:               now,
		UpdatedAt:               updatedAt,
	}
	_, err := db.NewInsert().Model(schema).Exec(ctx)
	require.NoError(t, err, "insert graph schema")
	return schema.ID
}

func timePtrOffset(t *testing.T, base time.Time, seconds int) time.Time {
	t.Helper()
	return base.Add(time.Duration(seconds) * time.Second)
}

// getListPacks drives the real ListPacks handler over an echo route so query
// params flow through the same code path as production.
func getListPacks(t *testing.T, h *schemas.Handler, projectID, query string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.GET("/projects/:projectId", h.ListPacks)
	req := httptest.NewRequest(http.MethodGet, "/projects/"+projectID+query, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestListPacksPaginationHandler verifies the REST handler's limit/offset
// handling mirrors the MCP schema-list tool (limit clamp to [1,100], offset
// passthrough), that the response echoes the applied values back, and that the
// catalog is strictly project-scoped (NULL-project rows are excluded).
func TestListPacksPaginationHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB, err := testutil.SetupTestDB(ctx, "schemascat")
	if err != nil {
		t.Skipf("skipping: test database unavailable: %v", err)
	}
	defer testDB.Close()
	db := testDB.GetDB()

	repo := schemas.NewRepository(db, slog.Default())
	h := schemas.NewHandler(schemas.NewService(repo, nil, slog.Default()))

	projectID := uuid.NewString()
	base := time.Now().UTC().Truncate(time.Microsecond)
	desc := func(s string) *string { return &s }
	alphaID := insertCatalogSchema(t, ctx, db, "alpha-pack", desc("alpha schema"), &projectID, timePtrOffset(t, base, 30))
	globalID := insertCatalogSchema(t, ctx, db, "global-pack", desc("global schema"), nil, timePtrOffset(t, base, 20))
	betaID := insertCatalogSchema(t, ctx, db, "beta-pack", desc("beta schema"), &projectID, timePtrOffset(t, base, 10))

	// Strictly project-scoped: only alpha and beta (both owned) are visible,
	// ordered DESC by updated_at. The NULL-project globalID row is excluded.
	orderedIDs := []string{alphaID, betaID}
	const wantTotal = 2

	decode := func(t *testing.T, rec *httptest.ResponseRecorder) schemas.SchemaListResponse {
		t.Helper()
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var resp schemas.SchemaListResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}
	names := func(resp schemas.SchemaListResponse) []string {
		out := make([]string, len(resp.Schemas))
		for i, s := range resp.Schemas {
			out[i] = s.Name
		}
		return out
	}
	// noGlobal asserts a response never includes the NULL-project row.
	noGlobal := func(t *testing.T, resp schemas.SchemaListResponse) {
		t.Helper()
		for _, s := range resp.Schemas {
			assert.NotEqual(t, globalID, s.ID, "NULL-project row must not be returned")
			assert.NotEqual(t, "global-pack", s.Name, "NULL-project row must not be returned")
		}
		assert.Equal(t, wantTotal, resp.Total, "total must exclude NULL-project rows")
	}

	t.Run("limit zero clamps to one", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, "?limit=0"))
		assert.Equal(t, 1, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		require.Len(t, resp.Schemas, 1)
		assert.Equal(t, alphaID, resp.Schemas[0].ID)
		assert.Equal(t, wantTotal, resp.Total)
		noGlobal(t, resp)
	})

	t.Run("negative limit clamps to one", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, "?limit=-7"))
		assert.Equal(t, 1, resp.Limit)
		require.Len(t, resp.Schemas, 1)
		noGlobal(t, resp)
	})

	t.Run("absent limit defaults to twenty", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, ""))
		assert.Equal(t, 20, resp.Limit)
		require.Len(t, resp.Schemas, 2)
		noGlobal(t, resp)
	})

	t.Run("invalid limit defaults to twenty", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, "?limit=abc"))
		assert.Equal(t, 20, resp.Limit)
		require.Len(t, resp.Schemas, 2)
		noGlobal(t, resp)
	})

	t.Run("limit above cap clamps to one hundred", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, "?limit=500"))
		assert.Equal(t, 100, resp.Limit)
		require.Len(t, resp.Schemas, 2)
		noGlobal(t, resp)
	})

	t.Run("offset passthrough pages the list", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, "?limit=1&offset=1"))
		assert.Equal(t, 1, resp.Limit)
		assert.Equal(t, 1, resp.Offset)
		require.Len(t, resp.Schemas, 1)
		assert.Equal(t, betaID, resp.Schemas[0].ID)
		assert.Equal(t, wantTotal, resp.Total)
		noGlobal(t, resp)
	})

	t.Run("offset past end returns empty page", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, "?limit=2&offset=9"))
		assert.Equal(t, 9, resp.Offset)
		assert.Empty(t, resp.Schemas)
		assert.Equal(t, wantTotal, resp.Total)
	})

	t.Run("negative offset treated as zero", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, "?limit=10&offset=-3"))
		assert.Equal(t, 0, resp.Offset)
		require.Len(t, resp.Schemas, 2)
		noGlobal(t, resp)
	})

	t.Run("full catalog keeps updated_at desc order", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, ""))
		assert.Equal(t, names(resp), []string{"alpha-pack", "beta-pack"})
		got := make([]string, len(resp.Schemas))
		for i, s := range resp.Schemas {
			got[i] = s.ID
		}
		assert.Equal(t, orderedIDs, got)
		noGlobal(t, resp)
	})

	t.Run("NULL-project row is excluded", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, ""))
		noGlobal(t, resp)
		for _, s := range resp.Schemas {
			assert.NotEqual(t, globalID, s.ID)
		}
	})

	t.Run("search filters and total follows filter", func(t *testing.T) {
		resp := decode(t, getListPacks(t, h, projectID, "?search=beta"))
		require.Len(t, resp.Schemas, 1)
		assert.Equal(t, betaID, resp.Schemas[0].ID)
		assert.Equal(t, 1, resp.Total)
	})
}

// TestListSchemaPacksRepository verifies repository-level query correctness:
// strict project scoping (NULL-project rows are never returned), updated_at
// DESC ordering, search, pagination, count/list consistency, and the
// MCP-parity wire shape (visibility always emitted; project_id kept for owned
// rows).
func TestListSchemaPacksRepository(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB, err := testutil.SetupTestDB(ctx, "schemaslist")
	if err != nil {
		t.Skipf("skipping: test database unavailable: %v", err)
	}
	defer testDB.Close()
	db := testDB.GetDB()

	repo := schemas.NewRepository(db, slog.Default())
	projectID := uuid.NewString()
	base := time.Now().UTC().Truncate(time.Microsecond)
	desc := func(s string) *string { return &s }

	// updated_at (newest first) for project-owned rows:
	// alpha(40) beta(20) delta(10) alpha-zed(0). gamma is NULL-project and
	// must never appear in a project catalog.
	aID := insertCatalogSchema(t, ctx, db, "alpha", desc("photo album"), &projectID, timePtrOffset(t, base, 40))
	gID := insertCatalogSchema(t, ctx, db, "gamma", nil, nil, timePtrOffset(t, base, 30)) // NULL project, excluded
	bID := insertCatalogSchema(t, ctx, db, "beta", desc("notes"), &projectID, timePtrOffset(t, base, 20))
	dID := insertCatalogSchema(t, ctx, db, "delta", desc(""), &projectID, timePtrOffset(t, base, 10))
	zID := insertCatalogSchema(t, ctx, db, "alpha-zed", desc("zen"), &projectID, timePtrOffset(t, base, 0))

	t.Run("project catalog contains only owned rows in desc order", func(t *testing.T) {
		rows, total, err := repo.ListSchemaPacks(ctx, projectID, "", 20, 0)
		require.NoError(t, err)
		assert.Equal(t, 4, total)
		require.Len(t, rows, 4)
		got := make([]string, len(rows))
		for i, r := range rows {
			got[i] = r.ID
		}
		assert.Equal(t, []string{aID, bID, dID, zID}, got)
		for _, r := range rows {
			assert.NotEqual(t, gID, r.ID, "NULL-project row must not be returned")
			assert.Equal(t, projectID, r.ProjectID)
		}
	})

	t.Run("NULL-project row is never returned", func(t *testing.T) {
		// Even when the search term matches the NULL-project row by name, it is
		// excluded because the query is strictly scoped to the project.
		rows, total, err := repo.ListSchemaPacks(ctx, projectID, "gamma", 20, 0)
		require.NoError(t, err)
		assert.Equal(t, 0, total)
		assert.Empty(t, rows)
	})

	t.Run("empty project id is rejected", func(t *testing.T) {
		_, _, err := repo.ListSchemaPacks(ctx, "", "", 20, 0)
		require.Error(t, err)
	})

	t.Run("search matches name case-insensitively", func(t *testing.T) {
		rows, total, err := repo.ListSchemaPacks(ctx, projectID, "ALPHA", 20, 0)
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		require.Len(t, rows, 2)
		assert.Equal(t, aID, rows[0].ID) // "alpha" before "alpha-zed" (newer)
		assert.Equal(t, zID, rows[1].ID)
	})

	t.Run("search matches description", func(t *testing.T) {
		rows, total, err := repo.ListSchemaPacks(ctx, projectID, "album", 20, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, rows, 1)
		assert.Equal(t, aID, rows[0].ID)
	})

	t.Run("pagination pages full ordered set while total stays constant", func(t *testing.T) {
		page1, total1, err := repo.ListSchemaPacks(ctx, projectID, "", 2, 0)
		require.NoError(t, err)
		assert.Equal(t, 4, total1)
		require.Len(t, page1, 2)
		assert.Equal(t, []string{aID, bID}, []string{page1[0].ID, page1[1].ID})

		page2, total2, err := repo.ListSchemaPacks(ctx, projectID, "", 2, 2)
		require.NoError(t, err)
		assert.Equal(t, 4, total2)
		require.Len(t, page2, 2)
		assert.Equal(t, []string{dID, zID}, []string{page2[0].ID, page2[1].ID})

		page3, _, err := repo.ListSchemaPacks(ctx, projectID, "", 2, 4)
		require.NoError(t, err)
		assert.Empty(t, page3)
	})

	t.Run("count consistency with search filter", func(t *testing.T) {
		rows, total, err := repo.ListSchemaPacks(ctx, projectID, "alpha", 1, 1)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, zID, rows[0].ID) // page 2 of [alpha, alpha-zed]
		assert.Equal(t, 2, total)
	})

	t.Run("wire shape mirrors MCP SchemaInfo", func(t *testing.T) {
		rows, _, err := repo.ListSchemaPacks(ctx, projectID, "", 20, 0)
		require.NoError(t, err)
		require.Len(t, rows, 4)

		raw, err := json.Marshal(rows)
		require.NoError(t, err)
		var decoded []map[string]any
		require.NoError(t, json.Unmarshal(raw, &decoded))

		var owned map[string]any
		for _, m := range decoded {
			if m["name"] == "gamma" {
				t.Fatalf("NULL-project row must not appear in payload: %v", m)
			}
			if m["name"] == "alpha" {
				owned = m
			}
		}
		require.NotNil(t, owned, "expected project row in payload")

		// visibility is always emitted (MCP tag has no omitempty).
		for _, m := range decoded {
			_, hasVis := m["visibility"]
			assert.True(t, hasVis, "row %v missing visibility key", m["name"])
		}
		// project-owned row keeps project_id.
		assert.Equal(t, projectID, owned["project_id"])
	})
}
