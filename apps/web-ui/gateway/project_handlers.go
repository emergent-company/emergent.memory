package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// listOrgs returns the orgs visible to the signed-in user (GET /api/orgs).
func (s *Server) listOrgs(c echo.Context) error {
	orgs, err := s.memory.ListOrgs(c.Request().Context())
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, orgs)
}

// listProjects returns the projects visible to the signed-in user
// (GET /api/projects).
func (s *Server) listProjects(c echo.Context) error {
	projects, err := s.memory.ListProjects(c.Request().Context())
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	if projects == nil {
		projects = []ProjectRef{}
	}
	return c.JSON(http.StatusOK, projects)
}

// createProjectRequest is the POST /api/projects body.
type createProjectRequest struct {
	Name  string `json:"name"`
	OrgID string `json:"orgId"`
}

// createProject creates a project in an org (POST /api/projects) and, for a
// session caller, makes it the active project by re-issuing the session cookie.
func (s *Server) createProject(c echo.Context) error {
	var in createProjectRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if strings.TrimSpace(in.Name) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	if strings.TrimSpace(in.OrgID) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "orgId is required"})
	}
	project, err := s.memory.CreateProject(c.Request().Context(), strings.TrimSpace(in.Name), strings.TrimSpace(in.OrgID))
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	orgID := project.OrgID
	if orgID == "" {
		orgID = in.OrgID // memory response may omit orgId; the request is authoritative
	}
	s.activateProjectInSession(c, project.ID, orgID)
	return c.JSON(http.StatusCreated, project)
}

// activateProject switches the active project for the session
// (POST /api/projects/:id/activate), preserving the session's tokens. The
// target is validated against the caller's access tree before the session
// claims are written: a project outside the caller's access fails closed with
// 403 and writes nothing (#1998).
func (s *Server) activateProject(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "project id required"})
	}
	// No session context at all (API-key / programmatic caller): the switch is
	// meaningless but not an error — the key path is always bound to its static
	// project.
	if _, hasSession := sessionContextFrom(c.Request().Context()); !hasSession {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true", "activeProjectId": id})
	}
	claims, err := s.currentSession(c)
	if err != nil {
		// A session context is attached but its claims cannot be loaded (e.g. an
		// expired cookie): fail closed rather than reporting a silent success.
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	// Validate the target against the caller's access tree — the same source
	// applyScope uses. ListProjects alone is insufficient: it filters on
	// project_memberships without requiring an org membership, so an out-of-tree
	// project would otherwise be accepted here and then 404/403 every scoped
	// request. The org is recovered from the tree (authoritative), not the
	// tenancy listing.
	tree, err := s.orgAccessTree(c)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusForbidden, map[string]string{"error": "scope unavailable"})
	}
	orgID, projectID, ok := resolveProjectRefAnyOrg(tree, id)
	if !ok {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "scope not permitted"})
	}
	claims.ActiveProjectID = projectID
	claims.OrgID = orgID
	s.setSessionCookie(c, *claims)
	return c.JSON(http.StatusOK, map[string]string{"ok": "true", "activeProjectId": projectID})
}

// activateProjectInSession re-issues the session cookie with a new active
// project (and org), preserving tokens/expiry, and records the project in the
// account's durable recent-projects cookie. No-op when there is no session
// (API-key caller) or the secret is unset.
func (s *Server) activateProjectInSession(c echo.Context, projectID, orgID string) {
	claims, err := s.currentSession(c)
	if err != nil {
		return
	}
	now := time.Now()
	if claims.ExpiresAt <= now.Unix() {
		return
	}
	claims.ActiveProjectID = projectID
	claims.OrgID = orgID
	s.setSessionCookie(c, *claims)
	s.recordRecentProject(c, claims.Sub, projectID) // no-ops when Sub == "" (dev/API-key)
}

// activateOrg makes orgID the active org in the session cookie and clears the
// active project (project scope belongs to the new org), preserving
// tokens/identity/expiry. No-op when there is no session or it is expired.
func (s *Server) activateOrg(c echo.Context, orgID string) {
	claims, err := s.currentSession(c)
	if err != nil {
		return
	}
	now := time.Now()
	if claims.ExpiresAt <= now.Unix() {
		return
	}
	claims.ActiveProjectID = ""
	claims.OrgID = orgID
	s.setSessionCookie(c, *claims)
}

// clearContext clears both the active project and the active org from the
// session cookie (no-org wizard state), preserving tokens/identity/expiry.
// No-op when there is no session or it is expired.
func (s *Server) clearContext(c echo.Context) {
	claims, err := s.currentSession(c)
	if err != nil {
		return
	}
	now := time.Now()
	if claims.ExpiresAt <= now.Unix() {
		return
	}
	claims.ActiveProjectID = ""
	claims.OrgID = ""
	s.setSessionCookie(c, *claims)
}
