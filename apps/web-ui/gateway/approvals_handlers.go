package main

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
)

// uiApprovals renders the tool-approval audit trail page. A failed fetch
// renders the whole-page error state.
func (s *Server) uiApprovals(c echo.Context) error {
	approvals, err := s.memory.ListToolApprovals(c.Request().Context())
	if err != nil {
		return s.page(c, pageTitle("Approvals"), ApprovalsPage(nil, err))
	}
	return s.page(c, pageTitle("Approvals"), ApprovalsPage(approvals, nil))
}

// uiApproveReject handles POST /settings/approvals/:questionId/respond — approves
// or rejects (with optional message) a pending tool approval, then redirects back.
func (s *Server) uiApproveReject(c echo.Context) error {
	questionID := c.Param("questionId")
	response := c.FormValue("response")
	message := c.FormValue("message")
	if response == "" {
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
