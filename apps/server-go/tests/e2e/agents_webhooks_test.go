package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/domain/agents"
	"github.com/emergent-company/emergent/internal/testutil"
)

// ==================== Webhook Hooks E2E Suite ====================

type AgentsWebhooksSuite struct {
	testutil.BaseSuite
}

func (s *AgentsWebhooksSuite) SetupSuite() {
	s.SetDBSuffix("agents_webhooks")
	s.BaseSuite.SetupSuite()
}

// createTestAgent creates an agent and returns its ID
func (s *AgentsWebhooksSuite) createTestAgent(name string) string {
	rec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         name,
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
			"triggerType":  "webhook",
		}),
	)
	s.Require().Equal(http.StatusCreated, rec.StatusCode, "failed to create test agent")

	var response agents.APIResponse[*agents.AgentDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)
	s.Require().True(response.Success)

	return response.Data.ID
}

// createTestHook creates a webhook hook for an agent and returns (hookID, plaintext token)
func (s *AgentsWebhooksSuite) createTestHook(agentID, label string) (string, string) {
	rec := s.Client.POST(
		fmt.Sprintf("/api/admin/agents/%s/hooks", agentID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"label": label,
		}),
	)
	s.Require().Equal(http.StatusCreated, rec.StatusCode, "failed to create test hook")

	var response agents.APIResponse[*agents.AgentWebhookHookDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)
	s.Require().True(response.Success)
	s.Require().NotNil(response.Data.Token, "token should be returned on creation")

	return response.Data.ID, *response.Data.Token
}

// ==================== 7.1: Webhook Hook CRUD Tests ====================

func (s *AgentsWebhooksSuite) TestCreateWebhookHook_Success() {
	agentID := s.createTestAgent("Webhook Agent Create " + uuid.New().String()[:8])

	rec := s.Client.POST(
		fmt.Sprintf("/api/admin/agents/%s/hooks", agentID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"label": "My CI Hook",
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentWebhookHookDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.NotEmpty(response.Data.ID)
	s.Equal(agentID, response.Data.AgentID)
	s.Equal(s.ProjectID, response.Data.ProjectID)
	s.Equal("My CI Hook", response.Data.Label)
	s.True(response.Data.Enabled)

	// Token must be returned exactly once on creation
	s.NotNil(response.Data.Token, "token should be returned on creation")
	s.Contains(*response.Data.Token, "whk_", "token should have whk_ prefix")
	s.GreaterOrEqual(len(*response.Data.Token), 20, "token should be a substantial random string")
}

func (s *AgentsWebhooksSuite) TestCreateWebhookHook_RequiresAuth() {
	agentID := s.createTestAgent("Webhook Agent Auth " + uuid.New().String()[:8])

	rec := s.Client.POST(
		fmt.Sprintf("/api/admin/agents/%s/hooks", agentID),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"label": "Unauthenticated Hook",
		}),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsWebhooksSuite) TestCreateWebhookHook_MissingLabel() {
	agentID := s.createTestAgent("Webhook Agent NoLabel " + uuid.New().String()[:8])

	rec := s.Client.POST(
		fmt.Sprintf("/api/admin/agents/%s/hooks", agentID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *AgentsWebhooksSuite) TestCreateWebhookHook_NonExistentAgent() {
	fakeID := uuid.New().String()

	rec := s.Client.POST(
		fmt.Sprintf("/api/admin/agents/%s/hooks", fakeID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"label": "Orphan Hook",
		}),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *AgentsWebhooksSuite) TestCreateWebhookHook_WithRateLimitConfig() {
	agentID := s.createTestAgent("Webhook Agent RateLimit " + uuid.New().String()[:8])

	rec := s.Client.POST(
		fmt.Sprintf("/api/admin/agents/%s/hooks", agentID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"label": "Rate Limited Hook",
			"rateLimitConfig": map[string]any{
				"requestsPerMinute": 30,
				"burstSize":         5,
			},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentWebhookHookDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)
	s.True(response.Success)
	s.NotNil(response.Data.RateLimitConfig)
	s.Equal(30, response.Data.RateLimitConfig.RequestsPerMinute)
	s.Equal(5, response.Data.RateLimitConfig.BurstSize)
}

func (s *AgentsWebhooksSuite) TestListWebhookHooks_Success() {
	agentID := s.createTestAgent("Webhook Agent List " + uuid.New().String()[:8])

	// Create two hooks
	s.createTestHook(agentID, "Hook One")
	s.createTestHook(agentID, "Hook Two")

	rec := s.Client.GET(
		fmt.Sprintf("/api/admin/agents/%s/hooks", agentID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]*agents.AgentWebhookHookDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)
	s.True(response.Success)
	s.Len(response.Data, 2)

	// Token should NOT be returned in list responses
	for _, hook := range response.Data {
		s.Nil(hook.Token, "token should not be returned in list responses")
		s.NotEmpty(hook.Label)
		s.Equal(agentID, hook.AgentID)
	}
}

func (s *AgentsWebhooksSuite) TestListWebhookHooks_Empty() {
	agentID := s.createTestAgent("Webhook Agent Empty " + uuid.New().String()[:8])

	rec := s.Client.GET(
		fmt.Sprintf("/api/admin/agents/%s/hooks", agentID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]*agents.AgentWebhookHookDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)
	s.True(response.Success)
	s.Empty(response.Data)
}

func (s *AgentsWebhooksSuite) TestListWebhookHooks_RequiresAuth() {
	agentID := s.createTestAgent("Webhook Agent ListAuth " + uuid.New().String()[:8])

	rec := s.Client.GET(
		fmt.Sprintf("/api/admin/agents/%s/hooks", agentID),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsWebhooksSuite) TestDeleteWebhookHook_Success() {
	agentID := s.createTestAgent("Webhook Agent Delete " + uuid.New().String()[:8])
	hookID, _ := s.createTestHook(agentID, "Hook To Delete")

	rec := s.Client.DELETE(
		fmt.Sprintf("/api/admin/agents/%s/hooks/%s", agentID, hookID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	// Verify it's gone
	listRec := s.Client.GET(
		fmt.Sprintf("/api/admin/agents/%s/hooks", agentID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	var listResponse agents.APIResponse[[]*agents.AgentWebhookHookDTO]
	err := json.Unmarshal(listRec.Body, &listResponse)
	s.Require().NoError(err)
	s.True(listResponse.Success)
	s.Empty(listResponse.Data)
}

func (s *AgentsWebhooksSuite) TestDeleteWebhookHook_RequiresAuth() {
	agentID := s.createTestAgent("Webhook Agent DelAuth " + uuid.New().String()[:8])
	hookID, _ := s.createTestHook(agentID, "Hook To Not Delete")

	rec := s.Client.DELETE(
		fmt.Sprintf("/api/admin/agents/%s/hooks/%s", agentID, hookID),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsWebhooksSuite) TestDeleteWebhookHook_NonExistentAgent() {
	fakeAgentID := uuid.New().String()
	fakeHookID := uuid.New().String()

	rec := s.Client.DELETE(
		fmt.Sprintf("/api/admin/agents/%s/hooks/%s", fakeAgentID, fakeHookID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

// ==================== 7.2: Public Webhook Receiver Tests ====================

func (s *AgentsWebhooksSuite) TestReceiveWebhook_Success() {
	agentID := s.createTestAgent("Webhook Receiver Agent " + uuid.New().String()[:8])
	hookID, token := s.createTestHook(agentID, "Receiver Hook")

	rec := s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", hookID),
		testutil.WithRawAuth("Bearer "+token),
		testutil.WithJSONBody(map[string]any{}),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.TriggerResponseDTO
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)
	s.True(response.Success)
	s.NotNil(response.RunID, "should return a run ID")
}

func (s *AgentsWebhooksSuite) TestReceiveWebhook_WithPayload() {
	agentID := s.createTestAgent("Webhook Payload Agent " + uuid.New().String()[:8])
	hookID, token := s.createTestHook(agentID, "Payload Hook")

	rec := s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", hookID),
		testutil.WithRawAuth("Bearer "+token),
		testutil.WithJSONBody(map[string]any{
			"prompt": "Run extraction on new data",
			"context": map[string]any{
				"source": "CI/CD pipeline",
				"commit": "abc123",
			},
		}),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.TriggerResponseDTO
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)
	s.True(response.Success)
	s.NotNil(response.RunID)
}

func (s *AgentsWebhooksSuite) TestReceiveWebhook_InvalidToken() {
	agentID := s.createTestAgent("Webhook Invalid Token Agent " + uuid.New().String()[:8])
	hookID, _ := s.createTestHook(agentID, "Invalid Token Hook")

	rec := s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", hookID),
		testutil.WithRawAuth("Bearer whk_definitely_not_a_valid_token_here"),
		testutil.WithJSONBody(map[string]any{}),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsWebhooksSuite) TestReceiveWebhook_MissingAuth() {
	agentID := s.createTestAgent("Webhook NoAuth Agent " + uuid.New().String()[:8])
	hookID, _ := s.createTestHook(agentID, "NoAuth Hook")

	rec := s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", hookID),
		testutil.WithJSONBody(map[string]any{}),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsWebhooksSuite) TestReceiveWebhook_NonExistentHook() {
	fakeHookID := uuid.New().String()

	rec := s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", fakeHookID),
		testutil.WithRawAuth("Bearer whk_some_token_that_wont_match"),
		testutil.WithJSONBody(map[string]any{}),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsWebhooksSuite) TestReceiveWebhook_TokenNotReusableAfterDelete() {
	agentID := s.createTestAgent("Webhook Deleted Hook Agent " + uuid.New().String()[:8])
	hookID, token := s.createTestHook(agentID, "Temporary Hook")

	// Verify token works initially
	rec := s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", hookID),
		testutil.WithRawAuth("Bearer "+token),
		testutil.WithJSONBody(map[string]any{}),
	)
	s.Equal(http.StatusOK, rec.StatusCode)

	// Delete the hook
	delRec := s.Client.DELETE(
		fmt.Sprintf("/api/admin/agents/%s/hooks/%s", agentID, hookID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, delRec.StatusCode)

	// Verify token no longer works
	rec = s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", hookID),
		testutil.WithRawAuth("Bearer "+token),
		testutil.WithJSONBody(map[string]any{}),
	)
	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsWebhooksSuite) TestReceiveWebhook_MultipleHooksIndependent() {
	agentID := s.createTestAgent("Webhook Multi-Hook Agent " + uuid.New().String()[:8])
	hookID1, token1 := s.createTestHook(agentID, "Hook Alpha")
	hookID2, token2 := s.createTestHook(agentID, "Hook Beta")

	// Both hooks should work with their own tokens
	rec1 := s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", hookID1),
		testutil.WithRawAuth("Bearer "+token1),
		testutil.WithJSONBody(map[string]any{}),
	)
	s.Equal(http.StatusOK, rec1.StatusCode)

	rec2 := s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", hookID2),
		testutil.WithRawAuth("Bearer "+token2),
		testutil.WithJSONBody(map[string]any{}),
	)
	s.Equal(http.StatusOK, rec2.StatusCode)

	// Cross-using tokens should fail
	recCross := s.Client.POST(
		fmt.Sprintf("/api/webhooks/agents/%s", hookID1),
		testutil.WithRawAuth("Bearer "+token2),
		testutil.WithJSONBody(map[string]any{}),
	)
	s.Equal(http.StatusUnauthorized, recCross.StatusCode)
}

// ==================== Suite Runner ====================

func TestAgentsWebhooksSuite(t *testing.T) {
	suite.Run(t, new(AgentsWebhooksSuite))
}
