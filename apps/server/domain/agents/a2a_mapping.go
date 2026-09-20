package agents

import "github.com/google/uuid"

// Pure mapping functions between the internal run model and A2A v1.0 wire
// types. The engine keeps its own status vocabulary; this facade translates.

// MapRunStatusToTaskState maps an internal AgentRunStatus to an A2A TaskState
// per design.md's status table:
//
//	submitted → TASK_STATE_SUBMITTED
//	working   → TASK_STATE_WORKING
//	completed → TASK_STATE_COMPLETED
//	failed    → TASK_STATE_FAILED
//	input-required → TASK_STATE_INPUT_REQUIRED
//	cancelled → TASK_STATE_CANCELED
//	cancelling → TASK_STATE_WORKING (transient; never emitted on the wire as "cancelling")
//	skipped   → TASK_STATE_COMPLETED (A2A has no SKIPPED)
func MapRunStatusToTaskState(status AgentRunStatus) TaskState {
	switch status {
	case RunStatusQueued:
		return TaskStateSubmitted
	case RunStatusRunning:
		return TaskStateWorking
	case RunStatusCancelling:
		return TaskStateWorking
	case RunStatusSuccess:
		return TaskStateCompleted
	case RunStatusSkipped:
		return TaskStateCompleted
	case RunStatusError:
		return TaskStateFailed
	case RunStatusPaused:
		return TaskStateInputRequired
	case RunStatusCancelled:
		return TaskStateCanceled
	default:
		return TaskStateUnspecified
	}
}

// MapRoleToA2A maps an internal message role to an A2A Role. Genuine user
// messages map to ROLE_USER; every other author maps to ROLE_AGENT. The
// executor persists assistant turns under the sanitized agent name (ADK event
// Author), NOT the literal "assistant" (see isAgentReplyRole), so a default of
// ROLE_USER would misclassify real agent output. Non-conversational kinds
// (system, tool, tool_result) are filtered before this is called — see
// isA2AHistoryRole.
func MapRoleToA2A(role string) Role {
	if role == "user" {
		return RoleUser
	}
	return RoleAgent
}

// isInternalOnlyRole reports whether a persisted run-message role must never be
// serialized to the A2A wire: system prompts, raw tool call/result payloads,
// and reasoning/operator (planning) text. These are internal implementation
// detail; only user turns and agent-authored turns are exposed.
func isInternalOnlyRole(role string) bool {
	switch role {
	case "system", "tool", "tool_result", "reasoning", "operator":
		return true
	default:
		return false
	}
}

// isA2AHistoryRole reports whether a persisted run-message role belongs in A2A
// task history. System prompts, raw tool output, and reasoning/operator text
// are internal implementation detail and must never be serialized onto the
// wire; only user turns and agent-authored turns are exposed. Tool-call data is
// surfaced separately as Artifact data parts (ToolCallDataPart), never as
// history messages.
func isA2AHistoryRole(role string) bool {
	return role != "" && !isInternalOnlyRole(role)
}

// TextPart builds a text part.
func TextPart(text string) Part {
	return Part{Text: strPtr(text)}
}

// ToolCallDataPart builds a `data` part carrying a tool call's name, input and
// output as a JSON object (A2A has no tool event, so trajectories are rendered
// as data parts).
func ToolCallDataPart(tc *AgentRunToolCall) Part {
	return Part{
		Data: map[string]any{
			"toolName": tc.ToolName,
			"input":    tc.Input,
			"output":   tc.Output,
		},
	}
}

// toolCallArtifacts converts run tool calls into A2A data-part artifacts so a
// tool-using run's trajectory is exposed via Task.artifacts. Each tool call
// becomes one artifact carrying a single data part (ToolCallDataPart), keyed by
// the tool call's own ID as the artifactId.
func toolCallArtifacts(toolCalls []*AgentRunToolCall) []Artifact {
	artifacts := make([]Artifact, 0, len(toolCalls))
	for _, tc := range toolCalls {
		if tc == nil {
			continue
		}
		artifacts = append(artifacts, Artifact{
			ArtifactID: tc.ID,
			Name:       tc.ToolName,
			Parts:      []Part{ToolCallDataPart(tc)},
		})
	}
	return artifacts
}

// newA2AMessageID generates a fresh A2A message identifier for generated
// messages (status prompts, input-required prompts) that have no backing
// AgentRunMessage row. A2A's messageId is required (non-omitempty), so an
// empty id would serialize as "" and break client correlation.
func newA2AMessageID() string {
	return uuid.NewString()
}

// textStatusMessage builds a ROLE_AGENT Message carrying a single text part.
// Used for TaskStatus.Message (failed reason, skip reason, cancellation, HITL
// question).
func textStatusMessage(text string) *Message {
	return &Message{
		MessageID: newA2AMessageID(),
		Role:      RoleAgent,
		Parts:     []Part{TextPart(text)},
	}
}

// AgentRunMessageToA2AMessage converts an internal run message to an A2A
// Message. Assistant text becomes a `text` part; tool trajectories are not
// stored on AgentRunMessage (they live on AgentRunToolCall), so the caller
// attaches data parts separately via ToolCallDataPart. Returns nil for system
// prompts and raw tool output so internal data never reaches A2A history.
func AgentRunMessageToA2AMessage(msg *AgentRunMessage) *Message {
	if msg == nil || !isA2AHistoryRole(msg.Role) {
		return nil
	}
	role := MapRoleToA2A(msg.Role)
	parts := make([]Part, 0, 1)

	if text, ok := msg.Content["text"]; ok {
		if textStr, ok := text.(string); ok && textStr != "" {
			parts = append(parts, TextPart(textStr))
		}
	}

	// Drop messages that end up with zero text parts: a history message with no
	// usable content (e.g. an agent turn whose payload was tool data) must not
	// surface as an empty-part Message on the wire.
	if len(parts) == 0 {
		return nil
	}

	return &Message{
		MessageID: msg.ID,
		Role:      role,
		Parts:     parts,
	}
}

// MessagesToA2A converts a slice of internal run messages to A2A messages.
func MessagesToA2A(messages []AgentRunMessage) []Message {
	result := make([]Message, 0, len(messages))
	for i := range messages {
		if m := AgentRunMessageToA2AMessage(&messages[i]); m != nil {
			result = append(result, *m)
		}
	}
	return result
}

// finalAssistantText returns the text of the last agent-authored message, if
// any. Agent turns are persisted under the sanitized agent name (not the
// literal "assistant"), so this accepts any non-user/tool/system role via
// isAgentReplyRole.
func finalAssistantText(messages []AgentRunMessage) string {
	var last string
	for i := range messages {
		if !isAgentReplyRole(messages[i].Role) || isInternalOnlyRole(messages[i].Role) {
			continue
		}
		if text, ok := messages[i].Content["text"]; ok {
			if textStr, ok := text.(string); ok && textStr != "" {
				last = textStr
			}
		}
	}
	return last
}

// finalTextArtifact builds a single text artifact from the final assistant text.
func finalTextArtifact(messages []AgentRunMessage) []Artifact {
	text := finalAssistantText(messages)
	if text == "" {
		return nil
	}
	return []Artifact{{
		ArtifactID: "result",
		Name:       "result",
		Parts:      []Part{TextPart(text)},
	}}
}

// runToA2ATaskStatus builds a TaskStatus from a run, folding the error/skip/
// cancellation/question text into status.message per design.md.
func runToA2ATaskStatus(run *AgentRun, question *AgentQuestion) TaskStatus {
	ts := TaskStatus{State: MapRunStatusToTaskState(run.Status)}

	switch run.Status {
	case RunStatusError:
		if run.ErrorMessage != nil {
			ts.Message = textStatusMessage(*run.ErrorMessage)
		}
	case RunStatusSkipped:
		if run.SkipReason != nil {
			ts.Message = textStatusMessage(*run.SkipReason)
		}
	case RunStatusCancelling:
		ts.Message = textStatusMessage("cancellation requested")
	case RunStatusPaused:
		if question != nil {
			ts.Message = textStatusMessage(question.Question)
		}
	}

	return ts
}

// RunToA2ATask converts a run (with its messages, pending question, and any
// caller-provided artifacts) to an A2A Task.
//
// Task.id is the stable run ID (never the internal resume_run_id);
// contextId is derived from run.ACPSessionID; history is reconstructed from
// messages; artifacts carries the final assistant text (as one text artifact)
// followed by the caller-provided artifacts (e.g. tool-call data artifacts).
func RunToA2ATask(run *AgentRun, messages []AgentRunMessage, question *AgentQuestion, artifacts []Artifact) Task {
	task := Task{
		ID:        run.ID,
		ContextID: derefString(run.ACPSessionID),
		Status:    runToA2ATaskStatus(run, question),
		History:   MessagesToA2A(messages),
	}

	task.Artifacts = append(finalTextArtifact(messages), artifacts...)

	return task
}
