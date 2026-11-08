package mcp

import (
	"context"
	"fmt"

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

	user := auth.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return nil, fmt.Errorf("feedback-submit: no authenticated user")
	}

	result, err := s.feedbackSvc.Submit(ctx, feedback.Submission{
		Message:        message,
		Category:       category,
		Severity:       severity,
		IncludeContext: includeContext,
		Source:         feedback.SourceMCP,
		Client:         "MCP",
		UserID:         user.ID,
		UserEmail:      user.Email,
		ProjectID:      projectID,
		OrgID:          auth.OrgIDFromContext(ctx),
	})
	if err != nil {
		return nil, fmt.Errorf("feedback-submit: %w", err)
	}

	reply := "Thanks — your feedback was filed."
	if result.Duplicate {
		reply = "This feedback was already filed a few minutes ago; here is the existing issue."
	}
	return s.wrapResult(map[string]any{
		"issue_number": result.IssueNumber,
		"issue_url":    result.IssueURL,
		"duplicate":    result.Duplicate,
		"message":      reply,
	})
}
