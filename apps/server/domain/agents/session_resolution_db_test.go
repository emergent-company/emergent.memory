package agents

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestEnsureSessionForContext_LazilyCreatesThenReuses is the A2A context
// continuity regression: an empty contextId lazily creates a Session, a
// supplied contextId resolves to the existing Session (so successive A2A turns
// share one thread), and an unknown contextId resolves to nil so the handler
// can reject it as a validation error.
func TestEnsureSessionForContext_LazilyCreatesThenReuses(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "ensure_session_for_context")
	t.Cleanup(tdb.Close)
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)
	agentName := "ctx-agent"

	// No contextId -> lazily create a fresh Session.
	created, err := repo.EnsureSessionForContext(ctx, projectID, "", &agentName)
	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotEmpty(t, created.ID)
	// The RETURNING column list must actually populate the struct: a bare
	// `Returning("id", ...)` (separate args) returns only `id` and leaves the
	// rest zero, silently dropping created_at/updated_at.
	require.Equal(t, projectID, created.ProjectID)
	require.NotNil(t, created.AgentName)
	require.Equal(t, agentName, *created.AgentName)
	require.False(t, created.CreatedAt.IsZero(), "created_at must be returned, not left zero")
	require.False(t, created.UpdatedAt.IsZero(), "updated_at must be returned, not left zero")

	// Supplied contextId -> reuse the same Session, no duplicate row.
	reused, err := repo.EnsureSessionForContext(ctx, projectID, created.ID, &agentName)
	require.NoError(t, err)
	require.NotNil(t, reused)
	require.Equal(t, created.ID, reused.ID, "a supplied contextId must resolve to the existing Session")

	var count int
	require.NoError(t, tdb.DB.NewRaw(`SELECT count(*) FROM kb.sessions WHERE project_id = ?`, projectID).Scan(ctx, &count))
	require.Equal(t, 1, count, "reuse must not create a second Session row")

	// Unknown contextId -> nil, nil (caller maps to a validation error).
	unknown, err := repo.EnsureSessionForContext(ctx, projectID, uuid.NewString(), &agentName)
	require.NoError(t, err)
	require.Nil(t, unknown, "an unknown contextId must not create a Session")
}

// TestEnsureConversationSession_AtomicCreateAndReuse proves the chat thread
// resolution is create-once, reuse-after: the first turn creates the Session and
// persists the conversation backlink, and later turns return the same id without
// inserting another row.
func TestEnsureConversationSession_AtomicCreateAndReuse(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "ensure_conv_session_reuse")
	t.Cleanup(tdb.Close)
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	convID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx, `INSERT INTO kb.chat_conversations (id, title, project_id) VALUES (?, ?, ?)`, convID, "conv", projectID)
	require.NoError(t, err)

	repo := NewRepository(tdb.DB)
	agentName := "chat-agent"

	first, err := repo.EnsureConversationSession(ctx, convID, projectID, &agentName)
	require.NoError(t, err)
	require.NotEmpty(t, first)

	second, err := repo.EnsureConversationSession(ctx, convID, projectID, &agentName)
	require.NoError(t, err)
	require.Equal(t, first, second, "repeat resolution must reuse the same Session")

	var linked string
	require.NoError(t, tdb.DB.NewRaw(`SELECT session_id::text FROM kb.chat_conversations WHERE id = ?`, convID).Scan(ctx, &linked))
	require.Equal(t, first, linked, "the backlink must be persisted on the conversation row")

	var count int
	require.NoError(t, tdb.DB.NewRaw(`SELECT count(*) FROM kb.sessions WHERE project_id = ?`, projectID).Scan(ctx, &count))
	require.Equal(t, 1, count)
}

// TestEnsureConversationSession_ConcurrentFirstTurnsShareOneSession is the
// serialization regression: concurrent first turns on the same conversation must
// all resolve to a single Session. Before the row lock, each racer could read a
// NULL backlink and create its own Session, leaving the thread with duplicate
// Session rows.
func TestEnsureConversationSession_ConcurrentFirstTurnsShareOneSession(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "ensure_conv_session_race")
	t.Cleanup(tdb.Close)
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	convID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx, `INSERT INTO kb.chat_conversations (id, title, project_id) VALUES (?, ?, ?)`, convID, "conv-race", projectID)
	require.NoError(t, err)

	repo := NewRepository(tdb.DB)
	agentName := "chat-agent"

	const racers = 8
	ids := make([]string, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = repo.EnsureConversationSession(ctx, convID, projectID, &agentName)
		}(i)
	}
	wg.Wait()

	for i := 0; i < racers; i++ {
		require.NoErrorf(t, errs[i], "racer %d failed", i)
		require.Equal(t, ids[0], ids[i], "all concurrent first turns must resolve to one Session")
	}

	var count int
	require.NoError(t, tdb.DB.NewRaw(`SELECT count(*) FROM kb.sessions WHERE project_id = ?`, projectID).Scan(ctx, &count))
	require.Equal(t, 1, count, "concurrent first turns must not create duplicate Session rows")
}

// TestCreateSession_ReturnsPopulatedFields pins the CreateSession RETURNING
// column list: every mapped column (id, project_id, agent_name, title,
// created_at, updated_at) must come back on the struct. If the list regresses to
// separate arguments — bun's signature is Returning(query string, args ...any) —
// only the first column is returned and created_at/updated_at stay zero.
func TestCreateSession_ReturnsPopulatedFields(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "create_session_fields")
	t.Cleanup(tdb.Close)
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	repo := NewRepository(tdb.DB)

	agentName := "populated-agent"
	title := "a session title"
	session := &Session{ProjectID: projectID, AgentName: &agentName, Title: &title}
	require.NoError(t, repo.CreateSession(ctx, session))

	require.NotEmpty(t, session.ID, "id must be returned")
	require.Equal(t, projectID, session.ProjectID)
	require.NotNil(t, session.AgentName)
	require.Equal(t, agentName, *session.AgentName)
	require.NotNil(t, session.Title)
	require.Equal(t, title, *session.Title)
	require.False(t, session.CreatedAt.IsZero(), "created_at must be returned")
	require.False(t, session.UpdatedAt.IsZero(), "updated_at must be returned")
}
