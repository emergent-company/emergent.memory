package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"

	"github.com/emergent-company/emergent.memory/internal/migrate"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// TestAgentsDefinitionLinkMigrationRewritesLosers pins migration 00303's two data
// fixes, run on legacy rows that already violate the new partial unique index:
//
//   - an extraction shell (name 'extraction-agent:*' holding the definition link)
//     is re-marked to 'extraction-agent:<defID>' and unlinked BEFORE the dedupe,
//     so it never becomes the definition's canonical runtime row;
//   - a loser carrying strategy_type 'chat-session:<defID>' (not just
//     'agent-def:<defID>') is rewritten to 'definition' and unlinked, so
//     ResolveDefinitionForAgent step 2 can no longer resolve it back to the
//     definition.
//
// It also proves the Up body is idempotent by re-applying it, and that the index
// still rejects a duplicate link with 23505.
func TestAgentsDefinitionLinkMigrationRewritesLosers(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_definition_link_migration")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	mig := migrate.NewMigrator(db, zap.NewNop())
	// The test DB is migrated to head; roll back 00303 so we can seed the legacy
	// linked/duplicate rows it is about to inspect, then re-apply it.
	require.NoError(t, mig.DownTo(ctx, 302))

	_, projectID := insertOrgAndProject(t, db, ctx)

	// Legacy extraction shell: name 'extraction-agent:*', holds the definition
	// link, disabled and never scheduled.
	extractDefID := insertAgentDefinition(t, db, ctx, projectID, "Extractor", "project")
	extractShellID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, enabled, trigger_type, agent_definition_id)
		 VALUES (?, ?, 'extraction-agent:Extractor', 'agent-def:'||?, '0 0 * * *', false, 'manual', ?)`,
		extractShellID, projectID, extractDefID, extractDefID)
	require.NoError(t, err)

	// A duplicate group: an enabled winner (agent-def marker) and a chat-session
	// loser, both linking the same definition.
	dupDefID := insertAgentDefinition(t, db, ctx, projectID, "Dup", "project")
	winnerID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, enabled, trigger_type, agent_definition_id, created_at)
		 VALUES (?, ?, 'Dup', 'agent-def:'||?, '0 0 * * *', true, 'schedule', ?, '2020-01-01T00:00:00Z')`,
		winnerID, projectID, dupDefID, dupDefID)
	require.NoError(t, err)
	loserID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, enabled, trigger_type, agent_definition_id, created_at)
		 VALUES (?, ?, 'Dup', 'chat-session:'||?, '0 0 * * *', false, 'schedule', ?, '2021-01-01T00:00:00Z')`,
		loserID, projectID, dupDefID, dupDefID)
	require.NoError(t, err)

	require.NoError(t, mig.UpTo(ctx, 303))

	// Extraction shell: re-marked and unlinked, never the definition's owner.
	var extractStrategy string
	var extractLink *string
	require.NoError(t, db.NewRaw(
		`SELECT strategy_type, agent_definition_id FROM kb.agents WHERE id = ?`, extractShellID).
		Scan(ctx, &extractStrategy, &extractLink))
	require.Equal(t, "extraction-agent:"+extractDefID, extractStrategy)
	require.Nil(t, extractLink, "the extraction shell still holds the definition link")

	// Winner: kept its definition link and name.
	var winnerLink *string
	var winnerName string
	require.NoError(t, db.NewRaw(
		`SELECT agent_definition_id, name FROM kb.agents WHERE id = ?`, winnerID).
		Scan(ctx, &winnerLink, &winnerName))
	require.NotNil(t, winnerLink)
	require.Equal(t, dupDefID, *winnerLink)
	require.Equal(t, "Dup", winnerName, "the winner was renamed as a loser")

	// Chat-session loser: unlinked, marker rewritten to 'definition', name
	// disambiguated so ResolveDefinitionForAgent step 4 can no longer hit it.
	var loserStrategy string
	var loserLink *string
	var loserName string
	require.NoError(t, db.NewRaw(
		`SELECT strategy_type, agent_definition_id, name FROM kb.agents WHERE id = ?`, loserID).
		Scan(ctx, &loserStrategy, &loserLink, &loserName))
	require.Equal(t, "definition", loserStrategy, "the chat-session marker was not neutralised")
	require.Nil(t, loserLink)
	require.NotEqual(t, "Dup", loserName, "the loser's name was not disambiguated")

	// Idempotency: roll back and re-apply 00303. The re-mark and dedupe are no-ops
	// on the already-transformed data, and the name is not double-suffixed.
	require.NoError(t, mig.DownTo(ctx, 302))
	require.NoError(t, mig.UpTo(ctx, 303))

	var reStrategy string
	var reLink *string
	var reName string
	require.NoError(t, db.NewRaw(
		`SELECT strategy_type, agent_definition_id, name FROM kb.agents WHERE id = ?`, loserID).
		Scan(ctx, &reStrategy, &reLink, &reName))
	require.Equal(t, "definition", reStrategy)
	require.Nil(t, reLink)
	require.Equal(t, loserName, reName, "re-applying the migration double-suffixed the loser's name")

	// The index still rejects a duplicate link with 23505.
	dup := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, agent_definition_id)
		 VALUES (?, ?, 'Dup Again', 'agent-def:'||?, '0 0 * * *', ?)`,
		dup, projectID, dupDefID, dupDefID)
	require.Error(t, err, "a duplicate (project_id, agent_definition_id) row was accepted after re-apply")
	require.True(t, pgutils.IsUniqueViolation(err), "expected a unique violation, got: %v", err)
}

// insertRuns inserts n completed kb.agent_runs rows for the given runtime agent.
func insertRuns(t *testing.T, db *bun.DB, ctx context.Context, agentID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := db.ExecContext(ctx,
			`INSERT INTO kb.agent_runs (id, agent_id, status, started_at) VALUES (?, ?, 'completed', now())`,
			uuid.NewString(), agentID)
		require.NoError(t, err)
	}
}

// TestAgentsDefinitionLinkMigrationKeeperRuleAndLoserDisable pins the round-5
// BLOCKER fixes against a fixture that reproduces the production shape exactly:
//
//   - a manual "alfred" row created earlier with one run, and a scheduled,
//     enabled "Test" row created later with many runs, both linking one
//     definition. The keeper rule must keep the actively scheduled, high-run
//     row and unlink the manual row — which, being manual, stays enabled with
//     its (empty) schedule untouched.
//   - a second group where the loser is itself a scheduled row: it must end up
//     disabled with its cron reset to the inert daily default.
func TestAgentsDefinitionLinkMigrationKeeperRuleAndLoserDisable(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_definition_link_keeper")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	mig := migrate.NewMigrator(db, zap.NewNop())
	require.NoError(t, mig.DownTo(ctx, 302))

	_, projectID := insertOrgAndProject(t, db, ctx)

	// Fixture A — the production shape.
	prodDefID := insertAgentDefinition(t, db, ctx, projectID, "ProdDef", "project")
	alfredID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, enabled, trigger_type, agent_definition_id, created_at)
		 VALUES (?, ?, 'alfred', 'agent-def:'||?, '', true, 'manual', ?, '2020-01-01T00:00:00Z')`,
		alfredID, projectID, prodDefID, prodDefID)
	require.NoError(t, err)
	testID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, enabled, trigger_type, agent_definition_id, created_at)
		 VALUES (?, ?, 'Test', 'agent-def:'||?, '0 * * * *', true, 'schedule', ?, '2021-01-01T00:00:00Z')`,
		testID, projectID, prodDefID, prodDefID)
	require.NoError(t, err)
	insertRuns(t, db, ctx, alfredID, 1)
	insertRuns(t, db, ctx, testID, 5)

	// Fixture B — a scheduled loser (zero runs) that must be disabled.
	schedDefID := insertAgentDefinition(t, db, ctx, projectID, "SchedDef", "project")
	schedKeeperID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, enabled, trigger_type, agent_definition_id, created_at)
		 VALUES (?, ?, 'SchedKeeper', 'agent-def:'||?, '0 * * * *', true, 'schedule', ?, '2020-01-01T00:00:00Z')`,
		schedKeeperID, projectID, schedDefID, schedDefID)
	require.NoError(t, err)
	schedLoserID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, enabled, trigger_type, agent_definition_id, created_at)
		 VALUES (?, ?, 'SchedLoser', 'agent-def:'||?, '0 * * * *', true, 'schedule', ?, '2021-01-01T00:00:00Z')`,
		schedLoserID, projectID, schedDefID, schedDefID)
	require.NoError(t, err)
	insertRuns(t, db, ctx, schedKeeperID, 5)
	// schedLoser has zero runs.

	require.NoError(t, mig.UpTo(ctx, 303))

	// Fixture A: "Test" (scheduled, high run count) keeps the link and its live
	// schedule; "alfred" (manual) is unlinked but left enabled with its empty
	// schedule untouched.
	var testLink *string
	var testName string
	var testEnabled bool
	var testCron string
	require.NoError(t, db.NewRaw(
		`SELECT agent_definition_id, name, enabled, cron_schedule FROM kb.agents WHERE id = ?`, testID).
		Scan(ctx, &testLink, &testName, &testEnabled, &testCron))
	require.NotNil(t, testLink)
	require.Equal(t, prodDefID, *testLink)
	require.Equal(t, "Test", testName, "the live schedule was renamed as a loser")
	require.True(t, testEnabled)
	require.Equal(t, "0 * * * *", testCron, "the live schedule's cron was mutated")

	var alfredLink *string
	var alfredName string
	var alfredEnabled bool
	var alfredCron string
	require.NoError(t, db.NewRaw(
		`SELECT agent_definition_id, name, enabled, cron_schedule FROM kb.agents WHERE id = ?`, alfredID).
		Scan(ctx, &alfredLink, &alfredName, &alfredEnabled, &alfredCron))
	require.Nil(t, alfredLink, "the manual row was not unlinked")
	require.NotEqual(t, "alfred", alfredName, "the manual row's name was not disambiguated")
	require.True(t, alfredEnabled, "the manual loser was disabled (it has no schedule to stop)")
	require.Equal(t, "", alfredCron, "the manual loser's schedule was mutated")

	// Fixture B: the scheduled loser is disabled and its cron reset.
	var loserLink *string
	var loserEnabled bool
	var loserCron string
	var loserName string
	require.NoError(t, db.NewRaw(
		`SELECT agent_definition_id, enabled, cron_schedule, name FROM kb.agents WHERE id = ?`, schedLoserID).
		Scan(ctx, &loserLink, &loserEnabled, &loserCron, &loserName))
	require.Nil(t, loserLink)
	require.False(t, loserEnabled, "the scheduled loser was not disabled")
	require.Equal(t, "0 0 * * *", loserCron, "the scheduled loser's cron was not reset")

	// Re-apply 00303: the keeper rule, loser-disabling and renames must be
	// idempotent — the unlinked rows are no longer candidates, so nothing is
	// double-suffixed and the disabled loser stays disabled.
	require.NoError(t, mig.DownTo(ctx, 302))
	require.NoError(t, mig.UpTo(ctx, 303))

	var reAlfredName string
	require.NoError(t, db.NewRaw(`SELECT name FROM kb.agents WHERE id = ?`, alfredID).Scan(ctx, &reAlfredName))
	require.Equal(t, alfredName, reAlfredName, "re-apply double-suffixed the manual loser's name")

	var reLoserName string
	var reLoserEnabled bool
	require.NoError(t, db.NewRaw(`SELECT name, enabled FROM kb.agents WHERE id = ?`, schedLoserID).Scan(ctx, &reLoserName, &reLoserEnabled))
	require.Equal(t, loserName, reLoserName, "re-apply double-suffixed the scheduled loser's name")
	require.False(t, reLoserEnabled, "re-apply re-enabled the scheduled loser")
}
