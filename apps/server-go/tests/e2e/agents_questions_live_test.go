package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/domain/agents"
	"github.com/emergent-company/emergent/internal/testutil"
)

// AgentsQuestionsLiveSuite tests agent questions against live test data in mcj-emergent.
//
// This test assumes you've already created test data using:
//
//	INSERT INTO kb.agent_runs (id, agent_id, status, started_at, step_count, max_steps, created_at)
//	VALUES ('00000000-0000-0000-0000-000000000001', '986be57b-d8d2-4468-b439-a511bf1728fb', 'paused_for_input', NOW(), 5, 100, NOW());
//
//	INSERT INTO kb.agent_questions (id, agent_id, run_id, project_id, question, options, status, created_at, updated_at)
//	VALUES ('00000000-0000-0000-0000-000000000002', '986be57b-d8d2-4468-b439-a511bf1728fb', '00000000-0000-0000-0000-000000000001', '44f0c1d9-7c3b-41d1-8393-32cf05ab1a77', 'What is your favorite color?', '[{"label": "Red", "value": "red"}, {"label": "Blue", "value": "blue"}, {"label": "Green", "value": "green"}]'::jsonb, 'pending', NOW(), NOW());
//
// Usage:
//
//	TEST_SERVER_URL=http://mcj-emergent:3002 go test -v -run TestAgentsQuestionsLiveSuite
type AgentsQuestionsLiveSuite struct {
	testutil.BaseSuite

	projectID  string
	runID      string
	questionID string
}

func TestAgentsQuestionsLiveSuite(t *testing.T) {
	suite.Run(t, new(AgentsQuestionsLiveSuite))
}

func (s *AgentsQuestionsLiveSuite) SetupSuite() {
	s.SetDBSuffix("agents_questions_live")
	s.BaseSuite.SetupSuite()

	// Skip if not running against external server
	if !s.Client.IsExternal() {
		s.T().Skip("Skipping live test - TEST_SERVER_URL not set")
	}

	s.T().Logf("Running against external server: %s", s.Client.BaseURL())

	// Use the pre-created test data IDs
	s.projectID = "44f0c1d9-7c3b-41d1-8393-32cf05ab1a77"
	s.runID = "00000000-0000-0000-0000-000000000001"
	s.questionID = "00000000-0000-0000-0000-000000000002"
}

// TestListAndRespondToQuestion tests the full lifecycle with live test data:
// 1. List pending questions
// 2. Verify test question is present
// 3. Respond to the question
// 4. Verify status changed to answered
func (s *AgentsQuestionsLiveSuite) TestListAndRespondToQuestion() {
	s.T().Log("=== Testing Agent Questions with Live Data ===")

	// Step 1: List pending questions for project
	s.T().Log("Step 1: Listing pending questions")
	pendingQuestions := s.listQuestionsByProject(s.projectID, "pending")
	s.T().Logf("✓ Found %d pending question(s)", len(pendingQuestions))

	// Verify our test question is in the list
	var testQuestion *agents.AgentQuestionDTO
	for _, q := range pendingQuestions {
		if q.ID == s.questionID {
			testQuestion = q
			break
		}
	}

	if testQuestion == nil {
		s.T().Skip("Test question not found - may have been answered already or not created")
	}

	s.T().Logf("\n✓ Found test question:")
	s.T().Logf("  ID: %s", testQuestion.ID)
	s.T().Logf("  Question: %s", testQuestion.Question)
	s.T().Logf("  Status: %s", testQuestion.Status)
	s.T().Logf("  Run ID: %s", testQuestion.RunID)

	if len(testQuestion.Options) > 0 {
		s.T().Log("  Options:")
		for _, opt := range testQuestion.Options {
			s.T().Logf("    - %s (%s)", opt.Label, opt.Value)
		}
	}

	s.Require().Equal(string("pending"), string(testQuestion.Status))
	s.Require().Equal("What is your favorite color?", testQuestion.Question)

	// Step 2: List questions by run ID
	s.T().Log("\nStep 2: Listing questions by run ID")
	runQuestions := s.listQuestionsByRun(s.projectID, s.runID)
	s.T().Logf("✓ Found %d question(s) for run", len(runQuestions))
	s.Require().Len(runQuestions, 1, "Expected 1 question for test run")
	s.Require().Equal(s.questionID, runQuestions[0].ID)

	// Step 3: Respond to the question
	s.T().Log("\nStep 3: Responding to question with 'blue'")
	s.respondToQuestion(s.projectID, s.questionID, "blue")
	s.T().Log("✓ Successfully responded")

	// Step 4: Verify question status changed
	s.T().Log("\nStep 4: Verifying question status changed")
	updatedQuestions := s.listQuestionsByRun(s.projectID, s.runID)
	s.Require().Len(updatedQuestions, 1)

	updated := updatedQuestions[0]
	s.T().Logf("\n✓ Question updated:")
	s.T().Logf("  ID: %s", updated.ID)
	s.T().Logf("  Status: %s", updated.Status)
	if updated.Response != nil {
		s.T().Logf("  Response: %s", *updated.Response)
	}
	if updated.RespondedAt != nil {
		s.T().Logf("  Responded At: %s", *updated.RespondedAt)
	}

	s.Require().Equal(string("answered"), string(updated.Status))
	s.Require().NotNil(updated.Response)
	s.Require().Equal("blue", *updated.Response)
	s.Require().NotNil(updated.RespondedAt)

	// Step 5: Verify no more pending questions for this question ID
	s.T().Log("\nStep 5: Verifying question no longer in pending list")
	newPendingQuestions := s.listQuestionsByProject(s.projectID, "pending")
	for _, q := range newPendingQuestions {
		s.Require().NotEqual(s.questionID, q.ID, "Question should not be in pending list")
	}
	s.T().Log("✓ Question correctly removed from pending list")

	s.T().Log("\n=== Full Agent Questions Lifecycle Test PASSED ===")
}

// =============================================================================
// Helper Methods
// =============================================================================

// listQuestionsByProject lists all questions for a project with optional status filter
func (s *AgentsQuestionsLiveSuite) listQuestionsByProject(projectID, status string) []*agents.AgentQuestionDTO {
	path := fmt.Sprintf("/api/projects/%s/agent-questions", projectID)
	if status != "" {
		path += "?status=" + status
	}

	resp := s.Client.GET(path, testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode, "Failed to list questions by project: %s", string(resp.Body))

	var body agents.APIResponse[[]*agents.AgentQuestionDTO]
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err, "Failed to parse questions response")

	return body.Data
}

// listQuestionsByRun lists all questions for a specific run
func (s *AgentsQuestionsLiveSuite) listQuestionsByRun(projectID, runID string) []*agents.AgentQuestionDTO {
	resp := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/questions", projectID, runID),
		testutil.WithAuth("e2e-test-user"),
	)

	s.Require().Equal(http.StatusOK, resp.StatusCode, "Failed to list questions by run: %s", string(resp.Body))

	var body agents.APIResponse[[]*agents.AgentQuestionDTO]
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err, "Failed to parse questions response")

	return body.Data
}

// respondToQuestion responds to a question
func (s *AgentsQuestionsLiveSuite) respondToQuestion(projectID, questionID, response string) {
	resp := s.Client.POST(
		fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", projectID, questionID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"response": response,
		}),
	)

	s.Require().True(
		resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted,
		"Failed to respond to question: %d - %s", resp.StatusCode, string(resp.Body),
	)
}
