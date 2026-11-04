package agents

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
)

// --- unit: failure classification ---

func TestClassifyRunFailure(t *testing.T) {
	cases := []struct {
		name     string
		execErr  error
		result   *ExecuteResult
		expected FailureClass
	}{
		{"quota typed", NewQuotaError("quota exceeded"), nil, FailureClassQuota},
		{"quota string", errString("AI provider quota exhausted (RESOURCE_EXHAUSTED)"), nil, FailureClassQuota},
		{"capability typed", NewCapabilityError("wrong specialist"), nil, FailureClassCapability},
		{"retryable 503", errString("503 UNAVAILABLE"), nil, FailureClassRetryable},
		{"deterministic", errString("invalid tool args"), nil, FailureClassDeterministic},
		{"timeout", nil, &ExecuteResult{Status: RunStatusError, Summary: map[string]any{"reason": "timeout"}}, FailureClassTimeout},
		{"error deterministic", nil, &ExecuteResult{Status: RunStatusError, Summary: map[string]any{"error": "boom"}}, FailureClassDeterministic},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.expected, classifyRunFailure(c.execErr, c.result))
		})
	}
}

func TestIsQuotaError(t *testing.T) {
	require.True(t, isQuotaError(NewQuotaError("x")))
	require.True(t, isQuotaError(errString("spending cap exceeded")))
	require.False(t, isQuotaError(errString("some other error")))
	require.False(t, isQuotaError(nil))
}

func TestIsCapabilityError(t *testing.T) {
	require.True(t, isCapabilityError(NewCapabilityError("wrong specialist")))
	require.False(t, isCapabilityError(errString("other")))
}

func errString(s string) error { return stringErr(s) }

// --- unit: terminator state ---

func TestWorkTerminatorState(t *testing.T) {
	s := &WorkTerminatorState{}
	require.False(t, s.ShouldFinalize())
	require.Equal(t, "", s.Kind())

	s.Finalize(WorkTerminatorComplete, map[string]any{"work_terminator": WorkTerminatorComplete})
	require.True(t, s.ShouldFinalize())
	require.Equal(t, WorkTerminatorComplete, s.Kind())

	// First call wins: a later block cannot overwrite a complete.
	s.Finalize(WorkTerminatorBlock, map[string]any{"work_terminator": WorkTerminatorBlock})
	require.Equal(t, WorkTerminatorComplete, s.Kind())
}

// --- unit: work config status defaults ---

func TestAgentWorkConfigStatusDefaultsExtended(t *testing.T) {
	require.Equal(t, "review", (AgentWorkConfig{}).ReviewStatus())
	require.Equal(t, "revision", (AgentWorkConfig{}).RevisionStatus())
	require.Equal(t, "blocked", (AgentWorkConfig{}).BlockedStatus())
	require.Equal(t, "done", (AgentWorkConfig{}).DoneStatus())
	require.Equal(t, defaultWorkFailureLimit, (AgentWorkConfig{}).FailureLimitValue())
	require.Equal(t, 7, (AgentWorkConfig{FailureLimit: 7}).FailureLimitValue())
}

// --- unit: tool construction ---

func TestBuildWorkCompleteTool(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := WorkToolDeps{
		Store:       &fakeWorkObjectStore{},
		Repo:        nil,
		Logger:      log,
		ProjectID:   "p-1",
		CanonicalID: uuid.NewString(),
		Terminator:  &WorkTerminatorState{},
	}
	tool, err := BuildWorkCompleteTool(deps)
	require.NoError(t, err)
	require.NotNil(t, tool)
	require.Equal(t, ToolNameWorkComplete, tool.Name())
}

func TestBuildWorkBlockTool(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := WorkToolDeps{
		Store:       &fakeWorkObjectStore{},
		Repo:        nil,
		Logger:      log,
		ProjectID:   "p-1",
		CanonicalID: uuid.NewString(),
		Terminator:  &WorkTerminatorState{},
	}
	tool, err := BuildWorkBlockTool(deps)
	require.NoError(t, err)
	require.NotNil(t, tool)
	require.Equal(t, ToolNameWorkBlock, tool.Name())
}

// --- DB: per-item budget sidecar ---

func TestWorkItemStateIncrementAndClear(t *testing.T) {
	ctx, _, repo, projectID := setupWorkObjectDispatch(t)
	canonicalID := uuid.NewString()

	require.Nil(t, mustState(t, ctx, repo, projectID, canonicalID))

	count, err := repo.IncrementWorkItemFailure(ctx, projectID, canonicalID, string(FailureClassRetryable), nil)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	count, err = repo.IncrementWorkItemFailure(ctx, projectID, canonicalID, string(FailureClassDeterministic), nil)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	st := mustState(t, ctx, repo, projectID, canonicalID)
	require.Equal(t, 2, st.FailureCount)
	require.Equal(t, string(FailureClassDeterministic), st.LastFailureClass)

	require.NoError(t, repo.ClearWorkItemState(ctx, projectID, canonicalID))
	require.Nil(t, mustState(t, ctx, repo, projectID, canonicalID))
}

func mustState(t *testing.T, ctx context.Context, repo *Repository, projectID, canonicalID string) *WorkItemState {
	t.Helper()
	s, err := repo.GetWorkItemState(ctx, projectID, canonicalID)
	require.NoError(t, err)
	return s
}

// --- DB: run-end → item-transition mapping ---

func newFinishWorkPool(repo *Repository) *WorkerPool {
	executor := &AgentExecutor{safeguards: config.AgentSafeguardsConfig{ConsecutiveFailureThreshold: 1000}}
	pool := NewWorkerPool(repo, executor, testAgentLogger(), 0, time.Second)
	pool.SetWorkObjectStore(&fakeWorkObjectStore{})
	return pool
}

func setupFinishWork(t *testing.T) (context.Context, *Repository, *WorkerPool, string, string, *AgentRunJob, *AgentRun) {
	t.Helper()
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"ready","inProgress":"in_progress"},"failureLimit":3}`)
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

	pool := newFinishWorkPool(repo)
	return ctx, repo, pool, projectID, canonicalID, job, run
}

func TestFinishWorkRun_TerminatorSuccess(t *testing.T) {
	ctx, repo, pool, projectID, canonicalID, job, run := setupFinishWork(t)

	agent, err := repo.FindByID(ctx, run.AgentID, nil)
	require.NoError(t, err)
	def, err := repo.ResolveDefinitionForAgent(ctx, agent)
	require.NoError(t, err)

	pool.finishWorkRun(ctx, testAgentLogger(), job, run, agent, def, &ExecuteResult{
		Status:  RunStatusSuccess,
		Summary: map[string]any{"work_terminator": WorkTerminatorComplete},
	}, nil)

	require.Nil(t, mustState(t, ctx, repo, projectID, canonicalID))
	var status string
	require.NoError(t, repo.db.NewRaw(`SELECT status FROM kb.agent_run_jobs WHERE id = ?`, job.ID).Scan(ctx, &status))
	require.Equal(t, string(JobStatusCompleted), status)
}

func TestFinishWorkRun_ProtocolViolation(t *testing.T) {
	ctx, repo, pool, projectID, canonicalID, job, run := setupFinishWork(t)

	agent, err := repo.FindByID(ctx, run.AgentID, nil)
	require.NoError(t, err)
	def, err := repo.ResolveDefinitionForAgent(ctx, agent)
	require.NoError(t, err)

	pool.finishWorkRun(ctx, testAgentLogger(), job, run, agent, def, &ExecuteResult{
		Status:  RunStatusSuccess,
		Summary: map[string]any{},
	}, nil)

	st := mustState(t, ctx, repo, projectID, canonicalID)
	require.Equal(t, 1, st.FailureCount)
	require.Equal(t, string(FailureClassProtocol), st.LastFailureClass)
}

func TestFinishWorkRun_Poison(t *testing.T) {
	ctx, repo, pool, projectID, canonicalID, job, run := setupFinishWork(t)

	agent, err := repo.FindByID(ctx, run.AgentID, nil)
	require.NoError(t, err)
	def, err := repo.ResolveDefinitionForAgent(ctx, agent)
	require.NoError(t, err)

	pool.finishWorkRun(ctx, testAgentLogger(), job, run, agent, def, nil, errString("invalid tool args"))

	st := mustState(t, ctx, repo, projectID, canonicalID)
	require.Equal(t, 1, st.FailureCount)
	require.Equal(t, string(FailureClassDeterministic), st.LastFailureClass)

	var taskCount int
	require.NoError(t, repo.db.NewRaw(
		`SELECT COUNT(*) FROM kb.tasks WHERE source_id = ? AND type = 'work-escalation'`,
		canonicalID,
	).Scan(ctx, &taskCount))
	require.Equal(t, 1, taskCount)
}

func TestFinishWorkRun_QuotaDoesNotConsumeBudget(t *testing.T) {
	ctx, repo, pool, projectID, canonicalID, job, run := setupFinishWork(t)

	agent, err := repo.FindByID(ctx, run.AgentID, nil)
	require.NoError(t, err)
	def, err := repo.ResolveDefinitionForAgent(ctx, agent)
	require.NoError(t, err)

	pool.finishWorkRun(ctx, testAgentLogger(), job, run, agent, def, nil, NewQuotaError("quota exceeded"))

	require.Nil(t, mustState(t, ctx, repo, projectID, canonicalID))
}

// stringErr is a minimal error wrapper for classification tests.
type stringErr string

func (e stringErr) Error() string { return string(e) }

// fakeListStore is a fake WorkObjectStore that returns a fixed head list from
// ListWorkObjectsByStatus (for reaper/reconciler tests) while embedding the
// no-op transition behaviour of fakeWorkObjectStore.
type fakeListStore struct {
	fakeWorkObjectStore
	heads []*graph.WorkObjectHead
}

func (f *fakeListStore) ListWorkObjectsByStatus(ctx context.Context, projectID, status string, olderThan time.Time, limit int) ([]*graph.WorkObjectHead, error) {
	return f.heads, nil
}

// --- DB: work-status reaper (2.4) ---

func TestWorkStatusReaper_ReleasesStrandedObject(t *testing.T) {
	ctx, _, repo, projectID := setupWorkObjectDispatch(t)
	canonicalID := uuid.NewString()

	store := &fakeListStore{heads: []*graph.WorkObjectHead{
		{ProjectID: projectID, CanonicalID: canonicalID, Status: "in_progress", Type: "ResearchRequest"},
	}}
	reaper := NewWorkStatusReaper(repo, testAgentLogger(), time.Minute, time.Minute)
	reaper.SetWorkObjectStore(store)

	reaper.reap(ctx)

	st := mustState(t, ctx, repo, projectID, canonicalID)
	require.Equal(t, 1, st.FailureCount)
}

func TestWorkStatusReaper_SkipsLiveRun(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := uuid.NewString()
	objType := "ResearchRequest"
	// A live (queued) run owns the object: the reaper must not release it.
	_, err := repo.CreateRunQueued(ctx, agentID, 1, CreateRunQueuedOptions{
		SubjectObjectID:   &canonicalID,
		SubjectObjectType: &objType,
	})
	require.NoError(t, err)

	store := &fakeListStore{heads: []*graph.WorkObjectHead{
		{ProjectID: projectID, CanonicalID: canonicalID, Status: "in_progress", Type: "ResearchRequest"},
	}}
	reaper := NewWorkStatusReaper(repo, testAgentLogger(), time.Minute, time.Minute)
	reaper.SetWorkObjectStore(store)

	reaper.reap(ctx)

	require.Nil(t, mustState(t, ctx, repo, projectID, canonicalID))
}

// --- DB: reconciler (2.5) ---

func TestWorkReconciler_EnqueuesReadyObject(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)
	// Make it a reaction agent listening on ResearchRequest.
	_, err := db.ExecContext(ctx,
		`UPDATE kb.agents SET trigger_type = 'reaction', reaction_config = '{"objectTypes":["ResearchRequest"],"events":["created"]}'::jsonb WHERE id = ?`,
		agentID)
	require.NoError(t, err)

	canonicalID := uuid.NewString()
	store := &fakeListStore{heads: []*graph.WorkObjectHead{
		{ProjectID: projectID, CanonicalID: canonicalID, Status: "ready", Type: "ResearchRequest"},
	}}
	rc := NewWorkReconciler(repo, testAgentLogger(), time.Minute)
	rc.SetWorkObjectStore(store)

	rc.reconcile(ctx)

	require.Equal(t, 1, countQueuedRuns(t, db, ctx, agentID, canonicalID))
}
