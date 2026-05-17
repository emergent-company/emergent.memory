package agents

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// --- ACP Status Constants ---

const (
	ACPStatusSubmitted     = "submitted"
	ACPStatusWorking       = "working"
	ACPStatusInputRequired = "input-required"
	ACPStatusCompleted     = "completed"
	ACPStatusFailed        = "failed"
	ACPStatusCancelling    = "cancelling"
	ACPStatusCancelled     = "cancelled"
)

// --- ACP SSE Event Type Constants ---

const (
	ACPEventRunCreated       = "run.created"
	ACPEventRunInProgress    = "run.in-progress"
	ACPEventRunAwaiting      = "run.awaiting"
	ACPEventRunCompleted     = "run.completed"
	ACPEventRunFailed        = "run.failed"
	ACPEventRunCancelled     = "run.cancelled"
	ACPEventMessageCreated   = "message.created"
	ACPEventMessagePart      = "message.part"
	ACPEventMessageCompleted = "message.completed"
	ACPEventGeneric          = "generic"
	ACPEventError            = "error"
	ACPEventToolCall         = "tool_call"
	ACPEventToolResult       = "tool_result"
)

// --- ACP Wire Types ---

// ACPAgentManifest is the ACP agent card returned by discovery endpoints.
type ACPAgentManifest struct {
	Name               string              `json:"name"`
	Description        string              `json:"description"`
	Provider           *ACPProvider        `json:"provider,omitempty"`
	Version            string              `json:"version"`
	Capabilities       *ACPCapabilities    `json:"capabilities,omitempty"`
	DefaultInputModes  []string            `json:"default_input_modes"`
	DefaultOutputModes []string            `json:"default_output_modes"`
	Status             *AgentStatusMetrics `json:"status,omitempty"`

	// Optional metadata fields (omitted when empty)
	Tags              []string `json:"tags,omitempty"`
	Domains           []string `json:"domains,omitempty"`
	RecommendedModels []string `json:"recommended_models,omitempty"`
	Documentation     string   `json:"documentation,omitempty"`
	Framework         string   `json:"framework,omitempty"`
}

// ACPProvider identifies the agent provider.
type ACPProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// ACPCapabilities describes what the agent supports.
type ACPCapabilities struct {
	Streaming      bool `json:"streaming"`
	HumanInTheLoop bool `json:"human_in_the_loop"`
	SessionSupport bool `json:"session_support"`
}

// AgentStatusMetrics holds live computed metrics for an agent.
type AgentStatusMetrics struct {
	AvgRunTokens      *float64 `json:"avg_run_tokens,omitempty"`
	AvgRunTimeSeconds *float64 `json:"avg_run_time_seconds,omitempty"`
	SuccessRate       *float64 `json:"success_rate,omitempty"`
}

// ACPMessage is an ACP protocol message with role and parts.
type ACPMessage struct {
	Role  string           `json:"role"`
	Parts []ACPMessagePart `json:"parts"`
}

// ACPMessagePart is a single part of an ACP message.
type ACPMessagePart struct {
	ContentType string              `json:"content_type"`
	Content     string              `json:"content,omitempty"`
	Metadata    *TrajectoryMetadata `json:"metadata,omitempty"`
}

// TrajectoryMetadata describes a tool call trajectory attached to a message part.
// Fields match the ACP spec TrajectoryMetadata schema exactly:
// tool_input and tool_output are JSON objects (not strings).
type TrajectoryMetadata struct {
	Kind       string          `json:"kind"`                  // always "trajectory"
	Message    *string         `json:"message,omitempty"`     // reasoning step text
	ToolName   *string         `json:"tool_name,omitempty"`   // name of the tool
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`  // full input params object
	ToolOutput json.RawMessage `json:"tool_output,omitempty"` // full output/result object
}

// ACPRunObject is the ACP run representation returned by run endpoints.
type ACPRunObject struct {
	ID           string           `json:"id"`
	AgentName    string           `json:"agent_name"`
	Status       string           `json:"status"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    *time.Time       `json:"updated_at,omitempty"`
	Output       []ACPMessage     `json:"output,omitempty"`
	AwaitRequest *ACPAwaitRequest `json:"await_request,omitempty"`
	Error        *ACPRunError     `json:"error,omitempty"`
	SessionID    *string          `json:"session_id,omitempty"`
	Metadata     map[string]any   `json:"metadata,omitempty"`
	ResumeRunID  *string          `json:"resume_run_id,omitempty"` // ID of the new run created on resume
}

// ACPRunError describes an error that occurred during a run.
type ACPRunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ACPAwaitRequest represents a human-in-the-loop question posed by the agent.
type ACPAwaitRequest struct {
	QuestionID string                `json:"question_id"`
	Question   string                `json:"question"`
	Options    []AgentQuestionOption `json:"options,omitempty"`
}

// ACPSessionRun represents a single run within a session, with its full event history inlined.
type ACPSessionRun struct {
	RunID          string        `json:"run_id"`
	Status         string        `json:"status"`
	TriggerMessage *string       `json:"trigger_message,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	CompletedAt    *time.Time    `json:"completed_at,omitempty"`
	Events         []ACPSSEEvent `json:"events"`
}

// ACPSessionObject is the ACP session representation.
// Per ACP spec, History is a list of URL references to run event streams —
// clients fetch each URL to load the full message history for that run.
type ACPSessionObject struct {
	ID        string    `json:"id"`
	AgentName *string   `json:"agent_name,omitempty"`
	Title     *string   `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// History contains runs with their full inlined event log.
	History []ACPSessionRun `json:"history"`
	// LastRunStatus is the ACP status of the most recent run in this session, if any.
	LastRunStatus *string `json:"last_run_status,omitempty"`
	// RunCount is the number of runs (user turns) in this session.
	RunCount int `json:"run_count"`
	// MessageCount is the number of user messages in this session.
	MessageCount int64 `json:"message_count"`
	// TotalTokens is the total number of input+output tokens consumed across all runs.
	TotalTokens int64 `json:"total_tokens"`
	// TotalCostUSD is the estimated total cost in USD across all runs.
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// ACPSSEEvent represents a persisted or streamed SSE event.
type ACPSSEEvent struct {
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
}

// ACPSSEBusPayload is the envelope emitted on the events.Service SSE bus for
// agent run events. The Type and payload key/value match the ACP spec Event
// discriminated union exactly, so clients can deserialize using the ACP SDK.
//
// Wire shape on the SSE bus:
//
//	event: entity.created
//	data: { "entity": "agent_run", "id": "<runID>", "data": { "type": "run.in-progress", "run": {...} }, ... }
//
// The "data" object inside EntityEvent.Data is an ACPSSEBusPayload.
type ACPSSEBusPayload struct {
	// Type is the ACP event type string, e.g. "run.in-progress", "message.part".
	Type string `json:"type"`
	// RunID is repeated at the top level for fast client-side filtering.
	RunID string `json:"run_id"`
	// The ACP payload key depends on Type:
	//   run.*        → "run"  (ACPRunObject)
	//   message.part → "part" (ACPMessagePart with TrajectoryMetadata or text/plain)
	// Stored as map to avoid a union type in Go while staying JSON-compatible.
	Payload map[string]any `json:"payload"`
}

// --- ACP Request Types ---

// ACPCreateRunRequest is the request body for POST /acp/v1/agents/:name/runs.
type ACPCreateRunRequest struct {
	Message   []ACPMessagePart  `json:"message" validate:"required,min=1"`
	Mode      string            `json:"mode,omitempty"` // sync (default), async, stream
	SessionID *string           `json:"session_id,omitempty"`
	EnvVars   map[string]string `json:"env_vars,omitempty"`
}

// ACPResumeRunRequest is the request body for POST /acp/v1/agents/:name/runs/:runId/resume.
type ACPResumeRunRequest struct {
	Message []ACPMessagePart `json:"message" validate:"required,min=1"`
	Mode    string           `json:"mode,omitempty"` // sync (default), async, stream
}

// ACPCreateSessionRequest is the request body for POST /acp/v1/sessions.
type ACPCreateSessionRequest struct {
	AgentName *string `json:"agent_name,omitempty"`
}

// --- Conversion Functions ---

// acpSlugRegexp matches any non-alphanumeric, non-hyphen character.
var acpSlugRegexp = regexp.MustCompile(`[^a-z0-9-]`)

// acpMultiHyphenRegexp matches consecutive hyphens.
var acpMultiHyphenRegexp = regexp.MustCompile(`-{2,}`)

// ACPSlugFromName normalizes a free-form agent name to an RFC 1123 DNS label:
// lowercase, replace non-alphanumeric with hyphens, collapse consecutive hyphens,
// trim leading/trailing hyphens, truncate to 63 characters.
func ACPSlugFromName(name string) string {
	s := strings.ToLower(name)
	s = acpSlugRegexp.ReplaceAllString(s, "-")
	s = acpMultiHyphenRegexp.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = s[:63]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// MapMemoryStatusToACP maps a Memory AgentRunStatus to the ACP status string.
// Now that internal statuses use ACP vocabulary, this is mostly a passthrough.
// skipped maps to completed since ACP has no distinct skipped status.
func MapMemoryStatusToACP(status AgentRunStatus) string {
	if status == RunStatusSkipped {
		return ACPStatusCompleted
	}
	return string(status)
}

// AgentDefinitionToManifest converts an AgentDefinition entity to an ACP agent manifest.
// If status is nil, the Status field is omitted from the manifest.
func AgentDefinitionToManifest(def *AgentDefinition, status *AgentStatusMetrics) ACPAgentManifest {
	manifest := ACPAgentManifest{
		Name:               ACPSlugFromName(def.Name),
		Version:            "0.2.0",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Capabilities: &ACPCapabilities{
			Streaming:      true,
			HumanInTheLoop: true,
			SessionSupport: true,
		},
		Status: status,
	}

	// Description: prefer ACPConfig.Description, fall back to definition description
	if def.ACPConfig != nil && def.ACPConfig.Description != "" {
		manifest.Description = def.ACPConfig.Description
	} else if def.Description != nil {
		manifest.Description = *def.Description
	}

	// Override input/output modes from ACPConfig if present
	if def.ACPConfig != nil {
		if len(def.ACPConfig.InputModes) > 0 {
			manifest.DefaultInputModes = def.ACPConfig.InputModes
		}
		if len(def.ACPConfig.OutputModes) > 0 {
			manifest.DefaultOutputModes = def.ACPConfig.OutputModes
		}
		if len(def.ACPConfig.Capabilities) > 0 {
			// ACPConfig.Capabilities is a string list of capability names;
			// the manifest Capabilities struct is fixed, so store them as tags.
			// This keeps backward compat with the existing ACPConfig shape.
		}
	}

	// Provider defaults (can be overridden by ACPConfig in future)
	manifest.Provider = &ACPProvider{
		Organization: "Emergent",
	}

	return manifest
}

// RunToACPObject converts a Memory AgentRun (with optional messages and pending question)
// to the ACP run wire format.
func RunToACPObject(run *AgentRun, messages []AgentRunMessage, question *AgentQuestion) ACPRunObject {
	agentName := ""
	if run.Agent != nil {
		agentName = ACPSlugFromName(run.Agent.Name)
	}

	obj := ACPRunObject{
		ID:        run.ID,
		AgentName: agentName,
		Status:    MapMemoryStatusToACP(run.Status),
		CreatedAt: run.CreatedAt,
		SessionID: run.ACPSessionID,
	}

	// UpdatedAt: use CompletedAt if available, otherwise nil
	if run.CompletedAt != nil {
		obj.UpdatedAt = run.CompletedAt
	}

	// Build output messages from Memory run messages
	if len(messages) > 0 {
		obj.Output = memoryMessagesToACP(messages)
	}

	// Populate await_request if the run is paused with a pending question
	if run.Status == RunStatusPaused && question != nil {
		obj.AwaitRequest = &ACPAwaitRequest{
			QuestionID: question.ID,
			Question:   question.Question,
			Options:    question.Options,
		}
	}

	// Populate error if the run failed
	if run.Status == RunStatusError && run.ErrorMessage != nil {
		obj.Error = &ACPRunError{
			Code:    "execution_error",
			Message: *run.ErrorMessage,
		}
	}

	// Add skip reason as metadata if skipped
	if run.Status == RunStatusSkipped && run.SkipReason != nil {
		obj.Metadata = map[string]any{
			"skipped":     true,
			"skip_reason": *run.SkipReason,
		}
	}

	// Expose resume_run_id from suspend_context if present
	if run.SuspendContext != nil {
		if v, ok := run.SuspendContext["resume_run_id"]; ok {
			if s, ok := v.(string); ok && s != "" {
				obj.ResumeRunID = &s
			}
		}
	}

	return obj
}

// memoryMessagesToACP converts a slice of Memory run messages to ACP message format.
func memoryMessagesToACP(messages []AgentRunMessage) []ACPMessage {
	var result []ACPMessage

	for _, msg := range messages {
		acpRole := memoryRoleToACP(msg.Role)
		parts := memoryContentToACPParts(msg.Content)

		if len(parts) > 0 {
			result = append(result, ACPMessage{
				Role:  acpRole,
				Parts: parts,
			})
		}
	}

	return result
}

// memoryRoleToACP maps Memory message roles to ACP roles.
func memoryRoleToACP(role string) string {
	switch role {
	case "assistant":
		return "agent"
	case "user":
		return "user"
	case "system":
		return "agent" // system messages are from the agent side
	case "tool_result":
		return "agent" // tool results are part of agent workflow
	default:
		return role
	}
}

// memoryContentToACPParts extracts ACP message parts from Memory's JSONB content.
// Memory content format: {"text": "...", "function_calls": [...], ...}
func memoryContentToACPParts(content map[string]any) []ACPMessagePart {
	var parts []ACPMessagePart

	// Extract text content
	if text, ok := content["text"]; ok {
		if textStr, ok := text.(string); ok && textStr != "" {
			parts = append(parts, ACPMessagePart{
				ContentType: "text/plain",
				Content:     textStr,
			})
		}
	}

	return parts
}

// ToolCallToTrajectoryMetadata converts a Memory AgentRunToolCall to ACP TrajectoryMetadata.
// tool_input and tool_output are kept as raw JSON objects per the ACP spec.
func ToolCallToTrajectoryMetadata(tc *AgentRunToolCall) TrajectoryMetadata {
	name := tc.ToolName
	inputJSON, _ := json.Marshal(tc.Input)
	outputJSON, _ := json.Marshal(tc.Output)

	return TrajectoryMetadata{
		Kind:       "trajectory",
		ToolName:   &name,
		ToolInput:  json.RawMessage(inputJSON),
		ToolOutput: json.RawMessage(outputJSON),
	}
}

// SessionToACPObject converts an ACPSession entity with associated runs to the ACP wire format.
// baseURL should be the scheme+host (e.g. "https://api.example.com") used to build history URLs.
// Per ACP spec, history entries are URL references to run event streams that clients fetch to
// reconstruct full message history.
// SessionToACPObject converts an ACPSession entity with associated runs to the ACP wire format.
// eventsByRunID maps run ID → ordered list of persisted events for that run.
func SessionToACPObject(session *ACPSession, runs []*AgentRun, eventsByRunID map[string][]*ACPRunEvent, stats *ACPSessionStats) ACPSessionObject {
	obj := ACPSessionObject{
		ID:        session.ID,
		AgentName: session.AgentName,
		Title:     session.Title,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
		History:   make([]ACPSessionRun, 0, len(runs)),
		RunCount:  len(runs),
	}
	if stats != nil {
		obj.MessageCount = stats.MessageCount
		obj.TotalTokens = stats.TotalTokens
		obj.TotalCostUSD = stats.TotalCostUSD
	}

	for _, run := range runs {
		rawEvents := eventsByRunID[run.ID]
		sseEvents := make([]ACPSSEEvent, len(rawEvents))
		for i, e := range rawEvents {
			sseEvents[i] = RunEventToACPSSEEvent(e)
		}
		obj.History = append(obj.History, ACPSessionRun{
			RunID:          run.ID,
			Status:         string(run.Status),
			TriggerMessage: run.TriggerMessage,
			CreatedAt:      run.CreatedAt,
			CompletedAt:    run.CompletedAt,
			Events:         sseEvents,
		})
	}

	// Derive session status from the last run (runs are ordered ASC by created_at)
	if len(runs) > 0 {
		lastStatus := string(runs[len(runs)-1].Status)
		obj.LastRunStatus = &lastStatus
	}

	return obj
}

// RunEventToACPSSEEvent converts a persisted ACPRunEvent to the wire format.
func RunEventToACPSSEEvent(event *ACPRunEvent) ACPSSEEvent {
	return ACPSSEEvent{
		Type:      event.EventType,
		Data:      event.Data,
		CreatedAt: event.CreatedAt,
	}
}
