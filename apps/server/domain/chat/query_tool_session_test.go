package chat_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// QueryToolSessionSuite exercises the /query endpoint end to end (in-process
// server, deterministic stub agent): the stateless tool path must reuse one
// conversation, while the run-scoped path must keep the #2266 parent link and
// its per-call conversation.
type QueryToolSessionSuite struct {
	testutil.BaseSuite
}

func TestQueryToolSessionSuite(t *testing.T) {
	suite.Run(t, new(QueryToolSessionSuite))
}

func (s *QueryToolSessionSuite) SetupSuite() {
	s.SetDBSuffix("query_tool_session")
	s.BaseSuite.SetupSuite()
}

// metaConversationID extracts the conversationId from the first `meta` SSE
// event. QueryStream writes it before the agent runs, so it is present even
// when the stub run later emits an error event.
func (s *QueryToolSessionSuite) metaConversationID(resp *testutil.SSEResponse) string {
	s.T().Helper()
	for _, ev := range resp.Events {
		var payload struct {
			Type           string `json:"type"`
			ConversationID string `json:"conversationId"`
		}
		if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
			continue
		}
		if payload.Type == "meta" && payload.ConversationID != "" {
			return payload.ConversationID
		}
	}
	s.T().Fatalf("no meta conversationId in SSE response: %s", resp.RawBody)
	return ""
}

// TestStatelessCallsReuseOneConversation is the regression for #28: two
// consecutive direct (no run context) search-knowledge calls must land in the
// same conversation and leave exactly one row in the rail.
func (s *QueryToolSessionSuite) TestStatelessCallsReuseOneConversation() {
	path := "/api/projects/" + s.ProjectID + "/query"
	body := map[string]any{"message": "who is sam?", "spawn_source": "search-knowledge"}

	first := s.Client.PostSSE(path, testutil.WithAuth("e2e-test-user"), testutil.WithJSONBody(body))
	s.Require().Equal(http.StatusOK, first.StatusCode, first.RawBody)
	firstID := s.metaConversationID(first)

	second := s.Client.PostSSE(path, testutil.WithAuth("e2e-test-user"), testutil.WithJSONBody(body))
	s.Require().Equal(http.StatusOK, second.StatusCode, second.RawBody)
	secondID := s.metaConversationID(second)

	s.Require().Equal(firstID, secondID,
		"two stateless calls from the same caller must reuse one conversation")

	var count int
	err := s.DB().NewRaw(`
		SELECT COUNT(*)
		FROM kb.chat_conversations
		WHERE project_id = ? AND spawn_source = 'search-knowledge'`, s.ProjectID).Scan(s.Ctx, &count)
	s.Require().NoError(err)
	s.Require().Equal(1, count, "N stateless calls must produce one conversation row, not N")
}

// TestRunScopedCallStillLinksParentConversation is the #2266 regression guard:
// a call that carries X-Parent-Run-Id must not be collapsed into the stateless
// tool session. It keeps its own conversation, linked to the run's parent
// conversation, and a second call in the same run still gets its own row.
func (s *QueryToolSessionSuite) TestRunScopedCallStillLinksParentConversation() {
	// Seed the caller run's chain: agent → session → parent conversation → run.
	agentID := uuid.New()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, trigger_type)
		VALUES (?, ?, ?, ?, '0 0 * * *', 'manual')`,
		agentID, s.ProjectID, "seed-"+agentID.String(), "seed:"+agentID.String()).Exec(s.Ctx)
	s.Require().NoError(err)

	sessionID := uuid.New()
	_, err = s.DB().NewRaw(`
		INSERT INTO kb.sessions (id, project_id) VALUES (?, ?)`,
		sessionID, s.ProjectID).Exec(s.Ctx)
	s.Require().NoError(err)

	parentID := uuid.New()
	_, err = s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations
			(id, title, project_id, is_private, owner_user_id, session_id, created_at, updated_at)
		VALUES (?, 'parent conversation', ?, true, ?, ?, now(), now())`,
		parentID, s.ProjectID, testutil.AdminUser.ID, sessionID).Exec(s.Ctx)
	s.Require().NoError(err)

	runID := uuid.New()
	_, err = s.DB().NewRaw(`
		INSERT INTO kb.agent_runs (id, agent_id, status, started_at, session_id)
		VALUES (?, ?, 'completed', now(), ?)`,
		runID, agentID, sessionID).Exec(s.Ctx)
	s.Require().NoError(err)

	path := "/api/projects/" + s.ProjectID + "/query"
	body := map[string]any{"message": "in-run question", "spawn_source": "search-knowledge"}

	post := func() string {
		resp := s.Client.PostSSE(path,
			testutil.WithAuth("e2e-test-user"),
			testutil.WithHeader("X-Parent-Run-Id", runID.String()),
			testutil.WithJSONBody(body),
		)
		s.Require().Equal(http.StatusOK, resp.StatusCode, resp.RawBody)
		return s.metaConversationID(resp)
	}

	firstChild := post()
	s.Require().NotEqual(parentID.String(), firstChild,
		"an in-run call must not reuse the parent or the stateless tool session")

	var firstParent string
	err = s.DB().NewRaw(`
		SELECT COALESCE(parent_conversation_id::text, '')
		FROM kb.chat_conversations WHERE id = ?`, firstChild).Scan(s.Ctx, &firstParent)
	s.Require().NoError(err)
	s.Require().Equal(parentID.String(), firstParent,
		"the in-run conversation must keep the #2266 parent-conversation link")

	secondChild := post()
	s.Require().NotEqual(firstChild, secondChild,
		"in-run behaviour is still one conversation per call")
	var secondParent string
	err = s.DB().NewRaw(`
		SELECT COALESCE(parent_conversation_id::text, '')
		FROM kb.chat_conversations WHERE id = ?`, secondChild).Scan(s.Ctx, &secondParent)
	s.Require().NoError(err)
	s.Require().Equal(parentID.String(), secondParent)
}
