package invites

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for invitations
type Handler struct {
	svc *Service
}

// NewHandler creates a new invites handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListPending returns pending invitations for the current user
// GET /api/invites/pending
func (h *Handler) ListPending(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	invites, err := h.svc.ListPendingForUser(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, invites)
}

// ListByProject returns invites for a specific project
// GET /api/projects/:projectId/invites
func (h *Handler) ListByProject(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id is required")
	}

	// TODO: Verify user has access to project

	invites, err := h.svc.ListByProject(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, invites)
}

// Create creates a new invitation
// POST /api/invites
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req CreateInviteRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// TODO: Verify user has admin access to project

	invite, err := h.svc.Create(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, invite)
}

// Accept accepts an invitation
// POST /api/invites/accept
func (h *Handler) Accept(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req AcceptInviteRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.Token == "" {
		return apperror.ErrBadRequest.WithMessage("token is required")
	}

	if err := h.svc.Accept(c.Request().Context(), user.ID, req.Token); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "accepted"})
}

// Decline declines an invitation
// POST /api/invites/:id/decline
func (h *Handler) Decline(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	inviteID := c.Param("id")
	if inviteID == "" {
		return apperror.ErrBadRequest.WithMessage("invite_id is required")
	}

	if err := h.svc.Decline(c.Request().Context(), user.ID, inviteID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "declined"})
}

// Delete revokes/cancels an invitation
// DELETE /api/invites/:id
func (h *Handler) Delete(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	inviteID := c.Param("id")
	if inviteID == "" {
		return apperror.ErrBadRequest.WithMessage("invite_id is required")
	}

	// TODO: Verify user has admin access to the project this invite belongs to

	if err := h.svc.Revoke(c.Request().Context(), inviteID); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
