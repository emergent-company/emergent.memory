package invites

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler handles HTTP requests for invitations
type Handler struct {
	svc  *Service
	cfg  *config.Config
	auth *auth.Middleware
	orgs *orgs.Repository
	db   bun.IDB
}

// NewHandler creates a new invites handler
func NewHandler(svc *Service, cfg *config.Config, authMiddleware *auth.Middleware, orgsRepo *orgs.Repository, db bun.IDB) *Handler {
	return &Handler{svc: svc, cfg: cfg, auth: authMiddleware, orgs: orgsRepo, db: db}
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
	user := auth.MustGetUser(c)

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
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id is required")
	}

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
	user := auth.MustGetUser(c)

	var req CreateInviteRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.OrgID == "" {
		return apperror.NewBadRequest("orgId is required")
	}

	// targetOrgID is the server-resolved organization the invitation will grant
	// access to. It is derived from kb.projects (project-scoped) or from the body
	// orgId only after a server-side membership check (org-scoped); it is never a
	// trusted client value.
	var targetOrgID string

	if req.ProjectID != "" {
		// Project-scoped: authorize the caller against the project's owning org
		// (issue #926), then bind the body orgId to that server-resolved org
		// (issue #960). Neither value is trusted: the owning org is derived from
		// kb.projects and the supplied orgId must agree with it.
		if err := h.auth.AuthorizeProject(c, req.ProjectID); err != nil {
			return err
		}
		owningOrg, err := h.svc.ProjectOrg(c.Request().Context(), req.ProjectID)
		if err != nil {
			return err
		}
		if req.OrgID != owningOrg {
			return apperror.NewBadRequest("orgId does not match the project's organization")
		}
		targetOrgID = owningOrg

		// A project-scoped invitation grants project membership only. org_admin
		// is an organization-level role with no meaning in project scope, and
		// accepting it would write the out-of-vocabulary value "org_admin" into
		// kb.project_memberships.role (issue #979). Reject it at create time so
		// the junk role is never persisted.
		if req.Role == "org_admin" {
			return apperror.NewBadRequest("org_admin role is not valid for a project-scoped invite")
		}
	} else {
		// Org-scoped: the caller must be a member of the target org (issue #960).
		// Membership is resolved server-side against kb.organization_memberships,
		// so a foreign orgId cannot self-satisfy the check.
		member, err := h.orgs.IsUserMember(c.Request().Context(), req.OrgID, user.ID)
		if err != nil {
			return err
		}
		if !member {
			return apperror.ErrForbidden
		}
		targetOrgID = req.OrgID
	}

	// Role-grant authorization (issue #967): an org_admin invitation grants the
	// invitee org_admin membership, so only a caller who already holds org_admin
	// authority over the target organization (or is an active superadmin_full)
	// may mint it. A plain member may invite at or below their own level; a role
	// above the caller's authority is refused, fail closed. The caller's
	// authority is resolved server-side from kb.organization_memberships and
	// core.superadmins — never from a request-controlled value.
	if req.Role == "org_admin" {
		allowed, err := h.mayGrantOrgAdmin(c.Request().Context(), targetOrgID, user.ID)
		if err != nil {
			return err
		}
		if !allowed {
			return apperror.NewForbidden("only an org_admin may invite another org_admin")
		}
	}

	// Attach inviter identity so the email template can show who sent the invite
	req.InviterID = user.ID
	if user.Email != "" {
		req.InviterName = user.Email
	}

	invite, err := h.svc.Create(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, invite)
}

// mayGrantOrgAdmin reports whether the caller may grant org_admin membership in
// orgID. A caller who is themselves an org_admin of orgID, or an active
// superadmin_full, may; anyone else may not (issue #967). The caller's authority
// is resolved server-side from kb.organization_memberships and core.superadmins,
// never from a client-supplied value.
func (h *Handler) mayGrantOrgAdmin(ctx context.Context, orgID, userID string) (bool, error) {
	role, err := h.orgs.GetMembershipRole(ctx, orgID, userID)
	if err != nil {
		return false, err
	}
	if role == "org_admin" {
		return true, nil
	}
	return auth.IsSuperadminFull(ctx, h.db)
}

// Accept accepts an invitation via POST (JSON body with token)
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
	user := auth.MustGetUser(c)

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

// AcceptViaLink accepts an invitation via GET with token as query parameter.
// If the user is not authenticated, redirects to the Zitadel login page.
// @Summary      Accept invitation via link
// @Description  Accepts a pending invitation using a token passed as a query parameter. Designed for email invite links. Redirects to login if not authenticated.
// @Tags         invites
// @Produce      json
// @Param        token query string true "Invitation token"
// @Success      200 {object} map[string]string "Acceptance confirmation"
// @Success      302 "Redirect to login"
// @Failure      400 {object} apperror.Error "Missing token"
// @Failure      404 {object} apperror.Error "Invitation not found or expired"
// @Router       /invites/accept [get]
func (h *Handler) AcceptViaLink(c echo.Context) error {
	token := c.QueryParam("token")
	if token == "" {
		return apperror.ErrBadRequest.WithMessage("token is required")
	}

	user := auth.GetUser(c)
	if user == nil {
		// Redirect to Zitadel login; pass the full invite URL as login_hint so
		// the user lands back here after authenticating.
		issuer := h.cfg.Zitadel.GetIssuer()
		returnURL := fmt.Sprintf("%s/invites/accept?token=%s", h.cfg.AppURL, url.QueryEscape(token))
		loginURL := fmt.Sprintf("%s/ui/login?redirect_uri=%s", issuer, url.QueryEscape(returnURL))
		return c.Redirect(http.StatusFound, loginURL)
	}

	if err := h.svc.Accept(c.Request().Context(), user.ID, token); err != nil {
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
	user := auth.MustGetUser(c)

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
	user := auth.MustGetUser(c)

	inviteID := c.Param("id")
	if inviteID == "" {
		return apperror.ErrBadRequest.WithMessage("invite_id is required")
	}

	// Resolve the invite server-side and require the caller to be a member of
	// its organization before revoking (issue #960). A non-member receives 404,
	// indistinguishable from a missing invite, so this endpoint is not an
	// existence oracle.
	invite, err := h.svc.GetByID(c.Request().Context(), inviteID)
	if err != nil {
		return err
	}

	member, err := h.orgs.IsUserMember(c.Request().Context(), invite.OrganizationID, user.ID)
	if err != nil {
		return err
	}
	if !member {
		return apperror.NewNotFound("invite", inviteID)
	}

	if err := h.svc.Revoke(c.Request().Context(), inviteID); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
