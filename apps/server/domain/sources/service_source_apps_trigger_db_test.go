package sources

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestSourceTriggerBindsDefinitionOnlyAgent pins #727. The trigger picker offers
// kb.agent_definitions rows, but kb.source_triggers.agent_id is a foreign key to
// kb.agents, so binding an agent that exists only as a definition must resolve —
// and materialize — its runtime row at bind time and store that id. Before the
// fix every such bind failed with "Agent '<definition id>' not found", which made
// the feature unusable for any project whose agents were created in the UI.
func TestSourceTriggerBindsDefinitionOnlyAgent(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_trigger_definition_agent")
	t.Cleanup(db.Close)
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "trigger-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "trigger-project")
	require.NoError(t, err)

	// A UI-created agent is a definition only: nothing has materialized a runtime
	// kb.agents row for it yet.
	defID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility) VALUES (?, ?, 'PR Trigger Test', 'project')`,
		defID, projectID)
	require.NoError(t, err)
	runtimeRows := func() int {
		var n int
		require.NoError(t, db.DB.NewRaw(`SELECT count(*) FROM kb.agents WHERE project_id = ?`, projectID).Scan(ctx, &n))
		return n
	}
	require.Zero(t, runtimeRows(), "fixture already has a kb.agents row")

	newSource := func() uuid.UUID {
		id := uuid.New()
		_, err := db.DB.ExecContext(ctx,
			`INSERT INTO kb.sources (id, project_id, provider, display_name, config, status)
			 VALUES (?, ?, 'github_app', 'bot', '{}'::jsonb, 'active')`, id, projectID)
		require.NoError(t, err)
		return id
	}
	srcID := newSource()

	agentRepo := agents.NewRepository(db.DB)
	svc := NewService(repo, &config.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), NewRegistry(newGitHubAppProvider()),
		WithAgentRepository(agentRepo))

	trigger, err := svc.CreateSourceTrigger(ctx, projectID, srcID, SourceTriggerInput{
		EventType: "pull_request", AgentID: defID.String(),
	})
	require.NoError(t, err)
	require.NotEqual(t, defID, trigger.AgentID,
		"stored the definition id: kb.source_triggers.agent_id is a kb.agents foreign key")
	// The panel label is resolved server-side: the picker's definition list cannot
	// map the stored runtime id.
	require.Equal(t, "PR Trigger Test", trigger.AgentName)

	runtimeAgent, err := agentRepo.FindByID(ctx, trigger.AgentID.String(), strPtr(projectID.String()))
	require.NoError(t, err)
	require.NotNil(t, runtimeAgent, "binding did not create the definition-backed runtime agent")
	require.Equal(t, "agent-def:"+defID.String(), runtimeAgent.StrategyType)
	require.NotNil(t, runtimeAgent.AgentDefinitionID)
	require.Equal(t, defID.String(), *runtimeAgent.AgentDefinitionID)
	require.Equal(t, agents.TriggerTypeManual, runtimeAgent.TriggerType)
	require.True(t, runtimeAgent.Enabled)

	// Dispatch enqueues a run for the stored id; this is the step that could not
	// resolve anything before the fix.
	run, err := agentRepo.CreateRunQueued(ctx, trigger.AgentID.String(), 1)
	require.NoError(t, err)
	require.Equal(t, trigger.AgentID.String(), run.AgentID)

	// Binding the same definition from another source reuses the one runtime row
	// instead of creating a duplicate (which would trip the project name index).
	second, err := svc.CreateSourceTrigger(ctx, projectID, newSource(), SourceTriggerInput{
		EventType: "pull_request", AgentID: defID.String(),
	})
	require.NoError(t, err)
	require.Equal(t, trigger.AgentID, second.AgentID)
	require.Equal(t, 1, runtimeRows())

	// The list endpoint returns the resolved name for the existing rows.
	listed, err := svc.ListSourceTriggers(ctx, projectID, srcID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "PR Trigger Test", listed[0].AgentName)

	// A plain runtime agent id still binds unchanged and keeps its own name.
	runtimeOnlyID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, 'legacy-agent', 'definition', '', ?)`,
		runtimeOnlyID, projectID)
	require.NoError(t, err)
	direct, err := svc.CreateSourceTrigger(ctx, projectID, newSource(), SourceTriggerInput{
		EventType: "pull_request", AgentID: runtimeOnlyID.String(),
	})
	require.NoError(t, err)
	require.Equal(t, runtimeOnlyID, direct.AgentID)
	require.Equal(t, "legacy-agent", direct.AgentName)

	// An id in neither table is still a 404 naming the id, and creates nothing.
	unknown := uuid.New()
	beforeRejected := runtimeRows()
	_, err = svc.CreateSourceTrigger(ctx, projectID, newSource(), SourceTriggerInput{
		EventType: "pull_request", AgentID: unknown.String(),
	})
	var notFound *apperror.Error
	require.ErrorAs(t, err, &notFound)
	require.Equal(t, 404, notFound.HTTPStatus)
	require.Equal(t, beforeRejected, runtimeRows(), "a rejected bind created a runtime agent")

	// A duplicate event type on the same source is a 409, and the agent
	// resolution that precedes the insert must not leave its freshly materialized
	// runtime row behind when the unique index rejects the binding.
	otherDefID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility) VALUES (?, ?, 'Duplicate Bind Agent', 'project')`,
		otherDefID, projectID)
	require.NoError(t, err)
	beforeDuplicate := runtimeRows()
	_, err = svc.CreateSourceTrigger(ctx, projectID, srcID, SourceTriggerInput{
		EventType: "pull_request", AgentID: otherDefID.String(),
	})
	var conflict *apperror.Error
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, 409, conflict.HTTPStatus)
	require.Equal(t, beforeDuplicate, runtimeRows(), "a duplicate-event bind left an orphaned runtime agent")

	// A runtime row created by an older path (A2A) from the name alone carries no
	// definition FK. It is adopted — and backfilled so the definition resolves
	// through the authoritative FK — rather than colliding on the project name
	// index.
	legacyDefID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility) VALUES (?, ?, 'Legacy Named Agent', 'project')`,
		legacyDefID, projectID)
	require.NoError(t, err)
	legacyRuntimeID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, 'Legacy Named Agent', 'definition', '', ?)`,
		legacyRuntimeID, projectID)
	require.NoError(t, err)
	adopted, err := svc.CreateSourceTrigger(ctx, projectID, newSource(), SourceTriggerInput{
		EventType: "pull_request", AgentID: legacyDefID.String(),
	})
	require.NoError(t, err)
	require.Equal(t, legacyRuntimeID, adopted.AgentID, "did not adopt the existing name-only runtime row")
	var backfilled *string
	require.NoError(t, db.DB.NewRaw(
		`SELECT agent_definition_id FROM kb.agents WHERE id = ?`, legacyRuntimeID).Scan(ctx, &backfilled))
	require.NotNil(t, backfilled, "adopted row kept a nil agent_definition_id")
	require.Equal(t, legacyDefID.String(), *backfilled)
}
