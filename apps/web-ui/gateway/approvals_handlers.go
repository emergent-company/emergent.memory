package main

import (
	"cmp"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
)

// uiApprovals renders the human-input surface: pending agent questions that can
// be answered here plus the tool-approval audit trail. This is the fallback
// target for an agent-input notification whose run has no chat conversation
// (issue #1375), so an open-ended `ask_user` question is answerable here rather
// than landing on a page that only lists tool approvals. A failed fetch renders
// the whole-page error state.
func (s *Server) uiApprovals(c echo.Context) error {
	ctx := c.Request().Context()
	approvals, err := s.memory.ListToolApprovals(ctx)
	if err != nil {
		return s.page(c, pageTitle("Approvals"), ApprovalsPage(nil, nil, err))
	}
	questions, qErr := s.memory.ListAgentQuestions(ctx)
	if qErr != nil {
		return s.page(c, pageTitle("Approvals"), ApprovalsPage(approvals, nil, qErr))
	}
	return s.page(c, pageTitle("Approvals"), ApprovalsPage(approvals, pendingQuestions(questions, approvals), nil))
}

// pendingQuestions narrows the project-wide agent-question list to the ones this
// page must render an answer control for: unanswered questions that are not
// already represented by a pending tool-approval row (a tool-confirmation
// question is shown on the approval row itself, with the same respond/cancel
// actions, so rendering it twice would be duplication).
func pendingQuestions(questions []AgentQuestionItem, approvals []ToolApprovalItem) []AgentQuestionItem {
	withApproval := make(map[string]struct{}, len(approvals))
	for _, a := range approvals {
		if a.Decision == "pending" {
			withApproval[a.QuestionID] = struct{}{}
		}
	}
	out := make([]AgentQuestionItem, 0, len(questions))
	for _, q := range questions {
		if q.Status != "pending" {
			continue
		}
		if _, dup := withApproval[q.ID]; dup {
			continue
		}
		out = append(out, q)
	}
	return out
}

// questionPlaceholder supplies the open-ended answer input's placeholder,
// falling back to a generic hint when the agent set none.
func questionPlaceholder(q AgentQuestionItem) string {
	return cmp.Or(q.Placeholder, "Type your answer…")
}

// uiApproveReject handles POST /settings/approvals/:questionId/respond — approves
// or rejects (with optional message) a pending tool approval, then redirects back.
func (s *Server) uiApproveReject(c echo.Context) error {
	questionID := c.Param("questionId")
	_ = c.Request().ParseForm()
	message := c.FormValue("message")
	var response string
	switch {
	case c.FormValue("multi") == "1":
		values := c.Request().Form["response"]
		if values == nil {
			values = []string{}
		}
		b, err := json.Marshal(values)
		if err != nil {
			return redirectWithError(c, "/settings/approvals", err)
		}
		response = string(b)
	case len(c.Request().Form["response"]) > 0 && c.Request().Form["response"][0] != "":
		response = c.Request().Form["response"][0]
	default:
		response = "reject"
	}
	if _, err := s.memory.RespondQuestion(c.Request().Context(), questionID, response, message); err != nil {
		return redirectWithError(c, "/settings/approvals", err)
	}
	return c.Redirect(http.StatusSeeOther, "/settings/approvals")
}

// uiCancelApproval handles POST /settings/approvals/:questionId/cancel — revokes
// a pending tool approval, then redirects back.
func (s *Server) uiCancelApproval(c echo.Context) error {
	questionID := c.Param("questionId")
	if _, err := s.memory.CancelQuestion(c.Request().Context(), questionID); err != nil {
		return redirectWithError(c, "/settings/approvals", err)
	}
	return c.Redirect(http.StatusSeeOther, "/settings/approvals")
}

// formatArgsJSON pretty-prints an argument summary for the audit view.
// Returns "" for an empty summary so the row can omit the block.
func formatArgsJSON(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}
