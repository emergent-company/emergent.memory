package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/emergent-company/go-daisy/components/layout"
	"github.com/labstack/echo/v4"
)

// --- Organizations ---

// orgFixtures returns a two-org access tree for render tests.
func orgFixtures() []OrgWithProjectsDto {
	return []OrgWithProjectsDto{
		{
			ID: "o1", Name: "Acme", Role: "org_admin",
			Projects: []ProjectAccessDto{
				{ID: "p1", Name: "Home", OrgID: "o1", Role: "project_admin"},
				{ID: "p2", Name: "Lab", OrgID: "o1", Role: "project_user"},
			},
		},
		{
			ID: "o2", Name: "Globex", Role: "org_member",
			Projects: []ProjectAccessDto{{ID: "p3", Name: "Main", OrgID: "o2", Role: "project_user"}},
		},
	}
}

// TestRenderOrgsPage asserts the orgs list renders the access tree (org name +
// role, nested project + role) and a "Create organization" action pointing at
// the standalone create page.
func TestRenderOrgsPage(t *testing.T) {
	data := orgsPageData{Orgs: orgFixtures()}
	html := renderHTML(t, OrgsPage(data))
	for _, want := range []string{
		"Organizations", "Acme", "Globex", "org admin", "org member",
		"Home", "Lab", "Main", "project admin", "project user",
		"Create organization", `href="/orgs/new"`,
		// each project row is a clickable activate form
		`action="/projects/activate"`, `name="projectId" value="p1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("orgs page missing %q", want)
		}
	}
	// one activate form per project (3 fixtures)
	if got := strings.Count(html, `action="/projects/activate"`); got != 3 {
		t.Errorf("want 3 activate forms (one per project), got %d", got)
	}
	// the create form no longer lives on the list page
	if strings.Contains(html, `action="/orgs"`) {
		t.Error("orgs list must not embed the create form (it moved to /orgs/new)")
	}
	if strings.Contains(html, "No organizations yet") {
		t.Error("populated orgs page must not render the empty state")
	}
}

// TestRenderOrgsPageEmpty asserts the empty state renders with a create CTA.
func TestRenderOrgsPageEmpty(t *testing.T) {
	html := renderHTML(t, OrgsPage(orgsPageData{}))
	for _, want := range []string{"No organizations yet", `href="/orgs/new"`, "Create organization"} {
		if !strings.Contains(html, want) {
			t.Errorf("empty orgs page missing %q", want)
		}
	}
	if strings.Contains(html, ">Acme<") {
		t.Error("empty orgs page must not render org content")
	}
}

// TestRenderOrgsPageError asserts the whole-page error state.
func TestRenderOrgsPageError(t *testing.T) {
	html := renderHTML(t, OrgsPage(orgsPageData{LoadErr: errTest}))
	if !strings.Contains(html, "Organizations unavailable") {
		t.Error("orgs load-error state missing")
	}
}

// TestRenderOrgsPageProjectlessOrg asserts an org with no projects renders the
// no-projects note.
func TestRenderOrgsPageProjectlessOrg(t *testing.T) {
	data := orgsPageData{Orgs: []OrgWithProjectsDto{{ID: "o1", Name: "Empty Co", Role: "org_admin"}}}
	html := renderHTML(t, OrgsPage(data))
	for _, want := range []string{"Empty Co", "org admin", "No projects in this organization yet."} {
		if !strings.Contains(html, want) {
			t.Errorf("projectless org missing %q", want)
		}
	}
}

// TestRenderOrgsPageInviteAffordance asserts the orgs access tree offers an
// org-level "Invite" action for org_admin rows (linking to /orgs/:id/invite)
// and none for org_member rows.
func TestRenderOrgsPageInviteAffordance(t *testing.T) {
	html := renderHTML(t, OrgsPage(orgsPageData{Orgs: orgFixtures()}))
	for _, want := range []string{`href="/orgs/o1/invite"`, "Invite"} {
		if !strings.Contains(html, want) {
			t.Errorf("orgs page missing org-admin invite affordance %q", want)
		}
	}
	if strings.Contains(html, `href="/orgs/o2/invite"`) {
		t.Error("org_member row must not offer the org invite affordance")
	}
}

// TestRenderOrgInvitePage asserts the org-level invite form: it posts to
// POST /orgs/:id/invite carrying the org id + the fixed org_admin role and NO
// projectId, with the live user-search autofill on the email field.
func TestRenderOrgInvitePage(t *testing.T) {
	data := orgInvitePageData{Org: &Org{ID: "o1", Name: "Acme"}}
	html := renderHTML(t, OrgInvitePage(data))
	for _, want := range []string{
		"Invite a member", "Acme",
		`action="/orgs/o1/invite"`, `name="orgId" value="o1"`,
		`name="email"`, `id="org-invite-email"`, `list="org-invite-email-options"`,
		`hx-get="/partial/invite-user-search"`,
		`name="role"`, `value="org_admin"`, "Send invitation",
		`href="/orgs/o1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("org invite page missing %q", want)
		}
	}
	if strings.Contains(html, `name="projectId"`) {
		t.Error("org invite form must not carry a projectId")
	}
	// a failed org fetch renders the whole-page error state, not the form
	broken := renderHTML(t, OrgInvitePage(orgInvitePageData{LoadErr: errTest}))
	if !strings.Contains(broken, "Organization unavailable") || strings.Contains(broken, "Send invitation") {
		t.Error("org invite load-error state missing")
	}
}

// TestRenderOrgCreatePage asserts /orgs/new renders the create form: name
// field posting to POST /orgs, with a breadcrumb back to the orgs list.
func TestRenderOrgCreatePage(t *testing.T) {
	html := renderHTML(t, OrgCreatePage(orgCreatePageData{}))
	for _, want := range []string{
		"New organization", `action="/orgs"`, `name="name"`, "Create organization",
		`href="/orgs"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("org create page missing %q", want)
		}
	}
	if h := renderHTML(t, OrgCreatePage(orgCreatePageData{FlashErr: errTest})); !strings.Contains(h, "backend unreachable") {
		t.Error("org create page should surface the PRG error")
	}
}

// --- Members (merged view) ---

// memberFixtures returns a two-member project: one admin, one user.
func memberFixtures() (admin, user ProjectMemberDto) {
	admin = ProjectMemberDto{
		ID: "u-admin", Email: "ada@example.com", DisplayName: "Ada Lovelace",
		FirstName: "Ada", LastName: "Lovelace",
		Role: "project_admin", JoinedAt: "2026-08-01T09:00:00Z",
	}
	user = ProjectMemberDto{
		ID: "u-user", Email: "grace@example.com", DisplayName: "Grace Hopper",
		Role: "project_user", JoinedAt: "2026-08-02T09:00:00Z",
	}
	return admin, user
}

// pendingInviteFixture is a still-pending invitation to the active project.
func pendingInviteFixture() SentInviteDto {
	return SentInviteDto{ID: "inv-1", Email: "lin@example.com", Role: "project_user", Status: "pending", CreatedAt: "2026-08-20T09:00:00Z"}
}

// TestRenderMembersPageMerged asserts the merged members view: confirmed
// members (identity + role + remove) AND pending invitations rendered as
// not-yet-confirmed people (email + invited role + "Not responded" badge +
// revoke), plus the "Add member" action.
func TestRenderMembersPageMerged(t *testing.T) {
	admin, user := memberFixtures()
	admin2 := admin
	admin2.ID = "u-admin2"
	admin2.DisplayName = "Lin"
	data := membersPageData{
		Members:     []ProjectMemberDto{admin, admin2, user},
		Pending:     []SentInviteDto{pendingInviteFixture()},
		ProjectName: "Home",
	}
	html := renderHTML(t, MembersPage(data))
	for _, want := range []string{
		"Members", "Home",
		"Ada Lovelace", "ada@example.com", "project admin",
		"Grace Hopper", "grace@example.com", "project user",
		// pending invitation in a not-yet-confirmed state
		"lin@example.com", "project user", "Not responded",
		`action="/members/u-admin/remove"`, `action="/members/u-user/remove"`,
		`action="/invites/inv-1/revoke"`,
		// confirmed member names link to the details page
		`href="/members/u-admin"`, `href="/members/u-user"`,
		"Add member", `href="/members/new"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("members page missing %q", want)
		}
	}
	// the pending email renders exactly once as a visible row label (the other
	// occurrence is the revoke button's aria-label)
	if got := strings.Count(html, ">lin@example.com<"); got != 1 {
		t.Errorf("pending email text-node count = %d, want 1", got)
	}
}

// TestRenderMembersSoleAdmin asserts the remove affordance is suppressed for
// the sole project_admin while other members keep theirs.
func TestRenderMembersSoleAdmin(t *testing.T) {
	admin, user := memberFixtures()
	html := renderHTML(t, MembersPage(membersPageData{Members: []ProjectMemberDto{admin, user}}))
	if strings.Contains(html, `action="/members/u-admin/remove"`) {
		t.Error("sole admin must not offer a remove affordance")
	}
	if !strings.Contains(html, `action="/members/u-user/remove"`) {
		t.Error("non-admin member should keep its remove affordance")
	}

	// two admins → both admins are removable (removing one leaves another)
	other := admin
	other.ID = "u-admin2"
	other.DisplayName = "Lin"
	two := renderHTML(t, MembersPage(membersPageData{Members: []ProjectMemberDto{admin, other}}))
	for _, want := range []string{`action="/members/u-admin/remove"`, `action="/members/u-admin2/remove"`} {
		if !strings.Contains(two, want) {
			t.Errorf("second admin case missing remove affordance %q", want)
		}
	}
}

// TestRenderMembersPageEmpty asserts the fully-empty state keeps the Add
// member CTA, and that a pending-only list renders (no empty state).
func TestRenderMembersPageEmpty(t *testing.T) {
	html := renderHTML(t, MembersPage(membersPageData{}))
	for _, want := range []string{"No members yet", "Add member", `href="/members/new"`} {
		if !strings.Contains(html, want) {
			t.Errorf("empty members page missing %q", want)
		}
	}
	if strings.Contains(html, "Not responded") {
		t.Error("empty members page must not render invite rows")
	}

	pendingOnly := renderHTML(t, MembersPage(membersPageData{Pending: []SentInviteDto{pendingInviteFixture()}}))
	if !strings.Contains(pendingOnly, "lin@example.com") || strings.Contains(pendingOnly, "No members yet") {
		t.Error("pending-only members view should list the invitation, not the empty state")
	}
}

// TestRenderMembersPageErrors asserts the load-error and pending-fetch-error
// states.
func TestRenderMembersPageErrors(t *testing.T) {
	if h := renderHTML(t, MembersPage(membersPageData{LoadErr: errTest})); !strings.Contains(h, "Members unavailable") {
		t.Error("members load-error state missing")
	}
	// a pending fetch failure degrades to a banner while members still render
	admin, _ := memberFixtures()
	h := renderHTML(t, MembersPage(membersPageData{Members: []ProjectMemberDto{admin}, PendingErr: errTest}))
	for _, want := range []string{"Couldn&#39;t load pending invitations: backend unreachable", "Ada Lovelace"} {
		if !strings.Contains(h, want) {
			t.Errorf("pending-error members page missing %q", want)
		}
	}
}

// --- Member invite form page (/members/new) ---

// TestRenderMemberInvitePage asserts the standalone invite form: email input
// with the live user-search datalist, role selector, posting to POST /members.
func TestRenderMemberInvitePage(t *testing.T) {
	data := memberInvitePageData{ProjectName: "Home", HasProject: true}
	html := renderHTML(t, MemberInvitePage(data))
	for _, want := range []string{
		"Invite a member", "Home",
		`action="/members"`, `name="email"`, `id="invite-email"`, `list="invite-email-options"`,
		`hx-get="/partial/invite-user-search"`, "Send invitation",
		`name="role"`, `value="project_admin"`, `value="project_user"`,
		`href="/members"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("invite page missing %q", want)
		}
	}
	if h := renderHTML(t, MemberInvitePage(memberInvitePageData{})); !strings.Contains(h, "Select or create a project first") || strings.Contains(h, `action="/members"`) {
		t.Error("no-project invite page should show the hint, not the form")
	}
}

// TestRenderUserSearchOptions asserts the search-suggestion fragment renders
// one option per match (email value + display label).
func TestRenderUserSearchOptions(t *testing.T) {
	users := []UserSearchResultDto{
		{ID: "u1", Email: "ada@example.com", DisplayName: "Ada Lovelace"},
		{ID: "u2", Email: "grace@example.com"},
	}
	html := renderHTML(t, userSearchOptions(users))
	for _, want := range []string{
		`<option value="ada@example.com">Ada Lovelace — ada@example.com</option>`,
		`<option value="grace@example.com"></option>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("search options missing %q", want)
		}
	}
	if h := renderHTML(t, userSearchOptions(nil)); strings.Contains(h, "<option") {
		t.Error("empty search must render no options")
	}
}

// --- Member details page (/members/:userId) ---

// TestRenderMemberDetailPage asserts the member details render:
// name/email/first+last name/role/joined, breadcrumb back to Members, the
// role-change control (current role + remove-and-re-invite form), and the
// not-found / load-error states.
func TestRenderMemberDetailPage(t *testing.T) {
	admin, _ := memberFixtures()
	html := renderHTML(t, MemberDetailPage(memberDetailPageData{Member: &admin, CanChangeRole: true}))
	for _, want := range []string{
		"Ada Lovelace", "ada@example.com", "Ada", "Lovelace",
		"project admin", "Joined", "Display name", "First name", "Last name",
		`href="/members"`,
		// role management: current role + change control
		"Current role", "Change role",
		`action="/members/u-admin/role"`, `hx-boost="false"`,
		`name="role"`, `value="project_user"`, `value="project_admin"`,
		// the confirm makes the remove + re-invite consequence explicit
		"removed and re-invited", "must accept the new invitation",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("member details page missing %q", want)
		}
	}

	notFound := renderHTML(t, MemberDetailPage(memberDetailPageData{NotFound: true}))
	if !strings.Contains(notFound, "Member not found") {
		t.Error("member not-found state missing")
	}

	if h := renderHTML(t, MemberDetailPage(memberDetailPageData{LoadErr: errTest})); !strings.Contains(h, "Member unavailable") {
		t.Error("member load-error state missing")
	}
}

// TestRenderMemberDetailSoleAdminRole asserts the role-change form is
// suppressed for the project's sole admin (removing them is server-side
// blocked) in favour of the assign-another-admin hint, and that a failed role
// change surfaces its PRG error on the details page.
func TestRenderMemberDetailSoleAdminRole(t *testing.T) {
	admin, _ := memberFixtures()
	html := renderHTML(t, MemberDetailPage(memberDetailPageData{Member: &admin}))
	if strings.Contains(html, `action="/members/u-admin/role"`) {
		t.Error("sole admin must not offer the role-change form")
	}
	if !strings.Contains(html, "only admin") {
		t.Error("sole admin role-change hint missing")
	}
	broken := renderHTML(t, MemberDetailPage(memberDetailPageData{Member: &admin, CanChangeRole: true, FlashErr: errTest}))
	if !strings.Contains(broken, "backend unreachable") {
		t.Error("details page should surface a failed role-change error")
	}
}

// --- Profile (split: /profile, /profile/invitations, /profile/tokens) ---

// pendingUserInviteFixture is a pending invite addressed to the signed-in user.
func pendingUserInviteFixture() PendingInviteDto {
	return PendingInviteDto{
		ID: "pinv-1", ProjectID: "p1", ProjectName: "Home",
		OrganizationID: "o1", OrganizationName: "Acme",
		Role: "project_admin", Token: "tok-abc", CreatedAt: "2026-08-19T09:00:00Z",
	}
}

// TestRenderInvitationsPendingRows asserts the /profile/invitations page lists
// each pending invitation with accept (token-carrying) and decline forms.
func TestRenderInvitationsPendingRows(t *testing.T) {
	data := invitationsPageData{
		Pending: []PendingInviteDto{pendingUserInviteFixture()},
	}
	html := renderHTML(t, InvitationsPage(data))
	for _, want := range []string{
		"Invitations", "Acme", "Project Home in Acme",
		"project admin",
		`action="/invites/pinv-1/accept"`, `name="token" value="tok-abc"`,
		`action="/invites/pinv-1/decline"`,
		"Accept", "Decline",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("invitations page missing %q", want)
		}
	}

	empty := renderHTML(t, InvitationsPage(invitationsPageData{}))
	if !strings.Contains(empty, "No pending invitations.") {
		t.Error("empty pending-invitations note missing")
	}

	broken := renderHTML(t, InvitationsPage(invitationsPageData{LoadErr: errTest}))
	if !strings.Contains(broken, "Invitations unavailable") || !strings.Contains(broken, "backend unreachable") {
		t.Error("invitations load-error state missing")
	}
}

// TestRenderProfilePageSplit asserts the /profile page renders ONLY the
// identity + edit form — no pending invitations section and no API-token
// section (both moved onto their own pages).
func TestRenderProfilePageSplit(t *testing.T) {
	data := profilePageData{
		Profile: &UserProfileDto{
			ID: "u1", FirstName: "Ada", LastName: "Lovelace",
			DisplayName: "Ada", PhoneE164: "+14155552671",
		},
		Email: "ada@example.com",
	}
	html := renderHTML(t, ProfilePage(data))
	for _, want := range []string{
		"Profile", "Ada", "Ada Lovelace", "+14155552671", "ada@example.com",
		`action="/profile"`,
		`name="displayName"`, `value="Ada"`,
		`name="firstName"`, `value="Ada"`,
		`name="lastName"`, `value="Lovelace"`,
		`name="phoneE164"`, `value="+14155552671"`,
		"Save changes",
		// the split rail
		`href="/profile" aria-current="page"`, `href="/profile/invitations"`, `href="/profile/tokens"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("profile page missing %q", want)
		}
	}
	for _, gone := range []string{"Pending invitations", "Acme", "personal script", `name="name"`} {
		if strings.Contains(html, gone) {
			t.Errorf("profile page must not render %q (split onto its own page)", gone)
		}
	}
}

// TestRenderProfilePageEmptyError asserts the no-name fallback and the
// load-error state.
func TestRenderProfilePageEmptyError(t *testing.T) {
	html := renderHTML(t, ProfilePage(profilePageData{Profile: &UserProfileDto{}}))
	if !strings.Contains(html, "No name set") {
		t.Error("profile without a name should render the fallback label")
	}
	if strings.Contains(html, `name="phoneE164" value="+14155552671"`) {
		t.Error("empty phone must not prefill a value")
	}
	if h := renderHTML(t, ProfilePage(profilePageData{LoadErr: errTest})); !strings.Contains(h, "Profile unavailable") {
		t.Error("profile load-error state missing")
	}
}

// --- Org selector in the create-project form (3.6, unchanged) ---

// createSubmit returns the create-project modal's submit <button> markup so
// assertions about its disabled/id attributes are scoped to that element (the
// modal also renders a disabled placeholder <option>).
func createSubmit(html string) string {
	i := strings.Index(html, `<button type="submit"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(html[i:], "</button>")
	if j < 0 {
		return ""
	}
	return html[i : i+j]
}

// TestRenderNewProjectModalOrgSelector asserts the create-project modal's org
// selector is populated from the org list and required, and that with no orgs
// the submit is disabled and a hint replaces the selector.
func TestRenderNewProjectModalOrgSelector(t *testing.T) {
	orgs := []Org{{ID: "o1", Name: "Acme"}, {ID: "o2", Name: "Globex"}}
	html := renderHTML(t, newProjectModal(orgs, nil))
	for _, want := range []string{
		`id="new-project-org"`,
		`name="orgId"`,
		`required`,
		`<option value="o1">Acme</option>`,
		`<option value="o2">Globex</option>`,
		"Select an organization",
		`action="/projects"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("new-project modal missing %q", want)
		}
	}
	if submit := createSubmit(html); submit == "" || strings.Contains(submit, "disabled") || !strings.Contains(submit, `id="new-project-create"`) {
		t.Errorf("create button must be enabled with an id when orgs exist, got %q", submit)
	}

	noOrgs := renderHTML(t, newProjectModal(nil, nil))
	if !strings.Contains(noOrgs, "You need an organization first.") {
		t.Error("no-orgs hint missing")
	}
	if strings.Contains(noOrgs, `id="new-project-org"`) {
		t.Error("org selector must not render when there are no orgs")
	}
	if submit := createSubmit(noOrgs); submit == "" || !strings.Contains(submit, "disabled") {
		t.Errorf("create button must be disabled when there are no orgs, got %q", submit)
	}
}

// --- Sidebar groups ---

// TestSidebarGroupsWorkspaceAccount asserts the nav has Organizations +
// Members under Workspace, no standalone Invites entry, and no "Account"
// group (identity moved to the topbar account menu); the active-state
// mechanism marks the members family.
func TestSidebarGroupsWorkspaceAccount(t *testing.T) {
	groups := sidebarGroups()
	byLabel := map[string][]layout.SidebarItem{}
	for _, g := range groups {
		byLabel[g.Label] = g.Items
	}
	if _, ok := byLabel["Workspace"]; ok {
		t.Error("Workspace group must be removed (orgs/members moved to the org context)")
	}
	if items, ok := byLabel["Account"]; ok && len(items) > 0 {
		t.Error("Account group must be removed (identity lives in the topbar account menu)")
	}

	// every item across every group: no /invites or account-scoped /orgs, /members
	for _, g := range groups {
		for _, item := range g.Items {
			if item.Href == "/invites" {
				t.Error("no sidebar item may link to the removed /invites page")
			}
			if item.Href == "/orgs" || item.Href == "/members" {
				t.Errorf("project sidebar must not contain account-scoped %q", item.Href)
			}
		}
	}

	// the org context sidebar exposes the org-scoped pages under /orgs/:id
	orgGroups := orgSidebarGroups("org-1")
	hrefs := map[string]bool{}
	for _, g := range orgGroups {
		for _, item := range g.Items {
			hrefs[item.Href] = true
		}
	}
	for _, want := range []string{"/orgs/org-1", "/orgs/org-1/members", "/orgs/org-1/settings"} {
		if !hrefs[want] {
			t.Errorf("org sidebar missing %q", want)
		}
	}
}

// --- Role helper units ---

func TestRoleLabelIntent(t *testing.T) {
	for role, want := range map[string]string{
		"org_admin": "org admin", "org_member": "org member",
		"project_admin": "project admin", "project_user": "project user",
		"mystery": "mystery",
	} {
		if got := roleLabel(role); got != want {
			t.Errorf("roleLabel(%q) = %q, want %q", role, got, want)
		}
	}
	if roleIntent("org_admin") == roleIntent("org_member") {
		t.Error("admin and member roles must map to different badge intents")
	}
	if roleIntent("project_admin") == roleIntent("project_user") {
		t.Error("project admin and user must map to different badge intents")
	}
	if roleIntent("project_admin") != roleIntent("org_admin") {
		t.Error("admin roles should share one intent")
	}
}

func TestCanRemoveMember(t *testing.T) {
	admin, user := memberFixtures()
	members := []ProjectMemberDto{admin, user}
	if canRemoveMember(members, admin) {
		t.Error("sole admin must not be removable")
	}
	if !canRemoveMember(members, user) {
		t.Error("project user should be removable")
	}
	second := admin
	second.ID = "u-admin2"
	two := []ProjectMemberDto{admin, second}
	if !canRemoveMember(two, admin) || !canRemoveMember(two, second) {
		t.Error("each admin should be removable when another admin remains")
	}
}

func TestMemberDisplayAndFullName(t *testing.T) {
	m := ProjectMemberDto{ID: "u1", FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com"}
	// display name wins, then first+last, then the email
	if got := memberDisplayName(m); got != "Ada Lovelace" {
		t.Errorf("display name fallback = %q, want first+last", got)
	}
	if got := memberFullName(m); got != "Ada Lovelace" {
		t.Errorf("full name = %q", got)
	}
	m.DisplayName = "Ada"
	if got := memberDisplayName(m); got != "Ada" {
		t.Errorf("display name = %q", got)
	}
	bare := ProjectMemberDto{ID: "u2", Email: "grace@example.com"}
	if got := memberDisplayName(bare); got != "grace@example.com" {
		t.Errorf("display name fallback = %q, want email", got)
	}
}

// --- Route-level flows ---

// orgMemberUIServer builds an echo server with the org/member/invite/profile
// UI routes bound to the given fake.
func orgMemberUIServer(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/orgs", s.uiOrgs)
	e.GET("/orgs/new", s.uiOrgCreatePage)
	e.POST("/orgs", s.uiCreateOrg)
	e.GET("/members", s.uiMembers)
	e.GET("/members/new", s.uiMemberInvitePage)
	e.POST("/members", s.uiMemberInviteCreate)
	e.GET("/members/:userId", s.uiMemberDetails)
	e.POST("/members/:userId/remove", s.uiRemoveMember)
	e.POST("/members/:userId/role", s.uiChangeMemberRole)
	e.POST("/invites/:id/revoke", s.uiRevokeInvite)
	e.POST("/invites/:id/accept", s.uiAcceptInvite)
	e.POST("/invites/:id/decline", s.uiDeclineInvite)
	e.GET("/profile", s.uiProfile)
	e.GET("/profile/invitations", s.uiProfileInvitations)
	e.POST("/profile", s.uiUpdateProfile)
	e.GET("/partial/invite-user-search", s.uiInviteUserSearch)
	return e
}

// postForm issues a urlencoded POST and returns the recorder.
func postForm(t *testing.T, e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

// TestUIOrgsRoutes exercises the orgs list + create-page GETs and the POST
// create-org PRG flow (success → /orgs?ok=1, failures → /orgs/new?err=).
func TestUIOrgsRoutes(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: orgFixtures()}
	s := &Server{cfg: Config{}, memory: f}
	e := orgMemberUIServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orgs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs = %d, want 200", rec.Code)
	}
	for _, want := range []string{"Acme", "org admin", "Home", "project admin", `href="/orgs/new"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET /orgs missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orgs/new", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `action="/orgs"`) {
		t.Fatalf("GET /orgs/new = %d, want the create form", rec.Code)
	}

	// create org → 303 to the list with ?ok=1
	rec = postForm(t, e, "/orgs", "name=Globex")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs/org-1" {
		t.Fatalf("POST /orgs = %d %q, want 303 /orgs/org-1", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.createdOrgs) != 1 || f.createdOrgs[0].Name != "Globex" {
		t.Errorf("CreateOrg not recorded: %+v", f.createdOrgs)
	}

	// HTMX (boosted) create org → 200 + HX-Redirect: creating an org switches
	// the session context, so the shell must full-load (topbar/sidebar) rather
	// than swap only #main-content (stale-shell bug class).
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/orgs", strings.NewReader("name=Initech"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/orgs/org-2" {
		t.Fatalf("HTMX POST /orgs = %d HX-Redirect %q, want 200 /orgs/org-2", rec.Code, rec.Header().Get("HX-Redirect"))
	}

	// empty name → error flash back to the form page
	rec = postForm(t, e, "/orgs", "name=")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs/new?err=organization+name+is+required" {
		t.Errorf("empty-name POST = %d %q, want /orgs/new error redirect", rec.Code, rec.Header().Get("Location"))
	}

	// memory failure → error flash back to the form page
	f.createOrgErr = errTest
	rec = postForm(t, e, "/orgs", "name=Globex")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.Contains(loc, "err=backend+unreachable") {
		t.Errorf("create failure = %d %q, want /orgs/new error redirect", rec.Code, loc)
	}
}

// TestUIMembersRoutes exercises the merged members GET (confirmed + pending
// invitations only), the details GET (found + not-found), and the remove /
// revoke / invite-create PRG flows.
func TestUIMembersRoutes(t *testing.T) {
	admin, user := memberFixtures()
	f := &fakeMemory{
		projects: []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
		members:  []ProjectMemberDto{admin, user},
		sentInvites: []SentInviteDto{
			{ID: "inv-1", Email: "lin@example.com", Role: "project_user", Status: "pending", CreatedAt: "2026-08-20T09:00:00Z"},
			{ID: "inv-2", Email: "old@example.com", Role: "project_admin", Status: "accepted", CreatedAt: "2026-08-18T09:00:00Z"},
		},
	}
	// dev mode: the static memory project is the active project
	s := &Server{cfg: Config{MemoryProjectID: "p1"}, memory: f}
	e := orgMemberUIServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/members", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /members = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"ada@example.com", "grace@example.com", "project admin", "project user",
		// pending invitation shows; the accepted one is not part of the people view
		"lin@example.com", "Not responded", `action="/invites/inv-1/revoke"`,
		`href="/members/new"`, `href="/members/u-admin"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /members missing %q", want)
		}
	}
	if strings.Contains(body, "old@example.com") {
		t.Error("accepted invitations must not render on the members view")
	}
	// sole admin (one admin here) → no remove form for the admin
	if strings.Contains(body, `action="/members/u-admin/remove"`) {
		t.Error("sole admin must not offer a remove affordance")
	}

	// member details: found + not-found
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/members/u-user", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /members/u-user = %d", rec.Code)
	}
	for _, want := range []string{"Grace Hopper", "grace@example.com", "project user", `href="/members"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("member details missing %q", want)
		}
	}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/members/nope", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Member not found") {
		t.Errorf("unknown member GET = %d, want the not-found state", rec.Code)
	}

	// invite form page
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/members/new", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `action="/members"`) {
		t.Errorf("GET /members/new = %d, want the invite form", rec.Code)
	}

	// create invite → resolved from the active project, 303 to /members?ok=1
	rec = postForm(t, e, "/members", url.Values{"email": {"new@example.com"}, "role": {"project_admin"}}.Encode())
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/members?ok=1" {
		t.Fatalf("POST /members = %d %q, want 303 /members?ok=1", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.createdInvites) != 1 {
		t.Fatalf("CreateInvite not recorded: %+v", f.createdInvites)
	}
	inv := f.createdInvites[0]
	if inv.OrgID != "o1" || inv.ProjectID != "p1" || inv.Email != "new@example.com" || inv.Role != "project_admin" {
		t.Errorf("created invite fields wrong: %+v", inv)
	}

	// invalid role → error flash back to the form page
	rec = postForm(t, e, "/members", url.Values{"email": {"x@example.com"}, "role": {"org_member"}}.Encode())
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=invalid+role") {
		t.Errorf("invalid role = %d %q, want /members/new error redirect", rec.Code, rec.Header().Get("Location"))
	}

	// no active project → error flash back to the form page
	s2 := &Server{cfg: Config{}, memory: f}
	e2 := orgMemberUIServer(s2)
	rec = postForm(t, e2, "/members", url.Values{"email": {"x@example.com"}, "role": {"project_user"}}.Encode())
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=no+active+project") {
		t.Errorf("no-project create = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}

	// remove member
	rec = postForm(t, e, "/members/u-user/remove", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/members?ok=1" || f.removedMember != "u-user" {
		t.Errorf("remove = %d %q (removed %q)", rec.Code, rec.Header().Get("Location"), f.removedMember)
	}

	// revoke a pending invite → back to /members with ?revoked=1
	rec = postForm(t, e, "/invites/inv-1/revoke", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/members?revoked=1" || f.canceledInvite != "inv-1" {
		t.Errorf("revoke = %d %q (canceled %q)", rec.Code, rec.Header().Get("Location"), f.canceledInvite)
	}
}

// TestUIChangeMemberRole exercises the details-page role change. Memory has no
// member-role PATCH, so a change is remove + re-invite: the handler removes the
// member, then invites their own email with the new role. It also covers the
// failure paths: invalid / unchanged role, last-admin removal, re-invite
// failure after removal (partial failure), and an unknown member.
func TestUIChangeMemberRole(t *testing.T) {
	admin, user := memberFixtures()
	newServer := func() (*echo.Echo, *fakeMemory) {
		f := &fakeMemory{
			projects: []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
			members:  []ProjectMemberDto{admin, user},
		}
		s := &Server{cfg: Config{MemoryProjectID: "p1"}, memory: f}
		return orgMemberUIServer(s), f
	}

	// success: project_user → project_admin = remove then re-invite the member's
	// own email (never a form-supplied address) with the new role.
	e, f := newServer()
	rec := postForm(t, e, "/members/u-user/role", url.Values{"role": {"project_admin"}}.Encode())
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/members?role-changed=1" {
		t.Fatalf("change role = %d %q, want 303 /members?role-changed=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.removedMember != "u-user" {
		t.Errorf("remove not recorded before re-invite: %q", f.removedMember)
	}
	if len(f.createdInvites) != 1 {
		t.Fatalf("re-invite not recorded: %+v", f.createdInvites)
	}
	inv := f.createdInvites[0]
	if inv.Email != "grace@example.com" || inv.Role != "project_admin" || inv.OrgID != "o1" || inv.ProjectID != "p1" {
		t.Errorf("re-invite fields wrong: %+v", inv)
	}

	// invalid role → rejected before any mutation
	e, f = newServer()
	rec = postForm(t, e, "/members/u-user/role", url.Values{"role": {"org_admin"}}.Encode())
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.Contains(loc, "/members/u-user?err=invalid+role") {
		t.Errorf("invalid role = %d %q, want details-page error redirect", rec.Code, loc)
	}
	if f.removedMember != "" || len(f.createdInvites) != 0 {
		t.Errorf("invalid role must not mutate: removed=%q invites=%d", f.removedMember, len(f.createdInvites))
	}

	// unchanged role → rejected, no remove/invite
	e, f = newServer()
	rec = postForm(t, e, "/members/u-user/role", url.Values{"role": {"project_user"}}.Encode())
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=member+already+has") || f.removedMember != "" {
		t.Errorf("unchanged role = %q removed=%q, want rejection without mutation", loc, f.removedMember)
	}

	// last-admin removal → clear error, no invite, no success redirect
	e, f = newServer()
	f.removeMemberErr = errors.New("memory: 403 last-admin: cannot remove the last admin")
	rec = postForm(t, e, "/members/u-admin/role", url.Values{"role": {"project_user"}}.Encode())
	loc = rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.Contains(loc, "err=cannot+change+the+last+admin") {
		t.Errorf("last-admin change = %d %q, want details-page error redirect", rec.Code, loc)
	}
	if strings.Contains(loc, "role-changed") || len(f.createdInvites) != 0 {
		t.Errorf("last-admin change must not report success or invite: %q %d", loc, len(f.createdInvites))
	}

	// re-invite failure after removal → explicit partial-failure message
	e, f = newServer()
	f.createInviteErr = errTest
	rec = postForm(t, e, "/members/u-user/role", url.Values{"role": {"project_admin"}}.Encode())
	loc = rec.Header().Get("Location")
	if !strings.Contains(loc, "err=member+removed") || !strings.Contains(loc, "invite+failed") {
		t.Errorf("partial failure = %q, want explicit removed-but-invite-failed error", loc)
	}
	if f.removedMember != "u-user" {
		t.Errorf("partial failure should record the removal, got %q", f.removedMember)
	}

	// unknown member → error, no mutation
	e, f = newServer()
	rec = postForm(t, e, "/members/nope/role", url.Values{"role": {"project_admin"}}.Encode())
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=member+not+found") || f.removedMember != "" {
		t.Errorf("unknown member = %q removed=%q, want not-found rejection", loc, f.removedMember)
	}
}

// TestUIProfileRoutes exercises the split profile area: the /profile GET
// (identity + edit form only, no invitations/tokens), the /profile/invitations
// GET (pending invites with accept/decline), the accept/decline PRG flows
// redirecting to /profile/invitations, and the profile-update PRG flow.
func TestUIProfileRoutes(t *testing.T) {
	f := &fakeMemory{
		profile: &UserProfileDto{
			ID: "u1", FirstName: "Ada", LastName: "Lovelace",
			DisplayName: "Ada", PhoneE164: "+14155552671",
		},
		pendingInvites: []PendingInviteDto{pendingUserInviteFixture()},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := orgMemberUIServer(s)

	// /profile = identity + edit form only (the split moved invitations out)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/profile", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Ada Lovelace", `value="+14155552671"`, `action="/profile"`,
		`href="/profile/invitations"`, `href="/profile/tokens"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /profile missing %q", want)
		}
	}
	for _, gone := range []string{"Acme", `action="/invites/pinv-1/accept"`, `action="/invites/pinv-1/decline"`, "personal script"} {
		if strings.Contains(body, gone) {
			t.Errorf("GET /profile must not render %q (split onto its own page)", gone)
		}
	}

	// /profile/invitations = the pending invites with accept/decline forms
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/profile/invitations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile/invitations = %d, want 200", rec.Code)
	}
	for _, want := range []string{
		"Acme", `action="/invites/pinv-1/accept"`, `action="/invites/pinv-1/decline"`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET /profile/invitations missing %q", want)
		}
	}

	// accept → /profile/invitations?accepted=1, token forwarded
	rec = postForm(t, e, "/invites/pinv-1/accept", "token=tok-abc")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/profile/invitations?accepted=1" || len(f.acceptedTokens) != 1 || f.acceptedTokens[0] != "tok-abc" {
		t.Errorf("accept = %d %q (tokens %v)", rec.Code, rec.Header().Get("Location"), f.acceptedTokens)
	}
	// decline → /profile/invitations?declined=1
	rec = postForm(t, e, "/invites/pinv-1/decline", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/profile/invitations?declined=1" || f.declinedInvite != "pinv-1" {
		t.Errorf("decline = %d %q (declined %q)", rec.Code, rec.Header().Get("Location"), f.declinedInvite)
	}
	// accept without a token → error flash to the invitations page
	rec = postForm(t, e, "/invites/pinv-1/accept", "")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/profile/invitations?err=invite+token+is+required") {
		t.Errorf("tokenless accept = %d %q, want invitations-page error redirect", rec.Code, rec.Header().Get("Location"))
	}

	// profile update → /profile?ok=1 with mapped fields
	bodyForm := url.Values{
		"firstName": {"Grace"}, "lastName": {"Hopper"},
		"displayName": {"Grace Hopper"}, "phoneE164": {"+12125551234"},
	}.Encode()
	rec = postForm(t, e, "/profile", bodyForm)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/profile?ok=1" {
		t.Fatalf("POST /profile = %d %q, want 303 /profile?ok=1", rec.Code, rec.Header().Get("Location"))
	}
	up := f.updatedProfile
	if up.FirstName != "Grace" || up.LastName != "Hopper" || up.DisplayName != "Grace Hopper" || up.PhoneE164 != "+12125551234" {
		t.Errorf("UpdateProfile fields wrong: %+v", up)
	}

	// a nil profile still renders the page (header + sections)
	f.profile = nil
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/profile", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Profile") {
		t.Errorf("nil profile GET = %d, want a rendered profile page", rec.Code)
	}
}

// TestUIProfilePageEmailFallsBackToProfile asserts the /profile identity card
// shows the email from the Memory profile when the session (IdP token) omits
// it.
func TestUIProfilePageEmailFallsBackToProfile(t *testing.T) {
	f := &fakeMemory{profile: &UserProfileDto{
		ID: "u1", FirstName: "Ada", LastName: "Lovelace",
		DisplayName: "Ada Lovelace", Email: "ada@example.com",
	}}
	s := &Server{cfg: Config{}, memory: f}
	e := orgMemberUIServer(s)

	req := httptest.NewRequest(http.MethodGet, "/profile", nil)
	// session present, but its IdP token carried no email
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{
		Token: "sess-token",
		Sub:   "389329982813372426",
		Name:  "Ada Lovelace",
		// Email intentionally empty → must fall back to the profile
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "ada@example.com") {
		t.Errorf("GET /profile missing the profile-fallback email, got %q", body)
	}
	if !strings.Contains(body, "Ada Lovelace") {
		t.Error("GET /profile missing the profile display name")
	}
}

// TestUIInviteUserSearchPartial exercises the user-search htmx partial: ≥2
// chars returns option elements; short queries return none and never call
// SearchUsers.
func TestUIInviteUserSearchPartial(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := orgMemberUIServer(s)

	f.searchUsers = []UserSearchResultDto{{ID: "u1", Email: "ada@example.com", DisplayName: "Ada Lovelace"}}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/invite-user-search?email=ad", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `value="ada@example.com"`) {
		t.Errorf("search partial = %d %q, want option", rec.Code, rec.Body.String())
	}
	if f.lastSearchTerm != "ad" {
		t.Errorf("SearchUsers called with %q, want ad", f.lastSearchTerm)
	}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/invite-user-search?email=a", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "<option") || f.lastSearchTerm != "ad" {
		t.Errorf("short search partial = %d %q, want empty (SearchUsers must not be called)", rec.Code, rec.Body.String())
	}
}
