package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/domain/agents"
	"github.com/emergent/emergent-core/internal/testutil"
)

// AgentsVisibilityTestSuite tests agent execution visibility and control features
type AgentsVisibilityTestSuite struct {
	testutil.BaseSuite
}

func TestAgentsVisibilitySuite(t *testing.T) {
	suite.Run(t, new(AgentsVisibilityTestSuite))
}

func (s *AgentsVisibilityTestSuite) SetupSuite() {
	s.SetDBSuffix("agents_visibility")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Test: Cancel Running Agent Run
// =============================================================================

func (s *AgentsVisibilityTestSuite) TestCancelRun_Success() {
	// Create an agent definition first
	agentDefPayload := map[string]any{
		"name":           "Test Agent for Cancel",
		"systemPrompt":   "You are a test agent",
		"tools":          []string{},
		"flowType":       "single",
		"maxSteps":       100,
		"defaultTimeout": 600,
	}

	defResp := s.Client.POST("/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(agentDefPayload),
	)
	s.Equal(http.StatusCreated, defResp.StatusCode)

	var defBody agents.APIResponse[*agents.AgentDefinitionDTO]
	err := json.Unmarshal(defResp.Body, &defBody)
	s.NoError(err)
	s.True(defBody.Success)

	defID := defBody.Data.ID

	// Create an agent using this definition
	agentPayload := map[string]any{
		"projectId":    s.ProjectID,
		"name":         "Test Agent for Cancel",
		"strategyType": defID,
		"cronSchedule": "0 0 * * *",
		"enabled":      true,
		"triggerType":  "manual",
	}

	agentResp := s.Client.POST("/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(agentPayload),
	)
	s.Equal(http.StatusCreated, agentResp.StatusCode)

	var agentBody agents.APIResponse[*agents.AgentDTO]
	err = json.Unmarshal(agentResp.Body, &agentBody)
	s.NoError(err)
	s.True(agentBody.Success)

	agentID := agentBody.Data.ID

	// Create a run manually in the database (simulating a running agent)
	runID := s.CreateTestRun(agentID, "running")

	// Cancel the run
	cancelResp := s.Client.POST(
		fmt.Sprintf("/api/admin/agents/%s/runs/%s/cancel", agentID, runID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, cancelResp.StatusCode)

	var cancelBody agents.APIResponse[map[string]string]
	err = json.Unmarshal(cancelResp.Body, &cancelBody)
	s.NoError(err)
	s.True(cancelBody.Success)

	// Verify run is cancelled
	runResp := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s", s.ProjectID, runID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, runResp.StatusCode)

	var runBody agents.APIResponse[*agents.AgentRunDTO]
	err = json.Unmarshal(runResp.Body, &runBody)
	s.NoError(err)

	s.Equal(agents.RunStatusCancelled, runBody.Data.Status)
}

func (s *AgentsVisibilityTestSuite) TestCancelRun_NotFound() {
	// Try to cancel non-existent run with valid UUIDs (should return 404)
	fakeAgentID := uuid.New().String()
	fakeRunID := uuid.New().String()

	cancelResp := s.Client.POST(
		fmt.Sprintf("/api/admin/agents/%s/runs/%s/cancel", fakeAgentID, fakeRunID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, cancelResp.StatusCode)
}

// =============================================================================
// Test: List Project Runs with Filtering
// =============================================================================

func (s *AgentsVisibilityTestSuite) TestListProjectRuns_WithFiltering() {
	// Create agent
	agentDefID := s.CreateTestAgentDefinition("Test Agent for Listing")
	agentID := s.CreateTestAgent(agentDefID, "Test Agent for Listing")

	// Create multiple runs with different statuses
	runningID := s.CreateTestRun(agentID, "running")
	_ = s.CreateTestRun(agentID, "success")
	_ = s.CreateTestRun(agentID, "error")
	_ = s.CreateTestRun(agentID, "paused")

	// Test: List all runs
	allResp := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, allResp.StatusCode)

	var allBody agents.APIResponse[agents.PaginatedResponse[*agents.AgentRunDTO]]
	err := json.Unmarshal(allResp.Body, &allBody)
	s.NoError(err)

	s.GreaterOrEqual(len(allBody.Data.Items), 4, "Should have at least 4 runs")

	// Test: Filter by status=running
	runningResp := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs?status=running", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, runningResp.StatusCode)

	var runningBody agents.APIResponse[agents.PaginatedResponse[*agents.AgentRunDTO]]
	err = json.Unmarshal(runningResp.Body, &runningBody)
	s.NoError(err)

	// Verify all returned items have status "running"
	foundRunningID := false
	for _, run := range runningBody.Data.Items {
		s.Equal(agents.RunStatusRunning, run.Status)
		if run.ID == runningID {
			foundRunningID = true
		}
	}
	s.True(foundRunningID, "Should find our running run")

	// Test: Filter by agentId
	agentFilterResp := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs?agentId=%s", s.ProjectID, agentID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, agentFilterResp.StatusCode)

	var agentFilterBody agents.APIResponse[agents.PaginatedResponse[*agents.AgentRunDTO]]
	err = json.Unmarshal(agentFilterResp.Body, &agentFilterBody)
	s.NoError(err)

	// All runs should belong to our agent
	for _, run := range agentFilterBody.Data.Items {
		s.Equal(agentID, run.AgentID)
	}

	// Test: Pagination
	paginatedResp := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs?limit=2&offset=0", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, paginatedResp.StatusCode)

	var paginatedBody agents.APIResponse[agents.PaginatedResponse[*agents.AgentRunDTO]]
	err = json.Unmarshal(paginatedResp.Body, &paginatedBody)
	s.NoError(err)

	s.Equal(2, paginatedBody.Data.Limit)
	s.Equal(0, paginatedBody.Data.Offset)
}

func (s *AgentsVisibilityTestSuite) TestListProjectRuns_VerifyMetrics() {
	// Create agent and run
	agentDefID := s.CreateTestAgentDefinition("Test Agent for Metrics")
	agentID := s.CreateTestAgent(agentDefID, "Test Agent for Metrics")
	runID := s.CreateTestRunWithMetrics(agentID, "success", 42, 100)

	// Get the run
	resp := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s", s.ProjectID, runID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var body agents.APIResponse[*agents.AgentRunDTO]
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	// Verify metrics are included
	s.Equal(42, body.Data.StepCount)
	s.NotNil(body.Data.MaxSteps)
	s.Equal(100, *body.Data.MaxSteps)
	s.Equal(agents.RunStatusSuccess, body.Data.Status)
}

// =============================================================================
// Test: Get Run Messages
// =============================================================================

func (s *AgentsVisibilityTestSuite) TestGetRunMessages_Success() {
	// Create agent and run
	agentDefID := s.CreateTestAgentDefinition("Test Agent for Messages")
	agentID := s.CreateTestAgent(agentDefID, "Test Agent for Messages")
	runID := s.CreateTestRun(agentID, "success")

	// Create test messages
	s.CreateTestMessage(runID, "user", "Hello agent", 0)
	s.CreateTestMessage(runID, "assistant", "Hello user", 1)
	s.CreateTestMessage(runID, "user", "Do something", 2)
	s.CreateTestMessage(runID, "assistant", "Done", 3)

	// Get messages
	resp := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/messages", s.ProjectID, runID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var body agents.APIResponse[[]*agents.AgentRunMessageDTO]
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	messages := body.Data
	s.Equal(4, len(messages))

	// Verify message order and content
	s.Equal("user", messages[0].Role)
	s.Equal(0, messages[0].StepNumber)

	s.Equal("assistant", messages[1].Role)
	s.Equal(1, messages[1].StepNumber)
}

// =============================================================================
// Test: Get Run Tool Calls
// =============================================================================

func (s *AgentsVisibilityTestSuite) TestGetRunToolCalls_Success() {
	// Create agent and run
	agentDefID := s.CreateTestAgentDefinition("Test Agent for Tool Calls")
	agentID := s.CreateTestAgent(agentDefID, "Test Agent for Tool Calls")
	runID := s.CreateTestRun(agentID, "success")

	// Create test tool calls
	s.CreateTestToolCall(runID, "list_files", map[string]any{"path": "/root"}, map[string]any{"files": []string{"a.txt", "b.txt"}}, "completed", 150, 1)
	s.CreateTestToolCall(runID, "read_file", map[string]any{"path": "a.txt"}, map[string]any{"content": "hello"}, "completed", 50, 2)
	s.CreateTestToolCall(runID, "bad_tool", map[string]any{}, map[string]any{"error": "not found"}, "error", 10, 3)

	// Get tool calls
	resp := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/tool-calls", s.ProjectID, runID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var body agents.APIResponse[[]*agents.AgentRunToolCallDTO]
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	toolCalls := body.Data
	s.Equal(3, len(toolCalls))

	// Verify first tool call
	s.Equal("list_files", toolCalls[0].ToolName)
	s.Equal("completed", toolCalls[0].Status)
	s.NotNil(toolCalls[0].DurationMs)
	s.Equal(150, *toolCalls[0].DurationMs)
	s.Equal(1, toolCalls[0].StepNumber)

	// Verify error tool call
	s.Equal("bad_tool", toolCalls[2].ToolName)
	s.Equal("error", toolCalls[2].Status)
}

// =============================================================================
// Helper Methods
// =============================================================================

func (s *AgentsVisibilityTestSuite) CreateTestAgentDefinition(name string) string {
	payload := map[string]any{
		"name":           name,
		"systemPrompt":   "You are a test agent",
		"tools":          []string{},
		"flowType":       "single",
		"maxSteps":       100,
		"defaultTimeout": 600,
	}

	resp := s.Client.POST("/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(payload),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	var body agents.APIResponse[*agents.AgentDefinitionDTO]
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)
	s.Require().True(body.Success)

	return body.Data.ID
}

func (s *AgentsVisibilityTestSuite) CreateTestAgent(defID, name string) string {
	payload := map[string]any{
		"projectId":    s.ProjectID,
		"name":         name,
		"strategyType": defID,
		"cronSchedule": "0 0 * * *",
		"enabled":      true,
		"triggerType":  "manual",
	}

	resp := s.Client.POST("/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(payload),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	var body agents.APIResponse[*agents.AgentDTO]
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)
	s.Require().True(body.Success)

	return body.Data.ID
}

func (s *AgentsVisibilityTestSuite) CreateTestRun(agentID, status string) string {
	runID := uuid.New().String()

	run := &agents.AgentRun{
		ID:        runID,
		AgentID:   agentID,
		Status:    agents.AgentRunStatus(status),
		StartedAt: time.Now(),
		StepCount: 0,
	}
	maxSteps := 100
	run.MaxSteps = &maxSteps

	_, err := s.DB().NewInsert().Model(run).Exec(s.Ctx)
	s.Require().NoError(err)

	return runID
}

func (s *AgentsVisibilityTestSuite) CreateTestRunWithMetrics(agentID, status string, stepCount, maxSteps int) string {
	runID := uuid.New().String()

	run := &agents.AgentRun{
		ID:        runID,
		AgentID:   agentID,
		Status:    agents.AgentRunStatus(status),
		StartedAt: time.Now().Add(-5 * time.Minute), // 5 minutes ago
		StepCount: stepCount,
	}
	run.MaxSteps = &maxSteps

	now := time.Now()
	run.CompletedAt = &now
	durationMs := 300000
	run.DurationMs = &durationMs

	_, err := s.DB().NewInsert().Model(run).Exec(s.Ctx)
	s.Require().NoError(err)

	return runID
}

func (s *AgentsVisibilityTestSuite) CreateTestMessage(runID, role, textContent string, stepNumber int) {
	content := map[string]any{
		"text": textContent,
	}

	msg := &agents.AgentRunMessage{
		ID:         uuid.New().String(),
		RunID:      runID,
		Role:       role,
		Content:    content,
		StepNumber: stepNumber,
		CreatedAt:  time.Now(),
	}

	_, err := s.DB().NewInsert().Model(msg).Exec(s.Ctx)
	s.Require().NoError(err)
}

func (s *AgentsVisibilityTestSuite) CreateTestToolCall(runID, toolName string, input, output map[string]any, status string, durationMs, stepNumber int) {
	tc := &agents.AgentRunToolCall{
		ID:         uuid.New().String(),
		RunID:      runID,
		ToolName:   toolName,
		Input:      input,
		Output:     output,
		Status:     status,
		StepNumber: stepNumber,
		CreatedAt:  time.Now(),
	}
	tc.DurationMs = &durationMs

	_, err := s.DB().NewInsert().Model(tc).Exec(s.Ctx)
	s.Require().NoError(err)
}
