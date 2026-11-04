package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// TestGetProjectRunFullResolvesTheRunAgentName pins the fix for a run transcript
// being attributed to the wrong agent in the chat workspace.
//
// GET /agent-runs/:runId/full feeds the run-history view, which heads the pane
// and labels the bubbles with the run's OWN agent. The run's DTO only carries
// `agentName` when the agent relation was loaded, and FindRunByIDForProject
// joins kb.agents purely for project scoping without reading the row — so
// without an explicit lookup the response says nothing about who produced the
// run and the reader's UI falls back to whichever agent it has selected.
func TestGetProjectRunFullResolvesTheRunAgentName(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "run_full_agent")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`,
		agentID, "Extraction", projectID)
	require.NoError(t, err)

	run, err := repo.CreateRun(ctx, agentID)
	require.NoError(t, err)
	require.NotNil(t, run)

	// Guard: if the repository query ever starts preloading the relation, this
	// test would pass for the wrong reason and the handler lookup would rot.
	loaded, err := repo.FindRunByIDForProject(ctx, run.ID, projectID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Nil(t, loaded.Agent, "FindRunByIDForProject must keep returning a run without its agent relation")
	require.Empty(t, loaded.ToDTO().AgentName, "the DTO starts without a name; the handler must resolve it")

	h := NewHandler(repo, nil, nil, "", nil, nil, nil, nil)
	e := echo.New()
	e.GET("/api/projects/:projectId/agent-runs/:runId/full", h.GetProjectRunFull)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/agent-runs/"+run.ID+"/full", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var env struct {
		Data struct {
			Run struct {
				AgentID   string `json:"agentId"`
				AgentName string `json:"agentName"`
			} `json:"run"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, agentID, env.Data.Run.AgentID)
	require.Equal(t, "Extraction", env.Data.Run.AgentName,
		"a run transcript must be attributable to the agent that produced it")
}
