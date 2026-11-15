package chat

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/sources"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestRememberForgetReusesSourceTriggerBoundRuntimeRow is the #790 regression:
// once a definition owns a runtime row (here because a source trigger bound it),
// both a Remember and a Forget run must execute as THAT row. Nothing else may
// mint a second row carrying agent_definition_id — the
// (project_id, agent_definition_id) partial unique index rejects it with 23505,
// which is the deterministic 500 the #776 fix removed from these handlers.
//
// The run kickoff of both handlers resolves its row through
// rememberForgetRuntimeAgent (every sync/async site of Remember, RememberFile and
// Forget); remember_forget_wiring_test.go pins that all six sites keep doing so.
func TestRememberForgetReusesSourceTriggerBoundRuntimeRow(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "remember_forget_runtime_row")
	t.Cleanup(tdb.Close)

	orgID, projectID := uuid.New(), uuid.New()
	_, err := tdb.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "remember-forget-org")
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "remember-forget-project")
	require.NoError(t, err)

	// A definition-only agent (created from the UI): nothing has materialized a
	// runtime kb.agents row for it yet.
	defID := uuid.New()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility, enabled)
		 VALUES (?, ?, 'Remember/Forget Agent', 'project', true)`,
		defID, projectID)
	require.NoError(t, err)

	srcID := uuid.New()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.sources (id, project_id, provider, display_name, config, status)
		 VALUES (?, ?, 'github_app', 'bot', '{}'::jsonb, 'active')`, srcID, projectID)
	require.NoError(t, err)

	agentRepo := agents.NewRepository(tdb.DB)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srcSvc := sources.NewService(
		sources.NewRepository(tdb.DB, log), &config.Config{}, log,
		sources.NewRegistry(),
		sources.WithAgentRepository(agentRepo),
	)

	// The source-trigger bind is what gives the definition its runtime row.
	trigger, err := srcSvc.CreateSourceTrigger(ctx, projectID, srcID, sources.SourceTriggerInput{
		EventType: "pull_request", AgentID: defID.String(),
	})
	require.NoError(t, err)
	boundAgentID := trigger.AgentID.String()

	projectIDStr := projectID.String()
	def, err := agentRepo.FindDefinitionByID(ctx, defID.String(), &projectIDStr)
	require.NoError(t, err)
	require.NotNil(t, def, "the definition must exist")

	h := &Handler{agentRepo: agentRepo}

	// A Remember run resolves the definition's row.
	rememberRow, err := h.rememberForgetRuntimeAgent(ctx, def, "async remember")
	require.NoError(t, err, "a remember run must reuse the definition's runtime row")
	require.Equal(t, boundAgentID, rememberRow.ID,
		"remember resolved a different runtime row than the source-trigger bind created")

	// A Forget run resolves the same row (it must not mint a feature-named one).
	forgetRow, err := h.rememberForgetRuntimeAgent(ctx, def, "sync forget")
	require.NoError(t, err, "a forget run must reuse the definition's runtime row")
	require.Equal(t, boundAgentID, forgetRow.ID, "forget did not reuse the same runtime row as remember")

	// Exactly one row carries the definition link: the bind's, not one per run.
	var linked int
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT count(*) FROM kb.agents WHERE project_id = ? AND agent_definition_id = ?`,
		projectID, defID).Scan(ctx, &linked))
	require.Equal(t, 1, linked, "a second row carrying the definition link was created")

	// And nothing feature-named ("Remember session for …") was minted.
	var total int
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT count(*) FROM kb.agents WHERE project_id = ?`, projectID).Scan(ctx, &total))
	require.Equal(t, 1, total, "a remember/forget run minted a runtime row of its own")
}
