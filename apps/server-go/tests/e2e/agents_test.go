package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/domain/agents"
	"github.com/emergent-company/emergent/internal/testutil"
)

type AgentsSuite struct {
	testutil.BaseSuite
}

func (s *AgentsSuite) SetupSuite() {
	s.SetDBSuffix("agents")
	s.BaseSuite.SetupSuite()
}

// ==================== Agent CRUD Tests ====================

func (s *AgentsSuite) TestCreateAgent_Success() {
	rec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Test Agent",
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.NotEmpty(response.Data.ID)
	s.Equal("Test Agent", response.Data.Name)
	s.Equal("extraction", response.Data.StrategyType)
	s.Equal("0 */5 * * * *", response.Data.CronSchedule)
	s.True(response.Data.Enabled)
	s.Equal(agents.TriggerTypeSchedule, response.Data.TriggerType)
	s.Equal(agents.ExecutionModeExecute, response.Data.ExecutionMode)
}

func (s *AgentsSuite) TestCreateAgent_RequiresAuth() {
	rec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Test Agent",
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsSuite) TestCreateAgent_MissingName() {
	rec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *AgentsSuite) TestCreateAgent_MissingProjectID() {
	rec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":         "Test Agent",
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *AgentsSuite) TestListAgents_Success() {
	// Create an agent first
	s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "List Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)

	rec := s.Client.GET(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]agents.AgentDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.GreaterOrEqual(len(response.Data), 1)
}

func (s *AgentsSuite) TestListAgents_RequiresProjectID() {
	rec := s.Client.GET(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *AgentsSuite) TestGetAgent_Success() {
	// Create an agent
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Get Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	// Get it
	rec := s.Client.GET(
		"/api/admin/agents/"+createResp.Data.ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Equal(createResp.Data.ID, response.Data.ID)
	s.Equal(createResp.Data.Name, response.Data.Name)
}

func (s *AgentsSuite) TestGetAgent_NotFound() {
	rec := s.Client.GET(
		"/api/admin/agents/"+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *AgentsSuite) TestUpdateAgent_Success() {
	// Create an agent
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Update Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	// Update it
	rec := s.Client.PATCH(
		"/api/admin/agents/"+createResp.Data.ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":    "Updated Agent Name",
			"enabled": false,
		}),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Equal("Updated Agent Name", response.Data.Name)
	s.False(response.Data.Enabled)
}

func (s *AgentsSuite) TestUpdateAgent_NotFound() {
	rec := s.Client.PATCH(
		"/api/admin/agents/"+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "Updated Name",
		}),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *AgentsSuite) TestDeleteAgent_Success() {
	// Create an agent
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Delete Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	// Delete it
	rec := s.Client.DELETE(
		"/api/admin/agents/"+createResp.Data.ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	// Verify it's gone
	getRec := s.Client.GET(
		"/api/admin/agents/"+createResp.Data.ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, getRec.StatusCode)
}

func (s *AgentsSuite) TestDeleteAgent_NotFound() {
	rec := s.Client.DELETE(
		"/api/admin/agents/"+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

// ==================== Agent Trigger Tests ====================

func (s *AgentsSuite) TestTriggerAgent_StubMode() {
	// Create an agent
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Trigger Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	// Trigger it (executor is nil in test server, so falls back to stub mode)
	rec := s.Client.POST(
		"/api/admin/agents/"+createResp.Data.ID+"/trigger",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.TriggerResponseDTO
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.NotNil(response.RunID)
	s.NotNil(response.Message)
	s.Contains(*response.Message, "stub mode")
}

func (s *AgentsSuite) TestTriggerAgent_NotFound() {
	rec := s.Client.POST(
		"/api/admin/agents/"+uuid.New().String()+"/trigger",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

// ==================== Agent Run History Tests ====================

func (s *AgentsSuite) TestGetAgentRuns_Success() {
	// Create an agent and trigger it to create a run
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Runs Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	// Trigger to create a run
	s.Client.POST(
		"/api/admin/agents/"+createResp.Data.ID+"/trigger",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	// Get runs
	rec := s.Client.GET(
		"/api/admin/agents/"+createResp.Data.ID+"/runs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]agents.AgentRunDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.GreaterOrEqual(len(response.Data), 1)
	s.Equal(createResp.Data.ID, response.Data[0].AgentID)
}

// ==================== Agent Definition CRUD Tests ====================

func (s *AgentsSuite) TestCreateDefinition_Success() {
	rec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":         "Test Definition",
			"description":  "A test agent definition",
			"systemPrompt": "You are a helpful assistant.",
			"tools":        []string{"search", "read_file"},
			"flowType":     "single",
			"visibility":   "project",
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentDefinitionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.NotEmpty(response.Data.ID)
	s.Equal("Test Definition", response.Data.Name)
	s.Equal(agents.FlowTypeSingle, response.Data.FlowType)
	s.Equal(agents.VisibilityProject, response.Data.Visibility)
	s.False(response.Data.IsDefault)
	s.Equal([]string{"search", "read_file"}, response.Data.Tools)
}

func (s *AgentsSuite) TestCreateDefinition_RequiresAuth() {
	rec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "Test Definition",
		}),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsSuite) TestCreateDefinition_RequiresProjectID() {
	rec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name": "Test Definition",
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *AgentsSuite) TestCreateDefinition_MissingName() {
	rec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"flowType": "single",
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *AgentsSuite) TestCreateDefinition_Defaults() {
	rec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "Defaults Definition " + uuid.New().String()[:8],
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentDefinitionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	// Check defaults
	s.Equal(agents.FlowTypeSingle, response.Data.FlowType)
	s.Equal(agents.VisibilityProject, response.Data.Visibility)
	s.False(response.Data.IsDefault)
	s.NotNil(response.Data.Tools)
	s.Empty(response.Data.Tools)
}

func (s *AgentsSuite) TestListDefinitions_Success() {
	// Create a definition first
	s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "List Def " + uuid.New().String()[:8],
		}),
	)

	rec := s.Client.GET(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]agents.AgentDefinitionSummaryDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.GreaterOrEqual(len(response.Data), 1)

	// Verify summary has expected fields
	def := response.Data[0]
	s.NotEmpty(def.ID)
	s.NotEmpty(def.Name)
	s.NotEmpty(def.ProjectID)
}

func (s *AgentsSuite) TestListDefinitions_RequiresProjectID() {
	rec := s.Client.GET(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *AgentsSuite) TestGetDefinition_Success() {
	// Create a definition
	createRec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":         "Get Def " + uuid.New().String()[:8],
			"description":  "A detailed definition",
			"systemPrompt": "System prompt here",
			"tools":        []string{"tool_a", "tool_b"},
			"maxSteps":     50,
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDefinitionDTO]
	json.Unmarshal(createRec.Body, &createResp)

	// Get it
	rec := s.Client.GET(
		"/api/admin/agent-definitions/"+createResp.Data.ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentDefinitionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Equal(createResp.Data.ID, response.Data.ID)
	s.Equal(createResp.Data.Name, response.Data.Name)
	s.NotNil(response.Data.SystemPrompt)
	s.Equal("System prompt here", *response.Data.SystemPrompt)
	s.Equal([]string{"tool_a", "tool_b"}, response.Data.Tools)
}

func (s *AgentsSuite) TestGetDefinition_NotFound() {
	rec := s.Client.GET(
		"/api/admin/agent-definitions/"+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *AgentsSuite) TestUpdateDefinition_Success() {
	// Create a definition
	createRec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":       "Update Def " + uuid.New().String()[:8],
			"visibility": "project",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDefinitionDTO]
	json.Unmarshal(createRec.Body, &createResp)

	// Update it
	rec := s.Client.PATCH(
		"/api/admin/agent-definitions/"+createResp.Data.ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":       "Updated Def Name",
			"visibility": "external",
			"tools":      []string{"new_tool"},
			"isDefault":  true,
			"maxSteps":   100,
		}),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentDefinitionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Equal("Updated Def Name", response.Data.Name)
	s.Equal(agents.VisibilityExternal, response.Data.Visibility)
	s.Equal([]string{"new_tool"}, response.Data.Tools)
	s.True(response.Data.IsDefault)
	s.NotNil(response.Data.MaxSteps)
	s.Equal(100, *response.Data.MaxSteps)
}

func (s *AgentsSuite) TestUpdateDefinition_NotFound() {
	rec := s.Client.PATCH(
		"/api/admin/agent-definitions/"+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "Updated Name",
		}),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *AgentsSuite) TestDeleteDefinition_Success() {
	// Create a definition
	createRec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "Delete Def " + uuid.New().String()[:8],
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDefinitionDTO]
	json.Unmarshal(createRec.Body, &createResp)

	// Delete it
	rec := s.Client.DELETE(
		"/api/admin/agent-definitions/"+createResp.Data.ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	// Verify it's gone
	getRec := s.Client.GET(
		"/api/admin/agent-definitions/"+createResp.Data.ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, getRec.StatusCode)
}

func (s *AgentsSuite) TestDeleteDefinition_NotFound() {
	rec := s.Client.DELETE(
		"/api/admin/agent-definitions/"+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

// ==================== Project-Scoped Run History Tests ====================

func (s *AgentsSuite) TestListProjectRuns_Success() {
	// Create an agent and trigger it
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Project Runs Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	// Trigger to generate a run
	triggerRec := s.Client.POST(
		"/api/admin/agents/"+createResp.Data.ID+"/trigger",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, triggerRec.StatusCode)

	// List project runs
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/agent-runs",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[agents.PaginatedResponse[*agents.AgentRunDTO]]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.GreaterOrEqual(response.Data.TotalCount, 1)
	s.GreaterOrEqual(len(response.Data.Items), 1)
}

func (s *AgentsSuite) TestListProjectRuns_WithFilters() {
	// Create an agent and trigger it
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Filter Runs Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	s.Client.POST(
		"/api/admin/agents/"+createResp.Data.ID+"/trigger",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	// Filter by agentId
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/agent-runs?agentId="+createResp.Data.ID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[agents.PaginatedResponse[*agents.AgentRunDTO]]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	// All returned runs should belong to this agent
	for _, run := range response.Data.Items {
		s.Equal(createResp.Data.ID, run.AgentID)
	}
}

func (s *AgentsSuite) TestListProjectRuns_RequiresAuth() {
	rec := s.Client.GET(
		"/api/projects/" + s.ProjectID + "/agent-runs",
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsSuite) TestGetProjectRun_Success() {
	// Create and trigger to get a run
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Get Run Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	triggerRec := s.Client.POST(
		"/api/admin/agents/"+createResp.Data.ID+"/trigger",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, triggerRec.StatusCode)

	var triggerResp agents.TriggerResponseDTO
	json.Unmarshal(triggerRec.Body, &triggerResp)
	s.Require().NotNil(triggerResp.RunID)

	// Get the run
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/agent-runs/"+*triggerResp.RunID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentRunDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Equal(*triggerResp.RunID, response.Data.ID)
	s.Equal(createResp.Data.ID, response.Data.AgentID)
}

func (s *AgentsSuite) TestGetProjectRun_NotFound() {
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/agent-runs/"+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *AgentsSuite) TestGetRunMessages_Empty() {
	// Create and trigger to get a run (stub mode, so no messages)
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Messages Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	triggerRec := s.Client.POST(
		"/api/admin/agents/"+createResp.Data.ID+"/trigger",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, triggerRec.StatusCode)

	var triggerResp agents.TriggerResponseDTO
	json.Unmarshal(triggerRec.Body, &triggerResp)
	s.Require().NotNil(triggerResp.RunID)

	// Get messages (should be empty since we're in stub mode)
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/agent-runs/"+*triggerResp.RunID+"/messages",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]agents.AgentRunMessageDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	// Stub runs don't produce messages, so it should be empty
	s.Empty(response.Data)
}

func (s *AgentsSuite) TestGetRunMessages_RunNotFound() {
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/agent-runs/"+uuid.New().String()+"/messages",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *AgentsSuite) TestGetRunToolCalls_Empty() {
	// Create and trigger to get a run
	createRec := s.Client.POST(
		"/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "ToolCalls Agent " + uuid.New().String()[:8],
			"strategyType": "extraction",
			"cronSchedule": "0 */5 * * * *",
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp agents.APIResponse[*agents.AgentDTO]
	json.Unmarshal(createRec.Body, &createResp)

	triggerRec := s.Client.POST(
		"/api/admin/agents/"+createResp.Data.ID+"/trigger",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, triggerRec.StatusCode)

	var triggerResp agents.TriggerResponseDTO
	json.Unmarshal(triggerRec.Body, &triggerResp)
	s.Require().NotNil(triggerResp.RunID)

	// Get tool calls (should be empty since we're in stub mode)
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/agent-runs/"+*triggerResp.RunID+"/tool-calls",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]agents.AgentRunToolCallDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Empty(response.Data)
}

func (s *AgentsSuite) TestGetRunToolCalls_RunNotFound() {
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/agent-runs/"+uuid.New().String()+"/tool-calls",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

// ==================== Definition with ACPConfig Tests ====================

func (s *AgentsSuite) TestCreateDefinition_WithACPConfig() {
	rec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":       "ACP Agent " + uuid.New().String()[:8],
			"visibility": "external",
			"acpConfig": map[string]any{
				"displayName":  "My External Agent",
				"description":  "An agent visible via ACP",
				"capabilities": []string{"data_analysis", "report_generation"},
				"inputModes":   []string{"text"},
				"outputModes":  []string{"text", "data"},
			},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentDefinitionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Equal(agents.VisibilityExternal, response.Data.Visibility)
	s.NotNil(response.Data.ACPConfig)
	s.Equal("My External Agent", response.Data.ACPConfig.DisplayName)
	s.Equal([]string{"data_analysis", "report_generation"}, response.Data.ACPConfig.Capabilities)
}

func (s *AgentsSuite) TestCreateDefinition_WithModelConfig() {
	temp := float32(0.7)
	maxTokens := 4096

	rec := s.Client.POST(
		"/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "Model Config Agent " + uuid.New().String()[:8],
			"model": map[string]any{
				"name":        "gemini-2.0-flash",
				"temperature": temp,
				"maxTokens":   maxTokens,
			},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentDefinitionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.NotNil(response.Data.Model)
	s.Equal("gemini-2.0-flash", response.Data.Model.Name)
}

func TestAgentsSuite(t *testing.T) {
	suite.Run(t, new(AgentsSuite))
}
