package agents_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/agents"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

func TestAgentsList(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	// Mock response
	fixtureAgents := testutil.FixtureAgents()
	apiResponse := map[string]interface{}{
		"success": true,
		"data":    fixtureAgents,
	}
	mock.OnJSON("GET", "/api/admin/agents", http.StatusOK, apiResponse)

	// Create client
	client, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth: sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: "test_key",
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test List
	result, err := client.Agents.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result.Data) != len(fixtureAgents) {
		t.Errorf("expected %d agents, got %d", len(fixtureAgents), len(result.Data))
	}

	if result.Data[0].ID != fixtureAgents[0].ID {
		t.Errorf("expected agent ID %s, got %s", fixtureAgents[0].ID, result.Data[0].ID)
	}
}

func TestAgentsGet(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureAgent := testutil.FixtureAgent()
	apiResponse := map[string]interface{}{
		"success": true,
		"data":    fixtureAgent,
	}
	mock.OnJSON("GET", "/api/admin/agents/agent_test123", http.StatusOK, apiResponse)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Agents.Get(context.Background(), "agent_test123")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if result.Data.ID != fixtureAgent.ID {
		t.Errorf("expected agent ID %s, got %s", fixtureAgent.ID, result.Data.ID)
	}

	if result.Data.Name != fixtureAgent.Name {
		t.Errorf("expected agent name %s, got %s", fixtureAgent.Name, result.Data.Name)
	}
}

func TestAgentsGetRuns(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureRuns := testutil.FixtureAgentRuns()
	apiResponse := map[string]interface{}{
		"success": true,
		"data":    fixtureRuns,
	}
	mock.On("GET", "/api/admin/agents/agent_test123/runs", func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameters
		if limit := r.URL.Query().Get("limit"); limit != "5" {
			t.Errorf("expected limit=5, got %s", limit)
		}

		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, apiResponse)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Agents.GetRuns(context.Background(), "agent_test123", 5)
	if err != nil {
		t.Fatalf("GetRuns() error = %v", err)
	}

	if len(result.Data) != len(fixtureRuns) {
		t.Errorf("expected %d runs, got %d", len(fixtureRuns), len(result.Data))
	}
}

func TestAgentsGetProjectRun(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureRun := testutil.FixtureAgentRun()
	apiResponse := map[string]interface{}{
		"success": true,
		"data":    fixtureRun,
	}
	mock.OnJSON("GET", "/api/projects/proj_test123/agent-runs/run_test123", http.StatusOK, apiResponse)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Agents.GetProjectRun(context.Background(), "proj_test123", "run_test123")
	if err != nil {
		t.Fatalf("GetProjectRun() error = %v", err)
	}

	if result.Data.ID != fixtureRun.ID {
		t.Errorf("expected run ID %s, got %s", fixtureRun.ID, result.Data.ID)
	}
}

func TestAgentsGetRunMessages(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureMessages := testutil.FixtureAgentRunMessages()
	apiResponse := map[string]interface{}{
		"success": true,
		"data":    fixtureMessages,
	}
	mock.OnJSON("GET", "/api/projects/proj_test123/agent-runs/run_test123/messages", http.StatusOK, apiResponse)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Agents.GetRunMessages(context.Background(), "proj_test123", "run_test123")
	if err != nil {
		t.Fatalf("GetRunMessages() error = %v", err)
	}

	if len(result.Data) != len(fixtureMessages) {
		t.Errorf("expected %d messages, got %d", len(fixtureMessages), len(result.Data))
	}

	if result.Data[0].ID != fixtureMessages[0].ID {
		t.Errorf("expected message ID %s, got %s", fixtureMessages[0].ID, result.Data[0].ID)
	}
}

func TestAgentsGetRunToolCalls(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureToolCalls := testutil.FixtureAgentRunToolCalls()
	apiResponse := map[string]interface{}{
		"success": true,
		"data":    fixtureToolCalls,
	}
	mock.OnJSON("GET", "/api/projects/proj_test123/agent-runs/run_test123/tool-calls", http.StatusOK, apiResponse)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Agents.GetRunToolCalls(context.Background(), "proj_test123", "run_test123")
	if err != nil {
		t.Fatalf("GetRunToolCalls() error = %v", err)
	}

	if len(result.Data) != len(fixtureToolCalls) {
		t.Errorf("expected %d tool calls, got %d", len(fixtureToolCalls), len(result.Data))
	}

	if result.Data[0].ToolName != fixtureToolCalls[0].ToolName {
		t.Errorf("expected tool name %s, got %s", fixtureToolCalls[0].ToolName, result.Data[0].ToolName)
	}
}

// Test agent questions methods

func TestAgentsGetRunQuestions(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureQuestions := testutil.FixtureAgentQuestions()
	apiResponse := map[string]interface{}{
		"success": true,
		"data":    fixtureQuestions,
	}
	mock.OnJSON("GET", "/api/projects/proj_test123/agent-runs/run_test123/questions", http.StatusOK, apiResponse)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Agents.GetRunQuestions(context.Background(), "proj_test123", "run_test123")
	if err != nil {
		t.Fatalf("GetRunQuestions() error = %v", err)
	}

	if len(result.Data) != len(fixtureQuestions) {
		t.Errorf("expected %d questions, got %d", len(fixtureQuestions), len(result.Data))
	}

	if result.Data[0].ID != fixtureQuestions[0].ID {
		t.Errorf("expected question ID %s, got %s", fixtureQuestions[0].ID, result.Data[0].ID)
	}

	if result.Data[0].Question != fixtureQuestions[0].Question {
		t.Errorf("expected question %s, got %s", fixtureQuestions[0].Question, result.Data[0].Question)
	}
}

func TestAgentsListProjectQuestions(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureQuestions := testutil.FixtureAgentQuestions()
	apiResponse := map[string]interface{}{
		"success": true,
		"data":    fixtureQuestions,
	}

	mock.On("GET", "/api/projects/proj_test123/agent-questions", func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, apiResponse)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Agents.ListProjectQuestions(context.Background(), "proj_test123", "")
	if err != nil {
		t.Fatalf("ListProjectQuestions() error = %v", err)
	}

	if len(result.Data) != len(fixtureQuestions) {
		t.Errorf("expected %d questions, got %d", len(fixtureQuestions), len(result.Data))
	}
}

func TestAgentsListProjectQuestionsWithStatus(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureQuestions := testutil.FixtureAgentQuestions()
	apiResponse := map[string]interface{}{
		"success": true,
		"data":    []agents.AgentQuestion{fixtureQuestions[0]}, // Only pending
	}

	mock.On("GET", "/api/projects/proj_test123/agent-questions", func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameter
		if status := r.URL.Query().Get("status"); status != "pending" {
			t.Errorf("expected status=pending, got %s", status)
		}

		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, apiResponse)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Agents.ListProjectQuestions(context.Background(), "proj_test123", "pending")
	if err != nil {
		t.Fatalf("ListProjectQuestions() error = %v", err)
	}

	if len(result.Data) != 1 {
		t.Errorf("expected 1 question, got %d", len(result.Data))
	}

	if result.Data[0].Status != "pending" {
		t.Errorf("expected status pending, got %s", result.Data[0].Status)
	}
}

func TestAgentsRespondToQuestion(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureQuestion := testutil.FixtureAgentQuestion()
	// Update the fixture to have answered status
	fixtureQuestion.Status = "answered"
	response := "Blue"
	fixtureQuestion.Response = &response

	apiResponse := map[string]interface{}{
		"success": true,
		"data":    fixtureQuestion,
	}

	mock.On("POST", "/api/projects/proj_test123/agent-questions/question_test123/respond", func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		// Verify request body
		var reqBody agents.RespondToQuestionRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if reqBody.Response != "Blue" {
			t.Errorf("expected response 'Blue', got %s", reqBody.Response)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		testutil.JSONResponse(t, w, apiResponse)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	req := &agents.RespondToQuestionRequest{
		Response: "Blue",
	}

	result, err := client.Agents.RespondToQuestion(context.Background(), "proj_test123", "question_test123", req)
	if err != nil {
		t.Fatalf("RespondToQuestion() error = %v", err)
	}

	if result.Data.Status != "answered" {
		t.Errorf("expected status answered, got %s", result.Data.Status)
	}

	if result.Data.Response == nil || *result.Data.Response != "Blue" {
		t.Errorf("expected response 'Blue', got %v", result.Data.Response)
	}
}

func TestAgentsRespondToQuestionError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	errorResponse := map[string]interface{}{
		"success": false,
		"error": map[string]interface{}{
			"code":    "QUESTION_NOT_FOUND",
			"message": "Question not found",
		},
	}

	mock.OnJSON("POST", "/api/projects/proj_test123/agent-questions/invalid_id/respond", http.StatusNotFound, errorResponse)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	req := &agents.RespondToQuestionRequest{
		Response: "Blue",
	}

	_, err := client.Agents.RespondToQuestion(context.Background(), "proj_test123", "invalid_id", req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
