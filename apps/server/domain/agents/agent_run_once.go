package agents

import (
	"context"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// agentRunOnce budget defaults. Kept below common MCP client tools/call
// timeouts so a synchronous call returns rather than hanging.
const (
	defaultAgentOnceMaxSteps = 12
	defaultAgentOnceTimeout  = 60 * time.Second
)

// agentRunner is the subset of AgentExecutor used by RunAgentOnce. *AgentExecutor
// satisfies it; tests inject a fake.
type agentRunner interface {
	Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error)
}

// agentOnceRepository is the narrow repository surface RunAgentOnce needs.
// *Repository satisfies it; tests inject a fake.
type agentOnceRepository interface {
	FindByID(ctx context.Context, id string, projectID *string) (*Agent, error)
	FindDefinitionByID(ctx context.Context, id string, projectID *string) (*AgentDefinition, error)
	FindRunByID(ctx context.Context, runID string) (*AgentRun, error)
	FindMessagesByRunID(ctx context.Context, runID string) ([]*AgentRunMessage, error)
	FindPendingQuestionsByRunID(ctx context.Context, runID string) ([]*AgentQuestion, error)
	GetOrgIDByProjectID(ctx context.Context, projectID string) (string, error)
}

// RunAgentOnce runs one agent synchronously with a bounded step/time budget and
// returns the assistant reply text plus the run ID. It is the execution backend
// for the per-agent MCP endpoint's call_agent tool.
//
// Failures are returned as *mcp.AgentRunError so the MCP transport can map them
// to structured tool errors. A human-in-the-loop pause yields
// mcp.AgentRunErrorPaused carrying the pending question; the caller must not
// fabricate a reply.
func (h *MCPToolHandler) RunAgentOnce(ctx context.Context, projectID, agentID, message string, budget mcp.AgentRunBudget) (string, string, error) {
	repo := h.onceRepo
	if repo == nil {
		if h.repo != nil {
			repo = h.repo
		}
	}
	runner := h.onceRunner
	if runner == nil {
		if h.executor != nil {
			runner = h.executor
		}
	}
	if repo == nil || runner == nil {
		return "", "", &mcp.AgentRunError{Kind: mcp.AgentRunErrorUnavailable, Message: "agent execution is unavailable"}
	}

	message = strings.TrimSpace(message)
	if message == "" {
		return "", "", &mcp.AgentRunError{Kind: mcp.AgentRunErrorFailed, Message: "message is required"}
	}

	agent, err := repo.FindByID(ctx, agentID, &projectID)
	if err != nil {
		return "", "", &mcp.AgentRunError{Kind: mcp.AgentRunErrorFailed, Message: "failed to load agent: " + err.Error()}
	}
	if agent == nil {
		return "", "", &mcp.AgentRunError{Kind: mcp.AgentRunErrorUnavailable, Message: "agent not found"}
	}
	if !agent.Enabled {
		return "", "", &mcp.AgentRunError{Kind: mcp.AgentRunErrorUnavailable, Message: "agent is disabled"}
	}

	var def *AgentDefinition
	if agent.AgentDefinitionID != nil && *agent.AgentDefinitionID != "" {
		def, err = repo.FindDefinitionByID(ctx, *agent.AgentDefinitionID, &projectID)
		if err != nil {
			return "", "", &mcp.AgentRunError{Kind: mcp.AgentRunErrorFailed, Message: "failed to load agent definition: " + err.Error()}
		}
	}

	orgID := auth.OrgIDFromContext(ctx)
	if orgID == "" {
		orgID, _ = repo.GetOrgIDByProjectID(ctx, projectID)
	}

	maxSteps := budget.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultAgentOnceMaxSteps
	}
	timeout := budget.Timeout
	if timeout <= 0 {
		timeout = defaultAgentOnceTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := runner.Execute(runCtx, ExecuteRequest{
		Agent:           agent,
		AgentDefinition: def,
		ProjectID:       projectID,
		OrgID:           orgID,
		UserMessage:     message,
		MaxSteps:        &maxSteps,
		Timeout:         &timeout,
	})
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}

	// A context deadline is a budget exhaustion regardless of how the executor
	// reported it (it may surface as an error or a RunStatusError result).
	if runCtx.Err() == context.DeadlineExceeded {
		runID := ""
		if result != nil {
			runID = result.RunID
		}
		return "", runID, &mcp.AgentRunError{
			Kind:    mcp.AgentRunErrorBudget,
			Message: "agent run exceeded the time budget",
			RunID:   runID,
		}
	}
	if err != nil {
		return "", "", &mcp.AgentRunError{Kind: mcp.AgentRunErrorFailed, Message: "agent run failed: " + err.Error()}
	}
	if result == nil {
		return "", "", &mcp.AgentRunError{Kind: mcp.AgentRunErrorFailed, Message: "agent run produced no result"}
	}

	runID := result.RunID
	switch result.Status {
	case RunStatusPaused:
		// A pause with a pending question needs human input; a pause without one
		// is the step-limit budget being exhausted.
		if question := pendingQuestionText(ctx, repo, runID); question != "" {
			return "", runID, &mcp.AgentRunError{
				Kind:     mcp.AgentRunErrorPaused,
				Message:  "agent run paused awaiting human input",
				Question: question,
				RunID:    runID,
			}
		}
		return "", runID, &mcp.AgentRunError{
			Kind:    mcp.AgentRunErrorBudget,
			Message: "agent run exceeded the step budget",
			RunID:   runID,
		}
	case RunStatusError:
		return "", runID, &mcp.AgentRunError{
			Kind:    mcp.AgentRunErrorFailed,
			Message: runFailureMessage(ctx, repo, runID, result),
			RunID:   runID,
		}
	}

	reply, err := assistantReply(ctx, repo, runID)
	if err != nil {
		return "", runID, err
	}
	return reply, runID, nil
}

// pendingQuestionText returns the first pending question for a run, or "".
func pendingQuestionText(ctx context.Context, repo agentOnceRepository, runID string) string {
	if runID == "" {
		return ""
	}
	questions, err := repo.FindPendingQuestionsByRunID(ctx, runID)
	if err != nil || len(questions) == 0 || questions[0] == nil {
		return ""
	}
	return questions[0].Question
}

// runFailureMessage prefers the persisted run error message, falling back to the
// executor summary and then a generic message.
func runFailureMessage(ctx context.Context, repo agentOnceRepository, runID string, result *ExecuteResult) string {
	if runID != "" {
		if run, err := repo.FindRunByID(ctx, runID); err == nil && run != nil && run.ErrorMessage != nil && *run.ErrorMessage != "" {
			return *run.ErrorMessage
		}
	}
	if result != nil && result.Summary != nil {
		if msg, _ := result.Summary["error"].(string); msg != "" {
			return msg
		}
	}
	return "agent run failed"
}

// assistantReply returns the last non-empty assistant message text from the
// run's persisted RAW messages. It filters strictly on role == "assistant": the
// ACP mapping collapses system and tool_result messages to "agent", so mapping
// first would leak system prompts or tool output to the caller. When no
// assistant text exists it returns an explicit structured error rather than an
// empty success.
func assistantReply(ctx context.Context, repo agentOnceRepository, runID string) (string, error) {
	if runID == "" {
		return "", &mcp.AgentRunError{
			Kind:    mcp.AgentRunErrorFailed,
			Message: "agent run produced no assistant reply",
		}
	}
	msgs, err := repo.FindMessagesByRunID(ctx, runID)
	if err != nil {
		return "", &mcp.AgentRunError{
			Kind:    mcp.AgentRunErrorFailed,
			Message: "failed to load run messages: " + err.Error(),
			RunID:   runID,
		}
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil || m.Role != "assistant" {
			continue
		}
		var b strings.Builder
		for _, part := range memoryContentToACPParts(m.Content) {
			if part.ContentType != "text/plain" || part.Content == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(part.Content)
		}
		if b.Len() > 0 {
			return b.String(), nil
		}
	}
	return "", &mcp.AgentRunError{
		Kind:    mcp.AgentRunErrorFailed,
		Message: "agent run produced no assistant reply",
		RunID:   runID,
	}
}
