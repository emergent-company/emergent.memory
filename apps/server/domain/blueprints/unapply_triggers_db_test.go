package blueprints

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
)

// TestUnapply_RemovesTriggerRegistrationsForDeletedRuntimeAgents exercises the
// blueprint Unapply path against Postgres: the runtime agents it deletes must
// have their in-memory trigger registrations (cron + reaction) torn down as part
// of the delete, not left alive until a restart.
func TestUnapply_RemovesTriggerRegistrationsForDeletedRuntimeAgents(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	repo := NewRepository(db, testLogger())
	agentRepo := agents.NewRepository(db)

	sched := scheduler.NewScheduler(testLogger())
	ts := agents.NewTriggerService(sched, nil, agentRepo, nil, testLogger())

	bp := testBlueprint(uniqueName("unapply-triggers"), "1.0.0")
	require.NoError(t, repo.Create(ctx, bp))

	userID := uuid.NewString()
	require.NoError(t, repo.RecordApplication(ctx, &BlueprintApplication{
		BlueprintID: bp.ID,
		ProjectID:   projectID,
		Version:     bp.Version,
		Checksum:    bp.Checksum,
		AppliedBy:   &userID,
		AppliedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}))

	agentID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, config)
		 VALUES (?, ?, 'graph', '', ?, CAST(? AS jsonb))`,
		agentID, "bp-runtime-agent", projectID, `{"sourceBlueprintId":"`+bp.ID+`"}`)
	require.NoError(t, err)

	// Register reaction + cron registrations for the runtime agent.
	ts.SyncAgentTrigger(&agents.Agent{
		ID:             agentID,
		Name:           "bp-runtime-agent",
		ProjectID:      projectID,
		TriggerType:    agents.TriggerTypeReaction,
		Enabled:        true,
		ReactionConfig: &agents.ReactionConfig{ObjectTypes: []string{"document"}, Events: []agents.ReactionEventType{agents.EventTypeCreated}},
	})
	require.NoError(t, sched.AddCronTask("agent:"+agentID, "0 */5 * * * *", func(context.Context) error {
		return nil
	}))
	require.Len(t, ts.GetEventListeners("document:created"), 1)
	require.Contains(t, sched.ListTasks(), "agent:"+agentID)

	svc := &Service{repo: repo, agentRepo: agentRepo, log: testLogger()}
	result, err := svc.Unapply(ctx, bp.ID, projectID)
	require.NoError(t, err)
	require.Equal(t, 1, result.Agents.Removed)

	// Row gone.
	n, err := db.NewSelect().Model((*agents.Agent)(nil)).Where("id = ?", agentID).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	// Registrations gone.
	assert.Empty(t, ts.GetEventListeners("document:created"), "reaction registration must be removed")
	assert.NotContains(t, sched.ListTasks(), "agent:"+agentID, "cron registration must be removed")
}

// TestUnapply_DoesNotDeleteAgentsOwnedByTheSameBlueprintInAnotherProject covers
// issue #1361 item 2: a blueprint source id can be applied in more than one
// project, so Unapply must scope the runtime-agent delete by project_id. Before
// the project scope was added, unapplying the blueprint in project A removed
// project B's agents too.
func TestUnapply_DoesNotDeleteAgentsOwnedByTheSameBlueprintInAnotherProject(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, projectA := seedProject(t, db)
	_, projectB := seedProject(t, db)

	repo := NewRepository(db, testLogger())
	agentRepo := agents.NewRepository(db)

	bp := testBlueprint(uniqueName("unapply-scope"), "1.0.0")
	require.NoError(t, repo.Create(ctx, bp))

	userID := uuid.NewString()
	for _, projectID := range []string{projectA, projectB} {
		require.NoError(t, repo.RecordApplication(ctx, &BlueprintApplication{
			BlueprintID: bp.ID,
			ProjectID:   projectID,
			Version:     bp.Version,
			Checksum:    bp.Checksum,
			AppliedBy:   &userID,
			AppliedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}))
	}

	insertAgent := func(projectID, name string) string {
		id := uuid.NewString()
		_, err := db.ExecContext(ctx,
			`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, config)
			 VALUES (?, ?, 'graph', '', ?, CAST(? AS jsonb))`,
			id, name, projectID, `{"sourceBlueprintId":"`+bp.ID+`"}`)
		require.NoError(t, err)
		return id
	}
	agentA := insertAgent(projectA, "scoped-agent-a")
	agentB := insertAgent(projectB, "scoped-agent-b")

	svc := &Service{repo: repo, agentRepo: agentRepo, log: testLogger()}
	result, err := svc.Unapply(ctx, bp.ID, projectA)
	require.NoError(t, err)
	require.Equal(t, 1, result.Agents.Removed, "only project A's agent may be removed")

	countRow := func(id string) int {
		n, err := db.NewSelect().Model((*agents.Agent)(nil)).Where("id = ?", id).Count(ctx)
		require.NoError(t, err)
		return n
	}
	assert.Equal(t, 0, countRow(agentA), "project A's agent must be deleted")
	assert.Equal(t, 1, countRow(agentB), "project B's agent must be untouched")
}
