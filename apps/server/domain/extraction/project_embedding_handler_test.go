package extraction

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestProjectEmbeddingProgressStatsByProject pins the correctness of the
// parallelized ProjectEmbeddingHandler.Progress path (#1141): the object and
// chunk queue aggregates run concurrently (errgroup) but must still be scoped
// to the requested project and combined into one response, with foreign-project
// jobs excluded from both legs.
func TestProjectEmbeddingProgressStatsByProject(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	foreignProjectID := seedEmbeddingProject(t, ctx, db)

	adminID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id, first_name, last_name, created_at, updated_at)
		VALUES (?, ?, 'Progress', 'Admin', now(), now())`, adminID, adminID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
		VALUES (?, ?, 'project_admin', now())`, projectID, adminID).Exec(ctx)
	require.NoError(t, err)

	seedObjects := func(project string, n int) {
		for range n {
			objID := seedEmbeddingObject(t, ctx, db, project, uuid.NewString())
			seedEmbeddingJobStatus(t, ctx, db, objID, "pending", nil)
		}
	}
	seedChunks := func(project string, n int) {
		for range n {
			docID := seedChunkDocument(t, ctx, db, project)
			chunkID := seedChunk(t, ctx, db, docID, 0, uuid.NewString())
			seedChunkEmbeddingJobStatus(t, ctx, db, chunkID, "pending", nil)
		}
	}

	seedObjects(projectID, 2)
	seedObjects(foreignProjectID, 5)
	seedChunks(projectID, 3)
	seedChunks(foreignProjectID, 7)

	graphSvc := NewGraphEmbeddingJobsService(db, quietLogger(), nil)
	chunkSvc := NewChunkEmbeddingJobsService(db, quietLogger(), nil)
	tokenSvc := apitoken.NewService(db, apitoken.NewRepository(db, quietLogger()), nil, quietLogger())
	h := NewProjectEmbeddingHandler(graphSvc, chunkSvc, tokenSvc)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/embeddings/progress", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(projectID)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: adminID})

	require.NoError(t, h.Progress(c))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp ProjectEmbeddingProgressResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Objects)
	require.NotNil(t, resp.Chunks)
	require.Equal(t, int64(2), resp.Objects.Pending, "object stats must be scoped to the project")
	require.Equal(t, int64(3), resp.Chunks.Pending, "chunk stats must be scoped to the project")

	// Relationship queue counts are served by GET /api/embeddings/progress, not
	// this project-scoped endpoint. Pin that the response shape no longer carries
	// an always-nil relationships field (issue #1150).
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotContains(t, body, "relationships")
}
