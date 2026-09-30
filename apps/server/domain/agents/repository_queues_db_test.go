package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

func insertDefinitionWithQueue(t *testing.T, db *bun.DB, ctx context.Context, projectID, name, queue string) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility, default_queue) VALUES (?, ?, ?, 'project', ?)`,
		uuid.NewString(), projectID, name, queue)
	require.NoError(t, err)
}

func insertAgentWithConfig(t *testing.T, db *bun.DB, ctx context.Context, projectID, name, config string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, config) VALUES (?, ?, 'definition', '', ?, ?::jsonb)`,
		id, name, projectID, config)
	require.NoError(t, err)
	return id
}

func queuedJob(t *testing.T, db *bun.DB, ctx context.Context, runID string) (queue string, priority int) {
	t.Helper()
	require.NoError(t, db.NewRaw(
		`SELECT queue, priority FROM kb.agent_run_jobs WHERE run_id = ? ORDER BY created_at DESC LIMIT 1`,
		runID,
	).Scan(ctx, &queue, &priority))
	return queue, priority
}

// TestCreateRunQueued_ResolvesDefinitionQueue verifies a run whose agent has no
// config override is routed to its definition's default queue.
func TestCreateRunQueued_ResolvesDefinitionQueue(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_queue_resolve")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	insertDefinitionWithQueue(t, tdb.DB, ctx, projectID, "security-agent", "security-review")
	agentID := insertAgentWithConfig(t, tdb.DB, ctx, projectID, "security-agent", "{}")

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)

	queue, priority := queuedJob(t, tdb.DB, ctx, run.ID)
	require.Equal(t, "security-review", queue)
	require.Equal(t, DefaultQueuePriority, priority)
}

// TestCreateRunQueued_RuntimeConfigOverridesDefinition verifies the runtime
// agent's config "queue" wins over the definition's default queue.
func TestCreateRunQueued_RuntimeConfigOverridesDefinition(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_queue_override")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	insertDefinitionWithQueue(t, tdb.DB, ctx, projectID, "security-agent", "security-review")
	agentID := insertAgentWithConfig(t, tdb.DB, ctx, projectID, "security-agent", `{"queue":"triage"}`)

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)

	queue, _ := queuedJob(t, tdb.DB, ctx, run.ID)
	require.Equal(t, "triage", queue)
}

// TestClaimNextJobInQueue_PriorityAndIsolation verifies that claims are
// queue-scoped and ordered by priority (lower first), and that a worker for one
// queue never claims another queue's job.
func TestClaimNextJobInQueue_PriorityAndIsolation(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_queue_claim")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "q-agent")

	low, err := repo.CreateRunQueued(ctx, agentID, 1, CreateRunQueuedOptions{Queue: "q1", Priority: 100})
	require.NoError(t, err)
	high, err := repo.CreateRunQueued(ctx, agentID, 1, CreateRunQueuedOptions{Queue: "q1", Priority: 10})
	require.NoError(t, err)
	other, err := repo.CreateRunQueued(ctx, agentID, 1, CreateRunQueuedOptions{Queue: "q2", Priority: 1})
	require.NoError(t, err)

	// Queue q1 claim must be the priority-10 job, never the q2 job.
	first, err := repo.ClaimNextJobInQueue(ctx, "q1")
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, high.ID, first.RunID)

	second, err := repo.ClaimNextJobInQueue(ctx, "q1")
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, low.ID, second.RunID)

	// Only the q2 job remains, and it is claimable from q2 only.
	third, err := repo.ClaimNextJobInQueue(ctx, "q2")
	require.NoError(t, err)
	require.NotNil(t, third)
	require.Equal(t, other.ID, third.RunID)

	none, err := repo.ClaimNextJobInQueue(ctx, "q1")
	require.NoError(t, err)
	require.Nil(t, none)
}

// TestQueueCRUD_DeleteGuardedAndDepth verifies queue listing depth and the
// delete guard that refuses to remove a queue with in-flight jobs.
func TestQueueCRUD_DeleteGuardedAndDepth(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_queue_crud")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "crud-agent")

	require.NoError(t, repo.UpsertQueue(ctx, &AgentQueue{
		ProjectID: projectID, Name: "review", DisplayName: "Review", Concurrency: 2, Priority: 50, Enabled: true,
	}))

	_, err := repo.CreateRunQueued(ctx, agentID, 1, CreateRunQueuedOptions{Queue: "review"})
	require.NoError(t, err)

	queues, err := repo.ListQueues(ctx, projectID)
	require.NoError(t, err)
	var found *AgentQueue
	for i := range queues {
		if queues[i].Name == "review" {
			found = &queues[i]
		}
	}
	require.NotNil(t, found)
	require.Equal(t, 1, found.Pending)
	require.Equal(t, 0, found.Processing)

	err = repo.DeleteQueue(ctx, projectID, "review")
	require.ErrorIs(t, err, ErrQueueNotEmpty)

	// Drain the queue then delete succeeds.
	job, err := repo.ClaimNextJobInQueue(ctx, "review")
	require.NoError(t, err)
	require.NotNil(t, job)
	require.NoError(t, repo.CompleteJob(ctx, job.ID, job.RunID))
	require.NoError(t, repo.DeleteQueue(ctx, projectID, "review"))

	_, err = repo.GetQueue(ctx, projectID, "review")
	require.ErrorIs(t, err, ErrQueueNotFound)
}

// TestEnsureDefaultQueues verifies a default queue is created for projects that
// lack one.
func TestEnsureDefaultQueues(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_queue_default")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	require.NoError(t, repo.EnsureDefaultQueues(ctx))
	q, err := repo.GetQueue(ctx, projectID, DefaultQueueName)
	require.NoError(t, err)
	require.True(t, q.Enabled)
	require.Equal(t, DefaultQueuePriority, q.Priority)
}
