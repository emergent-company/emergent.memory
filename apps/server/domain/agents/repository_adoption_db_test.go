package agents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// recordingTriggerSyncer records SyncAgentTrigger calls so a test can assert
// which runtime agent the handler registered a reaction trigger for.
type recordingTriggerSyncer struct {
	synced []*Agent
}

func (r *recordingTriggerSyncer) SyncAgentTrigger(agent *Agent) {
	r.synced = append(r.synced, agent)
}

func (r *recordingTriggerSyncer) RemoveAgentTrigger(agentID string) {}

// TestFindOrCreateRuntimeAgentForDefinitionAdoptionPredicate pins the #737/#738
// adoption fix: a same-name runtime row of another shape (its own prompt/model,
// no definition link) is a distinct agent and must NOT be re-pointed at the
// definition; only a row already carrying the definition's own strategy marker
// ("agent-def:<defID>") with a missing link is adopted.
func TestFindOrCreateRuntimeAgentForDefinitionAdoptionPredicate(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "adoption_predicate")
	t.Cleanup(tdb.Close)

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	countRows := func() int {
		var n int
		require.NoError(t, tdb.DB.NewRaw(`SELECT count(*) FROM kb.agents WHERE project_id = ?`, projectID).Scan(ctx, &n))
		return n
	}

	// An independent same-name runtime agent: created from the Schedules page or
	// the agents API, with its own prompt/model and no definition link.
	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "Shared Name", "project")
	independentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, prompt)
		 VALUES (?, 'Shared Name', 'definition', '0 * * * *', ?, 'its own prompt')`,
		independentID, projectID)
	require.NoError(t, err)
	require.Equal(t, 1, countRows())

	def, err := repo.FindDefinitionByID(ctx, defID, &projectID)
	require.NoError(t, err)
	require.NotNil(t, def)

	got, err := repo.FindOrCreateRuntimeAgentForDefinition(ctx, def)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotEqual(t, independentID, got.ID, "an unrelated same-name agent was adopted")
	require.Equal(t, projectID, got.ProjectID)
	require.Equal(t, defID, *got.AgentDefinitionID)

	// Two rows remain; the independent one keeps its own config and no link.
	require.Equal(t, 2, countRows())
	var indepDefID *string
	var indepPrompt *string
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT agent_definition_id, prompt FROM kb.agents WHERE id = ?`, independentID).
		Scan(ctx, &indepDefID, &indepPrompt))
	require.Nil(t, indepDefID, "the independent agent was stamped with the definition link")
	require.NotNil(t, indepPrompt)
	require.Equal(t, "its own prompt", *indepPrompt)
}

// TestFindOrCreateRuntimeAgentForDefinitionAdoptsLegacyShape pins the behaviour
// that must not regress: a same-name row already carrying the definition's own
// strategy marker but a missing link is adopted and backfilled.
func TestFindOrCreateRuntimeAgentForDefinitionAdoptsLegacyShape(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "adoption_legacy_shape")
	t.Cleanup(tdb.Close)

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "Legacy Agent", "project")
	legacyID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, 'Legacy Agent', ?, '0 0 * * *', ?)`,
		legacyID, "agent-def:"+defID, projectID)
	require.NoError(t, err)

	def, err := repo.FindDefinitionByID(ctx, defID, &projectID)
	require.NoError(t, err)
	require.NotNil(t, def)

	got, err := repo.FindOrCreateRuntimeAgentForDefinition(ctx, def)
	require.NoError(t, err)
	require.Equal(t, legacyID, got.ID, "did not adopt the legacy definition-shaped row")
	require.NotNil(t, got.AgentDefinitionID)
	require.Equal(t, defID, *got.AgentDefinitionID)

	var n int
	require.NoError(t, tdb.DB.NewRaw(`SELECT count(*) FROM kb.agents WHERE project_id = ?`, projectID).Scan(ctx, &n))
	require.Equal(t, 1, n, "adoption duplicated the legacy row")
}

// TestFindAgentsByDefinitionIDProjectScoped pins the #738 project-scope fix: a
// runtime row in project B that carries project A's definition id must not be
// returned by a project-A-scoped lookup, and FindOrCreateRuntimeAgentForDefinition
// must mint its own row in project A rather than reuse B's.
func TestFindAgentsByDefinitionIDProjectScoped(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "definition_link_project_scope")
	t.Cleanup(tdb.Close)

	orgID, projectA := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	projectB := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectB, orgID, "other-project")
	require.NoError(t, err)

	defID := insertAgentDefinition(t, tdb.DB, ctx, projectA, "Cross Project", "project")
	// A foreign-project row carrying project A's definition id.
	foreignID := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, agent_definition_id)
		 VALUES (?, ?, 'Cross Project', ?, '0 0 * * *', ?)`,
		foreignID, projectB, "agent-def:"+defID, defID)
	require.NoError(t, err)

	rows, err := repo.FindAgentsByDefinitionID(ctx, projectA, defID)
	require.NoError(t, err)
	require.Empty(t, rows, "a project-A-scoped lookup returned project B's row")

	def, err := repo.FindDefinitionByID(ctx, defID, &projectA)
	require.NoError(t, err)
	require.NotNil(t, def)

	got, err := repo.FindOrCreateRuntimeAgentForDefinition(ctx, def)
	require.NoError(t, err)
	require.NotEqual(t, foreignID, got.ID, "reused the foreign-project runtime row")
	require.Equal(t, projectA, got.ProjectID)
	require.Equal(t, defID, *got.AgentDefinitionID)
}

// TestFindByNameDeterministic pins the #737 fix: with two same-name rows the
// lookup returns the oldest (created_at, then id), not an arbitrary row.
func TestFindByNameDeterministic(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "find_by_name_deterministic")
	t.Cleanup(tdb.Close)

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	older := uuid.NewString()
	newer := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, created_at)
		 VALUES (?, 'dup-name', 'definition', '', ?, '2021-01-01T00:00:00Z')`,
		newer, projectID)
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, created_at)
		 VALUES (?, 'dup-name', 'definition', '', ?, '2020-01-01T00:00:00Z')`,
		older, projectID)
	require.NoError(t, err)

	got, err := repo.FindByName(ctx, projectID, "dup-name")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, older, got.ID, "FindByName did not return the earliest-created row")
}

// TestAgentsDefinitionLinkUniqueIndex pins the #738 partial unique index: after
// migration 00301 a second runtime row for the same (project_id,
// agent_definition_id) cannot be inserted.
func TestAgentsDefinitionLinkUniqueIndex(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_definition_link_unique")
	t.Cleanup(tdb.Close)

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "Unique Link", "project")

	first := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, agent_definition_id)
		 VALUES (?, ?, 'Unique Link', ?, '0 0 * * *', ?)`,
		first, projectID, "agent-def:"+defID, defID)
	require.NoError(t, err)

	second := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, agent_definition_id)
		 VALUES (?, ?, 'Unique Link', ?, '0 0 * * *', ?)`,
		second, projectID, "agent-def:"+defID, defID)
	require.Error(t, err, "a duplicate (project_id, agent_definition_id) row was accepted")
	require.True(t, pgutils.IsUniqueViolation(err), "expected a unique violation, got: %v", err)
}

// TestDefinitionForIDProjectScoped pins the project filter added to the agents
// create/update definition lookup: a definition id from another project must not
// resolve, so its work config is never consulted for this project's agent.
func TestDefinitionForIDProjectScoped(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "definition_for_id_project_scope")
	t.Cleanup(tdb.Close)

	orgID, projectA := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	projectB := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectB, orgID, "other-project")
	require.NoError(t, err)

	foreignDefID := insertAgentDefinition(t, tdb.DB, ctx, projectB, "Foreign Def", "project")
	h := &Handler{repo: repo}

	require.Nil(t, h.definitionForID(ctx, projectA, &foreignDefID),
		"a foreign-project definition id resolved for this project's agent")
	require.NotNil(t, h.definitionForID(ctx, projectB, &foreignDefID),
		"the definition did not resolve within its own project")
}

// TestSyncDefinitionWorkTriggersFallbackReResolvesWinner pins the #738 race
// recovery in syncDefinitionWorkTriggers: when its CreateAgent insert is rejected
// by the (project_id, agent_definition_id) partial unique index — because a
// competing binder linked the definition first — it must re-resolve that winner
// by definition link (not name) and apply the derived reaction config to it,
// leaving an unrelated same-name (unlinked) runtime row untouched.
//
// The race is reproduced deterministically across two connections. A BEFORE
// INSERT trigger blocks the handler's linked insert on a session advisory lock
// held by a dedicated controller connection. The controller waits until the
// handler's insert is actually blocked (by which point its FindAgentsByDefinitionID
// read has already returned empty), commits the competing winner, then releases
// the lock so the handler's insert proceeds into the unique-index violation.
// A trigger cannot mint the winner itself: any row it inserts would be rolled
// back together with the failed insert, leaving no winner to re-resolve.
func TestSyncDefinitionWorkTriggersFallbackReResolvesWinner(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sync_definition_trigger_fallback")
	t.Cleanup(tdb.Close)

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "Fallback Agent", "project")

	// An independent same-name runtime agent (own prompt, no definition link).
	independentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, prompt)
		 VALUES (?, 'Fallback Agent', 'definition', '0 * * * *', ?, 'its own prompt')`,
		independentID, projectID)
	require.NoError(t, err)

	// A dedicated controller connection holds a session advisory lock that the
	// trigger below blocks on, pausing the handler's linked insert.
	controller, err := tdb.Pool.Acquire(ctx)
	require.NoError(t, err)
	defer controller.Release()
	const gateKey int64 = 0x73796e6347617465 // "syncGate"
	_, err = controller.Exec(ctx, `SELECT pg_advisory_lock($1)`, gateKey)
	require.NoError(t, err)
	defer controller.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, gateKey)

	// The trigger blocks only the handler's own linked insert (the definition's
	// exact name, matching definition id, outer trigger depth). The controller's
	// winner insert carries a different name, so it passes straight through.
	_, err = tdb.DB.ExecContext(ctx, fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION block_for_definition() RETURNS trigger AS $$
		BEGIN
			IF pg_trigger_depth() <> 1 THEN
				RETURN NEW;
			END IF;
			IF NEW.agent_definition_id::text IS DISTINCT FROM '%s' THEN
				RETURN NEW;
			END IF;
			IF NEW.name IS DISTINCT FROM 'Fallback Agent' THEN
				RETURN NEW;
			END IF;
			PERFORM pg_advisory_xact_lock(%d);
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;`, defID, gateKey))
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx,
		`CREATE TRIGGER trg_block_definition BEFORE INSERT ON kb.agents
		 FOR EACH ROW EXECUTE FUNCTION block_for_definition()`)
	require.NoError(t, err)

	def := &AgentDefinition{
		ID:        defID,
		ProjectID: projectID,
		Name:      "Fallback Agent",
		Enabled:   true,
		WorkConfig: AgentWorkConfig{
			ObjectRules: []AgentObjectRule{
				{ObjectType: "Task", Triggers: []AgentWorkTrigger{{ObjectType: "Task", Status: "ready"}}},
			},
		},
	}

	syncer := &recordingTriggerSyncer{}
	h := &Handler{repo: repo, triggers: syncer}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.syncDefinitionWorkTriggers(ctx, def)
	}()

	// Wait until the handler's insert is blocked on the advisory lock: the point
	// where its first FindAgentsByDefinitionID has returned empty and the winner
	// can be committed without the handler seeing it first.
	require.True(t, waitForAdvisoryLockWait(t, ctx, tdb, gateKey),
		"handler's linked insert never blocked on the advisory lock")

	// Commit the competing linked winner, then release the gate so the handler's
	// insert proceeds into the unique-index violation.
	_, err = controller.Exec(ctx,
		`INSERT INTO kb.agents (project_id, name, strategy_type, cron_schedule, enabled, trigger_type, agent_definition_id)
		 VALUES ($1, $2, $3, '0 0 * * *', true, 'schedule', $4)`,
		projectID, "Fallback Agent (winner)", "agent-def:"+defID, defID)
	require.NoError(t, err)
	_, err = controller.Exec(ctx, `SELECT pg_advisory_unlock($1)`, gateKey)
	require.NoError(t, err)

	<-done

	// Exactly one definition-linked row, carrying the re-derived reaction config.
	var linked []string
	err = tdb.DB.NewRaw(
		`SELECT id FROM kb.agents WHERE project_id = ? AND agent_definition_id = ?`,
		projectID, defID).Scan(ctx, &linked)
	require.NoError(t, err)
	require.Len(t, linked, 1, "expected exactly one definition-linked runtime row")

	winner := &Agent{}
	require.NoError(t, tdb.DB.NewSelect().Model(winner).Where("id = ?", linked[0]).Scan(ctx))
	require.Equal(t, TriggerTypeReaction, winner.TriggerType)
	require.True(t, winner.Enabled)
	require.NotNil(t, winner.ReactionConfig)
	require.Equal(t, []string{"Task"}, winner.ReactionConfig.ObjectTypes)

	// The independent row is still unlinked, keeps its own prompt, and its
	// trigger type was not rewritten.
	var indepDefID *string
	var indepPrompt *string
	var indepTriggerType string
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT agent_definition_id, prompt, trigger_type FROM kb.agents WHERE id = ?`, independentID).
		Scan(ctx, &indepDefID, &indepPrompt, &indepTriggerType))
	require.Nil(t, indepDefID, "the independent agent was linked to the definition")
	require.NotNil(t, indepPrompt)
	require.Equal(t, "its own prompt", *indepPrompt)
	require.Equal(t, "schedule", indepTriggerType, "the independent agent's trigger was rewritten")

	// The failed insert was not persisted: two rows, not three.
	var n int
	require.NoError(t, tdb.DB.NewRaw(`SELECT count(*) FROM kb.agents WHERE project_id = ?`, projectID).Scan(ctx, &n))
	require.Equal(t, 2, n)

	// The winner is the row the handler registered, not the unpersisted insert.
	require.Len(t, syncer.synced, 1)
	require.Equal(t, linked[0], syncer.synced[0].ID)
}

// waitForAdvisoryLockWait blocks until a backend is waiting (granted=false) on
// the given advisory lock key within the current database. Polling pg_locks keeps
// the handshake event-driven rather than relying on a fixed sleep.
func waitForAdvisoryLockWait(t *testing.T, ctx context.Context, tdb *testdb.TestDB, key int64) bool {
	t.Helper()
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		err := tdb.DB.NewRaw(`
			SELECT count(*)
			FROM pg_locks
			WHERE locktype = 'advisory'
			  AND NOT granted
			  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
			  AND ((classid::bigint << 32) | objid::bigint) = ?`, key).Scan(ctx, &n)
		if err == nil && n > 0 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
	return false
}
