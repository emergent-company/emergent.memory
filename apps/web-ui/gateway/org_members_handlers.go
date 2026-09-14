package main

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// createOrg handles POST /api/orgs (name); 201 on success, 409 on
// duplicate-name / org-limit conflict.
func (s *Server) createOrg(c echo.Context) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	org, err := s.memory.CreateOrg(c.Request().Context(), name)
	if err != nil {
		if isMemoryStatus(err, http.StatusConflict) {
			return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, org)
}

// listMembers handles GET /api/members for the active project.
func (s *Server) listMembers(c echo.Context) error {
	members, err := s.memory.ListMembers(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	if members == nil {
		members = []ProjectMemberDto{}
	}
	return c.JSON(http.StatusOK, members)
}

// removeMember handles DELETE /api/members/:userId for the active project.
func (s *Server) removeMember(c echo.Context) error {
	userID := c.Param("userId")
	if userID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "userId is required"})
	}
	if err := s.memory.RemoveMember(c.Request().Context(), userID); err != nil {
		if isLastAdminError(err) {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "cannot remove the last admin; assign another admin first"})
		}
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "removed"})
}

// listInvites handles GET /api/invites (sent invites for the active project).
func (s *Server) listInvites(c echo.Context) error {
	invites, err := s.memory.ListInvites(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	if invites == nil {
		invites = []SentInviteDto{}
	}
	return c.JSON(http.StatusOK, invites)
}

// createInvite handles POST /api/invites (orgId, optional projectId, email, role).
type createInviteRequest struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

func (s *Server) createInvite(c echo.Context) error {
	var in createInviteRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	email := strings.TrimSpace(in.Email)
	if email == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "email is required"})
	}
	role := strings.TrimSpace(in.Role)
	if role != "org_admin" && role != "project_admin" && role != "project_user" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid role"})
	}
	invite, err := s.memory.CreateInvite(c.Request().Context(), CreateInviteDto{
		OrgID: strings.TrimSpace(in.OrgID), ProjectID: strings.TrimSpace(in.ProjectID),
		Email: email, Role: role,
	})
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, invite)
}

// listPendingInvites handles GET /api/invites/pending for the signed-in user.
func (s *Server) listPendingInvites(c echo.Context) error {
	invites, err := s.memory.ListPendingInvites(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	if invites == nil {
		invites = []PendingInviteDto{}
	}
	return c.JSON(http.StatusOK, invites)
}

// acceptInvite handles POST /api/invites/accept (token).
func (s *Server) acceptInvite(c echo.Context) error {
	var in struct {
		Token string `json:"token"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if strings.TrimSpace(in.Token) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "token is required"})
	}
	if err := s.memory.AcceptInvite(c.Request().Context(), in.Token); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "accepted"})
}

// declineInvite handles POST /api/invites/:id/decline.
func (s *Server) declineInvite(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invite id is required"})
	}
	if err := s.memory.DeclineInvite(c.Request().Context(), id); err != nil {
		if isMemoryStatus(err, http.StatusForbidden) {
			return c.JSON(http.StatusForbidden, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "declined"})
}

// cancelInvite handles DELETE /api/invites/:id (revoke).
func (s *Server) cancelInvite(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invite id is required"})
	}
	if err := s.memory.CancelInvite(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// searchUsers handles GET /api/users/search?email= (returns a bare array).
func (s *Server) searchUsers(c echo.Context) error {
	email := strings.TrimSpace(c.QueryParam("email"))
	if email == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "email query is required"})
	}
	users, err := s.memory.SearchUsers(c.Request().Context(), email)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	if users == nil {
		users = []UserSearchResultDto{}
	}
	return c.JSON(http.StatusOK, users)
}

// getProfile handles GET /api/user/profile.
func (s *Server) getProfile(c echo.Context) error {
	profile, err := s.memory.GetProfile(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, profile)
}

// updateProfile handles PUT /api/user/profile.
func (s *Server) updateProfile(c echo.Context) error {
	var in UpdateUserProfileDto
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	profile, err := s.memory.UpdateProfile(c.Request().Context(), in)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, profile)
}
