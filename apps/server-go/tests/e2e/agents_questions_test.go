package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/domain/agents"
	"github.com/emergent-company/emergent/internal/testutil"
)

// AgentsQuestionsSuite tests agent question CRUD and response endpoints.
type AgentsQuestionsSuite struct {
	testutil.BaseSuite
}

func TestAgentsQuestionsSuite(t *testing.T) {
	suite.Run(t, new(AgentsQuestionsSuite))
}

func (s *AgentsQuestionsSuite) SetupSuite() {
	s.SetDBSuffix("agents_questions")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Helpers
// =============================================================================

// createAgentWithRun creates an agent definition + agent + run with the given
// status. Returns (agentID, runID).
func (s *AgentsQuestionsSuite) createAgentWithRun(runStatus string) (string, string) {
	// Create agent definition via API
	defResp := s.Client.POST("/api/admin/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":         "Q&A Agent " + uuid.New().String()[:8],
			"systemPrompt": "You are a test agent",
			"tools":        []string{"ask_user"},
			"flowType":     "single",
		}),
	)
	s.Require().Equal(http.StatusCreated, defResp.StatusCode)

	var defBody agents.APIResponse[*agents.AgentDefinitionDTO]
	err := json.Unmarshal(defResp.Body, &defBody)
	s.Require().NoError(err)

	// Create agent via API
	agentResp := s.Client.POST("/api/admin/agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"projectId":    s.ProjectID,
			"name":         "Q&A Agent " + uuid.New().String()[:8],
			"strategyType": defBody.Data.ID,
			"cronSchedule": "0 0 * * *",
			"triggerType":  "manual",
		}),
	)
	s.Require().Equal(http.StatusCreated, agentResp.StatusCode)

	var agentBody agents.APIResponse[*agents.AgentDTO]
	err = json.Unmarshal(agentResp.Body, &agentBody)
	s.Require().NoError(err)

	agentID := agentBody.Data.ID

	// Insert a run with the desired status directly into DB
	runID := uuid.New().String()
	run := &agents.AgentRun{
		ID:        runID,
		AgentID:   agentID,
		Status:    agents.AgentRunStatus(runStatus),
		StartedAt: time.Now(),
		StepCount: 1,
	}
	maxSteps := 100
	run.MaxSteps = &maxSteps

	_, err = s.DB().NewInsert().Model(run).Exec(s.Ctx)
	s.Require().NoError(err)

	return agentID, runID
}

// createTestQuestion inserts a question record with the given parameters.
func (s *AgentsQuestionsSuite) createTestQuestion(agentID, runID, status string, options []agents.AgentQuestionOption) string {
	if options == nil {
		options = []agents.AgentQuestionOption{}
	}
	questionID := uuid.New().String()
	q := &agents.AgentQuestion{
		ID:        questionID,
		RunID:     runID,
		AgentID:   agentID,
		ProjectID: s.ProjectID,
		Question:  "What color do you prefer?",
		Options:   options,
		Status:    agents.AgentQuestionStatus(status),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_, err := s.DB().NewInsert().Model(q).Exec(s.Ctx)
	s.Require().NoError(err)
	return questionID
}

// =============================================================================
// Test 10.1: Create question, verify question record exists for paused run
// =============================================================================

func (s *AgentsQuestionsSuite) TestCreateQuestion_RecordCreated() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")

	// Insert a pending question (simulating what ask_user tool does)
	options := []agents.AgentQuestionOption{
		{Label: "Red", Value: "red"},
		{Label: "Blue", Value: "blue"},
	}
	questionID := s.createTestQuestion(agentID, runID, "pending", options)

	// Verify the question is retrievable via the list-by-run endpoint
	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/questions", s.ProjectID, runID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]*agents.AgentQuestionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Require().Len(response.Data, 1)

	q := response.Data[0]
	s.Equal(questionID, q.ID)
	s.Equal(runID, q.RunID)
	s.Equal(agentID, q.AgentID)
	s.Equal(s.ProjectID, q.ProjectID)
	s.Equal("What color do you prefer?", q.Question)
	s.Equal(agents.QuestionStatusPending, q.Status)
	s.Require().Len(q.Options, 2)
	s.Equal("Red", q.Options[0].Label)
	s.Equal("red", q.Options[0].Value)
	s.Equal("Blue", q.Options[1].Label)
	s.Equal("blue", q.Options[1].Value)
	s.Nil(q.Response)
}

// =============================================================================
// Test 10.2: Respond to pending question → 202
// =============================================================================

func (s *AgentsQuestionsSuite) TestRespondToQuestion_Success() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")
	questionID := s.createTestQuestion(agentID, runID, "pending", nil)

	// Respond to the question
	rec := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", s.ProjectID, questionID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"response": "I prefer blue",
		}),
	)
	s.Equal(http.StatusAccepted, rec.StatusCode)

	var response agents.APIResponse[*agents.AgentQuestionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Equal(questionID, response.Data.ID)
	s.Equal(agents.QuestionStatusAnswered, response.Data.Status)
	s.Require().NotNil(response.Data.Response)
	s.Equal("I prefer blue", *response.Data.Response)
	s.NotNil(response.Data.RespondedBy)
	s.NotNil(response.Data.RespondedAt)
}

func (s *AgentsQuestionsSuite) TestRespondToQuestion_MissingResponse() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")
	questionID := s.createTestQuestion(agentID, runID, "pending", nil)

	// Respond with empty body
	rec := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", s.ProjectID, questionID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"response": "",
		}),
	)
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *AgentsQuestionsSuite) TestRespondToQuestion_RequiresAuth() {
	rec := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", s.ProjectID, uuid.New().String()),
		testutil.WithJSONBody(map[string]any{
			"response": "test",
		}),
	)
	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *AgentsQuestionsSuite) TestRespondToQuestion_NotFound() {
	rec := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", s.ProjectID, uuid.New().String()),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"response": "test",
		}),
	)
	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *AgentsQuestionsSuite) TestRespondToQuestion_WrongProject() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")
	questionID := s.createTestQuestion(agentID, runID, "pending", nil)

	// Use a different (fake) project ID in the URL
	fakeProjectID := uuid.New().String()
	rec := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", fakeProjectID, questionID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"response": "test",
		}),
	)
	// Handler returns 404 when projectID doesn't match
	s.Equal(http.StatusNotFound, rec.StatusCode)
}

// =============================================================================
// Test 10.3: Respond to already-answered question → 409
// =============================================================================

func (s *AgentsQuestionsSuite) TestRespondToQuestion_AlreadyAnswered() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")
	questionID := s.createTestQuestion(agentID, runID, "answered", nil)

	rec := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", s.ProjectID, questionID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"response": "trying again",
		}),
	)
	s.Equal(http.StatusConflict, rec.StatusCode)
}

func (s *AgentsQuestionsSuite) TestRespondToQuestion_CancelledQuestion() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")
	questionID := s.createTestQuestion(agentID, runID, "cancelled", nil)

	rec := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", s.ProjectID, questionID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"response": "trying to answer cancelled",
		}),
	)
	s.Equal(http.StatusConflict, rec.StatusCode)
}

// =============================================================================
// Test 10.4: Respond while run is still running → 409
// =============================================================================

func (s *AgentsQuestionsSuite) TestRespondToQuestion_RunNotPaused() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("running")
	questionID := s.createTestQuestion(agentID, runID, "pending", nil)

	rec := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", s.ProjectID, questionID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"response": "test",
		}),
	)
	s.Equal(http.StatusConflict, rec.StatusCode)
}

func (s *AgentsQuestionsSuite) TestRespondToQuestion_RunCompleted() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("success")
	questionID := s.createTestQuestion(agentID, runID, "pending", nil)

	rec := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", s.ProjectID, questionID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"response": "test",
		}),
	)
	s.Equal(http.StatusConflict, rec.StatusCode)
}

// =============================================================================
// Test 10.5: Repository methods tested through question list endpoints
// =============================================================================

func (s *AgentsQuestionsSuite) TestListQuestionsByRun_Empty() {
	s.SkipIfExternalServer("requires direct DB access")

	_, runID := s.createAgentWithRun("running")

	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/questions", s.ProjectID, runID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]*agents.AgentQuestionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Len(response.Data, 0)
}

func (s *AgentsQuestionsSuite) TestListQuestionsByRun_MultipleQuestions() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")

	// Create multiple questions with different statuses
	s.createTestQuestion(agentID, runID, "cancelled", nil)
	s.createTestQuestion(agentID, runID, "pending", []agents.AgentQuestionOption{
		{Label: "A", Value: "a"},
	})

	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/questions", s.ProjectID, runID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]*agents.AgentQuestionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.Len(response.Data, 2)
}

func (s *AgentsQuestionsSuite) TestListQuestionsByRun_RunNotFound() {
	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/questions", s.ProjectID, uuid.New().String()),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *AgentsQuestionsSuite) TestListQuestionsByProject_Success() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")
	s.createTestQuestion(agentID, runID, "pending", nil)
	s.createTestQuestion(agentID, runID, "answered", nil)

	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-questions", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]*agents.AgentQuestionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	s.GreaterOrEqual(len(response.Data), 2)
}

func (s *AgentsQuestionsSuite) TestListQuestionsByProject_WithStatusFilter() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")
	s.createTestQuestion(agentID, runID, "pending", nil)
	s.createTestQuestion(agentID, runID, "answered", nil)
	s.createTestQuestion(agentID, runID, "cancelled", nil)

	// Filter by pending
	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-questions?status=pending", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]*agents.AgentQuestionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.True(response.Success)
	for _, q := range response.Data {
		s.Equal(agents.QuestionStatusPending, q.Status)
	}
}

func (s *AgentsQuestionsSuite) TestListQuestionsByProject_InvalidStatusFilter() {
	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-questions?status=invalid", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *AgentsQuestionsSuite) TestListQuestionsByProject_RequiresAuth() {
	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-questions", s.ProjectID),
	)
	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

// =============================================================================
// Test: Question with options round-trip
// =============================================================================

func (s *AgentsQuestionsSuite) TestQuestionWithOptions_RoundTrip() {
	s.SkipIfExternalServer("requires direct DB access")

	agentID, runID := s.createAgentWithRun("paused")
	options := []agents.AgentQuestionOption{
		{Label: "Option A", Value: "a", Description: "First option"},
		{Label: "Option B", Value: "b", Description: "Second option"},
		{Label: "Option C", Value: "c"},
	}
	questionID := s.createTestQuestion(agentID, runID, "pending", options)

	// Fetch via API and verify options serialization
	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/questions", s.ProjectID, runID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, rec.StatusCode)

	var response agents.APIResponse[[]*agents.AgentQuestionDTO]
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)
	s.Require().Len(response.Data, 1)

	q := response.Data[0]
	s.Equal(questionID, q.ID)
	s.Require().Len(q.Options, 3)
	s.Equal("Option A", q.Options[0].Label)
	s.Equal("a", q.Options[0].Value)
	s.Equal("First option", q.Options[0].Description)
	s.Equal("Option C", q.Options[2].Label)
	s.Empty(q.Options[2].Description) // omitempty
}
