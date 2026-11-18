package agents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/emergent-company/emergent.memory/internal/migrate"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// TestIsLiveRunConflict pins the conflict classifier to a real unique-violation
// shape. The branch was dead for a year because the index it names was built
// with the pre-ACP statuses and could never fire (issue #992); this test fails
// if the helper stops recognising the violation, rather than letting a genuine
// claim race surface as a generic insert error.
func TestIsLiveRunConflict(t *testing.T) {
	liveViolation := &pgconn.PgError{
		Code:           pgutils.CodeUniqueViolation,
		Severity:       "ERROR",
		Message:        `duplicate key value violates unique constraint "idx_agent_runs_live_subject"`,
		ConstraintName: "idx_agent_runs_live_subject",
	}

	require.True(t, isLiveRunConflict(liveViolation))
	// Exercise a wrapped error the way the repository's transaction layer
	// surfaces it, not only the bare driver error.
	require.True(t, isLiveRunConflict(fmt.Errorf("insert live work run: %w", liveViolation)))

	// A unique violation on another constraint, or the right index name on a
	// non-unique error, must not be classified as a claim conflict.
	require.False(t, isLiveRunConflict(&pgconn.PgError{
		Code:    pgutils.CodeUniqueViolation,
		Message: `duplicate key value violates unique constraint "agent_runs_pkey"`,
	}))
	require.False(t, isLiveRunConflict(&pgconn.PgError{
		Code:    pgutils.CodeForeignKeyViolation,
		Message: `insert or update violates foreign key constraint "idx_agent_runs_live_subject"`,
	}))
	require.False(t, isLiveRunConflict(nil))
}

// TestAgentRunLiveSubjectUniqueIndex proves the constraint the work claim
// relies on: at most one non-terminal (live) run per subject_object_id. Two
// live runs must violate idx_agent_runs_live_subject, while a terminal run plus
// a live one (and live runs for different subjects) are allowed.
func TestAgentRunLiveSubjectUniqueIndex(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_live_subject_index")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	_, projectID := insertOrgAndProject(t, db, ctx)
	agentID := insertRuntimeAgent(t, db, ctx, projectID, "live-subject-agent")

	insertRun := func(subject, status string) error {
		_, err := db.ExecContext(ctx,
			`INSERT INTO kb.agent_runs (id, agent_id, status, subject_object_id, subject_object_type, started_at, created_at)
			 VALUES (?, ?, ?, ?::uuid, 'GitHubIssue', now(), ?)`,
			uuid.NewString(), agentID, status, subject, time.Now())
		return err
	}

	subject := uuid.NewString()
	require.NoError(t, insertRun(subject, string(RunStatusRunning)))

	// A second live run for the same subject is the race the claim index exists
	// to prevent.
	err := insertRun(subject, string(RunStatusQueued))
	require.Error(t, err)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pgutils.CodeUniqueViolation, pgErr.Code)
	require.Equal(t, "idx_agent_runs_live_subject", pgErr.ConstraintName)
	require.True(t, isLiveRunConflict(err), "a real index violation must map to ErrWorkItemBusy")

	// A paused/input-required run also owns the slot.
	err = insertRun(subject, string(RunStatusPaused))
	require.Error(t, err)
	require.True(t, isLiveRunConflict(err))

	// Once the first run is terminal, a new live run may claim the subject.
	_, err = db.ExecContext(ctx,
		`UPDATE kb.agent_runs SET status = ?, completed_at = now() WHERE subject_object_id = ?::uuid`,
		string(RunStatusSuccess), subject)
	require.NoError(t, err)
	require.NoError(t, insertRun(subject, string(RunStatusQueued)))

	// The index is per subject, not global: another subject has its own slot.
	require.NoError(t, insertRun(uuid.NewString(), string(RunStatusRunning)))
	require.NoError(t, insertRun(uuid.NewString(), string(RunStatusPaused)))
}

// TestAgentRunLiveSubjectMigrationHealsDuplicates proves 00315's data step:
// legacy race duplicates (possible only while the old index was inert) are
// collapsed to one live run — an actively working run wins — and the rebuilt
// index then rejects a new duplicate.
func TestAgentRunLiveSubjectMigrationHealsDuplicates(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_live_subject_migration")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	// Roll back past 00315 so the old (inert) index is in place and duplicates
	// are insertable, exactly as they exist on a deployed database.
	mig := migrate.NewMigrator(db, zap.NewNop())
	require.NoError(t, mig.DownTo(ctx, 314))

	_, projectID := insertOrgAndProject(t, db, ctx)
	agentID := insertRuntimeAgent(t, db, ctx, projectID, "legacy-live-subject-agent")

	insertRun := func(subject, status string, createdAt time.Time) string {
		id := uuid.NewString()
		_, err := db.ExecContext(ctx,
			`INSERT INTO kb.agent_runs (id, agent_id, status, subject_object_id, subject_object_type, started_at, created_at)
			 VALUES (?, ?, ?, ?::uuid, 'GitHubIssue', now(), ?)`,
			id, agentID, status, subject, createdAt)
		require.NoError(t, err)
		return id
	}

	subject := uuid.NewString()
	// The older run is the one actually executing; it must be the survivor.
	winner := insertRun(subject, string(RunStatusRunning), time.Now().Add(-2*time.Hour))
	loser := insertRun(subject, string(RunStatusQueued), time.Now().Add(-time.Hour))

	var liveCount int
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_runs WHERE subject_object_id = ?::uuid
		  AND status IN ('submitted', 'working', 'input-required')`, subject).Scan(ctx, &liveCount))
	require.Equal(t, 2, liveCount, "two live runs must be seedable while the old index is inert")

	require.NoError(t, mig.UpTo(ctx, 315))

	var winnerStatus, loserStatus string
	var loserCompleted *time.Time
	require.NoError(t, db.NewRaw(`SELECT status FROM kb.agent_runs WHERE id = ?`, winner).Scan(ctx, &winnerStatus))
	require.NoError(t, db.NewRaw(`SELECT status, completed_at FROM kb.agent_runs WHERE id = ?`, loser).Scan(ctx, &loserStatus, &loserCompleted))
	require.Equal(t, string(RunStatusRunning), winnerStatus, "the executing run must survive the dedupe")
	require.Equal(t, string(RunStatusCancelled), loserStatus)
	require.NotNil(t, loserCompleted, "a cancelled duplicate is finalized")

	// The rebuilt index now rejects a new live run for the same subject.
	err := func() error {
		_, execErr := db.ExecContext(ctx,
			`INSERT INTO kb.agent_runs (id, agent_id, status, subject_object_id, started_at)
			 VALUES (?, ?, ?, ?::uuid, now())`,
			uuid.NewString(), agentID, string(RunStatusQueued), subject)
		return execErr
	}()
	require.Error(t, err)
	require.True(t, isLiveRunConflict(err))
}
