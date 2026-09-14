package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// uiCreateProject handles the project-switcher create form (POST /projects;
// form fields name, orgId). It creates the project, makes it the active
// session project, and redirects to the agents page (PRG). On failure it
// redirects back with the error surfaced via the ?err= flash.
func (s *Server) uiCreateProject(c echo.Context) error {
	name := strings.TrimSpace(c.FormValue("name"))
	orgID := strings.TrimSpace(c.FormValue("orgId"))
	if name == "" || orgID == "" {
		return redirectWithError(c, "/agents", fmt.Errorf("project name and organization are required"))
	}
	project, err := s.memory.CreateProject(c.Request().Context(), name, orgID)
	if err != nil {
		return redirectWithError(c, "/agents", err)
	}
	s.activateProjectInSession(c, project.ID, orgID)
	return c.Redirect(http.StatusSeeOther, "/agents")
}

// uiActivateProject handles the project-switcher activate form (POST
// /projects/activate; form field projectId). It re-issues the session with the
// new active project and redirects back to the page the switch happened on
// (PRG), falling back to /agents when no Referer is present.
func (s *Server) uiActivateProject(c echo.Context) error {
	id := strings.TrimSpace(c.FormValue("projectId"))
	if id == "" {
		return redirectWithError(c, "/agents", fmt.Errorf("project id is required"))
	}
	// Recover the project's org so org-scoped routes (backups, X-Org-ID) resolve
	// after the switch; best-effort — an unresolved org leaves it empty.
	orgID, _ := s.projectOrgID(c.Request().Context(), id)
	s.activateProjectInSession(c, id, orgID)
	back := c.Request().Header.Get("Referer")
	// Switching into a project from the org context must not bounce back to
	// the org page, whose resolveOrg re-activates the org and clears the
	// just-activated project. Land on the project page instead.
	if back == "" || isOrgContextPath(back) {
		back = "/agents"
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), back)
	return nil
}

// isOrgContextPath reports whether a Referer URL points at an org-scoped page:
// the org listing (/orgs) or an org-context page (/orgs/:id...). The latter
// re-activate the org on load, so a project switch from them must redirect to
// the project landing, not back to the org page. The org listing is included
// so clicking a project row there opens the project (lands on /agents) rather
// than bouncing back to /orgs.
func isOrgContextPath(referer string) bool {
	u, err := url.Parse(referer)
	if err != nil {
		return false
	}
	return u.Path == "/orgs" || strings.HasPrefix(u.Path, "/orgs/")
}
