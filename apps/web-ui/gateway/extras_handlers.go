package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (s *Server) getConversation(c echo.Context) error {
	out, err := s.memory.GetConversation(c.Request().Context(), c.Param("id"))
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, out)
}

func (s *Server) getConversationHistory(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	out, err := s.conversationTimeline(ctx, id)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	out.Items = renderHistoryHTML(out.Items)
	// Annotate answered ask_user tool calls with the user's response so the
	// client can keep the question widget and mark it answered. Non-fatal: on
	// failure the history still renders, just without the answer highlight.
	if items, aerr := injectQuestionAnswers(ctx, s.memory, out.Items); aerr == nil {
		out.Items = items
	}
	// Surface pending tool approvals (run paused awaiting a decision) so the
	// transcript shows the interactive Approve/Reject/Cancel card. Non-fatal:
	// on failure the history still renders, just without the approval cards.
	if approvals, aerr := s.memory.ListToolApprovals(ctx); aerr == nil {
		for _, a := range approvals {
			if a.ConversationID == id && a.Decision == "pending" {
				out.PendingApprovals = append(out.PendingApprovals, PendingApprovalItem{
					QuestionID: a.QuestionID,
					Tool:       a.ToolName,
					Input:      a.ArgsSummary,
				})
			}
		}
	}
	data, err := marshalNoEscape(out)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, data)
}

func (s *Server) listMCPServers(c echo.Context) error {
	out, err := s.memory.ListMCPServers(c.Request().Context())
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, out)
}

func (s *Server) getMCPServer(c echo.Context) error {
	out, err := s.memory.GetMCPServer(c.Request().Context(), c.Param("id"))
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, out)
}

func (s *Server) createMCPServer(c echo.Context) error {
	var in MCPServer
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
	}
	out, err := s.memory.CreateMCPServer(c.Request().Context(), &in)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusCreated, out)
}

func (s *Server) updateMCPServer(c echo.Context) error {
	var in MCPServer
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	out, err := s.memory.UpdateMCPServer(c.Request().Context(), c.Param("id"), &in)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, out)
}

func (s *Server) deleteMCPServer(c echo.Context) error {
	if err := s.memory.DeleteMCPServer(c.Request().Context(), c.Param("id")); err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) listModels(c echo.Context) error {
	out, err := s.memory.ListModels(c.Request().Context())
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, out)
}
