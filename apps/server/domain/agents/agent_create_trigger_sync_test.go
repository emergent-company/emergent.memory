package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestHandlerCreateAgent_RegistersTriggerAfterStartup is the regression test for
// #2388: reaction agents created AFTER the startup SyncAllTriggers must fire on
// matching events, not only those present at boot. It drives the real REST
// handler write path against Postgres, runs SyncAllTriggers first (registering
// nothing), then creates a reaction agent and asserts a matching created event
// dispatches to it.
func TestHandlerCreateAgent_RegistersTriggerAfterStartup(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "agents_create_trigger")

	ctx := context.Background()
	repo := NewRepository(tdb.DB)
	sched := scheduler.NewScheduler(agentDeleteTestLogger())
	ts := NewTriggerService(sched, nil, repo, nil, agentDeleteTestLogger())

	// Startup sync: no agents exist yet, so nothing is registered.
	require.NoError(t, ts.SyncAllTriggers(ctx))

	// Capture dispatch through the overridable hook (executor is nil here).
	dispatched := make(chan string, 1)
	ts.dispatch = func(ctx context.Context, agent *Agent, projectID, objectType, objectID string) error {
		dispatched <- agent.ID
		return nil
	}

	h := NewHandler(repo, nil, nil, "", nil, nil, nil, nil)
	h.WithTriggerService(ts)

	// Create a reaction agent AFTER startup via the REST handler.
	body := map[string]any{
		"projectId":    projectID,
		"name":         "post-start-reaction",
		"strategyType": "graph",
		"cronSchedule": "0 0 * * *",
		"triggerType":  "reaction",
		"reactionConfig": map[string]any{
			"objectTypes": []string{"document"},
			"events":      []string{"created"},
		},
	}
	reqBody, err := json.Marshal(body)
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/agents", bytes.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/admin/agents")
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: uuid.NewString(), ProjectID: projectID})

	require.NoError(t, h.CreateAgent(c))
	require.Equal(t, http.StatusCreated, rec.Code, "create must succeed")

	// The newly created agent is registered for document:created.
	listeners := ts.GetEventListeners("document:created")
	require.Len(t, listeners, 1, "reaction agent created after startup must be registered")

	// Deliver a matching created event; the agent must dispatch.
	ts.HandleEvent(context.Background(), "document", EventTypeCreated, projectID, map[string]any{"id": uuid.NewString()})

	select {
	case id := <-dispatched:
		assert.Equal(t, listeners[0].ID, id, "matching created event must dispatch to the post-startup reaction agent")
	case <-time.After(2 * time.Second):
		t.Fatal("matching created event did not dispatch to the post-startup reaction agent")
	}
}
