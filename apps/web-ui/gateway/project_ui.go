package main

import (
	"fmt"
	"net/http"
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
	// The create form posts to the global POST /projects route (scoped surfaces
	// are GET-only here), so scopeLocation no-ops and the PRG target must be
	// scoped directly. Return to the org landing (which surfaces the ?err= flash)
	// when the org is known, else the global org list.
	back := orgScopeURL(orgID)
	if name == "" || orgID == "" {
		return redirectWithError(c, back, fmt.Errorf("project name and organization are required"))
	}
	project, err := s.memory.CreateProject(c.Request().Context(), name, orgID)
	if err != nil {
		return redirectWithError(c, back, err)
	}
	s.activateProjectInSession(c, project.ID, orgID)
	return c.Redirect(http.StatusSeeOther, projectScopeURL(orgID, project.ID)+"/agents")
}

// uiActivateProject handles the project-switcher activate form (POST
// /projects/activate; form field projectId). It validates the target against
// the caller's access tree (the same source applyScope uses) before re-issuing
// the session with the new active project — an out-of-tree project is refused
// with an error flash and no session/recent write (#1998) — then lands on the
// target project's scoped URL.
func (s *Server) uiActivateProject(c echo.Context) error {
	id := strings.TrimSpace(c.FormValue("projectId"))
	if id == "" {
		// No project to activate: land on the org list (a global page) rather
		// than the now-cut unscoped /agents.
		return redirectWithError(c, "/orgs", fmt.Errorf("project id is required"))
	}
	// Validate against the caller's access tree; fail closed on an out-of-tree
	// project (a project membership without an org membership is invisible to
	// the tree and must not be activatable). The org is recovered from the tree,
	// so it is authoritative here.
	tree, err := s.orgAccessTree(c)
	if err != nil {
		captureError(err)
		return redirectWithError(c, "/orgs", fmt.Errorf("scope unavailable"))
	}
	orgID, projectID, ok := resolveProjectRefAnyOrg(tree, id)
	if !ok {
		return redirectWithError(c, "/orgs", fmt.Errorf("scope not permitted"))
	}
	s.activateProjectInSession(c, projectID, orgID)
	// Land on the target project's scoped URL so the switch is reflected in the
	// address bar (an org-context page must not bounce back to the org page,
	// whose resolveOrg re-activates the org).
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), projectScopeURL(orgID, projectID)+"/agents")
	return nil
}
