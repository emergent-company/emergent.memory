package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// --- unit: work config gating ---

func TestAgentWorkConfigIsZero(t *testing.T) {
	require.True(t, (AgentWorkConfig{}).IsZero())
	require.False(t, (AgentWorkConfig{
		Status: AgentWorkStatusConfig{Ready: "ready", InProgress: "in_progress"},
	}).IsZero())
	require.False(t, (AgentWorkConfig{RequiresReview: true}).IsZero())
	require.False(t, (AgentWorkConfig{FailureLimit: 3}).IsZero())
	require.False(t, (AgentWorkConfig{
		RetryPolicy: AgentRetryPolicy{MaxAttempts: 3},
	}).IsZero())
}

func TestAgentWorkConfigStatusDefaults(t *testing.T) {
	require.Equal(t, "ready", (AgentWorkConfig{}).ReadyStatus())
	require.Equal(t, "in_progress", (AgentWorkConfig{}).InProgressStatus())
	require.Equal(t, "todo", (AgentWorkConfig{Status: AgentWorkStatusConfig{Ready: "todo"}}).ReadyStatus())
	require.Equal(t, "doing", (AgentWorkConfig{Status: AgentWorkStatusConfig{InProgress: "doing"}}).InProgressStatus())
}

func TestShouldEnqueueWork(t *testing.T) {
	ts := &TriggerService{}
	require.False(t, ts.shouldEnqueueWork(nil))
	require.False(t, ts.shouldEnqueueWork(&AgentDefinition{}))
	require.True(t, ts.shouldEnqueueWork(&AgentDefinition{DispatchMode: DispatchModeQueued}))
	require.True(t, ts.shouldEnqueueWork(&AgentDefinition{
		WorkConfig: AgentWorkConfig{Status: AgentWorkStatusConfig{Ready: "ready"}},
	}))
}

// --- DB: enqueue-on-create, dedup, assignee routing ---

func setupWorkObjectDispatch(t *testing.T) (context.Context, *bun.DB, *Repository, string) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_work_dispatch")
	t.Cleanup(tdb.Close)
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	return ctx, tdb.DB, NewRepository(tdb.DB), projectID
}

func insertWorkDefinition(t *testing.T, db *bun.DB, ctx context.Context, projectID, name, workConfig string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility, work_config) VALUES (?, ?, ?, 'project', ?::jsonb)`,
		id, projectID, name, workConfig)
	require.NoError(t, err)
	return id
}

func insertWorkAgent(t *testing.T, db *bun.DB, ctx context.Context, projectID, name, defID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, agent_definition_id) VALUES (?, ?, 'definition', '', ?, ?)`,
		id, name, projectID, defID)
	require.NoError(t, err)
	return id
}

// insertWorkObject creates a version-1 graph object so the processing-log
// foreign key (graph_object_id → kb.graph_objects.id, where version-1 id ==
// canonical_id) is satisfied during dispatch.
func insertWorkObject(t *testing.T, db *bun.DB, ctx context.Context, projectID, canonicalID, key, status string) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.graph_objects (id, project_id, type, canonical_id, version, key, status) VALUES (?, ?, 'ResearchRequest', ?, 1, ?, ?)`,
		canonicalID, projectID, canonicalID, key, status)
	require.NoError(t, err)
}

func newWorkTriggerService(repo *Repository) *TriggerService {
	return NewTriggerService(scheduler.NewScheduler(testAgentLogger()), nil, repo, nil, testAgentLogger())
}

func countQueuedRuns(t *testing.T, db *bun.DB, ctx context.Context, agentID, canonicalID string) int {
	t.Helper()
	var count int
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_runs WHERE agent_id = ? AND subject_object_id = ?::uuid AND status = ?`,
		agentID, canonicalID, string(RunStatusQueued),
	).Scan(ctx, &count))
	return count
}

// TestDispatchMatchedAgent_EnqueuesOnWorkConfig verifies that a reaction
// dispatch for an agent whose definition carries a work config enqueues a
// queued run linked to the work object (subject_object_id = canonical_id)
// instead of executing inline.
func TestDispatchMatchedAgent_EnqueuesOnWorkConfig(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"ready","inProgress":"in_progress"},"failureLimit":3}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	ts := newWorkTriggerService(repo)
	agent, err := repo.FindByID(ctx, agentID, nil)
	require.NoError(t, err)
	require.NotNil(t, agent)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "gemini-model-2026", "ready")
	ts.dispatchMatchedAgent(ctx, agent, "ResearchRequest", EventTypeCreated, projectID, map[string]any{
		"id":      canonicalID,
		"version": 1,
	})

	require.Equal(t, 1, countQueuedRuns(t, db, ctx, agentID, canonicalID))

	// The run carries the subject linkage and is not executed inline (still queued).
	var subjectType string
	require.NoError(t, db.NewRaw(
		`SELECT subject_object_type FROM kb.agent_runs WHERE agent_id = ? AND subject_object_id = ?`,
		agentID, canonicalID,
	).Scan(ctx, &subjectType))
	require.Equal(t, "ResearchRequest", subjectType)

	// A job row exists on the agent's queue.
	var jobCount int
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_run_jobs WHERE run_id IN (SELECT id FROM kb.agent_runs WHERE agent_id = ? AND subject_object_id = ?)`,
		agentID, canonicalID,
	).Scan(ctx, &jobCount))
	require.Equal(t, 1, jobCount)
}

// TestDispatchMatchedAgent_Dedup verifies a repeated created delivery for the
// same agent, object, and version produces a single run via the processing log
// dedup key.
func TestDispatchMatchedAgent_Dedup(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	ts := newWorkTriggerService(repo)
	agent, err := repo.FindByID(ctx, agentID, nil)
	require.NoError(t, err)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "gemini-model-2026", "ready")
	input := map[string]any{"id": canonicalID, "version": 1}

	ts.dispatchMatchedAgent(ctx, agent, "ResearchRequest", EventTypeCreated, projectID, input)
	ts.dispatchMatchedAgent(ctx, agent, "ResearchRequest", EventTypeCreated, projectID, input)

	require.Equal(t, 1, countQueuedRuns(t, db, ctx, agentID, canonicalID))

	var logCount int
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_processing_log WHERE agent_id = ? AND graph_object_id = ? AND object_version = 1 AND event_type = 'created'`,
		agentID, canonicalID,
	).Scan(ctx, &logCount))
	require.Equal(t, 1, logCount)
}

// TestDispatchMatchedAgent_AssigneeRouting verifies an assigned object wakes
// only the listener whose name matches the assignee.
func TestDispatchMatchedAgent_AssigneeRouting(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)

	researcherDef := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	reviewerDef := insertWorkDefinition(t, db, ctx, projectID, "reviewer",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	researcherID := insertWorkAgent(t, db, ctx, projectID, "researcher", researcherDef)
	reviewerID := insertWorkAgent(t, db, ctx, projectID, "reviewer", reviewerDef)

	ts := newWorkTriggerService(repo)
	researcher, err := repo.FindByID(ctx, researcherID, nil)
	require.NoError(t, err)
	reviewer, err := repo.FindByID(ctx, reviewerID, nil)
	require.NoError(t, err)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "gemini-model-2026", "ready")
	input := map[string]any{
		"id":      canonicalID,
		"version": 1,
		"data":    map[string]any{"assignee": "researcher"},
	}

	// The assignee is "researcher": the researcher is enqueued, the reviewer is not.
	ts.dispatchMatchedAgent(ctx, researcher, "ResearchRequest", EventTypeCreated, projectID, input)
	ts.dispatchMatchedAgent(ctx, reviewer, "ResearchRequest", EventTypeCreated, projectID, input)

	require.Equal(t, 1, countQueuedRuns(t, db, ctx, researcherID, canonicalID))
	require.Equal(t, 0, countQueuedRuns(t, db, ctx, reviewerID, canonicalID))
}

// TestDispatchMatchedAgent_NoWorkConfigKeepsInline verifies an agent without a
// work config (and not queued) is not enqueued: no queued run is created, so
// today's inline behaviour is unchanged.
func TestDispatchMatchedAgent_NoWorkConfigKeepsInline(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "legacy-agent", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "legacy-agent", defID)

	ts := newWorkTriggerService(repo)
	agent, err := repo.FindByID(ctx, agentID, nil)
	require.NoError(t, err)

	canonicalID := uuid.NewString()
	// dispatchMatchedAgent with a nil executor would panic in the legacy inline
	// path, so drive the gate directly and assert the enqueue path is not taken.
	require.False(t, ts.shouldEnqueueWork(nil))
	def, err := repo.ResolveDefinitionForAgent(ctx, agent)
	require.NoError(t, err)
	require.NotNil(t, def)
	require.False(t, ts.shouldEnqueueWork(def))

	require.Equal(t, 0, countQueuedRuns(t, db, ctx, agentID, canonicalID))
}

// --- DB: worker-pool claim (lost race → skip, no failure-budget burn) ---

// fakeWorkObjectStore stubs the WorkObjectStore surface for the worker-pool
// claim test.
type fakeWorkObjectStore struct {
	claimed bool
	err     error
}

func (f *fakeWorkObjectStore) GetHeadObject(ctx context.Context, projectID, canonicalID string) (*graph.WorkObjectHead, error) {
	return &graph.WorkObjectHead{CanonicalID: canonicalID, Type: "ResearchRequest", Version: 1}, nil
}

func (f *fakeWorkObjectStore) ClaimWorkObject(ctx context.Context, projectID, canonicalID, readyStatus, inProgressStatus string) (bool, error) {
	return f.claimed, f.err
}

func (f *fakeWorkObjectStore) TransitionWorkObject(ctx context.Context, projectID, canonicalID string, t graph.WorkObjectTransition) (bool, error) {
	return true, nil
}

func (f *fakeWorkObjectStore) CompleteWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, doneStatus, reviewStatus string, requiresReview bool) (bool, error) {
	return true, nil
}

func (f *fakeWorkObjectStore) BlockWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, blockedStatus string) (bool, error) {
	return true, nil
}

func (f *fakeWorkObjectStore) UnassignWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, readyStatus string) (bool, error) {
	return true, nil
}

func (f *fakeWorkObjectStore) ApproveWorkObject(ctx context.Context, projectID, canonicalID, reviewStatus, doneStatus, reviewerID string) (bool, error) {
	return true, nil
}

func (f *fakeWorkObjectStore) RequestChangesWorkObject(ctx context.Context, projectID, canonicalID, reviewStatus, revisionStatus string) (bool, error) {
	return true, nil
}

func (f *fakeWorkObjectStore) ReassignWorkObject(ctx context.Context, projectID, canonicalID string, assignee *string) (bool, error) {
	return true, nil
}

func (f *fakeWorkObjectStore) CancelWorkObject(ctx context.Context, projectID, canonicalID, blockedStatus string) (bool, error) {
	return true, nil
}

func (f *fakeWorkObjectStore) ListWorkObjectsByStatus(ctx context.Context, projectID, status string, olderThan time.Time, limit int) ([]*graph.WorkObjectHead, error) {
	return nil, nil
}

// TestWorkerPoolClaim_LostRaceSkipsRun verifies that a worker whose claim finds
// the work object already taken marks the run skipped and completes the job —
// without executing the agent or touching the failure breaker.
func TestWorkerPoolClaim_LostRaceSkipsRun(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := uuid.NewString()
	objType := "ResearchRequest"
	run, err := repo.CreateRunQueued(ctx, agentID, 1, CreateRunQueuedOptions{
		SubjectObjectID:   &canonicalID,
		SubjectObjectType: &objType,
	})
	require.NoError(t, err)

	job, err := repo.ClaimNextJobInQueue(ctx, projectID, DefaultQueueName)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, run.ID, job.RunID)

	pool := NewWorkerPool(repo, nil, testAgentLogger(), 0, time.Second)
	pool.SetWorkObjectStore(&fakeWorkObjectStore{claimed: false})

	pool.executeJob(ctx, testAgentLogger(), job)

	// The run is skipped, the job is completed, and the failure breaker is
	// untouched (agent remains enabled with zero consecutive failures).
	var status string
	require.NoError(t, db.NewRaw(`SELECT status FROM kb.agent_runs WHERE id = ?`, run.ID).Scan(ctx, &status))
	require.Equal(t, string(RunStatusSkipped), status)

	var jobStatus string
	require.NoError(t, db.NewRaw(`SELECT status FROM kb.agent_run_jobs WHERE id = ?`, job.ID).Scan(ctx, &jobStatus))
	require.Equal(t, string(JobStatusCompleted), jobStatus)

	var failures int
	var enabled bool
	require.NoError(t, db.NewRaw(`SELECT consecutive_failures, enabled FROM kb.agents WHERE id = ?`, agentID).Scan(ctx, &failures, &enabled))
	require.Equal(t, 0, failures)
	require.True(t, enabled)
}
