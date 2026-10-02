package agents

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func agentDeleteTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedAgentDeleteProject creates a throwaway database with an org + project and
// returns the DB and project id. Skips (or fails under REQUIRE_DB) when no
// database is available.
func seedAgentDeleteProject(t *testing.T, suffix string) (*testdb.TestDB, string) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), suffix)
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	orgID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`,
		orgID, "agent-del-org-"+orgID)
	require.NoError(t, err)

	projectID := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "agent-del-project-"+projectID)
	require.NoError(t, err)

	return tdb, projectID
}

// insertAgentRowForDelete row for delete tests. configJSON must be a valid JSON
// object (use "{}" for none).
func insertAgentRowForDelete(t *testing.T, tdb *testdb.TestDB, id, name, projectID, configJSON string) {
	t.Helper()
	_, err := tdb.DB.ExecContext(context.Background(),
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, config)
		 VALUES (?, ?, 'graph', '', ?, CAST(? AS jsonb))`,
		id, name, projectID, configJSON)
	require.NoError(t, err)
}

// registerCronForTest registers a cron task under the same name
// RemoveAgentTrigger uses, so tests can assert the scheduler entry is torn down.
func registerCronForTest(t *testing.T, sched *scheduler.Scheduler, agentID string) {
	t.Helper()
	require.NoError(t, sched.AddCronTask(triggerTaskName(agentID), "0 */5 * * * *", func(context.Context) error {
		return nil
	}))
}

// TestHandlerDeleteAgent_TearsDownTriggerRegistrations exercises the API delete
// path end to end against Postgres: DELETE removes the row, and the single-step
// repository delete drops the agent's cron and reaction registrations.
func TestHandlerDeleteAgent_TearsDownTriggerRegistrations(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "agents_delete_handler")

	ctx := context.Background()
	agentID := uuid.NewString()
	insertAgentRowForDelete(t, tdb, agentID, "handler-del-agent", projectID, "{}")

	repo := NewRepository(tdb.DB)
	sched := scheduler.NewScheduler(agentDeleteTestLogger())
	ts := NewTriggerService(sched, nil, repo, nil, agentDeleteTestLogger())

	ts.registerEventTrigger(makeTestAgent(agentID, "handler-del-agent", projectID, &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	}))
	registerCronForTest(t, sched, agentID)

	require.Len(t, ts.GetEventListeners("document:created"), 1)
	require.Contains(t, sched.ListTasks(), triggerTaskName(agentID))

	h := NewHandler(repo, nil, nil, "", nil, nil, nil, nil)
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/agents/"+agentID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/admin/agents/:id")
	c.SetParamNames("id")
	c.SetParamValues(agentID)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: uuid.NewString(), ProjectID: projectID})

	require.NoError(t, h.DeleteAgent(c))
	require.Equal(t, http.StatusOK, rec.Code)

	// Row is gone.
	n, err := tdb.DB.NewSelect().Model((*Agent)(nil)).Where("id = ?", agentID).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "agent row must be deleted")

	// Registrations are gone.
	assert.Empty(t, ts.GetEventListeners("document:created"), "reaction registration must be removed")
	assert.NotContains(t, sched.ListTasks(), triggerTaskName(agentID), "cron registration must be removed")
}

// TestDeleteAgentsBySourceBlueprint_TearsDownEveryTriggerRegistration covers the
// blueprint Unapply delete path (which removes many runtime agents at once): each
// deleted agent id must lose its trigger registrations, and unrelated agents must
// be untouched.
func TestDeleteAgentsBySourceBlueprint_TearsDownEveryTriggerRegistration(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "agents_delete_blueprint")

	ctx := context.Background()
	blueprintID := uuid.NewString()
	stampedConfig := `{"sourceBlueprintId":"` + blueprintID + `"}`

	ids := []string{uuid.NewString(), uuid.NewString()}
	for _, id := range ids {
		insertAgentRowForDelete(t, tdb, id, "bp-del-agent-"+id[:8], projectID, stampedConfig)
	}
	keepID := uuid.NewString()
	insertAgentRowForDelete(t, tdb, keepID, "bp-keep-agent", projectID, "{}")

	repo := NewRepository(tdb.DB)
	sched := scheduler.NewScheduler(agentDeleteTestLogger())
	ts := NewTriggerService(sched, nil, repo, nil, agentDeleteTestLogger())

	for _, id := range append(append([]string{}, ids...), keepID) {
		ts.registerEventTrigger(makeTestAgent(id, "bp-agent", projectID, &ReactionConfig{
			ObjectTypes: []string{"document"},
			Events:      []ReactionEventType{EventTypeCreated},
		}))
		registerCronForTest(t, sched, id)
	}
	require.Len(t, ts.GetEventListeners("document:created"), 3)

	n, err := repo.DeleteAgentsBySourceBlueprint(ctx, blueprintID)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	for _, id := range ids {
		assert.NotContains(t, sched.ListTasks(), triggerTaskName(id), "deleted agent cron must be removed")
	}
	// The surviving agent keeps its registration.
	require.Len(t, ts.GetEventListeners("document:created"), 1)
	assert.Equal(t, keepID, ts.GetEventListeners("document:created")[0].ID)
	assert.Contains(t, sched.ListTasks(), triggerTaskName(keepID))

	remaining, err := tdb.DB.NewSelect().Model((*Agent)(nil)).
		Where("project_id = ?", projectID).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, remaining)
}
