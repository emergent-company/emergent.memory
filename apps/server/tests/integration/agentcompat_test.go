package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/agentcompat"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

func strPtr(s string) *string { return &s }

// AgentCompatTestSuite exercises the OpenAI-compatible endpoints:
//
//	POST /v1/chat/completions
//	GET  /v1/models
//
// Modes:
//
//  1. In-process (default): real Postgres test DB; LLM-dependent tests
//     skip when no provider key is found in env.
//
//  2. External (TEST_SERVER_URL set): sends real HTTP to a running server.
//     Requires TEST_API_TOKEN and TEST_PROJECT_ID env vars.
//
// Tests cover:
//  1. GET /v1/models                          — list shape, at least one agent
//  2. POST non-streaming                      — response shape, finish_reason
//  3. POST streaming (SSE)                    — SSE mechanics, opening delta, [DONE]
//  4. No auth                                 → 401
//  5. Empty model                             → 400
//  6. Non-existent model                      → 400 "not found"
//  7. Reserved tool prefix (memory_*)         → 400
//  8. Client tool definition (no-op)          — 200 / 400 without LLM
//  9. Agent:prefix model name accepted        — same as bare name
// 10. (LLM) Client tool roundtrip            — tool_calls + resume
type AgentCompatTestSuite struct {
	suite.Suite

	testDB    *testutil.TestDB
	inProcess *testutil.TestServer

	client    *testutil.HTTPClient
	ctx       context.Context
	projectID string
	orgID     string
	authToken string
	external  bool

	// agentName used by all tests — ensured in SetupTest.
	agentName string
}

func TestAgentCompatSuite(t *testing.T) {
	suite.Run(t, new(AgentCompatTestSuite))
}

// ---------------------------------------------------------------------------
// Suite lifecycle
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) SetupSuite() {
	s.ctx = context.Background()
	s.external = os.Getenv("TEST_SERVER_URL") != ""

	if s.external {
		baseURL := os.Getenv("TEST_SERVER_URL")
		s.authToken = os.Getenv("TEST_API_TOKEN")
		s.projectID = os.Getenv("TEST_PROJECT_ID")
		if s.authToken == "" || s.projectID == "" {
			s.T().Fatal("TEST_SERVER_URL set but TEST_API_TOKEN or TEST_PROJECT_ID missing")
		}
		s.client = testutil.NewExternalHTTPClient(baseURL)
		return
	}

	testDB, err := testutil.SetupTestDB(s.ctx, "agentcompat")
	s.Require().NoError(err, "setup test db")
	s.testDB = testDB
	s.authToken = "e2e-test-user"
}

func (s *AgentCompatTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *AgentCompatTestSuite) SetupTest() {
	if s.external {
		// external: agentName must be set in env or we use the default
		s.agentName = os.Getenv("TEST_AGENT_NAME")
		if s.agentName == "" {
			s.agentName = "graph-query-agent"
		}
		return
	}

	// In-process: truncate + recreate fixtures for each test.
	err := testutil.TruncateTables(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	err = testutil.SetupTestFixtures(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()

	err = testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID)
	s.Require().NoError(err)

	// Ensure graph-query-agent exists (internal — provides tools for queries).
	agentRepo := agents.NewRepository(s.testDB.DB)
	_, err = agentRepo.EnsureGraphQueryAgent(s.ctx, s.projectID)
	s.Require().NoError(err, "ensure graph-query-agent definition")

	// Create an external-visibility test agent so it appears in GET /v1/models.
	// HandleChatCompletion accepts any definition regardless of visibility.
	extAgentName := "test-compat-agent"
	systemPrompt := "You are a helpful assistant for integration testing."
	extDef := &agents.AgentDefinition{
		ProjectID:    s.projectID,
		Name:         extAgentName,
		Description:  strPtr("Integration test agent for agentcompat suite"),
		SystemPrompt: &systemPrompt,
		Visibility:   agents.VisibilityExternal,
		Skills:       []string{},
		BannedTools:  []string{},
	}
	if createErr := agentRepo.CreateDefinition(s.ctx, extDef); createErr != nil {
		// May already exist from a prior run — look it up.
		existing, lookupErr := agentRepo.FindDefinitionByName(s.ctx, s.projectID, extAgentName)
		s.Require().NoError(lookupErr, "look up existing test agent")
		s.Require().NotNil(existing, fmt.Sprintf("test agent definition must exist (create err: %v)", createErr))
	}
	s.agentName = extAgentName

	s.inProcess = testutil.NewTestServerWithLLM(s.testDB)
	s.client = testutil.NewHTTPClient(s.inProcess.Echo)
}

func (s *AgentCompatTestSuite) TearDownTest() {
	if s.inProcess != nil && s.inProcess.StopFn != nil {
		s.inProcess.StopFn()
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) hasLLM() bool {
	testutil.LoadEnvFiles()
	for _, k := range []string{"DEEPSEEK_API_KEY", "OPENAI_API_KEY", "GOOGLE_API_KEY"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

func (s *AgentCompatTestSuite) requireLLM() {
	s.T().Helper()
	if !s.hasLLM() {
		s.T().Skip("no LLM provider key configured — skipping LLM-dependent test")
	}
}

func (s *AgentCompatTestSuite) postChat(body map[string]any) *testutil.HTTPResponse {
	return s.client.POST(
		"/v1/chat/completions",
		testutil.WithAuth(s.authToken),
		testutil.WithProjectID(s.projectID),
		testutil.WithJSONBody(body),
	)
}

func (s *AgentCompatTestSuite) postChatSSE(body map[string]any) *testutil.SSEResponse {
	return s.client.PostSSE(
		"/v1/chat/completions",
		testutil.WithAuth(s.authToken),
		testutil.WithProjectID(s.projectID),
		testutil.WithJSONBody(body),
	)
}

func (s *AgentCompatTestSuite) getModels() *testutil.HTTPResponse {
	return s.client.GET(
		"/v1/models",
		testutil.WithAuth(s.authToken),
		testutil.WithProjectID(s.projectID),
	)
}

// parseChatResponse unmarshals a non-streaming response body into the DTO.
func parseChatResponse(t *testing.T, body []byte) *agentcompat.ChatCompletionResponse {
	t.Helper()
	var resp agentcompat.ChatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("parseChatResponse: %v — body: %s", err, body)
	}
	return &resp
}

// parseAPIError unmarshals an error response.
func parseAPIError(t *testing.T, body []byte) *agentcompat.APIError {
	t.Helper()
	var e agentcompat.APIError
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("parseAPIError: %v — body: %s", err, body)
	}
	return &e
}

// assertValidChatResponse verifies the required fields of a non-streaming
// OpenAI-compatible chat completion response.
func (s *AgentCompatTestSuite) assertValidChatResponse(body []byte, model string) *agentcompat.ChatCompletionResponse {
	s.T().Helper()
	resp := parseChatResponse(s.T(), body)

	s.NotEmpty(resp.ID, "id must be non-empty")
	s.Equal("chat.completion", resp.Object)
	s.Greater(resp.Created, int64(0), "created timestamp must be positive")
	s.Equal(model, resp.Model)
	s.Require().NotEmpty(resp.Choices, "choices must be non-empty")

	c := resp.Choices[0]
	s.Equal(0, c.Index)
	s.Equal("assistant", c.Message.Role, "message role must be assistant")
	validFinish := map[string]bool{"stop": true, "length": true, "tool_calls": true}
	s.True(validFinish[c.FinishReason],
		fmt.Sprintf("finish_reason %q must be stop|length|tool_calls", c.FinishReason))

	return resp
}

// assertValidSSEStream verifies the structure of an OpenAI-compatible SSE
// streaming response: opening delta with role, at least one terminal chunk
// with finish_reason, and [DONE] sentinel.
func (s *AgentCompatTestSuite) assertValidSSEStream(sse *testutil.SSEResponse, model string) {
	s.T().Helper()
	s.Equal(http.StatusOK, sse.StatusCode)
	s.Contains(sse.ContentType, "text/event-stream")
	s.NotEmpty(sse.Events, "must have at least one SSE event")

	// Find the [DONE] sentinel.
	hasDone := false
	for _, ev := range sse.Events {
		if ev.Data == "[DONE]" {
			hasDone = true
			break
		}
	}
	s.True(hasDone, "SSE stream must end with [DONE]")

	// First data event must be an opening delta with role:"assistant".
	var firstChunk agentcompat.ChatCompletionChunk
	for _, ev := range sse.Events {
		if ev.Data == "[DONE]" || ev.Data == "" {
			continue
		}
		if err := json.Unmarshal([]byte(ev.Data), &firstChunk); err == nil {
			break
		}
	}
	s.Equal("chat.completion.chunk", firstChunk.Object)
	s.Equal(model, firstChunk.Model)
	s.Require().NotEmpty(firstChunk.Choices, "first chunk must have choices")
	s.Equal("assistant", firstChunk.Choices[0].Delta.Role, "opening delta must have role=assistant")

	// At least one chunk must carry a non-nil finish_reason.
	hasFinish := false
	for _, ev := range sse.Events {
		if ev.Data == "[DONE]" || ev.Data == "" {
			continue
		}
		var chunk agentcompat.ChatCompletionChunk
		if err := json.Unmarshal([]byte(ev.Data), &chunk); err != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				hasFinish = true
			}
		}
	}
	s.True(hasFinish, "at least one chunk must have a non-empty finish_reason")
}

// ---------------------------------------------------------------------------
// Test 1 — GET /v1/models
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) TestListModels() {
	resp := s.getModels()
	s.Equal(http.StatusOK, resp.StatusCode)

	var list agentcompat.ModelList
	s.Require().NoError(json.Unmarshal(resp.Body, &list))

	s.Equal("list", list.Object)
	s.NotEmpty(list.Data, "at least one agent definition must appear in /v1/models")

	// Every model must have the required fields.
	for _, m := range list.Data {
		s.NotEmpty(m.ID, "model id must be non-empty")
		s.Equal("model", m.Object)
		s.Greater(m.Created, int64(0))
		s.Equal("memory", m.OwnedBy)
		// IDs use "agent:<name>" format.
		s.True(strings.HasPrefix(m.ID, "agent:"),
			fmt.Sprintf("model id %q must have 'agent:' prefix", m.ID))
	}
}

// ---------------------------------------------------------------------------
// Test 2 — POST /v1/chat/completions non-streaming
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) TestChatCompletion_NonStreaming() {
	resp := s.postChat(map[string]any{
		"model":    s.agentName,
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	})
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.Body)

	cr := s.assertValidChatResponse(resp.Body, s.agentName)
	s.T().Logf("finish_reason: %s", cr.Choices[0].FinishReason)
}

// Test with the "agent:<name>" prefix variant.
func (s *AgentCompatTestSuite) TestChatCompletion_AgentPrefix() {
	model := "agent:" + s.agentName
	resp := s.postChat(map[string]any{
		"model":    model,
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	})
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.Body)
	s.assertValidChatResponse(resp.Body, model)
}

// ---------------------------------------------------------------------------
// Test 3 — POST /v1/chat/completions streaming
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) TestChatCompletion_Streaming() {
	sse := s.postChatSSE(map[string]any{
		"model":    s.agentName,
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
		"stream":   true,
	})
	s.T().Logf("raw SSE body:\n%s", sse.RawBody)
	s.assertValidSSEStream(sse, s.agentName)
}

// ---------------------------------------------------------------------------
// Test 4 — No auth → 401
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) TestChatCompletion_NoAuth() {
	resp := s.client.POST(
		"/v1/chat/completions",
		testutil.WithProjectID(s.projectID),
		testutil.WithJSONBody(map[string]any{
			"model":    s.agentName,
			"messages": []map[string]any{{"role": "user", "content": "hello"}},
		}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// Also test that GET /v1/models requires auth.
func (s *AgentCompatTestSuite) TestListModels_NoAuth() {
	resp := s.client.GET("/v1/models", testutil.WithProjectID(s.projectID))
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// Test 5 — Empty model → 400
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) TestChatCompletion_EmptyModel() {
	resp := s.postChat(map[string]any{
		"model":    "",
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	e := parseAPIError(s.T(), resp.Body)
	s.NotEmpty(e.Error.Message)
}

// ---------------------------------------------------------------------------
// Test 6 — Non-existent model → 400
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) TestChatCompletion_NonExistentModel() {
	resp := s.postChat(map[string]any{
		"model":    "agent:does-not-exist-xyz",
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	e := parseAPIError(s.T(), resp.Body)
	s.Contains(strings.ToLower(e.Error.Message), "not found",
		"error message should mention 'not found'")
}

// ---------------------------------------------------------------------------
// Test 7 — Reserved tool name (memory_* prefix) → 400
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) TestChatCompletion_ReservedToolPrefix() {
	resp := s.postChat(map[string]any{
		"model":    s.agentName,
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "memory_search_knowledge",
					"description": "try to use a reserved name",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	e := parseAPIError(s.T(), resp.Body)
	s.Contains(e.Error.Message, "reserved",
		"error message should mention the reserved prefix")
}

// ---------------------------------------------------------------------------
// Test 8 — Valid client tool definition accepted (no LLM required)
// ---------------------------------------------------------------------------

// TestChatCompletion_ClientToolDef verifies that a request carrying a valid
// client-tool definition is accepted (returns 200, not a validation error).
// The agent may or may not call the tool depending on whether an LLM is
// available; what we assert is that the response is a valid completion shape.
func (s *AgentCompatTestSuite) TestChatCompletion_ClientToolDef() {
	resp := s.postChat(map[string]any{
		"model":    s.agentName,
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "my_custom_tool",
					"description": "A test tool for the test suite",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{},
					},
				},
			},
		},
	})
	// Must not be a validation error (400); 200 means the tool def was accepted.
	s.Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.Body)
	s.assertValidChatResponse(resp.Body, s.agentName)
}

// ---------------------------------------------------------------------------
// Test 9 — Empty messages → 400
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) TestChatCompletion_EmptyMessages() {
	resp := s.postChat(map[string]any{
		"model":    s.agentName,
		"messages": []map[string]any{},
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	e := parseAPIError(s.T(), resp.Body)
	s.NotEmpty(e.Error.Message)
}

// ---------------------------------------------------------------------------
// Test 10 — Missing project ID → no project context → error
// ---------------------------------------------------------------------------

func (s *AgentCompatTestSuite) TestChatCompletion_NoProjectID() {
	// Only meaningful in-process; external tokens may be project-scoped already.
	if s.external {
		s.T().Skip("skipping in external mode — token may already carry project ID")
	}

	resp := s.client.POST(
		"/v1/chat/completions",
		testutil.WithAuth(s.authToken),
		// intentionally no WithProjectID
		testutil.WithJSONBody(map[string]any{
			"model":    s.agentName,
			"messages": []map[string]any{{"role": "user", "content": "hello"}},
		}),
	)
	// Service returns an error when project ID is missing.
	s.Equal(http.StatusBadRequest, resp.StatusCode, "body: %s", resp.Body)
}

// ---------------------------------------------------------------------------
// Test 11 (LLM) — Client tool roundtrip: POST → tool_calls → resume → stop
// ---------------------------------------------------------------------------

// TestChatCompletion_ClientToolRoundtrip exercises the full suspend/resume flow:
//  1. POST with a client tool definition and a message that causes the agent to call it.
//  2. Response has finish_reason:"tool_calls" + system_fingerprint:"run_<id>".
//  3. POST again with the system_fingerprint + a tool result message → final "stop".
//
// This test is skipped when no LLM is configured.
func (s *AgentCompatTestSuite) TestChatCompletion_ClientToolRoundtrip() {
	s.requireLLM()

	toolName := "get_current_time"
	// Craft a message that strongly suggests to the agent to call the tool.
	userMsg := fmt.Sprintf(
		"Please call the %s tool now and tell me what time it returns.", toolName)

	// Step 1 — initial request with a client tool.
	resp := s.postChat(map[string]any{
		"model": s.agentName,
		"messages": []map[string]any{
			{"role": "user", "content": userMsg},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        toolName,
					"description": "Returns the current UTC time as a string.",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	})
	s.Require().Equal(http.StatusOK, resp.StatusCode, "step1 body: %s", resp.Body)

	cr := parseChatResponse(s.T(), resp.Body)
	if cr.Choices[0].FinishReason != "tool_calls" {
		// Agent chose not to call the tool — still a valid run, just not a roundtrip.
		s.T().Logf("agent did not call the tool (finish_reason=%s); roundtrip skipped",
			cr.Choices[0].FinishReason)
		return
	}

	// Step 2 — verify we got a system_fingerprint with the run ID.
	s.Require().NotEmpty(cr.SystemFingerprint, "paused run must carry system_fingerprint")
	s.True(strings.HasPrefix(cr.SystemFingerprint, "run_"),
		fmt.Sprintf("system_fingerprint %q must start with 'run_'", cr.SystemFingerprint))
	s.Require().NotEmpty(cr.Choices[0].Message.ToolCalls,
		"paused response must include tool_calls")

	tc := cr.Choices[0].Message.ToolCalls[0]
	s.Equal(toolName, tc.Function.Name)
	s.T().Logf("tool call id=%s args=%s", tc.ID, tc.Function.Arguments)

	// Step 3 — resume: POST the tool result back.
	resumeResp := s.postChat(map[string]any{
		"model":              s.agentName,
		"system_fingerprint": cr.SystemFingerprint, // echo back the run ID
		"messages": []map[string]any{
			{"role": "user", "content": userMsg},
			{"role": "assistant", "tool_calls": cr.Choices[0].Message.ToolCalls},
			{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      `{"time":"2026-06-09T12:00:00Z"}`,
			},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        toolName,
					"description": "Returns the current UTC time as a string.",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	})
	s.Require().Equal(http.StatusOK, resumeResp.StatusCode, "step3 (resume) body: %s", resumeResp.Body)

	finalCR := parseChatResponse(s.T(), resumeResp.Body)
	s.T().Logf("final finish_reason: %s", finalCR.Choices[0].FinishReason)
	// After resume the run should complete (stop or length — not tool_calls again).
	s.NotEqual("tool_calls", finalCR.Choices[0].FinishReason,
		"resumed run must not re-pause on the same tool immediately")
}

// ---------------------------------------------------------------------------
// Test 12 (LLM) — streaming response with actual content
// ---------------------------------------------------------------------------

// TestChatCompletion_StreamingWithContent verifies that when a real LLM is
// configured the streaming response contains actual text delta chunks.
func (s *AgentCompatTestSuite) TestChatCompletion_StreamingWithContent() {
	s.requireLLM()

	sse := s.postChatSSE(map[string]any{
		"model": s.agentName,
		"messages": []map[string]any{
			{"role": "user", "content": "Reply with the single word: hello"},
		},
		"stream": true,
	})
	s.T().Logf("raw SSE body:\n%s", sse.RawBody)
	s.assertValidSSEStream(sse, s.agentName)

	// Collect all text content from delta chunks.
	var content strings.Builder
	for _, ev := range sse.Events {
		if ev.Data == "[DONE]" || ev.Data == "" {
			continue
		}
		var chunk agentcompat.ChatCompletionChunk
		if err := json.Unmarshal([]byte(ev.Data), &chunk); err != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != nil {
				content.WriteString(*ch.Delta.Content)
			}
		}
	}
	s.T().Logf("full streamed content: %q", content.String())
	s.NotEmpty(content.String(), "LLM should produce at least some text content")
}
