package agentcompat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	Done            bool       // stream finished (last event, check Usage/ResumeRunID/ErrorMsg)
	Usage           *Usage

	// ResumeRunID is set on the Done event when the run was paused for a client
	// tool call. The handler emits this as system_fingerprint so the client can resume.
	ResumeRunID string

	// ErrorMsg is set on the Done event when the run failed. The handler emits
	// an SSE error chunk so the client can detect the failure instead of seeing
	// a silent [DONE].
	ErrorMsg string
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

	// Fetch project_info and inject it into the system prompt so the agent
	// understands the KB context without needing to call project-get as a tool.
	projectInfo, _ := s.agentRepo.GetProjectInfo(ctx, projectID)

	// Inject prior conversation turns + project info into the system prompt appendix.
	appendix := buildSystemAppendix(req.Messages, len(req.Tools) > 0, projectInfo)

	execReq := agents.ExecuteRequest{
		Agent:                agent,
		AgentDefinition:      agentDef,
		ProjectID:            projectID,
		OrgID:                user.OrgID,
		UserMessage:          userMsg,
		SystemPromptAppendix: appendix,
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

	var clientPause clientPauseState
	extraTools, err := buildClientTools(req.Tools, &clientPause)
	if err != nil {
		return nil, fmt.Errorf("building client tools: %w", err)
	}

	// Fetch project_info so the resumed run also has KB context in its system prompt.
	projectInfo, _ := s.agentRepo.GetProjectInfo(ctx, projectID)

	resumeReq := agents.ExecuteRequest{
		AgentDefinition: agentDef,
		ProjectID:       projectID,
		OrgID:           user.OrgID,
		// Pass only the raw tool result Content — not the wrapper struct.
		// injectToolResponse injects this as responseBody["result"] so the LLM
		// sees the actual tool output, not Go struct field names.
		UserMessage:          toolResult.Content,
		SystemPromptAppendix: buildSystemAppendix(req.Messages, len(req.Tools) > 0, projectInfo),
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
		return s.buildClientToolCallResult(req.Model, result.RunID, pause), nil
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
		return s.buildClientToolCallResult(req.Model, result.RunID, pause), nil
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
			// Emit an error Done event so the handler can write an SSE error chunk
			// instead of silently closing the stream with [DONE].
			ch <- StreamEvent{Done: true, ErrorMsg: err.Error()}
			return
		}
		// Final event: either done or paused-for-client-tool.
		if result.Status == agents.RunStatusPaused && pause.hasPendingCall() {
			// Carry the run ID so writeStream can emit system_fingerprint in the
			// terminal chunk, enabling streaming clients to resume the paused run.
			ch <- StreamEvent{Done: true, ResumeRunID: result.RunID}
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
			ch <- StreamEvent{Done: true, ErrorMsg: err.Error()}
			return
		}
		if result.Status == agents.RunStatusPaused && pause.hasPendingCall() {
			ch <- StreamEvent{Done: true, ResumeRunID: result.RunID}
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

func (s *Service) buildClientToolCallResult(model, runID string, pause *clientPauseState) *Result {
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
		Model:   model,
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
func buildUserMessage(messages []ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// buildSystemAppendix combines:
//   - project_info context (injected so the agent knows the KB purpose without a tool call)
//   - conversation history (prior turns from the messages[] array)
//   - tool-naming convention block (when client tools are present)
//
// All three are appended to the agent's system instruction so the executor
// starts with full context on every fresh ADK session.
func buildSystemAppendix(messages []ChatMessage, hasClientTools bool, projectInfo string) string {
	var sb strings.Builder

	if projectInfo != "" {
		sb.WriteString("## Knowledge base context\n\n")
		sb.WriteString(projectInfo)
		sb.WriteString("\n")
	}

	if history := buildConversationHistory(messages); history != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("## Conversation history\n\n")
		sb.WriteString("The following is the conversation so far. Use it to answer the user's latest message.\n\n")
		sb.WriteString(history)
	}

	if tool := SystemPromptAppendix(hasClientTools); tool != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(tool)
	}

	return sb.String()
}

// buildConversationHistory serialises all messages except the final user message
// into a readable transcript. Returns "" when there is only one message.
func buildConversationHistory(messages []ChatMessage) string {
	// Find the index of the last user message — that becomes the live UserMessage
	// sent to the executor, so we exclude it from the history block.
	lastUser := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = i
			break
		}
	}
	// Nothing to replay if the conversation is just one user message.
	if lastUser <= 0 {
		return ""
	}

	var sb strings.Builder
	for i := 0; i < lastUser; i++ {
		m := messages[i]
		switch m.Role {
		case "system":
			// Mid-conversation system messages (e.g. context injections from the
			// client) are preserved — they may carry important runtime context.
			// The agent's own system prompt is injected separately; duplication
			// risk is low because the agent definition prompt is not in messages[].
			if m.Content != "" {
				sb.WriteString(fmt.Sprintf("[System]: %s\n", m.Content))
			}
		case "user":
			sb.WriteString(fmt.Sprintf("User: %s\n", m.Content))
		case "assistant":
			// Preserve content even when tool_calls are also present — the OpenAI
			// spec allows both (e.g. chain-of-thought before a tool call).
			if m.Content != "" {
				sb.WriteString(fmt.Sprintf("Assistant: %s\n", m.Content))
			}
			for _, tc := range m.ToolCalls {
				sb.WriteString(fmt.Sprintf("Assistant called tool %q with args: %s\n",
					tc.Function.Name, tc.Function.Arguments))
			}
		case "tool":
			sb.WriteString(fmt.Sprintf("Tool result (id=%s): %s\n", m.ToolCallID, m.Content))
		}
	}
	return sb.String()
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

// shortID returns a cryptographically random 8-byte hex string (16 chars).
// Used as the suffix for chat completion IDs and tool call IDs.
func shortID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: time-based (avoids panic if rand unavailable, which is rare)
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
