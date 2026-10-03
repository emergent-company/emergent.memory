package backups

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestOverwriteRestore_ReregistersRestoredAgentTriggers covers issue #1426: the
// overwrite restore's raw kb.agents rewrite bypassed the trigger-registration
// seam. Agents the snapshot re-creates must be reconciled from the restored rows
// (a changed reaction config must replace the stale registration) and agents the
// snapshot newly adds must be registered. Before the fix, only removed-and-not-
// recreated ids were torn down, so a re-created agent kept its old registration
// and a newly added agent was never registered until the next restart.
func TestOverwriteRestore_ReregistersRestoredAgentTriggers(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testutil.SetupTestDBOrFail(t, ctx, "restore_agent_reregister")
	defer db.Close()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "restore-reregister-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "restore-reregister-project")
	require.NoError(t, err)

	existingID := uuid.NewString() // live before restore; snapshot re-creates with a CHANGED config
	addedID := uuid.NewString()    // absent before restore; snapshot adds it

	// Pre-restore live row for the existing agent, with the OLD reaction config.
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, trigger_type, enabled, config, reaction_config)
		 VALUES (?, ?, 'graph', '', ?, 'reaction', true, '{}'::jsonb, '{"objectTypes":["document"],"events":["created"]}'::jsonb)`,
		existingID, "existing-agent", projectID)
	require.NoError(t, err)

	// Production wiring: TriggerService registers its deletion + restore
	// listeners on the agents repository, and the restorer's notifiers route to
	// that repository.
	agentRepo := agents.NewRepository(db.DB)
	sched := scheduler.NewScheduler(slog.Default())
	ts := agents.NewTriggerService(sched, nil, agentRepo, nil, slog.Default())

	// Register the existing agent as the live server has it: OLD config.
	ts.SyncAgentTrigger(&agents.Agent{
		ID:             existingID,
		Name:           "existing-agent",
		ProjectID:      projectID,
		TriggerType:    agents.TriggerTypeReaction,
		Enabled:        true,
		ReactionConfig: &agents.ReactionConfig{ObjectTypes: []string{"document"}, Events: []agents.ReactionEventType{agents.EventTypeCreated}},
	})
	require.Len(t, ts.GetEventListeners("document:created"), 1)

	var deletedNotified []string
	restorer := &Restorer{
		db:   db.DB,
		repo: NewRepository(db.DB, slog.Default()),
		log:  slog.Default(),
	}
	restorer.SetAgentDeletionNotifier(func(ids []string) {
		deletedNotified = append(deletedNotified, ids...)
		agentRepo.NotifyAgentsDeleted(ids)
	})
	restorer.SetAgentRestoreNotifier(agentRepo.NotifyAgentsRestored)

	// Snapshot: the existing agent now reacts to document:updated (changed), and
	// adds a brand-new agent that reacts to chunk:created.
	archive := &Archive{tableData: map[string][]byte{
		"agents": mustNDJSON(t, []map[string]any{
			{
				"id":            existingID,
				"project_id":    projectID,
				"name":          "existing-agent",
				"strategy_type": "graph",
				"cron_schedule": "",
				"trigger_type":  "reaction",
				"enabled":       true,
				"reaction_config": map[string]any{
					"objectTypes": []string{"document"},
					"events":      []string{"updated"},
				},
			},
			{
				"id":            addedID,
				"project_id":    projectID,
				"name":          "added-agent",
				"strategy_type": "graph",
				"cron_schedule": "",
				"trigger_type":  "reaction",
				"enabled":       true,
				"reaction_config": map[string]any{
					"objectTypes": []string{"chunk"},
					"events":      []string{"created"},
				},
			},
		}),
	}}

	job := &Restore{
		ID:             uuid.NewString(),
		OrganizationID: orgID,
		BackupID:       uuid.NewString(),
		Mode:           RestoreModeOverwrite,
	}
	backup := &Backup{ID: job.BackupID, OrganizationID: orgID, ProjectID: projectID, Status: BackupStatusReady}

	require.NoError(t, restorer.restoreOverwrite(ctx, job, RestoreRequest{}, backup, archive))

	// Both restored agents are present; neither was reported as deleted.
	assert.Empty(t, deletedNotified)

	// The stale registration for the changed agent is gone.
	assert.Empty(t, ts.GetEventListeners("document:created"),
		"old registration for the re-created agent must be torn down")

	// The re-created agent is registered under its NEW config.
	updated := listenerIDs(ts.GetEventListeners("document:updated"))
	assert.ElementsMatch(t, []string{existingID}, updated,
		"re-created agent must be re-registered from the restored row")

	// The newly added agent is registered.
	added := listenerIDs(ts.GetEventListeners("chunk:created"))
	assert.ElementsMatch(t, []string{addedID}, added,
		"newly added restored agent must be registered")
}

// listenerIDs returns the agent ids of a listener slice for membership
// assertions (assert the set, not the count).
func listenerIDs(listeners []*agents.Agent) []string {
	ids := make([]string, 0, len(listeners))
	for _, a := range listeners {
		ids = append(ids, a.ID)
	}
	return ids
}
