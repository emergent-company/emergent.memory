package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/emergent-company/go-daisy/components/ui"
	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// --- Web UI page payloads (rendered by org_members_ui.templ) ---

// orgsPageData is the payload for OrgsPage: the signed-in user's org→project
// access tree plus PRG flash feedback from the create-org flow (the create
// form lives on its own page at /orgs/new).
type orgsPageData struct {
	Orgs     []OrgWithProjectsDto
	LoadErr  error
	FlashMsg string
	FlashErr error
}

// orgCreatePageData is the payload for OrgCreatePage (/orgs/new): the
// create-organization form plus PRG error feedback from the create flow.
type orgCreatePageData struct {
	FlashErr error
}

// membersPageData is the payload for MembersPage: the active project's people
// — confirmed members (with their roles) plus still-pending invitations (from
// ListInvites) that read as not-yet-confirmed members. PendingErr degrades the
// invite fetch to a banner while the member list stays usable.
type membersPageData struct {
	Members     []ProjectMemberDto
	Pending     []SentInviteDto // Status == "pending" invitations to the active project
	PendingErr  error
	ProjectName string // active project name (best-effort; "" when unresolvable)
	LoadErr     error
	FlashMsg    string
	FlashErr    error
}

// memberInvitePageData is the payload for MemberInvitePage (/members/new):
// the invite form context (the active project) plus PRG error feedback from
// the invite-create flow.
type memberInvitePageData struct {
	ProjectName string // active project name (best-effort)
	HasProject  bool   // invite form is usable (active project + org resolved)
	FlashErr    error
}

// memberDetailPageData is the payload for MemberDetailPage (/members/:userId):
// one confirmed member of the active project, resolved from ListMembers.
// NotFound renders the "member not found" state; LoadErr the fetch-error state.
// CanChangeRole gates the role-change affordance (memory rejects removing the
// project's sole admin, so that member's role cannot be changed either).
// FlashErr carries PRG feedback from a failed role change.
type memberDetailPageData struct {
	Member        *ProjectMemberDto
	CanChangeRole bool
	NotFound      bool
	LoadErr       error
	FlashErr      error
}

// profilePageData is the payload for ProfilePage (/profile): the signed-in
// user's profile (from memory) and the session email (from the IdP token,
// falling back to the profile's email field) for identity context. Pending
// invitations live on their own page (invitationsPageData at
// /profile/invitations) and account API tokens on /profile/tokens
// (apiTokensPageData) — neither is loaded here.
type profilePageData struct {
	Profile  *UserProfileDto
	Email    string
	LoadErr  error
	FlashMsg string
	FlashErr error
}

// invitationsPageData is the payload for InvitationsPage
// (/profile/invitations): the pending invitations addressed to the signed-in
// user (accept/decline) plus PRG flash feedback from the accept/decline flows.
// A failed fetch renders the whole-page error state (LoadErr).
type invitationsPageData struct {
	Pending    []PendingInviteDto
	PendingErr error // reserved for a section-level degrade; unused while the list is the page's only content
	LoadErr    error
	FlashMsg   string
	FlashErr   error
}

// --- GET page handlers ---

// uiOrgs renders the organizations page: the access tree (orgs with the
// user's org role, nested projects with the project role) with a
// "Create organization" action pointing at /orgs/new. ?ok=1 surfaces a
// create-org success flash.
func (s *Server) uiOrgs(c echo.Context) error {
	ctx := c.Request().Context()
	data := orgsPageData{}
	if c.QueryParam("ok") != "" {
		data.FlashMsg = "Organization created."
	}
	data.FlashErr = flashError(c)
	orgs, err := s.memory.GetOrgsAndProjects(ctx)
	if err != nil {
		data.LoadErr = err
	} else {
		data.Orgs = orgs
	}
	return s.page(c, pageTitle("Organizations"), OrgsPage(data))
}

// uiOrgCreatePage renders the standalone create-organization page
// (GET /orgs/new): the name field posting to POST /orgs. ?err= surfaces PRG
// feedback from the create flow.
func (s *Server) uiOrgCreatePage(c echo.Context) error {
	return s.page(c, pageTitle("New organization"), OrgCreatePage(orgCreatePageData{FlashErr: flashError(c)}))
}

// uiMembers renders the members page: one view of the active project's people
// — confirmed members plus pending invitations (labelled "not responded"),
// with an "Add member" action pointing at /members/new. ?ok=1 / ?revoked=1
// surface remove/revoke success flashes; ?role-changed=1 surfaces the
// remove + re-invite success from the details page's change-role flow.
func (s *Server) uiMembers(c echo.Context) error {
	ctx := c.Request().Context()
	data := membersPageData{}
	switch {
	case c.QueryParam("role-changed") != "":
		data.FlashMsg = "Role changed — the member was removed and re-invited; they regain access once they accept the new invitation."
	case c.QueryParam("revoked") != "":
		data.FlashMsg = "Invite revoked."
	case c.QueryParam("ok") != "":
		data.FlashMsg = "Member removed."
	}
	data.FlashErr = flashError(c)
	if pr, ok := s.activeProjectRef(ctx); ok {
		data.ProjectName = pr.Name
	}
	members, err := s.memory.ListMembers(ctx)
	if err != nil {
		data.LoadErr = err
	} else {
		data.Members = members
	}
	invites, err := s.memory.ListInvites(ctx)
	if err != nil {
		data.PendingErr = err
	} else {
		for _, inv := range invites {
			if inv.Status == "pending" {
				data.Pending = append(data.Pending, inv)
			}
		}
	}
	return s.page(c, pageTitle("Members"), MembersPage(data))
}

// uiMemberInvitePage renders the standalone invite-a-member page
// (GET /members/new): email (with live user-search autofill) + role selector,
// posting to POST /members. ?err= surfaces PRG feedback from the create flow.
func (s *Server) uiMemberInvitePage(c echo.Context) error {
	data := memberInvitePageData{FlashErr: flashError(c)}
	if pr, ok := s.activeProjectRef(c.Request().Context()); ok {
		data.ProjectName = pr.Name
		data.HasProject = pr.ID != "" && pr.OrgID != ""
	}
	return s.page(c, pageTitle("Invite a member"), MemberInvitePage(data))
}

// uiMemberDetails renders one confirmed member's details page
// (GET /members/:userId). The member is resolved by re-fetching ListMembers
// and matching the id; an unknown id renders the not-found state. The page
// offers role management when the member is removable (memory rejects removing
// the sole project admin). ?err= surfaces PRG feedback from a failed role
// change.
func (s *Server) uiMemberDetails(c echo.Context) error {
	ctx := c.Request().Context()
	userID := strings.TrimSpace(c.Param("userId"))
	data := memberDetailPageData{FlashErr: flashError(c)}
	members, err := s.memory.ListMembers(ctx)
	if err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Member unavailable"), MemberDetailPage(data))
	}
	if i := slices.IndexFunc(members, func(m ProjectMemberDto) bool { return m.ID == userID }); i >= 0 {
		data.Member = &members[i]
	}
	if data.Member == nil {
		data.NotFound = true
		return s.page(c, pageTitle("Member not found"), MemberDetailPage(data))
	}
	data.CanChangeRole = canRemoveMember(members, *data.Member)
	return s.page(c, pageTitle(memberDisplayName(*data.Member)), MemberDetailPage(data))
}

// uiProfile renders the signed-in user's profile (/profile): the identity
// summary and the edit form, with the profile sub-nav ("Profile" active).
// ?ok=1 surfaces the edit-profile success flash; ?avatar=1 / ?avatar=removed
// surface the profile-photo upload/remove flashes (see uiUploadAvatar /
// uiRemoveAvatar).
func (s *Server) uiProfile(c echo.Context) error {
	ctx := c.Request().Context()
	data := profilePageData{}
	switch {
	case c.QueryParam("ok") != "":
		data.FlashMsg = "Profile updated."
	case c.QueryParam("avatar") == "removed":
		data.FlashMsg = "Profile photo removed."
	case c.QueryParam("avatar") != "":
		data.FlashMsg = "Profile photo updated."
	}
	data.FlashErr = flashError(c)
	s.loadProfileFields(ctx, &data)
	return s.page(c, pageTitle("Profile"), ProfilePage(data))
}

// uiProfileInvitations renders the signed-in user's pending invitations
// (/profile/invitations): each invitation with accept (token-bound) and
// decline forms, with the profile sub-nav ("Invitations" active). ?accepted=1
// / ?declined=1 surface the corresponding PRG success flashes.
func (s *Server) uiProfileInvitations(c echo.Context) error {
	ctx := c.Request().Context()
	data := invitationsPageData{}
	switch {
	case c.QueryParam("accepted") != "":
		data.FlashMsg = "Invite accepted."
	case c.QueryParam("declined") != "":
		data.FlashMsg = "Invite declined."
	}
	data.FlashErr = flashError(c)
	s.loadPendingInvites(ctx, &data)
	return s.page(c, pageTitle("Invitations"), InvitationsPage(data))
}

// loadProfileFields loads the identity fields for the profile page: the
// session email and the memory profile (failure renders the whole-page
// error). The session email falls back to the profile's email field when the
// IdP token omits it. Pending invitations and account tokens moved off the
// profile page, so they are no longer loaded here.
func (s *Server) loadProfileFields(ctx context.Context, data *profilePageData) {
	if sc, ok := sessionContextFrom(ctx); ok {
		data.Email = sc.Email
	}
	profile, err := s.memory.GetProfile(ctx)
	if err != nil {
		data.LoadErr = err
	} else {
		data.Profile = profile
		if data.Email == "" && profile != nil {
			data.Email = profile.Email
		}
	}
}

// loadPendingInvites fills the invitations page's pending-invite list; a
// failure renders the whole-page error state.
func (s *Server) loadPendingInvites(ctx context.Context, data *invitationsPageData) {
	pending, err := s.memory.ListPendingInvites(ctx)
	if err != nil {
		data.LoadErr = err
		return
	}
	data.Pending = pending
}

// uiInviteUserSearch returns the user-search suggestion <option>s for the
// invite form's email field (GET /partial/invite-user-search?email=…), as an
// htmx partial swapped into the datalist. Searches under 2 characters return
// no suggestions; a search failure degrades to no suggestions (the email field
// still accepts any typed address).
func (s *Server) uiInviteUserSearch(c echo.Context) error {
	email := strings.TrimSpace(c.QueryParam("email"))
	var users []UserSearchResultDto
	if len(email) >= 2 {
		users, _ = s.memory.SearchUsers(c.Request().Context(), email)
	}
	render.RenderPartial(c.Response().Writer, c.Request(), userSearchOptions(users))
	return nil
}

// --- PRG form handlers (posted from org_members_ui.templ pages) ---

// uiCreateOrg handles the new-organization form (POST /orgs; form field
// name). On success it activates the new org and redirects to its landing
// page; on failure back to the form page (/orgs/new) with the error.
func (s *Server) uiCreateOrg(c echo.Context) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return redirectWithError(c, "/orgs/new", fmt.Errorf("organization name is required"))
	}
	org, err := s.memory.CreateOrg(c.Request().Context(), name)
	if err != nil {
		return redirectWithError(c, "/orgs/new", err)
	}
	s.activateOrg(c, org.ID)
	// The create-org form lives inside the boosted #main-content region
	// (OrgCreatePage), and creating an org switches the session context — a
	// plain 303 would be followed by htmx as a boosted GET, swapping only
	// #main-content while the shell (topbar org switcher, sidebar) still shows
	// the previous org. HX-Redirect forces a full page load (see the org-delete
	// and project-activate precedent); plain POSTs keep the 303.
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/orgs/"+url.PathEscape(org.ID))
	return nil
}

// uiRemoveMember handles one member-remove form (POST /members/:userId/remove)
// on the members page. Memory enforces the last-admin guard server-side (403),
// so a doomed remove attempt surfaces as a normal ?err= flash.
func (s *Server) uiRemoveMember(c echo.Context) error {
	userID := strings.TrimSpace(c.Param("userId"))
	if userID == "" {
		return redirectWithError(c, "/members", fmt.Errorf("member id is required"))
	}
	if err := s.removeProjectMember(c.Request().Context(), userID); err != nil {
		return redirectWithError(c, "/members", err)
	}
	return c.Redirect(http.StatusSeeOther, "/members?ok=1")
}

// uiChangeMemberRole handles the change-role form on the member details page
// (POST /members/:userId/role; form field role). Memory has no member-role
// PATCH, so a role change is remove + re-invite (see changeMemberRole). The
// member's current email is read server-side from ListMembers, never trusted
// from the form, so the re-invite always targets the member being changed.
// On success it redirects to the members page; every failure — invalid role,
// unknown member, last-admin removal, or a failed re-invite after removal —
// redirects back to the member's details page with a clear flash instead of a
// misleading success.
func (s *Server) uiChangeMemberRole(c echo.Context) error {
	ctx := c.Request().Context()
	userID := strings.TrimSpace(c.Param("userId"))
	detailsPath := "/members/" + url.PathEscape(userID)
	if userID == "" {
		return redirectWithError(c, "/members", fmt.Errorf("member id is required"))
	}
	role := strings.TrimSpace(c.FormValue("role"))
	if role != "project_admin" && role != "project_user" {
		return redirectWithError(c, detailsPath, fmt.Errorf("invalid role"))
	}
	ref, ok := s.activeProjectRef(ctx)
	if !ok || ref.OrgID == "" {
		return redirectWithError(c, detailsPath, fmt.Errorf("no active project to change roles in"))
	}
	member, err := s.findMember(ctx, userID)
	if err != nil {
		return redirectWithError(c, detailsPath, err)
	}
	if member == nil {
		return redirectWithError(c, detailsPath, fmt.Errorf("member not found"))
	}
	if member.Role == role {
		return redirectWithError(c, detailsPath, fmt.Errorf("member already has the %s role", roleLabel(role)))
	}
	if err := s.changeMemberRole(ctx, ref, *member, role); err != nil {
		return redirectWithError(c, detailsPath, err)
	}
	return c.Redirect(http.StatusSeeOther, "/members?role-changed=1")
}

// uiMemberInviteCreate handles the invite form on /members/new (POST /members;
// form fields email, role). The target org/project come from the session's
// active project (never from hidden form fields), so the invitation always
// lands in the project the sender is managing. On success it redirects to the
// members page with ?ok=1; on failure back to the form with the error.
func (s *Server) uiMemberInviteCreate(c echo.Context) error {
	ctx := c.Request().Context()
	ref, ok := s.activeProjectRef(ctx)
	if !ok {
		return redirectWithError(c, "/members/new", fmt.Errorf("no active project to invite into"))
	}
	if ref.OrgID == "" {
		return redirectWithError(c, "/members/new", fmt.Errorf("the active project has no organization"))
	}
	email := strings.TrimSpace(c.FormValue("email"))
	if email == "" {
		return redirectWithError(c, "/members/new", fmt.Errorf("email is required"))
	}
	role := strings.TrimSpace(c.FormValue("role"))
	if role != "project_admin" && role != "project_user" {
		return redirectWithError(c, "/members/new", fmt.Errorf("invalid role"))
	}
	if err := s.createProjectInvite(ctx, ref, email, role); err != nil {
		return redirectWithError(c, "/members/new", err)
	}
	return c.Redirect(http.StatusSeeOther, "/members?ok=1")
}

// uiRevokeInvite handles one revoke form (POST /invites/:id/revoke) on the
// members page: it cancels a still-pending sent invite.
func (s *Server) uiRevokeInvite(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, "/members", fmt.Errorf("invite id is required"))
	}
	if err := s.memory.CancelInvite(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/members", err)
	}
	return c.Redirect(http.StatusSeeOther, "/members?revoked=1")
}

// uiAcceptInvite handles one accept form (POST /invites/:id/accept) on the
// invitations page (/profile/invitations): it accepts a pending invite via its
// token (the form carries the token, not the id — accepting is token-bound).
func (s *Server) uiAcceptInvite(c echo.Context) error {
	token := strings.TrimSpace(c.FormValue("token"))
	if token == "" {
		return redirectWithError(c, "/profile/invitations", fmt.Errorf("invite token is required"))
	}
	if err := s.memory.AcceptInvite(c.Request().Context(), token); err != nil {
		return redirectWithError(c, "/profile/invitations", err)
	}
	return c.Redirect(http.StatusSeeOther, "/profile/invitations?accepted=1")
}

// uiDeclineInvite handles one decline form (POST /invites/:id/decline) on the
// invitations page (/profile/invitations) for a pending invite addressed to
// the signed-in user.
func (s *Server) uiDeclineInvite(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, "/profile/invitations", fmt.Errorf("invite id is required"))
	}
	if err := s.memory.DeclineInvite(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/profile/invitations", err)
	}
	return c.Redirect(http.StatusSeeOther, "/profile/invitations?declined=1")
}

// uiUpdateProfile handles the profile edit form (POST /profile; form fields
// firstName, lastName, displayName, phoneE164). On success it redirects to the
// profile page with ?ok=1.
func (s *Server) uiUpdateProfile(c echo.Context) error {
	in := UpdateUserProfileDto{
		FirstName:   strings.TrimSpace(c.FormValue("firstName")),
		LastName:    strings.TrimSpace(c.FormValue("lastName")),
		DisplayName: strings.TrimSpace(c.FormValue("displayName")),
		PhoneE164:   strings.TrimSpace(c.FormValue("phoneE164")),
	}
	if _, err := s.memory.UpdateProfile(c.Request().Context(), in); err != nil {
		return redirectWithError(c, "/profile", err)
	}
	return c.Redirect(http.StatusSeeOther, "/profile?ok=1")
}

// avatarMaxUploadSize caps profile-photo uploads at 512 KiB, mirroring the
// memory backend's maxAvatarSize for PUT /api/user/avatar.
const avatarMaxUploadSize = 512 * 1024

// uiUploadAvatar handles the profile-photo upload form (POST /profile/avatar;
// multipart field "file", PRG): it forwards the image to memory, records the
// returned avatar URL as the session's avatar override (re-issuing the cookie
// so the account menu picks it up immediately), and redirects to the profile
// page. In dev mode there is no session to carry an override, so it redirects
// home like the other session-only profile flows.
func (s *Server) uiUploadAvatar(c echo.Context) error {
	if s.cfg.AuthMode != "session" {
		return c.Redirect(http.StatusFound, "/")
	}
	file, err := c.FormFile("file")
	if err != nil {
		return redirectWithError(c, "/profile", fmt.Errorf("upload: select a file"))
	}
	if file.Size > avatarMaxUploadSize {
		return redirectWithError(c, "/profile", fmt.Errorf("upload: file too large (max %d bytes)", avatarMaxUploadSize))
	}
	src, err := file.Open()
	if err != nil {
		return redirectWithError(c, "/profile", err)
	}
	defer func() { _ = src.Close() }()

	dto, err := s.memory.UploadAvatar(c.Request().Context(), file.Filename, src)
	if err != nil {
		return redirectWithError(c, "/profile", err)
	}
	if claims, cerr := s.currentSession(c); cerr == nil {
		claims.AvatarOverrideURL = dto.AvatarUrl
		s.setSessionCookie(c, *claims)
	}
	return c.Redirect(http.StatusSeeOther, "/profile?avatar=1")
}

// uiRemoveAvatar handles the profile-photo remove form (POST
// /profile/avatar/remove, PRG): it deletes the photo in memory, clears the
// session's avatar override (re-issuing the cookie), and redirects to the
// profile page.
func (s *Server) uiRemoveAvatar(c echo.Context) error {
	if s.cfg.AuthMode != "session" {
		return c.Redirect(http.StatusFound, "/")
	}
	if _, err := s.memory.DeleteAvatar(c.Request().Context()); err != nil {
		return redirectWithError(c, "/profile", err)
	}
	if claims, cerr := s.currentSession(c); cerr == nil {
		claims.AvatarOverrideURL = ""
		s.setSessionCookie(c, *claims)
	}
	return c.Redirect(http.StatusSeeOther, "/profile?avatar=removed")
}

// avatarProxy serves the signed-in user's avatar image bytes (GET
// /api/user/avatar) — the gateway-relative URL UserProfileDto.AvatarUrl points
// at. Memory's raw bytes stream through with their Content-Type; a backend 404
// (no avatar) maps to a gateway 404 so an <img> referencing a stale override
// simply renders nothing.
func (s *Server) avatarProxy(c echo.Context) error {
	body, contentType, err := s.memory.GetAvatar(c.Request().Context())
	if err != nil {
		if isMemoryNotFound(err) {
			return c.NoContent(http.StatusNotFound)
		}
		return c.NoContent(http.StatusBadGateway)
	}
	defer func() { _ = body.Close() }()
	if contentType != "" {
		c.Response().Header().Set(echo.HeaderContentType, contentType)
	}
	if _, err := io.Copy(c.Response(), body); err != nil {
		return err
	}
	return nil
}

// --- helpers ---

// activeProjectRef resolves the session's active project against ListProjects,
// falling back to the static dev-mode project when no session is attached. ok
// is false when the project cannot be resolved (unknown id or a failed list).
func (s *Server) activeProjectRef(ctx context.Context) (*ProjectRef, bool) {
	id := s.cfg.MemoryProjectID
	if sc, ok := sessionContextFrom(ctx); ok && sc.ProjectID != "" {
		id = sc.ProjectID
	}
	if id == "" {
		return nil, false
	}
	projects, err := s.memory.ListProjects(ctx)
	if err != nil {
		return nil, false
	}
	for i := range projects {
		if projects[i].ID == id {
			return &projects[i], true
		}
	}
	return nil, false
}

// roleIntent maps a membership/invite role to a badge colour: admin roles read
// as primary, plain memberships as neutral/ghost.
func roleIntent(role string) ui.BadgeIntent {
	switch role {
	case "org_admin", "project_admin":
		return ui.BadgePrimary
	case "project_user":
		return ui.BadgeNeutral
	default:
		return ui.BadgeGhost
	}
}

// roleLabel is the human display name of a role enum value.
func roleLabel(role string) string {
	switch role {
	case "org_admin":
		return "org admin"
	case "org_member":
		return "org member"
	case "project_admin":
		return "project admin"
	case "project_user":
		return "project user"
	default:
		return role
	}
}

// profileDisplayName is the best label for the profile header: display name,
// else first+last, else a placeholder.
func profileDisplayName(p UserProfileDto) string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	if full := profileFullName(p); full != "" {
		return full
	}
	return "No name set"
}

// profileFullName joins the first and last name when present.
func profileFullName(p UserProfileDto) string {
	return strings.TrimSpace(p.FirstName + " " + p.LastName)
}

// adminCount counts the project_admins in a member list.
func adminCount(members []ProjectMemberDto) int {
	n := 0
	for _, m := range members {
		if m.Role == "project_admin" {
			n++
		}
	}
	return n
}

// canRemoveMember reports whether the members page may offer a remove
// affordance for m. The client cannot know whether removing an admin leaves
// the project without any (memory enforces that server-side), so the rule is:
// a project_admin is removable only when at least one other admin remains.
func canRemoveMember(members []ProjectMemberDto, m ProjectMemberDto) bool {
	if m.Role != "project_admin" {
		return true
	}
	return adminCount(members) > 1
}

// errLastAdmin marks Memory's 403 rejection of a remove that would leave the
// active project without an admin. Handlers inspect it with errors.Is to offer
// a flow-specific message (remove vs. role change) without string matching.
var errLastAdmin = errors.New("cannot remove the last admin")

// isLastAdminError reports whether err is Memory's last-admin guard. Memory
// surfaces it as a text error, so the check is a substring test.
func isLastAdminError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "last-admin")
}

// findMember returns the active project's member with id, or nil when no such
// member exists. A ListMembers failure is returned as-is.
func (s *Server) findMember(ctx context.Context, userID string) (*ProjectMemberDto, error) {
	members, err := s.memory.ListMembers(ctx)
	if err != nil {
		return nil, err
	}
	if i := slices.IndexFunc(members, func(m ProjectMemberDto) bool { return m.ID == userID }); i >= 0 {
		return &members[i], nil
	}
	return nil, nil
}

// removeProjectMember removes a member and maps Memory's last-admin 403 to the
// errLastAdmin sentinel (with an operator-facing message) so callers can either
// surface it directly or reword it for their flow. Shared by the members-page
// remove flow and the remove half of a role change.
func (s *Server) removeProjectMember(ctx context.Context, userID string) error {
	if err := s.memory.RemoveMember(ctx, userID); err != nil {
		if isLastAdminError(err) {
			return fmt.Errorf("%w; assign another admin first", errLastAdmin)
		}
		return err
	}
	return nil
}

// createProjectInvite invites email into the given org/project with role — the
// single invite-creation path shared by the standalone invite form and the
// re-invite half of a role change.
func (s *Server) createProjectInvite(ctx context.Context, ref *ProjectRef, email, role string) error {
	_, err := s.memory.CreateInvite(ctx, CreateInviteDto{
		OrgID:     ref.OrgID,
		ProjectID: ref.ID,
		Email:     email,
		Role:      role,
	})
	return err
}

// changeMemberRole performs the remove + re-invite that stands in for a role
// change (Memory has no member-role PATCH). It removes the member, then invites
// the same email into the same org/project with the new role. Removal is the
// point of no return: if the re-invite then fails the error makes the partial
// failure explicit ("member removed, but …") so the operator re-invites by hand
// instead of believing the change succeeded.
func (s *Server) changeMemberRole(ctx context.Context, ref *ProjectRef, member ProjectMemberDto, role string) error {
	if member.Email == "" {
		return fmt.Errorf("member has no email address to re-invite")
	}
	if err := s.removeProjectMember(ctx, member.ID); err != nil {
		if errors.Is(err, errLastAdmin) {
			return fmt.Errorf("cannot change the last admin's role; assign another admin first")
		}
		return err
	}
	if err := s.createProjectInvite(ctx, ref, member.Email, role); err != nil {
		return fmt.Errorf("member removed, but the new %s invite failed: %w — re-invite them from /members/new", roleLabel(role), err)
	}
	return nil
}

// memberDisplayName is the best human label for a member row: display name,
// else first+last, else the email.
func memberDisplayName(m ProjectMemberDto) string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	if firstLast := strings.TrimSpace(m.FirstName + " " + m.LastName); firstLast != "" {
		return firstLast
	}
	return m.Email
}

// memberFullName joins the member's first and last name when present.
func memberFullName(m ProjectMemberDto) string {
	return strings.TrimSpace(m.FirstName + " " + m.LastName)
}

// pendingInviteMeta renders one pending invite's context line: the target
// project (when the invite is project-scoped), the organization, and when it
// was sent (plus expiry when known).
func pendingInviteMeta(inv PendingInviteDto) string {
	var b strings.Builder
	if inv.ProjectName != "" {
		b.WriteString("Project ")
		b.WriteString(inv.ProjectName)
		b.WriteString(" in ")
	}
	b.WriteString(inv.OrganizationName)
	b.WriteString(" · Invited ")
	b.WriteString(relTime(inv.CreatedAt))
	if inv.ExpiresAt != "" {
		b.WriteString(" · expires ")
		b.WriteString(relTime(inv.ExpiresAt))
	}
	return b.String()
}
