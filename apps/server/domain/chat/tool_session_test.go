package chat_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// countToolSessions counts the conversation rows a tool session owns for a
// project. This is the row-count assertion the dedupe must satisfy: N stateless
// calls produce 1 row, not N.
func (e *repoEnv) countToolSessions(tool string) int {
	e.t.Helper()
	var n int
	err := e.db.DB.NewRaw(`
		SELECT COUNT(*)
		FROM kb.chat_conversations
		WHERE project_id = ? AND spawn_source = ?`, e.projectID, tool).Scan(e.ctx, &n)
	require.NoError(e.t, err)
	return n
}

// TestToolSessionReusesOneConversationPerCaller covers the core regression:
// two consecutive stateless calls from the same caller must resolve to the same
// conversation, and the second call must not insert a row.
func TestToolSessionReusesOneConversationPerCaller(t *testing.T) {
	e := newRepoEnv(t)

	first, created, err := e.svc.GetOrCreateToolSession(
		e.ctx, e.projectID, e.ownerID, e.ownerID, "search-knowledge", "first question")
	require.NoError(t, err)
	require.True(t, created, "the first stateless call creates the tool session")
	require.Equal(t, "Tool session: search-knowledge", first.Title)
	require.NotNil(t, first.SpawnSource)
	require.Equal(t, "search-knowledge", *first.SpawnSource)

	second, created, err := e.svc.GetOrCreateToolSession(
		e.ctx, e.projectID, e.ownerID, e.ownerID, "search-knowledge", "second question")
	require.NoError(t, err)
	require.False(t, created, "the second stateless call must reuse the existing tool session")
	require.Equal(t, first.ID, second.ID, "same caller + tool must resolve the same conversation")

	require.Equal(t, 1, e.countToolSessions("search-knowledge"),
		"two stateless calls must leave exactly one conversation row")
}

// TestToolSessionSeparatesCallersAndTools covers the key dimensions: a
// different caller or a different tool kind gets its own conversation.
func TestToolSessionSeparatesCallersAndTools(t *testing.T) {
	e := newRepoEnv(t)

	owner, created, err := e.svc.GetOrCreateToolSession(
		e.ctx, e.projectID, e.ownerID, e.ownerID, "search-knowledge", "owner question")
	require.NoError(t, err)
	require.True(t, created)

	foreign, created, err := e.svc.GetOrCreateToolSession(
		e.ctx, e.projectID, e.foreignID, e.foreignID, "search-knowledge", "foreign question")
	require.NoError(t, err)
	require.True(t, created, "a different caller needs its own conversation")
	require.NotEqual(t, owner.ID, foreign.ID)

	otherTool, created, err := e.svc.GetOrCreateToolSession(
		e.ctx, e.projectID, e.ownerID, e.ownerID, "some-other-tool", "owner question")
	require.NoError(t, err)
	require.True(t, created, "a different tool kind needs its own conversation")
	require.NotEqual(t, owner.ID, otherTool.ID)

	require.Equal(t, 2, e.countToolSessions("search-knowledge"),
		"one search-knowledge row per caller, plus one for the other tool")
}

// TestToolSessionOwnerlessCredentialsStayDistinct covers the fallback identity:
// an ownerless credential has no user id, so the API token id is the key.
// Distinct machine callers must not be collapsed into one conversation, and the
// conversation stays project-shared/unowned because it has no owner.
func TestToolSessionOwnerlessCredentialsStayDistinct(t *testing.T) {
	e := newRepoEnv(t)

	firstToken := uuid.New().String()
	secondToken := uuid.New().String()

	first, created, err := e.svc.GetOrCreateToolSession(
		e.ctx, e.projectID, "", firstToken, "search-knowledge", "machine question")
	require.NoError(t, err)
	require.True(t, created)
	require.Nil(t, first.OwnerUserID, "an ownerless credential creates an unowned conversation")
	require.False(t, first.IsPrivate, "an unowned conversation must be project-shared")

	// Same token: reused.
	same, created, err := e.svc.GetOrCreateToolSession(
		e.ctx, e.projectID, "", firstToken, "search-knowledge", "again")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, same.ID)

	// Different ownerless token: a different deterministic id.
	other, created, err := e.svc.GetOrCreateToolSession(
		e.ctx, e.projectID, "", secondToken, "search-knowledge", "other machine")
	require.NoError(t, err)
	require.True(t, created)
	require.NotEqual(t, first.ID, other.ID)

	require.Equal(t, 2, e.countToolSessions("search-knowledge"))
}

// TestToolSessionConcurrentFirstCallsCreateOneRow is the race guard: concurrent
// first calls for the same key must all resolve to one row. The deterministic
// UUIDv5 id and the ON CONFLICT DO NOTHING upsert are what make this hold.
func TestToolSessionConcurrentFirstCallsCreateOneRow(t *testing.T) {
	e := newRepoEnv(t)

	const callers = 8
	start := make(chan struct{})
	ids := make([]uuid.UUID, callers)
	errs := make([]error, callers)

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			conv, _, err := e.svc.GetOrCreateToolSession(
				e.ctx, e.projectID, e.ownerID, e.ownerID, "search-knowledge", "concurrent question")
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = conv.ID
		}(i)
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "concurrent caller %d", i)
	}
	for i := 1; i < callers; i++ {
		require.Equal(t, ids[0], ids[i], "concurrent caller %d must resolve the winning conversation", i)
	}
	require.Equal(t, 1, e.countToolSessions("search-knowledge"),
		"concurrent first calls must leave exactly one conversation row")
}
