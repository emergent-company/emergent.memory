package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// errOrgIDRequired is returned when an org-scoped route lacks an :id.
var errOrgIDRequired = errors.New("organization id is required")

// --- Organization context pages (org sidebar + landing + members + settings hub) ---

// orgPageData is the payload for OrgLandingPage: the active org and its
// projects. LoadErr degrades to the whole-page error state. FlashMsg/FlashErr
// carry PRG feedback from the delete/transfer-project flows. TransferOrgs are
// the acting user's destination orgs (their org access tree minus the source)
// and CanTransfer gates the per-row Transfer action (user is org_admin of the
// source org and at least one candidate exists).
type orgPageData struct {
	Org          *Org
	Projects     []ProjectRef
	LoadErr      error
	FlashMsg     string
	FlashErr     error
	TransferOrgs []Org
	CanTransfer  bool
}

// orgMembersPageData is the payload for OrgMembersPage: the active org's
// members (OrgMemberDto from GET /api/orgs/{id}/members) plus PRG flash
// feedback (the ?invited= success flash from the org-invite flow).
type orgMembersPageData struct {
	Org      *Org
	Members  []OrgMemberDto
	LoadErr  error
	FlashMsg string
	FlashErr error
}

// orgInvitePageData is the payload for OrgInvitePage (GET /orgs/:id/invite):
// the org being invited into plus PRG error feedback from the invite-create
// flow. LoadErr degrades to the whole-page error state.
type orgInvitePageData struct {
	Org      *Org
	LoadErr  error
	FlashErr error
}

// orgSettingsPageData is the payload for OrgSettingsPage (the Tools section of
// the org Settings hub): the active org's tool-setting overrides plus PRG
// feedback from the toggle/delete flows.
type orgSettingsPageData struct {
	Org      *Org
	Settings []OrgToolSettingDto
	LoadErr  error
	FlashMsg string
	FlashErr error
}

// orgSettingsDangerZonePageData is the payload for
// OrgSettingsDangerZonePage (the Danger zone section of the org Settings hub):
// the active org for the delete-organization confirm form. LoadErr degrades to
// the whole-page error state.
type orgSettingsDangerZonePageData struct {
	Org     *Org
	LoadErr error
}

// orgSettingsGeneralPageData is the payload for OrgSettingsGeneralPage (the
// General section of the org Settings hub): the active org for the rename form
// plus PRG feedback from the rename flow.
type orgSettingsGeneralPageData struct {
	Org      *Org
	LoadErr  error
	FlashMsg string
	FlashErr error
}

// resolveOrg loads the org named by the :id path param and makes it the
// session's active org context (so the shell renders the org sidebar). It is a
// no-op activation in dev/API-key mode (no session) but still returns the org.
func (s *Server) resolveOrg(c echo.Context) (*Org, error) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return nil, errOrgIDRequired
	}
	org, err := s.memory.GetOrg(c.Request().Context(), id)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, fmt.Errorf("organization not found")
	}
	s.activateOrg(c, id)
	// activateOrg re-issues the cookie for future requests, but page() reads
	// the request context, so mirror the org context there too (otherwise the
	// activating request renders with a stale/no sidebar).
	if sc, ok := sessionContextFrom(c.Request().Context()); ok {
		sc.OrgID = id
		sc.ProjectID = ""
	}
	return org, nil
}

// uiOrg renders the org landing (GET /orgs/:id): the org's projects with an
// empty state + create CTA when there are none. The ?deleted= and ?err= query
// params surface PRG flash feedback from the delete-project flow; ?moved= +
// ?project= surface the transfer confirmation.
func (s *Server) uiOrg(c echo.Context) error {
	org, err := s.resolveOrg(c)
	if err != nil {
		return s.page(c, pageTitle("Organization"), OrgLandingPage(orgPageData{LoadErr: err}))
	}
	projects, err := s.memory.ListProjectsIncludingPending(c.Request().Context())
	captureError(err)
	// The org access tree supplies the acting user's orgs with roles (the same
	// fetch the orgs/members pages back on): candidates for the transfer
	// destination picker and the per-row Transfer-action gating.
	tree, err := s.memory.GetOrgsAndProjects(c.Request().Context())
	captureError(err)
	var mine []ProjectRef
	for _, p := range projects {
		if p.OrgID == org.ID {
			mine = append(mine, p)
		}
	}
	data := orgPageData{Org: org, Projects: mine}
	data.TransferOrgs, data.CanTransfer = transferState(tree, org.ID)
	data.FlashErr = flashError(c)
	if q := c.QueryParam("deleted"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			// Memory's project delete is async (202) — the project is
			// soft-deleted and purged later, so the flash says the deletion was
			// scheduled rather than claiming it is already gone.
			if n == 1 {
				data.FlashMsg = "Project scheduled for deletion."
			} else {
				data.FlashMsg = strconv.Itoa(n) + " projects scheduled for deletion."
			}
		}
	}
	if c.QueryParam("restored") != "" {
		data.FlashMsg = "Deletion cancelled."
	}
	if q := c.QueryParam("moved"); q != "" {
		data.FlashMsg = transferMovedMessage(projects, tree, c.QueryParam("project"), q)
	}
	return s.page(c, pageTitle(org.Name), OrgLandingPage(data))
}

// transferState derives the org-landing transfer affordance for the acting user
// from their org access tree: the candidate destination orgs (their orgs minus
// the source) and whether the per-project Transfer action is available — they
// are org_admin of the source org and at least one candidate destination
// exists. The memory service stays authoritative on the transfer itself
// (design D4/D5); this only decides UI visibility.
func transferState(tree []OrgWithProjectsDto, sourceOrgID string) (destinations []Org, canTransfer bool) {
	var role string
	for _, o := range tree {
		if o.ID == sourceOrgID {
			role = o.Role
			continue
		}
		destinations = append(destinations, Org{ID: o.ID, Name: o.Name})
	}
	return destinations, role == "org_admin" && len(destinations) > 0
}

// transferMovedMessage names the transferred project and its destination org in
// the success flash. uiTransferProject redirects with ?moved=<destinationOrgID>
// and ?project=<projectID>; both names resolve from data uiOrg already loads —
// the user's full project list and org access tree — so naming costs no extra
// round-trip. Falls back to a generic message when either name is unavailable
// (e.g. the access-tree fetch failed or the project left the user's view).
func transferMovedMessage(projects []ProjectRef, tree []OrgWithProjectsDto, projectID, destOrgID string) string {
	projName := ""
	for _, p := range projects {
		if p.ID == projectID {
			projName = p.Name
			break
		}
	}
	destName := ""
	for _, o := range tree {
		if o.ID == destOrgID {
			destName = o.Name
			break
		}
	}
	if projName == "" || destName == "" {
		return "Project moved."
	}
	return projName + " moved to " + destName + "."
}

// uiOrgMembers renders the active org's members (GET /orgs/:id/members).
// ?invited=1 surfaces the org-invite success flash.
func (s *Server) uiOrgMembers(c echo.Context) error {
	data := orgMembersPageData{FlashErr: flashError(c)}
	if c.QueryParam("invited") != "" {
		data.FlashMsg = "Invitation sent."
	}
	org, err := s.resolveOrg(c)
	if err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Members"), OrgMembersPage(data))
	}
	members, err := s.memory.ListOrgMembers(c.Request().Context(), org.ID)
	if err != nil {
		data.Org = org
		data.LoadErr = err
		return s.page(c, pageTitle("Members"), OrgMembersPage(data))
	}
	data.Org = org
	data.Members = members
	return s.page(c, pageTitle("Members"), OrgMembersPage(data))
}

// uiOrgInvitePage renders the standalone invite-to-organization page
// (GET /orgs/:id/invite): email (with live user-search autofill) + the fixed
// org_admin role, posting to POST /orgs/:id/invite. ?err= surfaces PRG
// feedback from the invite-create flow.
func (s *Server) uiOrgInvitePage(c echo.Context) error {
	org, err := s.resolveOrg(c)
	if err != nil {
		return s.page(c, pageTitle("Invite a member"), OrgInvitePage(orgInvitePageData{LoadErr: err, FlashErr: flashError(c)}))
	}
	return s.page(c, pageTitle("Invite a member"), OrgInvitePage(orgInvitePageData{Org: org, FlashErr: flashError(c)}))
}

// uiOrgInviteCreate handles the invite-to-organization form (POST
// /orgs/:id/invite; form fields email, role). The org comes from the route
// (:id) and the project is deliberately omitted, so the invitation is
// organization-scoped. Only the org_admin role is accepted here — project
// roles are invited from the project members page. On success it redirects to
// the org members page with ?invited=1; on failure back to the invite page
// with the error. Memory stays authoritative: an org invite it rejects (e.g.
// not an org_admin) surfaces through the same ?err= flash with no state change.
func (s *Server) uiOrgInviteCreate(c echo.Context) error {
	orgID := strings.TrimSpace(c.Param("id"))
	if orgID == "" {
		return redirectWithError(c, "/orgs", errOrgIDRequired)
	}
	back := "/orgs/" + url.PathEscape(orgID) + "/invite"
	email := strings.TrimSpace(c.FormValue("email"))
	if email == "" {
		return redirectWithError(c, back, fmt.Errorf("email is required"))
	}
	role := strings.TrimSpace(c.FormValue("role"))
	if role != "org_admin" {
		return redirectWithError(c, back, fmt.Errorf("invalid role"))
	}
	if _, err := s.memory.CreateInvite(c.Request().Context(), CreateInviteDto{
		OrgID: orgID,
		Email: email,
		Role:  role,
	}); err != nil {
		return redirectWithError(c, back, err)
	}
	return c.Redirect(http.StatusSeeOther, "/orgs/"+url.PathEscape(orgID)+"/members?invited=1")
}

// uiOrgSettings renders the Tools section of the active org's Settings hub
// (GET /orgs/:id/settings) with PRG feedback from the toggle/delete flows.
func (s *Server) uiOrgSettings(c echo.Context) error {
	org, err := s.resolveOrg(c)
	if err != nil {
		return s.page(c, pageTitle("Settings"), OrgSettingsPage(orgSettingsPageData{LoadErr: err}))
	}
	settings, err := s.memory.ListOrgToolSettings(c.Request().Context(), org.ID)
	if err != nil {
		return s.page(c, pageTitle("Settings"), OrgSettingsPage(orgSettingsPageData{Org: org, LoadErr: err}))
	}
	var flashMsg string
	if c.QueryParam("updated") != "" {
		flashMsg = "Tool setting updated."
	}
	return s.page(c, pageTitle("Settings"), OrgSettingsPage(orgSettingsPageData{Org: org, Settings: settings, FlashMsg: flashMsg, FlashErr: flashError(c)}))
}

// uiOrgSettingsDangerZone renders the Danger zone section of the active org's
// Settings hub (GET /orgs/:id/settings/danger-zone): the delete-organization
// confirm form. The browser title stays "Settings" so pageTestID remains the
// stable "page-settings" anchor shared by both hub sections.
func (s *Server) uiOrgSettingsDangerZone(c echo.Context) error {
	org, err := s.resolveOrg(c)
	if err != nil {
		return s.page(c, pageTitle("Settings"), OrgSettingsDangerZonePage(orgSettingsDangerZonePageData{LoadErr: err}))
	}
	return s.page(c, pageTitle("Settings"), OrgSettingsDangerZonePage(orgSettingsDangerZonePageData{Org: org}))
}

// uiOrgSettingsGeneral renders the General section of the active org's Settings
// hub (GET /orgs/:id/settings/general): the rename form. ?renamed=1 surfaces the
// success flash; ?err= the failure flash.
func (s *Server) uiOrgSettingsGeneral(c echo.Context) error {
	org, err := s.resolveOrg(c)
	if err != nil {
		return s.page(c, pageTitle("Settings"), OrgSettingsGeneralPage(orgSettingsGeneralPageData{LoadErr: err, FlashErr: flashError(c)}))
	}
	var flashMsg string
	if c.QueryParam("renamed") != "" {
		flashMsg = "Organization renamed."
	}
	return s.page(c, pageTitle("Settings"), OrgSettingsGeneralPage(orgSettingsGeneralPageData{Org: org, FlashMsg: flashMsg, FlashErr: flashError(c)}))
}

// uiOrgRename renames the org (POST /orgs/:id/rename; form field name). The org
// comes from the route (:id); memory stays authoritative on validation
// (non-empty, ≤120 chars), not-found and authorization, whose errors surface
// via the standard ?err= flash with no state change. Success redirects to the
// General section with ?renamed=1.
func (s *Server) uiOrgRename(c echo.Context) error {
	orgID := strings.TrimSpace(c.Param("id"))
	if orgID == "" {
		return redirectWithError(c, "/orgs", errOrgIDRequired)
	}
	back := "/orgs/" + url.PathEscape(orgID) + "/settings/general"
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return redirectWithError(c, back, fmt.Errorf("name is required"))
	}
	if _, err := s.memory.UpdateOrg(c.Request().Context(), orgID, name); err != nil {
		return redirectWithError(c, back, err)
	}
	return c.Redirect(http.StatusSeeOther, back+"?renamed=1")
}

// uiOrgToolSettingUpdate toggles one org tool setting (POST
// /orgs/:id/tool-settings/:toolName). The form carries the target enabled
// state; config is preserved by the backend when omitted.
func (s *Server) uiOrgToolSettingUpdate(c echo.Context) error {
	orgID := strings.TrimSpace(c.Param("id"))
	toolName := strings.TrimSpace(c.Param("toolName"))
	if orgID == "" || toolName == "" {
		return redirectWithError(c, "/orgs", errOrgIDRequired)
	}
	enabled := c.FormValue("enabled") == "true"
	if _, err := s.memory.UpsertOrgToolSetting(c.Request().Context(), orgID, toolName, UpsertOrgToolSettingInput{Enabled: enabled}); err != nil {
		return redirectWithError(c, "/orgs/"+url.PathEscape(orgID)+"/settings", err)
	}
	return c.Redirect(http.StatusSeeOther, "/orgs/"+url.PathEscape(orgID)+"/settings?updated=1")
}

// uiOrgToolSettingDelete removes one org tool-setting override (POST
// /orgs/:id/tool-settings/:toolName/delete), reverting the tool to its global
// default.
func (s *Server) uiOrgToolSettingDelete(c echo.Context) error {
	orgID := strings.TrimSpace(c.Param("id"))
	toolName := strings.TrimSpace(c.Param("toolName"))
	if orgID == "" || toolName == "" {
		return redirectWithError(c, "/orgs", errOrgIDRequired)
	}
	if err := s.memory.DeleteOrgToolSetting(c.Request().Context(), orgID, toolName); err != nil {
		return redirectWithError(c, "/orgs/"+url.PathEscape(orgID)+"/settings", err)
	}
	return c.Redirect(http.StatusSeeOther, "/orgs/"+url.PathEscape(orgID)+"/settings?updated=1")
}

// uiDeleteOrg deletes the org (POST /orgs/:id/delete), clears the session
// context, and returns to the wizard.
func (s *Server) uiDeleteOrg(c echo.Context) error {
	orgID := strings.TrimSpace(c.Param("id"))
	if orgID == "" {
		return redirectWithError(c, "/orgs", errOrgIDRequired)
	}
	if err := s.memory.DeleteOrg(c.Request().Context(), orgID); err != nil {
		return redirectWithError(c, "/orgs/"+url.PathEscape(orgID)+"/settings/danger-zone", err)
	}
	s.clearContext(c)
	return c.Redirect(http.StatusSeeOther, "/orgs/new")
}

// uiDeleteProjects deletes one or more projects (POST /projects/delete; form or
// query field projectId repeated, plus orgId for the redirect back). Single
// deletes arrive via the row action menu (HTMX), bulk deletes via the table
// form (plain POST).
func (s *Server) uiDeleteProjects(c echo.Context) error {
	orgID := strings.TrimSpace(c.FormValue("orgId"))
	if orgID == "" {
		orgID = strings.TrimSpace(c.QueryParam("orgId"))
	}
	back := "/orgs"
	if orgID != "" {
		back = "/orgs/" + url.PathEscape(orgID)
	}
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, back, err)
	}
	var ids []string
	for _, v := range c.Request().Form["projectId"] {
		if v = strings.TrimSpace(v); v != "" {
			ids = append(ids, v)
		}
	}
	if len(ids) == 0 {
		return redirectWithError(c, back, fmt.Errorf("no projects selected"))
	}
	var firstErr error
	for _, id := range ids {
		if err := s.memory.DeleteProject(c.Request().Context(), id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return redirectWithError(c, back, firstErr)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), back+"?deleted="+strconv.Itoa(len(ids)))
	return nil
}

// uiTransferProject reparents one project to another org the acting user
// belongs to (POST /projects/transfer; form fields projectId, destinationOrgId,
// and orgId — the source org, read from form or query like uiDeleteProjects,
// used only for the redirect back). Local guards reject a missing/unknown
// destination and a destination equal to the source with an error flash and NO
// backend call; everything else — role checks, project existence, naming
// collisions — is delegated to the memory service, whose errors surface via the
// standard ?err= flash with no state change. On success the redirect (HTMX
// single-row submit via render.RedirectAfterMutation, plain form posts get the
// PRG 303 fallback) returns to the source org view with a ?moved= +
// ?project= confirmation flash naming the project and its new organization.
func (s *Server) uiTransferProject(c echo.Context) error {
	ctx := c.Request().Context()
	orgID := strings.TrimSpace(c.FormValue("orgId"))
	if orgID == "" {
		orgID = strings.TrimSpace(c.QueryParam("orgId"))
	}
	back := "/orgs"
	if orgID != "" {
		back = "/orgs/" + url.PathEscape(orgID)
	}
	projectID := strings.TrimSpace(c.FormValue("projectId"))
	if projectID == "" {
		projectID = strings.TrimSpace(c.QueryParam("projectId"))
	}
	destOrgID := strings.TrimSpace(c.FormValue("destinationOrgId"))
	if destOrgID == "" {
		destOrgID = strings.TrimSpace(c.QueryParam("destinationOrgId"))
	}
	if projectID == "" {
		return redirectWithError(c, back, fmt.Errorf("no project selected"))
	}
	// Local guard (spec): a transfer to the project's current organization — or
	// one with no destination at all — is rejected here, no backend request.
	if destOrgID == "" {
		return redirectWithError(c, back, fmt.Errorf("a destination organization is required"))
	}
	if destOrgID == orgID {
		return redirectWithError(c, back, fmt.Errorf("the project is already in that organization"))
	}
	// The destination must be one of the acting user's orgs (the same access
	// tree the dialog's options come from). Anything beyond membership — role
	// checks, project existence, name collisions — is the service's call.
	orgs, err := s.memory.GetOrgsAndProjects(ctx)
	if err != nil {
		return redirectWithError(c, back, err)
	}
	known := false
	for _, o := range orgs {
		if o.ID == destOrgID {
			known = true
			break
		}
	}
	if !known {
		return redirectWithError(c, back, fmt.Errorf("destination organization is not available to you"))
	}
	if err := s.memory.TransferProject(ctx, projectID, destOrgID); err != nil {
		return redirectWithError(c, back, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), back+"?moved="+url.QueryEscape(destOrgID)+"&project="+url.QueryEscape(projectID))
	return nil
}

// uiRestoreProject cancels one project's pending deletion (POST
// /projects/restore; form or query field projectId, plus orgId for the redirect
// back — both read like uiDeleteProjects). A missing projectId is rejected
// locally with an error flash; everything else is delegated to the memory
// service, whose errors surface via the standard ?err= flash with no state
// change. On success the redirect (HTMX single-row submit via
// render.RedirectAfterMutation, plain form posts get the PRG 303 fallback)
// returns to the org view with a ?restored=1 confirmation flash.
func (s *Server) uiRestoreProject(c echo.Context) error {
	orgID := strings.TrimSpace(c.FormValue("orgId"))
	if orgID == "" {
		orgID = strings.TrimSpace(c.QueryParam("orgId"))
	}
	back := "/orgs"
	if orgID != "" {
		back = "/orgs/" + url.PathEscape(orgID)
	}
	projectID := strings.TrimSpace(c.FormValue("projectId"))
	if projectID == "" {
		projectID = strings.TrimSpace(c.QueryParam("projectId"))
	}
	if projectID == "" {
		return redirectWithError(c, back, fmt.Errorf("no project selected"))
	}
	if err := s.memory.RestoreProject(c.Request().Context(), projectID); err != nil {
		return redirectWithError(c, back, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), back+"?restored=1")
	return nil
}

// uiRoot resolves the landing redirect (GET /): dev/API-key mode keeps the
// static-project landing; session mode resolves last-used project → activate,
// else the active org → org landing, else the first org → activate + landing,
// else the no-org wizard.
func (s *Server) uiRoot(c echo.Context) error {
	sc, hasSession := sessionContextFrom(c.Request().Context())
	if !hasSession {
		return c.Redirect(http.StatusFound, "/agents")
	}
	if sc.ProjectID != "" {
		return c.Redirect(http.StatusFound, "/agents")
	}
	if rc, ok := s.readRecentProjects(c); ok {
		if ids := rc.Recent[sc.Sub]; len(ids) > 0 {
			s.activateProjectInSession(c, ids[0], "")
			return c.Redirect(http.StatusFound, "/agents")
		}
	}
	if sc.OrgID != "" {
		return c.Redirect(http.StatusFound, "/orgs/"+url.PathEscape(sc.OrgID))
	}
	orgs, err := s.memory.ListOrgs(c.Request().Context())
	if err == nil && len(orgs) > 0 {
		s.activateOrg(c, orgs[0].ID)
		return c.Redirect(http.StatusFound, "/orgs/"+url.PathEscape(orgs[0].ID))
	}
	return c.Redirect(http.StatusFound, "/orgs/new")
}

// orgMemberDisplayName is the best human label for an org member: display name,
// else first+last, else the email.
func orgMemberDisplayName(m OrgMemberDto) string {
	if m.DisplayName != nil && *m.DisplayName != "" {
		return *m.DisplayName
	}
	first, last := "", ""
	if m.FirstName != nil {
		first = *m.FirstName
	}
	if m.LastName != nil {
		last = *m.LastName
	}
	if full := strings.TrimSpace(first + " " + last); full != "" {
		return full
	}
	return m.Email
}
