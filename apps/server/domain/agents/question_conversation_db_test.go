package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// insertAgentQuestion inserts a minimal pending agent question for a run. It
// mirrors the shape ListQuestionsByProject selects, exercising only the columns
// needed by the join (run_id/agent_id/project_id/question); the rest default.
func insertAgentQuestion(t *testing.T, db *bun.DB, ctx context.Context, runID, agentID, projectID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agent_questions (id, run_id, agent_id, project_id, question) VALUES (?, ?, ?, ?, ?)`,
		id, runID, agentID, projectID, "Proceed?")
	require.NoError(t, err)
	return id
}

// TestListQuestionsByProject_ResolvesConversationID pins the conversation link
// on the question list: a question whose run is linked to a chat conversation
// (agent_runs.session_id → chat_conversations.session_id) must come back with
// its ConversationID populated, and a question whose run has no conversation
// must come back nil. It also guards that the status filter and ordering shape
// survive the NewSelect → NewRaw switch.
func TestListQuestionsByProject_ResolvesConversationID(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "list_questions_conversation")
	t.Cleanup(tdb.Close)

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "q-agent")

	// A run with a linked chat conversation: one session shared by the run and
	// the conversation (the join key).
	sessionID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx, `INSERT INTO kb.sessions (id, project_id) VALUES (?, ?)`, sessionID, projectID)
	require.NoError(t, err)
	convID := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.chat_conversations (id, title, project_id, session_id) VALUES (?, ?, ?, ?)`,
		convID, "linked", projectID, sessionID)
	require.NoError(t, err)
	linkedRunID := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_runs (id, agent_id, status, started_at, session_id) VALUES (?, ?, ?, now(), ?)`,
		linkedRunID, agentID, "running", sessionID)
	require.NoError(t, err)
	insertAgentQuestion(t, tdb.DB, ctx, linkedRunID, agentID, projectID)

	// A run with no chat conversation (no session linkage).
	plainRunID := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_runs (id, agent_id, status, started_at) VALUES (?, ?, ?, now())`,
		plainRunID, agentID, "running")
	require.NoError(t, err)
	insertAgentQuestion(t, tdb.DB, ctx, plainRunID, agentID, projectID)

	repo := NewRepository(tdb.DB)
	questions, err := repo.ListQuestionsByProject(ctx, projectID, nil)
	require.NoError(t, err)
	require.Len(t, questions, 2)

	byRun := map[string]*AgentQuestion{}
	for _, q := range questions {
		byRun[q.RunID] = q
	}
	linked, ok := byRun[linkedRunID]
	require.True(t, ok, "linked question must be returned")
	require.NotNil(t, linked.ConversationID, "linked run's question must carry the conversation id")
	require.Equal(t, convID, *linked.ConversationID)

	plain, ok := byRun[plainRunID]
	require.True(t, ok, "plain question must be returned")
	require.Nil(t, plain.ConversationID, "a run with no conversation must resolve nil")

	// Status filter preserved: an answered filter must not match the pending rows.
	answered := QuestionStatusAnswered
	filtered, err := repo.ListQuestionsByProject(ctx, projectID, &answered)
	require.NoError(t, err)
	require.Empty(t, filtered, "no question is answered; the status filter must still apply")
}
