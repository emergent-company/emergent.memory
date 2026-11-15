package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/emergent-company/emergent.memory/internal/migrate"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// TestAgentsDefinitionLinkMigrationRewritesLosers pins migration 00302's two data
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
	// The test DB is migrated to head; roll back 00302 so we can seed the legacy
	// linked/duplicate rows it is about to inspect, then re-apply it.
	require.NoError(t, mig.DownTo(ctx, 301))

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

	require.NoError(t, mig.UpTo(ctx, 302))

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

	// Idempotency: roll back and re-apply 00302. The re-mark and dedupe are no-ops
	// on the already-transformed data, and the name is not double-suffixed.
	require.NoError(t, mig.DownTo(ctx, 301))
	require.NoError(t, mig.UpTo(ctx, 302))

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
