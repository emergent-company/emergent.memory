package chat_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// deviceCeiling is the stored scope set every device credential carries. It is
// repeated here (pkg/auth and domain/apitoken already pin it) so this suite
// exercises the real validation-time ceiling, not a widened test-only set.
var deviceCeiling = []string{"device:api", "agents:read", "data:read"}

// ChatCancelRunSuite covers the device-scoped chat run cancel route (#81):
// scope gate, ownership gate, and the additive run-id meta frame (#67).
//
// The device cases use a real emt_* device credential row (user_id = the
// provisioning user) so the server's device surface guard and exact-ceiling
// validation both run; a device token that could cancel another user's run is
// the regression this suite exists to prevent.
type ChatCancelRunSuite struct {
	testutil.BaseSuite

	// userBID is a second org/project member who owns their own conversations.
	userBID string
}

func TestChatCancelRunSuite(t *testing.T) {
	suite.Run(t, new(ChatCancelRunSuite))
}

func (s *ChatCancelRunSuite) SetupSuite() {
	s.SetDBSuffix("chat_cancel_run")
	s.BaseSuite.SetupSuite()
}

func (s *ChatCancelRunSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	s.userBID = "00000000-0000-0000-0000-00000000c0de"
	s.Require().NoError(testutil.CreateTestUser(s.Ctx, s.DB(), testutil.TestUser{
		ID:            s.userBID,
		ZitadelUserID: "e2e-chat-cancel-user-b",
		Email:         "cancel-userb@test.local",
		FirstName:     "Cancel",
		LastName:      "B",
		Scopes:        []string{"chat:use", "chat:admin"},
	}))
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), s.OrgID, s.userBID, "member"))
}

// seedConversationWithRun inserts a private conversation owned by ownerID and a
// running agent run linked to that conversation's session. It returns the
// conversation and run ids.
func (s *ChatCancelRunSuite) seedConversationWithRun(ownerID, status string) (string, string) {
	agentRepo := agents.NewRepository(s.DB())

	agent := &agents.Agent{
		ProjectID:    s.ProjectID,
		Name:         "cancel-test-" + uuid.NewString(),
		StrategyType: "manual",
		CronSchedule: "0 0 * * *",
		TriggerType:  "manual",
	}
	s.Require().NoError(agentRepo.Create(s.Ctx, agent))

	convID := uuid.NewString()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, created_at, updated_at)
		VALUES (?, ?, ?::uuid, true, ?::uuid, NOW(), NOW())
	`, convID, "cancel-test", s.ProjectID, ownerID).Exec(s.Ctx)
	s.Require().NoError(err)

	agentName := "cancel-session"
	sessionID, err := agentRepo.EnsureConversationSession(s.Ctx, convID, s.ProjectID, &agentName)
	s.Require().NoError(err)

	run, err := agentRepo.CreateRun(s.Ctx, agent.ID)
	s.Require().NoError(err)
	s.Require().NoError(agentRepo.UpdateRunSessionID(s.Ctx, run.ID, sessionID))
	if status != "working" {
		_, err = s.DB().NewRaw(`UPDATE kb.agent_runs SET status = ? WHERE id = ?`, status, run.ID).Exec(s.Ctx)
		s.Require().NoError(err)
	}

	return convID, run.ID
}

func (s *ChatCancelRunSuite) runStatus(runID string) string {
	var status string
	s.Require().NoError(s.DB().NewRaw(
		`SELECT status::text FROM kb.agent_runs WHERE id = ?`, runID,
	).Scan(s.Ctx, &status))
	return status
}

func (s *ChatCancelRunSuite) cancelAs(token, runID string, projectID bool) *testutil.HTTPResponse {
	opts := []testutil.RequestOption{testutil.WithAuth(token)}
	if projectID {
		opts = append(opts, testutil.WithProjectID(s.ProjectID))
	}
	return s.Client.POST("/api/chat/runs/"+runID+"/cancel", opts...)
}

// A device credential may stop a run in its own user's conversation. The
// request carries no X-Project-ID: the project comes from the token binding,
// exactly as the gateway proxies it.
func (s *ChatCancelRunSuite) TestDeviceTokenCancelsOwnConversationRun() {
	deviceToken := "emt_chat_cancel_dev_own"
	s.Require().NoError(testutil.CreateTestAPIToken(
		s.Ctx, s.DB(), testutil.AdminUser.ID, deviceToken, deviceCeiling, s.ProjectID,
	))

	_, runID := s.seedConversationWithRun(testutil.AdminUser.ID, "working")

	resp := s.cancelAs(deviceToken, runID, false)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body=%s", resp.String())

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Cancelled bool   `json:"cancelled"`
			Status    string `json:"status"`
			RunID     string `json:"runId"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.True(body.Success)
	s.True(body.Data.Cancelled)
	s.Equal(runID, body.Data.RunID)
	s.Equal("cancelling", body.Data.Status)
	s.Equal("cancelling", s.runStatus(runID), "run must accept the durable cancel")
}

// The same device credential must NOT be able to cancel a run in another
// member's conversation, even though both are in the same project. It must be
// indistinguishable from an unknown run (404) and must leave the run untouched.
func (s *ChatCancelRunSuite) TestDeviceTokenCannotCancelAnotherUsersRun() {
	deviceToken := "emt_chat_cancel_dev_foreign"
	s.Require().NoError(testutil.CreateTestAPIToken(
		s.Ctx, s.DB(), testutil.AdminUser.ID, deviceToken, deviceCeiling, s.ProjectID,
	))

	_, foreignRunID := s.seedConversationWithRun(s.userBID, "working")

	resp := s.cancelAs(deviceToken, foreignRunID, false)
	s.Equal(http.StatusNotFound, resp.StatusCode, "body=%s", resp.String())
	s.Equal("working", s.runStatus(foreignRunID), "a denied cancel must not stop the run")

	// The owner can still stop it — proving the 404 was ownership, not a broken
	// run id.
	ownerResp := s.cancelAs("e2e-chat-cancel-user-b", foreignRunID, true)
	s.Require().Equal(http.StatusOK, ownerResp.StatusCode, "body=%s", ownerResp.String())
	s.Equal("cancelling", s.runStatus(foreignRunID))
}

// A normal web session (no emt_* token, so RequireAPITokenScopes is skipped)
// still cancels its own run: the new chat route is not device-only.
func (s *ChatCancelRunSuite) TestWebSessionCancelsOwnConversationRun() {
	_, runID := s.seedConversationWithRun(testutil.AdminUser.ID, "working")

	resp := s.cancelAs("e2e-test-user", runID, true)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body=%s", resp.String())
	s.Equal("cancelling", s.runStatus(runID))
}

// A terminal run is not falsely reported as cancelled.
func (s *ChatCancelRunSuite) TestTerminalRunReportsNotCancelled() {
	_, runID := s.seedConversationWithRun(testutil.AdminUser.ID, "completed")

	resp := s.cancelAs("e2e-test-user", runID, true)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body=%s", resp.String())

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Cancelled bool   `json:"cancelled"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.True(body.Success)
	s.False(body.Data.Cancelled)
	s.Equal("completed", body.Data.Status)
	s.Equal("completed", s.runStatus(runID))
}

// --- SSE run-id frame (#67) ---

// postChatStream sends a chat turn and returns the parsed SSE body.
func (s *ChatCancelRunSuite) postChatStream(conversationID string) *httptest.ResponseRecorder {
	payload, err := json.Marshal(map[string]any{
		"conversationId": conversationID,
		"message":        "hello",
	})
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodPost, "/api/chat/stream", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer e2e-test-user")
	req.Header.Set("X-Project-ID", s.ProjectID)

	rec := httptest.NewRecorder()
	s.Server.Echo.ServeHTTP(rec, req)
	return rec
}

func (s *ChatCancelRunSuite) seedConversation(agentDefinitionID *string) string {
	convID := uuid.NewString()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, agent_definition_id, created_at, updated_at)
		VALUES (?, ?, ?::uuid, true, ?::uuid, ?::uuid, NOW(), NOW())
	`, convID, "sse-test", s.ProjectID, testutil.AdminUser.ID, agentDefinitionID).Exec(s.Ctx)
	s.Require().NoError(err)
	return convID
}

// The agent-backed stream emits an additive second meta frame carrying the run
// id once the run exists. The first meta frame keeps its original shape.
func (s *ChatCancelRunSuite) TestAgentBackedStreamCarriesRunID() {
	s.T().Setenv("CHAT_TEST_DETERMINISTIC", "1")

	agentRepo := agents.NewRepository(s.DB())
	def, err := agentRepo.FindDefinitionByName(s.Ctx, s.ProjectID, "graph-query-agent")
	s.Require().NoError(err)
	s.Require().NotNil(def)

	convID := s.seedConversation(&def.ID)
	rec := s.postChatStream(convID)
	s.Require().Equal(http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	events, err := testutil.ParseSSEResponseFromBytes(rec.Body.Bytes())
	s.Require().NoError(err)

	var metas []map[string]any
	for _, ev := range events {
		var data map[string]any
		if err := ev.ParseSSEJSON(&data); err != nil {
			continue
		}
		if data["type"] == "meta" {
			metas = append(metas, data)
		}
	}
	s.Require().GreaterOrEqual(len(metas), 2, "expected the original + run-id meta frames")

	// First frame: conversation only, exactly as before.
	s.Equal(convID, metas[0]["conversationId"])
	_, firstHasRun := metas[0]["runId"]

	// Last frame: same conversation + a non-empty run id.
	runID, ok := metas[len(metas)-1]["runId"].(string)
	s.Require().True(ok, "agent-backed meta must carry runId: %+v", metas)
	s.NotEmpty(runID)
	s.Equal(convID, metas[len(metas)-1]["conversationId"])

	if !firstHasRun {
		return
	}
	// If a future change also stamps the first frame, it must still be the same
	// conversation/run pair; either ordering keeps the contract additive.
	s.Equal(runID, metas[0]["runId"])
}

// TestAgentBackedStreamPersistsAssistantMessageBeforeReturn guards the fix for
// issue #302: the assistant turn must be written through the request's DB
// handle from the request goroutine, not from a fire-and-forget goroutine.
//
// Under BaseSuite that handle is the per-test transaction — a single
// connection. A background writer racing the handler's citation query on that
// connection made pgx's stdlib driver reset the shared result reader, and the
// citation query's Rows.Next panicked with "index out of range [0] with length
// 0". The trigger below stretches the assistant INSERT past the end of the
// stream, so a fire-and-forget writer is guaranteed to still be in flight when
// the HTTP response returns; the assertion then fails deterministically (and
// the sanity check on elapsed time proves the trigger actually ran).
func (s *ChatCancelRunSuite) TestAgentBackedStreamPersistsAssistantMessageBeforeReturn() {
	s.T().Setenv("CHAT_TEST_DETERMINISTIC", "1")

	const assistantInsertDelay = 400 * time.Millisecond

	// DDL runs inside the test transaction, so it is rolled back with the test.
	_, err := s.DB().NewRaw(fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION public.test_slow_assistant_insert() RETURNS trigger
		LANGUAGE plpgsql AS $fn$
		BEGIN
			PERFORM pg_sleep(%g);
			RETURN NEW;
		END
		$fn$
	`, assistantInsertDelay.Seconds())).Exec(s.Ctx)
	s.Require().NoError(err)
	_, err = s.DB().NewRaw(`
		CREATE TRIGGER test_slow_assistant_insert
		BEFORE INSERT ON kb.chat_messages
		FOR EACH ROW WHEN (NEW.role = 'assistant')
		EXECUTE FUNCTION public.test_slow_assistant_insert()
	`).Exec(s.Ctx)
	s.Require().NoError(err)

	agentRepo := agents.NewRepository(s.DB())
	def, err := agentRepo.FindDefinitionByName(s.Ctx, s.ProjectID, "graph-query-agent")
	s.Require().NoError(err)
	s.Require().NotNil(def)

	convID := s.seedConversation(&def.ID)

	start := time.Now()
	rec := s.postChatStream(convID)
	elapsed := time.Since(start)
	s.Require().Equal(http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	// The assistant turn must already be committed to the request's own DB
	// handle by the time the stream returns.
	var stored int
	s.Require().NoError(s.DB().NewRaw(
		`SELECT count(*) FROM kb.chat_messages WHERE conversation_id = ?::uuid AND role = 'assistant'`,
		convID,
	).Scan(s.Ctx, &stored))
	s.Equal(1, stored, "assistant message must be persisted before the stream returns")

	s.GreaterOrEqual(elapsed, assistantInsertDelay,
		"the assistant INSERT did not go through the slow trigger; the ordering assertion above would be vacuous")
}

// The direct-LLM path's meta frame is unchanged: still exactly one meta and no
// runId field at all.
func (s *ChatCancelRunSuite) TestDirectLLMStreamMetaUnchanged() {
	s.T().Setenv("CHAT_TEST_DETERMINISTIC", "1")

	convID := s.seedConversation(nil)
	rec := s.postChatStream(convID)
	s.Require().Equal(http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	events, err := testutil.ParseSSEResponseFromBytes(rec.Body.Bytes())
	s.Require().NoError(err)

	var metas []map[string]any
	for _, ev := range events {
		var data map[string]any
		if err := ev.ParseSSEJSON(&data); err != nil {
			continue
		}
		if data["type"] == "meta" {
			metas = append(metas, data)
		}
	}
	s.Require().Len(metas, 1, "direct-LLM stream must still emit exactly one meta frame")
	s.Equal(convID, metas[0]["conversationId"])
	_, hasRun := metas[0]["runId"]
	s.False(hasRun, "direct-LLM meta must not gain a runId: %+v", metas[0])
}
