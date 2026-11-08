package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emergent-company/emergent.memory/domain/feedback"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// executeSubmitFeedback files one feedback submission as a GitHub issue through
// the feedback domain. Identity comes from the authenticated caller, never from
// the tool arguments.
func (s *Service) executeSubmitFeedback(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.feedbackSvc == nil {
		return nil, fmt.Errorf("feedback-submit: feedback is not enabled on this deployment")
	}

	message, _ := args["message"].(string)
	if message == "" {
		return nil, fmt.Errorf("feedback-submit: missing required parameter: message")
	}
	category, _ := args["category"].(string)
	severity, _ := args["severity"].(string)

	includeContext := true
	if v, ok := args["include_context"].(bool); ok {
		includeContext = v
	}

	user, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("feedback-submit: no authenticated user")
	}

	// name the real MCP client when the transport carried clientInfo through
	// the session; the per-agent endpoint has no session map, so it falls back.
	client := clientNameFromContext(ctx)
	if client == "" {
		client = "MCP"
	}

	result, err := s.feedbackSvc.Submit(ctx, feedback.Submission{
		Message:        message,
		Category:       category,
		Severity:       severity,
		IncludeContext: includeContext,
		Source:         feedback.SourceMCP,
		Client:         client,
		UserID:         user.ID,
		UserEmail:      user.Email,
		ProjectID:      projectID,
		OrgID:          auth.OrgIDFromContext(ctx),
	})
	if err != nil {
		// A concurrent identical submission is holding the claim: a defined,
		// retryable answer (REST returns 409 + Retry-After for the same case),
		// not a tool failure.
		var inProgress *feedback.InProgressError
		if errors.As(err, &inProgress) {
			seconds := int(inProgress.RetryAfter.Round(time.Second) / time.Second)
			if seconds < 1 {
				seconds = 1
			}
			return s.wrapResult(map[string]any{
				"status":              "in_progress",
				"retry_after_seconds": seconds,
				"message":             "An identical submission is already being filed; retry shortly.",
			})
		}
		return nil, fmt.Errorf("feedback-submit: %w", err)
	}

	reply := "Thanks — your feedback was filed."
	status := "filed"
	if result.Duplicate {
		reply = "This feedback was already filed a few minutes ago; here is the existing issue."
		status = "duplicate"
	}
	return s.wrapResult(map[string]any{
		"status":       status,
		"issue_number": result.IssueNumber,
		"issue_url":    result.IssueURL,
		"duplicate":    result.Duplicate,
		"message":      reply,
	})
}
