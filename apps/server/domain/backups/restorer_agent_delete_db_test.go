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

// TestOverwriteRestore_TearsDownTriggersForAgentsNotRecreated covers issue
// #1361 item 1: the overwrite restore wipes kb.agents with raw SQL, bypassing
// the repository deletion seam. The restore must therefore explicitly fire the
// same teardown for the agent ids it removes and does not re-create, while
// leaving registrations for ids the snapshot re-creates in place (the rows
// reappear with the same ids, so tearing them down would strand live agents).
func TestOverwriteRestore_TearsDownTriggersForAgentsNotRecreated(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testutil.SetupTestDBOrFail(t, ctx, "restore_agent_delete")
	defer db.Close()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "restore-agent-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "restore-agent-project")
	require.NoError(t, err)

	keptID := uuid.NewString() // present in the snapshot -> re-created with the same id
	goneID := uuid.NewString() // absent from the snapshot -> removed for good
	for _, spec := range []struct{ id, name string }{
		{keptID, "restored-agent"},
		{goneID, "dropped-agent"},
	} {
		_, err := db.DB.ExecContext(ctx,
			`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, config)
			 VALUES (?, ?, 'graph', '', ?, '{}'::jsonb)`,
			spec.id, spec.name, projectID)
		require.NoError(t, err)
	}

	// Production wiring: TriggerService registers its teardown listener on the
	// agents repository, and the restorer's notifier routes to that repository.
	agentRepo := agents.NewRepository(db.DB)
	sched := scheduler.NewScheduler(slog.Default())
	ts := agents.NewTriggerService(sched, nil, agentRepo, nil, slog.Default())

	for _, spec := range []struct{ id, name string }{
		{keptID, "restored-agent"},
		{goneID, "dropped-agent"},
	} {
		ts.SyncAgentTrigger(&agents.Agent{
			ID:             spec.id,
			Name:           spec.name,
			ProjectID:      projectID,
			TriggerType:    agents.TriggerTypeReaction,
			Enabled:        true,
			ReactionConfig: &agents.ReactionConfig{ObjectTypes: []string{"document"}, Events: []agents.ReactionEventType{agents.EventTypeCreated}},
		})
	}
	require.Len(t, ts.GetEventListeners("document:created"), 2)

	var notified []string
	restorer := &Restorer{
		db:   db.DB,
		repo: NewRepository(db.DB, slog.Default()),
		log:  slog.Default(),
	}
	restorer.SetAgentDeletionNotifier(func(ids []string) {
		notified = append(notified, ids...)
		agentRepo.NotifyAgentsDeleted(ids)
	})

	// Snapshot carries only the kept agent, so the wipe removes both rows and
	// re-creates just that one.
	archive := &Archive{tableData: map[string][]byte{
		"agents": mustNDJSON(t, []map[string]any{{
			"id":            keptID,
			"project_id":    projectID,
			"name":          "restored-agent",
			"strategy_type": "graph",
			"cron_schedule": "",
		}}),
	}}

	job := &Restore{
		ID:             uuid.NewString(),
		OrganizationID: orgID,
		BackupID:       uuid.NewString(),
		Mode:           RestoreModeOverwrite,
	}
	backup := &Backup{ID: job.BackupID, OrganizationID: orgID, ProjectID: projectID, Status: BackupStatusReady}

	require.NoError(t, restorer.restoreOverwrite(ctx, job, RestoreRequest{}, backup, archive))

	// Only the id the snapshot dropped is reported to the seam.
	assert.Equal(t, []string{goneID}, notified)

	// The dropped row is gone; the restored row survives with its original id.
	keptCount, err := db.DB.NewSelect().Model((*agents.Agent)(nil)).Where("id = ?", keptID).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, keptCount)
	goneCount, err := db.DB.NewSelect().Model((*agents.Agent)(nil)).Where("id = ?", goneID).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, goneCount)

	// Trigger registrations: dropped id torn down, restored id untouched.
	listeners := ts.GetEventListeners("document:created")
	require.Len(t, listeners, 1)
	assert.Equal(t, keptID, listeners[0].ID)
}
