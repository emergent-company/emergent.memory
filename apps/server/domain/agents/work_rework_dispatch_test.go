package agents

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// reworkWorkConfig is a work config whose status_changed trigger is the
// "revision" status, so a listening agent wakes on a review→revision transition.
const reworkWorkConfig = `{"status":{"ready":"ready","inProgress":"in_progress","review":"review","revision":"revision"},"triggerStatuses":["revision"]}`

// setupReworkDispatch wires a real events.Service shared by the graph service
// (which emits status_changed on request-changes) and a TriggerService (which
// subscribes onEntityEvent → dispatches status_changed listeners), so the
// rework-dispatch-ownership fix can be asserted end-to-end.
func setupReworkDispatch(t *testing.T) (context.Context, *bun.DB, *Repository, *graph.Service, *TriggerService, string) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_rework_dispatch")
	t.Cleanup(tdb.Close)
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000

	repo := NewRepository(tdb.DB)
	gRepo := graph.NewRepository(tdb.DB, log, cfg)

	eventsSvc := events.NewService(log)
	gSvc := graph.NewService(gRepo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, nil, eventsSvc)

	ts := NewTriggerService(scheduler.NewScheduler(testAgentLogger()), nil, repo, eventsSvc, testAgentLogger())

	return ctx, tdb.DB, repo, gSvc, ts, projectID
}

// insertReworkListeningAgent inserts a reaction agent listening on
// status_changed with the given work config, then registers it with the trigger
// service so a status_changed event will wake it.
func insertReworkListeningAgent(t *testing.T, db *bun.DB, ctx context.Context, repo *Repository, ts *TriggerService, projectID, name, workConfig string) string {
	t.Helper()
	defID := insertWorkDefinition(t, db, ctx, projectID, name, workConfig)
	id := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, trigger_type, project_id, agent_definition_id, reaction_config) VALUES (?, ?, 'definition', '', 'reaction', ?, ?, ?::jsonb)`,
		id, name, projectID, defID, `{"objectTypes":["ResearchRequest"],"events":["status_changed"],"concurrencyStrategy":"skip"}`)
	require.NoError(t, err)

	agent, err := repo.FindByID(ctx, id, &projectID)
	require.NoError(t, err)
	require.NotNil(t, agent)
	ts.SyncAgentTrigger(agent)
	return id
}

// assertQueuedRunsSettled waits for the queued-run count for (agent, subject) to
// reach want, then asserts it stays there through a short grace window so a
// late-arriving async dispatch is still observed. It asserts on the resulting
// run set, never on goroutine ordering.
func assertQueuedRunsSettled(t *testing.T, ctx context.Context, db *bun.DB, agentID, canonicalID string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for countQueuedRuns(t, db, ctx, agentID, canonicalID) != want {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d queued runs (agent %s, object %s): got %d", want, agentID, canonicalID, countQueuedRuns(t, db, ctx, agentID, canonicalID))
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Grace window: let any spurious async dispatch land before asserting the set.
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, want, countQueuedRuns(t, db, ctx, agentID, canonicalID))
}

// TestRequestChanges_OwnerNotDoubleDispatched verifies the fix's core invariant:
// a request-changes transition emits a status_changed event that carries the
// owning agent's id, and the event-driven dispatch suppresses that owner — so
// the explicit feedback-carrying rework enqueue is the only queued run.
func TestRequestChanges_OwnerNotDoubleDispatched(t *testing.T) {
	ctx, db, repo, gSvc, ts, projectID := setupReworkDispatch(t)
	ownerID := insertReworkListeningAgent(t, db, ctx, repo, ts, projectID, "researcher", reworkWorkConfig)

	svc := newWorkActions(repo, gSvc)
	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-rework-owner", "review", "researcher")

	res, err := svc.RequestChanges(ctx, projectID, canonicalID, "reviewer-1", "please fix the summary")
	require.NoError(t, err)
	require.Equal(t, "revision", res.Head.Status)
	require.NotEmpty(t, res.RunID)

	// Exactly one queued run for the owner (the explicit rework run); the
	// event-path dispatch for the same agent is suppressed by rework_agent_id.
	assertQueuedRunsSettled(t, ctx, db, ownerID, canonicalID, 1)
	requireRunCarriesFeedback(t, ctx, repo, res.RunID)
}

// TestRequestChanges_SecondAgentStillDispatches verifies the suppression is
// owner-scoped: a second work agent that also declares the revision status is
// still dispatched, because its id differs from the payload's rework_agent_id.
func TestRequestChanges_SecondAgentStillDispatches(t *testing.T) {
	ctx, db, repo, gSvc, ts, projectID := setupReworkDispatch(t)
	ownerID := insertReworkListeningAgent(t, db, ctx, repo, ts, projectID, "researcher", reworkWorkConfig)
	secondID := insertReworkListeningAgent(t, db, ctx, repo, ts, projectID, "designer", reworkWorkConfig)

	// The object is unassigned so the second agent is not filtered by assignee
	// routing; a terminal prior run by the owner lets request-changes resolve the
	// rework owner (and so set rework_agent_id) without an assignee.
	svc := newWorkActions(repo, gSvc)
	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-rework-second", "review", "")

	objType := "ResearchRequest"
	seed, err := repo.CreateRunQueued(ctx, ownerID, 1, CreateRunQueuedOptions{
		SubjectObjectID:   &canonicalID,
		SubjectObjectType: &objType,
	})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE kb.agent_runs SET status = 'error' WHERE id = ?`, seed.ID)
	require.NoError(t, err)

	res, err := svc.RequestChanges(ctx, projectID, canonicalID, "reviewer-1", "fix it again")
	require.NoError(t, err)
	require.Equal(t, "revision", res.Head.Status)
	require.NotEmpty(t, res.RunID)

	// The owner's only queued run is the explicit feedback-carrying rework run.
	assertQueuedRunsSettled(t, ctx, db, ownerID, canonicalID, 1)
	requireRunCarriesFeedback(t, ctx, repo, res.RunID)

	// The second agent still gets its own status_changed dispatch.
	assertQueuedRunsSettled(t, ctx, db, secondID, canonicalID, 1)
}

// TestTransitionIntoTriggerStatus_DispatchesNormally verifies a plain
// (non-request-changes) transition into a trigger status still dispatches: no
// rework_agent_id marker means the owner is not suppressed.
func TestTransitionIntoTriggerStatus_DispatchesNormally(t *testing.T) {
	ctx, db, repo, gSvc, ts, projectID := setupReworkDispatch(t)
	ownerID := insertReworkListeningAgent(t, db, ctx, repo, ts, projectID, "researcher", reworkWorkConfig)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-plain-transition", "ready", "researcher")

	ok, err := gSvc.TransitionWorkObject(ctx, projectID, canonicalID, graph.WorkObjectTransition{
		FromStatus: "ready",
		ToStatus:   "revision",
	})
	require.NoError(t, err)
	require.True(t, ok)

	assertQueuedRunsSettled(t, ctx, db, ownerID, canonicalID, 1)
}
