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
	// instead of creating a duplicate.
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

	// A runtime row created by an older path (A2A) from the name alone carries the
	// definition's own strategy marker ("agent-def:<defID>") but no definition FK.
	// It is adopted — and backfilled so the definition resolves through the
	// authoritative FK — rather than duplicated. Only this definition-shaped row
	// is adopted; a same-name row of another shape is a distinct agent.
	legacyDefID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility) VALUES (?, ?, 'Legacy Named Agent', 'project')`,
		legacyDefID, projectID)
	require.NoError(t, err)
	legacyRuntimeID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, 'Legacy Named Agent', ?, '', ?)`,
		legacyRuntimeID, "agent-def:"+legacyDefID.String(), projectID)
	require.NoError(t, err)
	adopted, err := svc.CreateSourceTrigger(ctx, projectID, newSource(), SourceTriggerInput{
		EventType: "pull_request", AgentID: legacyDefID.String(),
	})
	require.NoError(t, err)
	require.Equal(t, legacyRuntimeID, adopted.AgentID, "did not adopt the legacy definition-shaped runtime row")
	var backfilled *string
	require.NoError(t, db.DB.NewRaw(
		`SELECT agent_definition_id FROM kb.agents WHERE id = ?`, legacyRuntimeID).Scan(ctx, &backfilled))
	require.NotNil(t, backfilled, "adopted row kept a nil agent_definition_id")
	require.Equal(t, legacyDefID.String(), *backfilled)
}

// TestSourceTriggerRefusesDisabledDefinition pins the #738 disabled-definition
// fix: binding a disabled definition is refused at the bind surface with a 400
// class error, rather than leaving a binding that queues work the worker always
// fails as "agent is disabled".
func TestSourceTriggerRefusesDisabledDefinition(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_trigger_disabled_definition")
	t.Cleanup(db.Close)
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "disabled-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "disabled-project")
	require.NoError(t, err)

	disabledDefID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility, enabled) VALUES (?, ?, 'Disabled Agent', 'project', false)`,
		disabledDefID, projectID)
	require.NoError(t, err)

	srcID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.sources (id, project_id, provider, display_name, config, status)
		 VALUES (?, ?, 'github_app', 'bot', '{}'::jsonb, 'active')`, srcID, projectID)
	require.NoError(t, err)

	agentRepo := agents.NewRepository(db.DB)
	svc := NewService(repo, &config.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), NewRegistry(newGitHubAppProvider()),
		WithAgentRepository(agentRepo))

	_, err = svc.CreateSourceTrigger(ctx, projectID, srcID, SourceTriggerInput{
		EventType: "pull_request", AgentID: disabledDefID.String(),
	})
	require.Error(t, err)
	var invalid *apperror.Error
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, 422, invalid.HTTPStatus)
	require.Contains(t, invalid.Message, "Disabled Agent")

	var n int
	require.NoError(t, db.DB.NewRaw(`SELECT count(*) FROM kb.agents WHERE project_id = ?`, projectID).Scan(ctx, &n))
	require.Equal(t, 0, n, "a disabled-definition bind created a runtime agent")
}

// TestSourceTriggerRefusesDisabledRuntimeAgent pins the #738 disabled-runtime-agent
// fix: binding a runtime kb.agents id whose row is disabled is refused at the bind
// surface with a 400-class error, rather than leaving a binding that queues work
// the worker always fails as "agent is disabled". The definition check in
// resolveTriggerAgentID covers definition ids; this covers a disabled runtime row.
func TestSourceTriggerRefusesDisabledRuntimeAgent(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_trigger_disabled_runtime_agent")
	t.Cleanup(db.Close)
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "disabled-runtime-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "disabled-runtime-project")
	require.NoError(t, err)

	// A disabled runtime agent with no definition FK, so the definition check
	// above the runtime-agent check in resolveTriggerAgentID finds nothing and the
	// disabled-runtime-agent refusal is the path exercised.
	disabledAgentID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, enabled)
		 VALUES (?, ?, 'Disabled Runtime', 'definition', '', false)`,
		disabledAgentID, projectID)
	require.NoError(t, err)

	srcID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.sources (id, project_id, provider, display_name, config, status)
		 VALUES (?, ?, 'github_app', 'bot', '{}'::jsonb, 'active')`, srcID, projectID)
	require.NoError(t, err)

	agentRepo := agents.NewRepository(db.DB)
	svc := NewService(repo, &config.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), NewRegistry(newGitHubAppProvider()),
		WithAgentRepository(agentRepo))

	_, err = svc.CreateSourceTrigger(ctx, projectID, srcID, SourceTriggerInput{
		EventType: "pull_request", AgentID: disabledAgentID.String(),
	})
	require.Error(t, err)
	var invalid *apperror.Error
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, 422, invalid.HTTPStatus)
	require.Contains(t, invalid.Message, "Disabled Runtime")

	var n int
	require.NoError(t, db.DB.NewRaw(`SELECT count(*) FROM kb.agents WHERE project_id = ?`, projectID).Scan(ctx, &n))
	require.Equal(t, 1, n, "a disabled-runtime-agent bind created an extra runtime agent")
}

// TestSourceTriggerSkipsForeignProjectRuntimeRow pins the #738 project-scope fix
// at the bind surface: a runtime row in another project that carries this
// project's definition id must not be bound (its runs would be enqueued in the
// other project); the bind mints a row in this project instead.
func TestSourceTriggerSkipsForeignProjectRuntimeRow(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_trigger_foreign_project")
	t.Cleanup(db.Close)
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectA := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "foreign-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectA, orgID, "project-a")
	require.NoError(t, err)
	projectB := uuid.New()
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectB, orgID, "project-b")
	require.NoError(t, err)

	defID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility) VALUES (?, ?, 'Foreign Linked', 'project')`,
		defID, projectA)
	require.NoError(t, err)
	// A runtime row in project B that carries project A's definition id.
	foreignAgent := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, agent_definition_id)
		 VALUES (?, ?, 'Foreign Linked', ?, '0 0 * * *', ?)`,
		foreignAgent, projectB, "agent-def:"+defID.String(), defID)
	require.NoError(t, err)

	srcID := uuid.New()
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.sources (id, project_id, provider, display_name, config, status)
		 VALUES (?, ?, 'github_app', 'bot', '{}'::jsonb, 'active')`, srcID, projectA)
	require.NoError(t, err)

	agentRepo := agents.NewRepository(db.DB)
	svc := NewService(repo, &config.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), NewRegistry(newGitHubAppProvider()),
		WithAgentRepository(agentRepo))

	trigger, err := svc.CreateSourceTrigger(ctx, projectA, srcID, SourceTriggerInput{
		EventType: "pull_request", AgentID: defID.String(),
	})
	require.NoError(t, err)
	require.NotEqual(t, foreignAgent, trigger.AgentID, "a foreign-project runtime row was bound")

	bound, err := agentRepo.FindByID(ctx, trigger.AgentID.String(), strPtr(projectA.String()))
	require.NoError(t, err)
	require.NotNil(t, bound)
	require.Equal(t, projectA.String(), bound.ProjectID)
}
