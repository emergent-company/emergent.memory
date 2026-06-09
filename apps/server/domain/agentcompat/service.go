package agentcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Service implements the OpenAI-compatible agent layer.
// It wraps the existing agent executor and repository to expose Memory agents
// via the /v1/chat/completions and /v1/models endpoints.
type Service struct {
	agentRepo *agents.Repository
	executor  *agents.AgentExecutor
	log       *slog.Logger
}

// NewService constructs a Service.
func NewService(agentRepo *agents.Repository, executor *agents.AgentExecutor, log *slog.Logger) *Service {
	return &Service{agentRepo: agentRepo, executor: executor, log: log}
}

// ─── Result ───────────────────────────────────────────────────────────────

// Result is the internal outcome of a HandleChatCompletion call.
// Either StreamEvents is non-nil (streaming) or Response is set (non-streaming).
// When PausedForClientTool is true, the run is suspended awaiting a client tool
// result; ResumeRunID carries the run ID to echo back in system_fingerprint.
type Result struct {
	// Non-streaming final response.
	Response *ChatCompletionResponse

	// Streaming: receives events in order, closed when done or paused.
	// The handler reads from this channel and writes SSE chunks.
	StreamEvents <-chan StreamEvent

	// When the run was suspended to call a client tool, this is set.
	PausedForClientTool bool
	ResumeRunID         string
	PendingToolCalls    []ToolCall
}

// StreamEvent is the internal event type passed over the Result.StreamEvents channel.
type StreamEvent struct {
	// Exactly one of the following is set per event.
	TextDelta       string     // incremental LLM text token
	ClientToolCalls []ToolCall // agent wants the client to execute these tools
	Done            bool       // stream finished (last event, check Usage)
	Usage           *Usage
}

// ─── Public API ───────────────────────────────────────────────────────────

// HandleChatCompletion is the main entry point for POST /v1/chat/completions.
func (s *Service) HandleChatCompletion(ctx context.Context, req *ChatCompletionRequest, user *auth.AuthUser) (*Result, error) {
	projectID := effectiveProjectID(user)
	if projectID == "" {
		return nil, fmt.Errorf("project ID required: set X-Project-ID header or use a project-scoped API token")
	}

	// Resolve agent definition from model name.
	agentName := parseModelName(req.Model)
	if agentName == "" {
		return nil, fmt.Errorf("invalid model %q: expected format 'agent:<name>' or bare agent name", req.Model)
	}

	agentDef, err := s.agentRepo.FindDefinitionByName(ctx, projectID, agentName)
	if err != nil {
		return nil, fmt.Errorf("agent %q lookup failed: %w", agentName, err)
	}
	if agentDef == nil {
		return nil, fmt.Errorf("model %q not found: no agent definition named %q exists in this project", req.Model, agentName)
	}

	// Validate client tools (check for reserved memory_ prefix).
	if conflict := ValidateClientTools(req.Tools); conflict != "" {
		return nil, fmt.Errorf("invalid tools: %s", conflict)
	}

	// If system_fingerprint starts with "run_", this is a resume request.
	if runID, ok := parseResumeToken(req.SystemFingerprint); ok {
		return s.handleResume(ctx, runID, agentDef, req, user, projectID)
	}

	return s.handleNewRun(ctx, agentDef, req, user, projectID)
}

// HandleModelList returns GET /v1/models — all external agent definitions as
// OpenAI "model" objects.
func (s *Service) HandleModelList(ctx context.Context, user *auth.AuthUser) (*ModelList, error) {
	projectID := effectiveProjectID(user)
	if projectID == "" {
		return &ModelList{Object: "list", Data: []Model{}}, nil
	}

	defs, err := s.agentRepo.FindExternalAgentDefinitions(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}

	models := make([]Model, 0, len(defs))
	for _, d := range defs {
		models = append(models, Model{
			ID:      "agent:" + d.Name,
			Object:  "model",
			Created: d.CreatedAt.Unix(),
			OwnedBy: "memory",
		})
	}
	return &ModelList{Object: "list", Data: models}, nil
}

// ─── New run ──────────────────────────────────────────────────────────────

func (s *Service) handleNewRun(
	ctx context.Context,
	agentDef *agents.AgentDefinition,
	req *ChatCompletionRequest,
	user *auth.AuthUser,
	projectID string,
) (*Result, error) {
	// Find or create the runtime agent record.
	// The kb.agent_runs table requires a valid agent_id, so we ensure a
	// runtime agent row exists that is backed by this definition.
	agent, err := s.agentRepo.FindByName(ctx, projectID, agentDef.Name)
	if err != nil {
		return nil, fmt.Errorf("agent record lookup failed: %w", err)
	}
	if agent == nil {
		// Create a lightweight runtime agent for this definition.
		agent = &agents.Agent{
			ProjectID:    projectID,
			Name:         agentDef.Name,
			StrategyType: "agent-def:" + agentDef.ID,
			CronSchedule: "0 0 * * *", // required by schema, not used
			TriggerType:  "manual",
		}
		if createErr := s.agentRepo.Create(ctx, agent); createErr != nil {
			return nil, fmt.Errorf("failed to create runtime agent for definition %q: %w", agentDef.Name, createErr)
		}
	}

	userMsg := buildUserMessage(req.Messages)

	// State shared between the tool closure and the stream handler.
	var clientPause clientPauseState

	// Build per-request client tools as ADK tool.Tool values that signal a pause
	// when the LLM calls them.
	extraTools, err := buildClientTools(req.Tools, &clientPause)
	if err != nil {
		return nil, fmt.Errorf("building client tools: %w", err)
	}

	execReq := agents.ExecuteRequest{
		Agent:                agent,
		AgentDefinition:      agentDef,
		ProjectID:            projectID,
		OrgID:                user.OrgID,
		UserMessage:          userMsg,
		SystemPromptAppendix: SystemPromptAppendix(len(req.Tools) > 0),
		UserID:               user.ID,
		ExtraTools:           extraTools,
	}

	if req.Stream {
		return s.executeStreaming(ctx, execReq, req, &clientPause)
	}
	return s.executeSync(ctx, execReq, req, &clientPause)
}

// ─── Resume ───────────────────────────────────────────────────────────────

func (s *Service) handleResume(
	ctx context.Context,
	runID string,
	agentDef *agents.AgentDefinition,
	req *ChatCompletionRequest,
	user *auth.AuthUser,
	projectID string,
) (*Result, error) {
	priorRun, err := s.agentRepo.FindRunByIDProjectScoped(ctx, runID, projectID)
	if err != nil {
		return nil, fmt.Errorf("run %q not found: %w", runID, err)
	}
	if priorRun.Status != agents.RunStatusPaused {
		return nil, fmt.Errorf("run %q is not paused (status: %s)", runID, priorRun.Status)
	}

	// Extract tool result from messages: look for the last tool-role message.
	toolResult, err := extractToolResult(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("tool result missing: %w", err)
	}

	// Encode result as JSON so injectToolResponse can use it via UserMessage.
	resultJSON, err := json.Marshal(toolResult)
	if err != nil {
		return nil, fmt.Errorf("encoding tool result: %w", err)
	}

	var clientPause clientPauseState
	extraTools, err := buildClientTools(req.Tools, &clientPause)
	if err != nil {
		return nil, fmt.Errorf("building client tools: %w", err)
	}

	resumeReq := agents.ExecuteRequest{
		ProjectID:            projectID,
		OrgID:                user.OrgID,
		UserMessage:          string(resultJSON),
		SystemPromptAppendix: SystemPromptAppendix(len(req.Tools) > 0),
		UserID:               user.ID,
		ExtraTools:           extraTools,
	}

	if req.Stream {
		return s.resumeStreaming(ctx, priorRun, resumeReq, req, &clientPause)
	}
	return s.resumeSync(ctx, priorRun, resumeReq, req, &clientPause)
}

// ─── Sync execution ───────────────────────────────────────────────────────

func (s *Service) executeSync(
	ctx context.Context,
	execReq agents.ExecuteRequest,
	req *ChatCompletionRequest,
	pause *clientPauseState,
) (*Result, error) {
	var textBuf strings.Builder

	execReq.StreamCallback = func(ev agents.StreamEvent) {
		if ev.Type == agents.StreamEventTextDelta {
			textBuf.WriteString(ev.Text)
		}
	}

	result, err := s.executor.Execute(ctx, execReq)
	if err != nil {
		return nil, err
	}

	// If run paused for client tool — build a tool_calls response.
	if result.Status == agents.RunStatusPaused && pause.hasPendingCall() {
		return s.buildClientToolCallResult(result.RunID, pause), nil
	}

	return &Result{Response: s.buildResponse(req.Model, textBuf.String(), result)}, nil
}

func (s *Service) resumeSync(
	ctx context.Context,
	priorRun *agents.AgentRun,
	resumeReq agents.ExecuteRequest,
	req *ChatCompletionRequest,
	pause *clientPauseState,
) (*Result, error) {
	var textBuf strings.Builder

	resumeReq.StreamCallback = func(ev agents.StreamEvent) {
		if ev.Type == agents.StreamEventTextDelta {
			textBuf.WriteString(ev.Text)
		}
	}

	result, err := s.executor.Resume(ctx, priorRun, resumeReq)
	if err != nil {
		return nil, err
	}

	if result.Status == agents.RunStatusPaused && pause.hasPendingCall() {
		return s.buildClientToolCallResult(result.RunID, pause), nil
	}

	return &Result{Response: s.buildResponse(req.Model, textBuf.String(), result)}, nil
}

// ─── Streaming execution ──────────────────────────────────────────────────

func (s *Service) executeStreaming(
	ctx context.Context,
	execReq agents.ExecuteRequest,
	req *ChatCompletionRequest,
	pause *clientPauseState,
) (*Result, error) {
	ch := make(chan StreamEvent, 64)

	execReq.StreamCallback = buildStreamCallback(ch, req.Tools, pause)

	go func() {
		defer close(ch)
		result, err := s.executor.Execute(ctx, execReq)
		if err != nil {
			s.log.Warn("agentcompat: streaming execute error", slog.String("error", err.Error()))
			return
		}
		// Final event: either done or paused-for-client-tool.
		if result.Status == agents.RunStatusPaused && pause.hasPendingCall() {
			// tool_calls are already streamed by buildStreamCallback
			ch <- StreamEvent{Done: true, Usage: nil}
		} else {
			ch <- StreamEvent{Done: true}
		}
	}()

	return &Result{StreamEvents: ch}, nil
}

func (s *Service) resumeStreaming(
	ctx context.Context,
	priorRun *agents.AgentRun,
	resumeReq agents.ExecuteRequest,
	req *ChatCompletionRequest,
	pause *clientPauseState,
) (*Result, error) {
	ch := make(chan StreamEvent, 64)

	resumeReq.StreamCallback = buildStreamCallback(ch, req.Tools, pause)

	go func() {
		defer close(ch)
		result, err := s.executor.Resume(ctx, priorRun, resumeReq)
		if err != nil {
			s.log.Warn("agentcompat: streaming resume error", slog.String("error", err.Error()))
			return
		}
		if result.Status == agents.RunStatusPaused && pause.hasPendingCall() {
			ch <- StreamEvent{Done: true}
		} else {
			ch <- StreamEvent{Done: true}
		}
	}()

	return &Result{StreamEvents: ch}, nil
}

// buildStreamCallback returns a StreamCallback that converts agent events into
// StreamEvent values sent over ch.  Internal (memory_*) tool calls are filtered
// out; client tool calls are surfaced as ClientToolCalls events.
func buildStreamCallback(ch chan<- StreamEvent, clientTools []ClientToolDef, pause *clientPauseState) agents.StreamCallback {
	return func(ev agents.StreamEvent) {
		switch ev.Type {
		case agents.StreamEventTextDelta:
			if ev.Text != "" {
				ch <- StreamEvent{TextDelta: ev.Text}
			}

		case agents.StreamEventToolCallStart:
			// Client tool calls are signalled via the pause state when the tool
			// function runs.  We stream them once the pause is detected (after
			// StreamEventToolCallEnd fires) so we have the full args.

		case agents.StreamEventToolCallEnd:
			// Check whether the pause state has been set for a client tool.
			if pause.hasPendingCall() {
				argsJSON, _ := json.Marshal(pause.args())
				calls := []ToolCall{{
					ID:   pause.callID(),
					Type: "function",
					Function: FunctionCall{
						Name:      pause.toolName(),
						Arguments: string(argsJSON),
					},
				}}
				ch <- StreamEvent{ClientToolCalls: calls}
			}
		}
	}
}

// ─── Client tool injection ────────────────────────────────────────────────

// clientPauseState is shared between the client tool closure and the stream
// callback.  When the LLM calls a client-supplied tool, the closure sets the
// fields here and the executor pauses via AskPauseState.
type clientPauseState struct {
	mu      sync.Mutex
	pending bool
	id      string
	name    string
	rawArgs map[string]any
}

func (s *clientPauseState) set(id, name string, args map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = true
	s.id = id
	s.name = name
	s.rawArgs = args
}

func (s *clientPauseState) hasPendingCall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

func (s *clientPauseState) callID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

func (s *clientPauseState) toolName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

func (s *clientPauseState) args() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rawArgs
}

// buildClientTools converts each ClientToolDef into an ADK tool.Tool whose
// handler records the call in pause and returns a sentinel asking the executor
// to suspend.  The executor sees the tool return normally — the actual
// suspension is driven by the suspend_context set on the run before the
// tool returns.
func buildClientTools(defs []ClientToolDef, pause *clientPauseState) ([]tool.Tool, error) {
	tools := make([]tool.Tool, 0, len(defs))
	for _, def := range defs {
		d := def // capture
		inputSchema := clientToolInputSchema(d.Function.Parameters)

		schema := inputSchema // capture for closure
		t, err := functiontool.New(
			functiontool.Config{
				Name:        d.Function.Name,
				Description: d.Function.Description,
				InputSchema: schema,
			},
			func(ctx tool.Context, args map[string]any) (map[string]any, error) {
				// Generate a tool call ID (OpenAI format: "call_<name>_<ts>").
				callID := fmt.Sprintf("call_%s_%d", sanitizeName(d.Function.Name), time.Now().UnixMilli()%1_000_000)
				pause.set(callID, d.Function.Name, args)
				// Return a sentinel that will be overwritten once the client
				// sends the real result on resume.
				return map[string]any{
					"_client_tool": true,
					"call_id":      callID,
					"status":       "pending",
				}, nil
			},
		)
		if err != nil {
			return nil, fmt.Errorf("building client tool %q: %w", d.Function.Name, err)
		}
		tools = append(tools, t)
	}
	return tools, nil
}

// clientToolInputSchema converts a raw JSON Schema (from the client) into
// an ADK *jsonschema.Schema.  Unknown fields are silently dropped; if the
// schema cannot be parsed a minimal object schema is returned.
func clientToolInputSchema(params json.RawMessage) *jsonschema.Schema {
	empty := &jsonschema.Schema{
		Type:       "object",
		Properties: map[string]*jsonschema.Schema{},
	}
	if len(params) == 0 {
		return empty
	}

	// Unmarshal into the jsonschema struct directly.
	var s jsonschema.Schema
	if err := json.Unmarshal(params, &s); err != nil {
		return empty
	}
	return &s
}

// ─── Response builders ────────────────────────────────────────────────────

func (s *Service) buildResponse(model, text string, result *agents.ExecuteResult) *ChatCompletionResponse {
	stop := "stop"
	if result != nil && result.Status == agents.RunStatusError {
		stop = "length"
	}
	return &ChatCompletionResponse{
		ID:      "chatcmpl-" + shortID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{
			Index: 0,
			Message: ChatMessage{
				Role:    "assistant",
				Content: text,
			},
			FinishReason: stop,
		}},
	}
}

func (s *Service) buildClientToolCallResult(runID string, pause *clientPauseState) *Result {
	argsJSON, _ := json.Marshal(pause.args())
	calls := []ToolCall{{
		ID:   pause.callID(),
		Type: "function",
		Function: FunctionCall{
			Name:      pause.toolName(),
			Arguments: string(argsJSON),
		},
	}}

	finishReason := "tool_calls"
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-" + shortID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "agent",
		Choices: []Choice{{
			Index: 0,
			Message: ChatMessage{
				Role:      "assistant",
				ToolCalls: calls,
			},
			FinishReason: finishReason,
		}},
		// Encode run ID so client can resume.
		SystemFingerprint: resumeToken(runID),
	}

	return &Result{
		Response:            resp,
		PausedForClientTool: true,
		ResumeRunID:         runID,
		PendingToolCalls:    calls,
	}
}

// ─── Message parsing helpers ───────────────────────────────────────────────

// buildUserMessage extracts the last user-role message from the conversation.
// Preceding messages (system, assistant, tool) are concatenated into the
// system prompt appendix so the agent has full context.
func buildUserMessage(messages []ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// toolResult is the decoded result of a client tool call from the messages array.
type toolResult struct {
	ToolCallID string
	Content    string
}

// extractToolResult finds the last {role:"tool"} message and returns its content.
func extractToolResult(messages []ChatMessage) (*toolResult, error) {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role == "tool" {
			return &toolResult{
				ToolCallID: m.ToolCallID,
				Content:    m.Content,
			}, nil
		}
	}
	return nil, fmt.Errorf("no tool role message found in conversation — send the tool result in a message with role='tool'")
}

// ─── Model name helpers ────────────────────────────────────────────────────

// parseModelName strips the optional "agent:" prefix.
// "agent:graph-query-agent" → "graph-query-agent"
// "graph-query-agent" → "graph-query-agent"
func parseModelName(model string) string {
	if after, ok := strings.CutPrefix(model, "agent:"); ok {
		return strings.TrimSpace(after)
	}
	return strings.TrimSpace(model)
}

// ─── Resume token helpers ─────────────────────────────────────────────────

const resumePrefix = "run_"

func resumeToken(runID string) string { return resumePrefix + runID }
func parseResumeToken(fp string) (string, bool) {
	after, ok := strings.CutPrefix(fp, resumePrefix)
	return after, ok && after != ""
}

// ─── Misc helpers ─────────────────────────────────────────────────────────

func effectiveProjectID(user *auth.AuthUser) string {
	if user.APITokenProjectID != "" {
		return user.APITokenProjectID
	}
	return user.ProjectID
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func shortID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano()%0xFFFF_FFFF)
}
