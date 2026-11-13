package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/httputil"
)

// TestEnqueueWorkItemResolvesDefinitionOnlyAgent pins the third #727 surface: the
// queue-enqueue endpoint accepted an agent id or name that only existed as a
// kb.agent_definitions row (every agent created from the Agents page), 404'd,
// and never enqueued the run. It now materializes the definition-backed runtime
// row and queues the work against that id, because kb.agent_runs.agent_id
// references kb.agents.
func TestEnqueueWorkItemResolvesDefinitionOnlyAgent(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "enqueue_work_item_definition_resolution")
	t.Cleanup(tdb.Close)

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "Queue Agent", "project")

	repo := NewRepository(tdb.DB)
	require.NoError(t, repo.EnsureDefaultQueue(ctx, projectID))
	h := &Handler{repo: repo}

	countRuntimeRows := func() int {
		var n int
		require.NoError(t, tdb.DB.NewRaw(
			`SELECT count(*) FROM kb.agents WHERE project_id = ?`, projectID).Scan(ctx, &n))
		return n
	}
	require.Zero(t, countRuntimeRows(), "fixture already has a kb.agents row")

	enqueue := func(body string) (*httptest.ResponseRecorder, error) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(string(auth.UserContextKey), &auth.AuthUser{ProjectID: projectID})
		c.SetParamNames("projectId", "name")
		c.SetParamValues(projectID, DefaultQueueName)
		return rec, h.EnqueueWorkItem(c)
	}

	rec, err := enqueue(`{"agentName":"Queue Agent","message":"hello"}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, rec.Code, "body: %s", rec.Body.String())

	var resp httputil.APIResponse[EnqueueWorkItemResponseDTO]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data.RunID)
	require.Equal(t, DefaultQueueName, resp.Data.Queue)

	// The run is queued against the materialized runtime row, not the definition.
	require.NotEqual(t, defID, resp.Data.AgentID, "enqueued the definition id instead of the runtime id")
	require.Equal(t, 1, countRuntimeRows(), "enqueue did not materialize exactly one runtime agent")

	var runAgentID, strategy string
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT r.agent_id, a.strategy_type FROM kb.agent_runs r
		 JOIN kb.agents a ON a.id = r.agent_id WHERE r.id = ?`,
		resp.Data.RunID).Scan(ctx, &runAgentID, &strategy))
	require.Equal(t, resp.Data.AgentID, runAgentID)
	require.Equal(t, "agent-def:"+defID, strategy)

	// A second enqueue by definition id reuses the same runtime row.
	rec, err = enqueue(`{"agentId":"` + defID + `"}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, rec.Code, "body: %s", rec.Body.String())
	require.Equal(t, 1, countRuntimeRows(), "the second enqueue created a duplicate runtime agent")

	// An id in neither table still 404s and creates nothing.
	_, err = enqueue(`{"agentId":"11111111-1111-1111-1111-111111111111"}`)
	var notFound *apperror.Error
	require.ErrorAs(t, err, &notFound)
	require.Equal(t, http.StatusNotFound, notFound.HTTPStatus)
	require.Equal(t, 1, countRuntimeRows(), "a failed enqueue created a runtime agent")
}
