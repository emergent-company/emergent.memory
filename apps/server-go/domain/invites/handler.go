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
// @Summary      List pending invitations
// @Description  Returns all pending (not yet accepted or declined) invitations sent to the current user's email address
// @Tags         invites
// @Produce      json
// @Success      200 {array} PendingInvite "List of pending invitations with project and organization details"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/invites/pending [get]
// @Security     bearerAuth
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
// @Summary      List project invitations
// @Description  Returns all invitations (pending, accepted, declined, revoked) for a specific project (requires project access)
// @Tags         invites
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {array} SentInvite "List of invitations for the project"
// @Failure      400 {object} apperror.Error "Missing project_id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/projects/{projectId}/invites [get]
// @Security     bearerAuth
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
// @Summary      Create project invitation
// @Description  Sends an invitation email to a user to join a project with specified role (requires project admin access)
// @Tags         invites
// @Accept       json
// @Produce      json
// @Param        request body CreateInviteRequest true "Invitation details (orgId, projectId, email, role)"
// @Success      201 {object} Invite "Created invitation with token"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/invites [post]
// @Security     bearerAuth
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
// @Summary      Accept invitation
// @Description  Accepts a pending invitation using the provided token, granting the user access to the project with specified role
// @Tags         invites
// @Accept       json
// @Produce      json
// @Param        request body AcceptInviteRequest true "Invitation token"
// @Success      200 {object} map[string]string "Acceptance confirmation"
// @Failure      400 {object} apperror.Error "Invalid request or missing token"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Invitation not found or expired"
// @Router       /api/invites/accept [post]
// @Security     bearerAuth
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
// @Summary      Decline invitation
// @Description  Declines a pending invitation, marking it as declined (user will not gain access to the project)
// @Tags         invites
// @Produce      json
// @Param        id path string true "Invitation ID (UUID)"
// @Success      200 {object} map[string]string "Decline confirmation"
// @Failure      400 {object} apperror.Error "Missing invite_id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Invitation not found"
// @Router       /api/invites/{id}/decline [post]
// @Security     bearerAuth
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
// @Summary      Revoke invitation
// @Description  Revokes/cancels a pending invitation (requires project admin access). Revoked invitations cannot be accepted.
// @Tags         invites
// @Produce      json
// @Param        id path string true "Invitation ID (UUID)"
// @Success      204 "Invitation revoked successfully"
// @Failure      400 {object} apperror.Error "Missing invite_id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Invitation not found"
// @Router       /api/invites/{id} [delete]
// @Security     bearerAuth
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
