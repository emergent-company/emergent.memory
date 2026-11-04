package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestAgentRunToDTOSubjectObjectID verifies ToDTO carries the board/work
// subject-object linkage (subject_object_id) onto AgentRunDTO, and stays nil for
// runs that were not object-driven.
func TestAgentRunToDTOSubjectObjectID(t *testing.T) {
	subject := uuid.NewString()
	with := (&AgentRun{ID: "r1", SubjectObjectID: &subject}).ToDTO()
	require.NotNil(t, with.SubjectObjectID)
	require.Equal(t, subject, *with.SubjectObjectID)

	without := (&AgentRun{ID: "r2"}).ToDTO()
	require.Nil(t, without.SubjectObjectID)
}

// TestListBoardRunsByProjectID verifies the board-run projection: only the
// project's runs with a non-null subject_object_id are returned (excluding
// same-project manual runs and other-project board runs), newest first, with the
// Agent relation loaded so ToDTO resolves AgentName.
func TestListBoardRunsByProjectID(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_board_runs")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	repo := NewRepository(db)

	_, projectID := insertOrgAndProject(t, db, ctx)
	_, otherProjectID := insertOrgAndProject(t, db, ctx)

	agentID := insertRuntimeAgent(t, db, ctx, projectID, "board-agent")
	otherAgentID := insertRuntimeAgent(t, db, ctx, otherProjectID, "other-agent")

	// Board run in the target project.
	boardRunID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agent_runs (id, agent_id, status, started_at, subject_object_id) VALUES (?, ?, 'completed', now(), ?::uuid)`,
		boardRunID, agentID, uuid.NewString())
	require.NoError(t, err)

	// Same project, but no subject_object_id → not a board run.
	manualRunID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agent_runs (id, agent_id, status, started_at) VALUES (?, ?, 'completed', now())`,
		manualRunID, agentID)
	require.NoError(t, err)

	// Board run in a different project.
	otherBoardRunID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agent_runs (id, agent_id, status, started_at, subject_object_id) VALUES (?, ?, 'completed', now(), ?::uuid)`,
		otherBoardRunID, otherAgentID, uuid.NewString())
	require.NoError(t, err)

	runs, err := repo.ListBoardRunsByProjectID(ctx, projectID, 50)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, boardRunID, runs[0].ID)
	require.NotNil(t, runs[0].SubjectObjectID)
	require.NotNil(t, runs[0].Agent)
	require.Equal(t, "board-agent", runs[0].ToDTO().AgentName)
}
