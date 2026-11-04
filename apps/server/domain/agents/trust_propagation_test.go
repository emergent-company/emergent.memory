package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// These tests pin the transitive trust propagation (issue #954): the fail-closed
// TrustedInternal marker is fixed at run creation and carried through delegation
// (child spawn) and resume, so the internal-agent reachability invariant holds
// for the whole call chain rather than the first hop.

// trustTestExecutor returns a real AgentExecutor wired against a real repository
// whose model factory fails (so runPipeline errors out after the run row is
// created — enough to observe the persisted trust marker without an LLM).
func trustTestExecutor(repo *Repository) *AgentExecutor {
	return &AgentExecutor{
		repo:         repo,
		modelFactory: adk.NewModelFactory(&config.LLMConfig{}, testAgentLogger(), nil, nil, nil),
		safeguards:   config.AgentSafeguardsConfig{ExecutionEnabled: true},
		log:          testAgentLogger(),
	}
}

func insertOrgAndProject(t *testing.T, db *bun.DB, ctx context.Context) (orgID, projectID string) {
	t.Helper()
	orgID = uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "trust-test-org")
	require.NoError(t, err)
	projectID = uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "trust-test-project")
	require.NoError(t, err)
	return orgID, projectID
}

func insertAgentDefinition(t *testing.T, db *bun.DB, ctx context.Context, projectID, name, visibility string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.agent_definitions (id, project_id, name, visibility) VALUES (?, ?, ?, ?)`, id, projectID, name, visibility)
	require.NoError(t, err)
	return id
}

func insertRuntimeAgent(t *testing.T, db *bun.DB, ctx context.Context, projectID, name string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, ?, ?, ?)`, id, name, "definition", "", projectID)
	require.NoError(t, err)
	return id
}

func insertAgentRun(t *testing.T, db *bun.DB, ctx context.Context, agentID, status string, trusted bool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.agent_runs (id, agent_id, status, started_at, trusted_internal) VALUES (?, ?, ?, now(), ?)`, id, agentID, status, trusted)
	require.NoError(t, err)
	return id
}

func runTrustedInternal(t *testing.T, db *bun.DB, ctx context.Context, runID string) bool {
	t.Helper()
	var trusted bool
	err := db.NewRaw(`SELECT trusted_internal FROM kb.agent_runs WHERE id = ?`, runID).Scan(ctx, &trusted)
	require.NoError(t, err)
	return trusted
}

// TestExecuteSingleSpawn_UntrustedParentPropagatesFalseToChild is the fail-first
// regression for the transitive reach (issue #954 Finding 1): an external-facing
// (untrusted) parent spawning a project-visibility child must mark the child
// untrusted too, so the child's coordination tools cannot reach internal agents
// one hop deeper. This fails if executeSingleSpawn drops TrustedInternal.
func TestExecuteSingleSpawn_UntrustedParentPropagatesFalseToChild(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_trust_spawn")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "project-child", string(VisibilityProject))
	insertRuntimeAgent(t, tdb.DB, ctx, projectID, "project-child")
	parentAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "parent-agent")
	parentRunID := insertAgentRun(t, tdb.DB, ctx, parentAgentID, "", false)

	repo := NewRepository(tdb.DB)
	ae := trustTestExecutor(repo)
	deps := &CoordinationToolDeps{
		Executor:        ae,
		Repo:            repo,
		Logger:          testAgentLogger(),
		ProjectID:       projectID,
		ParentRunID:     parentRunID,
		RootRunID:       parentRunID,
		TrustedInternal: false, // external-facing parent
		MaxDepth:        5,
	}

	res := executeSingleSpawn(ctx, deps, SpawnRequest{AgentName: "project-child", Task: "do work"})
	require.NotEmpty(t, res.RunID, "project child must still be spawned (it is not internal)")
	require.False(t, runTrustedInternal(t, tdb.DB, ctx, res.RunID),
		"a child of an external-facing parent must be persisted untrusted (trusted_internal=false)")
}

// TestExecuteSingleSpawn_TrustedParentPropagatesTrueToChild is the preserved-
// delegation check: a trusted parent (internal→internal / project→internal)
// spawning an internal child is allowed, and the child inherits trust=true.
func TestExecuteSingleSpawn_TrustedParentPropagatesTrueToChild(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_trust_spawn_trusted")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "internal-child", string(VisibilityInternal))
	insertRuntimeAgent(t, tdb.DB, ctx, projectID, "internal-child")
	parentAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "parent-agent")
	parentRunID := insertAgentRun(t, tdb.DB, ctx, parentAgentID, "", true)

	repo := NewRepository(tdb.DB)
	ae := trustTestExecutor(repo)
	deps := &CoordinationToolDeps{
		Executor:        ae,
		Repo:            repo,
		Logger:          testAgentLogger(),
		ProjectID:       projectID,
		ParentRunID:     parentRunID,
		RootRunID:       parentRunID,
		TrustedInternal: true, // trusted parent (session UI / MCP / agent→agent)
		MaxDepth:        5,
	}

	res := executeSingleSpawn(ctx, deps, SpawnRequest{AgentName: "internal-child", Task: "do work"})
	require.Empty(t, res.Error, "a trusted parent must be able to spawn an internal child")
	require.NotEmpty(t, res.RunID)
	require.True(t, runTrustedInternal(t, tdb.DB, ctx, res.RunID),
		"a child of a trusted parent must be persisted trusted (trusted_internal=true)")
}

// TestExecuteSingleSpawn_UntrustedParentCannotSpawnInternal proves the gate still
// holds end-to-end through the real spawn path: an external-facing parent
// attempting to spawn an internal target is rejected before any child run starts.
func TestExecuteSingleSpawn_UntrustedParentCannotSpawnInternal(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_trust_spawn_blocked")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "internal-child", string(VisibilityInternal))
	insertRuntimeAgent(t, tdb.DB, ctx, projectID, "internal-child")
	parentAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "parent-agent")
	parentRunID := insertAgentRun(t, tdb.DB, ctx, parentAgentID, "", false)

	repo := NewRepository(tdb.DB)
	ae := trustTestExecutor(repo)
	deps := &CoordinationToolDeps{
		Executor:        ae,
		Repo:            repo,
		Logger:          testAgentLogger(),
		ProjectID:       projectID,
		ParentRunID:     parentRunID,
		RootRunID:       parentRunID,
		TrustedInternal: false,
		MaxDepth:        5,
	}

	res := executeSingleSpawn(ctx, deps, SpawnRequest{AgentName: "internal-child", Task: "do work"})
	require.Equal(t, RunStatusError, res.Status)
	require.Contains(t, res.Error, "internal")
	require.Empty(t, res.RunID, "no child run must be started for a blocked internal spawn")
}

// TestResume_InheritsPriorRunTrust_NotUpgraded is the fail-first regression for
// the resume half of the transitive reach: a suspended external-facing run
// (trusted_internal=false) re-woken through a trusted path must stay untrusted.
// The resume request deliberately sets TrustedInternal=true to simulate a trusted
// wake path attempting an upgrade; Resume must ignore it and inherit the prior
// run's persisted value. This fails if Resume re-derives trust from the request.
func TestResume_InheritsPriorRunTrust_NotUpgraded(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_trust_resume")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "resume-agent")
	priorID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusPaused), false)

	repo := NewRepository(tdb.DB)
	ae := trustTestExecutor(repo)

	priorRun := &AgentRun{ID: priorID, AgentID: agentID, Status: RunStatusPaused, StepCount: 0, TrustedInternal: false}
	res, err := ae.Resume(ctx, priorRun, ExecuteRequest{
		Agent:           &Agent{ID: agentID, ProjectID: projectID, Name: "resume-agent"},
		ProjectID:       projectID,
		OrgID:           "",
		UserMessage:     "continue",
		TrustedInternal: true, // a trusted wake path must NOT upgrade the run
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotEmpty(t, res.RunID)
	require.False(t, runTrustedInternal(t, tdb.DB, ctx, res.RunID),
		"a resumed external-facing run must stay untrusted (trusted_internal=false)")
}
