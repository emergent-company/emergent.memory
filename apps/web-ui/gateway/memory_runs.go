package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

// --- agent run transcripts (kb.agent_runs + messages + tool calls) ---

// AgentRunMessage mirrors memory's AgentRunMessageDTO: one LLM message during
// an agent run. Content is a JSON object whose keys vary by producer — ADK
// runs store {"text", "function_calls", "function_responses"}, and some
// producers emit an ADK/ACP-style "parts" array.
type AgentRunMessage struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	Role       string         `json:"role"`
	Content    map[string]any `json:"content"`
	StepNumber int            `json:"stepNumber"`
	CreatedAt  string         `json:"createdAt"`
}

// AgentRunToolCall mirrors memory's AgentRunToolCallDTO: one tool invocation
// during an agent run.
type AgentRunToolCall struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	ToolName   string         `json:"toolName"`
	Input      map[string]any `json:"input"`
	Output     map[string]any `json:"output"`
	Status     string         `json:"status"`
	StepNumber int            `json:"stepNumber"`
	CreatedAt  string         `json:"createdAt"`
}

// AgentRunFull mirrors memory's AgentRunFullDTO — the response of
// GET /agent-runs/:runId/full: the run bundled with its transcript.
type AgentRunFull struct {
	Run       *ScheduledAgentRun  `json:"run"`
	Messages  []*AgentRunMessage  `json:"messages"`
	ToolCalls []*AgentRunToolCall `json:"toolCalls"`
	ParentRun *ScheduledAgentRun  `json:"parentRun,omitempty"`
}

// GetRunFull fetches one run's full transcript (run + messages + tool calls).
func (m *MemoryClient) GetRunFull(ctx context.Context, runID string) (*AgentRunFull, error) {
	var env successEnvelope[AgentRunFull]
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/agent-runs/"+url.PathEscape(runID)+"/full", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// GetRunQuestions lists one run's agent questions (newest first). The response
// field is nil for unanswered questions.
func (m *MemoryClient) GetRunQuestions(ctx context.Context, runID string) ([]AgentQuestionItem, error) {
	var env successEnvelope[[]AgentQuestionItem]
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/agent-runs/"+url.PathEscape(runID)+"/questions", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// --- agent run spans (session viewer trace waterfall) ---

// AgentRun is the subset of memory's run DTO (GET /agent-runs/:runId) the
// gateway needs: the OpenTelemetry trace id (when the run was traced) and the
// run's flattened trace spans, which memory now returns inline in the DTO.
type AgentRun struct {
	TraceID string      `json:"traceId,omitempty"`
	Spans   []TraceSpan `json:"spans,omitempty"`
}

// TraceSpan is one span of a run's trace, flattened by memory for the session
// viewer's waterfall. parentSpanId is empty for the root span; times are Unix
// nanoseconds; inputTokens/outputTokens are 0 when absent.
type TraceSpan struct {
	SpanID        string `json:"spanId"`
	ParentSpanID  string `json:"parentSpanId"`
	Name          string `json:"name"`
	StartUnixNano int64  `json:"startUnixNano"`
	EndUnixNano   int64  `json:"endUnixNano"`
	InputTokens   int64  `json:"inputTokens"`
	OutputTokens  int64  `json:"outputTokens"`
	Model         string `json:"model"`
}

// GetAgentRun fetches one agent run and returns its DTO. Agent-run responses
// are wrapped in the {success,data,error} envelope like the agent-definition
// routes.
func (m *MemoryClient) GetAgentRun(ctx context.Context, runID string) (*AgentRun, error) {
	var env successEnvelope[AgentRun]
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/agent-runs/"+url.PathEscape(runID), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// --- chat ---

type ChatRequest struct {
	AgentDefinitionID string `json:"agentDefinitionId,omitempty"`
	ConversationID    string `json:"conversationId,omitempty"`
	Message           string `json:"message"`
	CanonicalID       string `json:"canonicalId,omitempty"`
}

// ChatStream opens a streaming chat request and returns the raw SSE body.
// The caller owns the returned body and must close it.
func (m *MemoryClient) ChatStream(ctx context.Context, req ChatRequest) (io.ReadCloser, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/api/chat/stream", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	hreq.Header.Set("Content-Type", "application/json")
	for k, v := range sessionHeaders(ctx) {
		hreq.Header.Set(k, v)
	}
	resp, err := m.streamHTTP.Do(hreq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		err := parseMemoryError(resp.StatusCode, raw)
		captureMemoryError(http.MethodPost, "/api/chat/stream", resp.StatusCode, err)
		return nil, err
	}
	return resp.Body, nil
}

// RespondQuestionResult is the subset of the memory respond response the
// gateway relays to the client.
type RespondQuestionResult struct {
	QuestionID  string `json:"questionId"`
	ResumeRunID string `json:"resumeRunId"`
	Status      string `json:"status"`
}

// RespondQuestion answers a pending agent question, resuming the paused run in
// the background. Memory returns JSON (202) with the resume run id — NOT an
// SSE stream — so the resumed output is read later from conversation history.
func (m *MemoryClient) RespondQuestion(ctx context.Context, questionID, response, message string) (*RespondQuestionResult, error) {
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			ID          string `json:"id"`
			ResumeRunID string `json:"resumeRunId"`
			Status      string `json:"status"`
		} `json:"data"`
	}
	body := map[string]string{"response": response}
	if message != "" {
		body["message"] = message
	}
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/agent-questions/" + url.PathEscape(questionID) + "/respond"
	if err := m.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return &RespondQuestionResult{
		QuestionID:  out.Data.ID,
		ResumeRunID: out.Data.ResumeRunID,
		Status:      out.Data.Status,
	}, nil
}

// CancelQuestion revokes a pending agent question (tool-policy confirmation),
// resuming the paused run with the call treated as not taken.
func (m *MemoryClient) CancelQuestion(ctx context.Context, questionID string) (*RespondQuestionResult, error) {
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			ID          string `json:"id"`
			ResumeRunID string `json:"resumeRunId"`
			Status      string `json:"status"`
		} `json:"data"`
	}
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/agent-questions/" + url.PathEscape(questionID) + "/cancel"
	if err := m.do(ctx, http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &RespondQuestionResult{
		QuestionID:  out.Data.ID,
		ResumeRunID: out.Data.ResumeRunID,
		Status:      out.Data.Status,
	}, nil
}

// AgentQuestionOption mirrors memory's AgentQuestionOption: one selectable
// option in an ask_user question.
type AgentQuestionOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// AgentQuestionItem mirrors memory's AgentQuestionDTO for the fields the
// gateway needs: to annotate transcript tool calls with the user's answer and
// to render pending questions on the run page. Response is nil while the
// question is unanswered.
type AgentQuestionItem struct {
	ID              string                `json:"id"`
	RunID           string                `json:"runId"`
	Question        string                `json:"question"`
	Options         []AgentQuestionOption `json:"options"`
	InteractionType string                `json:"interactionType"`
	Placeholder     string                `json:"placeholder,omitempty"`
	Status          string                `json:"status"`
	Response        *string               `json:"response"`
	ResumeRunID     *string               `json:"resumeRunId"`
}

// ListAgentQuestions returns all agent questions for the project (newest
// first). The response field is nil for unanswered questions.
func (m *MemoryClient) ListAgentQuestions(ctx context.Context) ([]AgentQuestionItem, error) {
	var out struct {
		Success bool                `json:"success"`
		Data    []AgentQuestionItem `json:"data"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+url.PathEscape(m.projectIDFor(ctx))+"/agent-questions", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ToolApprovalItem is one human-in-the-loop tool-approval audit record.
type ToolApprovalItem struct {
	ID             string         `json:"id"`
	RunID          string         `json:"runId"`
	AgentID        string         `json:"agentId"`
	ProjectID      string         `json:"projectId"`
	QuestionID     string         `json:"questionId"`
	ToolName       string         `json:"toolName"`
	ConversationID string         `json:"conversationId,omitempty"`
	ArgsSummary    map[string]any `json:"argsSummary"`
	Decision       string         `json:"decision"`
	Message        string         `json:"message,omitempty"`
	DecidedBy      *string        `json:"decidedBy,omitempty"`
	DecidedAt      *time.Time     `json:"decidedAt,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// ListToolApprovals returns the tool-approval audit trail for the project
// (newest first).
func (m *MemoryClient) ListToolApprovals(ctx context.Context) ([]ToolApprovalItem, error) {
	var out struct {
		Success bool               `json:"success"`
		Data    []ToolApprovalItem `json:"data"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+url.PathEscape(m.projectIDFor(ctx))+"/agent-approvals", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
