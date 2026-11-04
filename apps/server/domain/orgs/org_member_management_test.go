package orgs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func orgAdminFake() *fakeOrgRepo {
	return &fakeOrgRepo{member: true, role: OrgRoleAdmin}
}

func TestServiceAddMember_Success(t *testing.T) {
	repo := orgAdminFake()
	repo.foundUser = &orgMemberUser{ID: "user-2", Email: "new@example.com"}
	repo.addedMember = &OrgMemberDTO{ID: "user-2", Email: "new@example.com", Role: OrgRoleMember}
	svc := testOrgService(repo)

	member, err := svc.AddMember(context.Background(), "org-1", "admin-1", " New@Example.com ", OrgRoleMember)

	require.NoError(t, err)
	require.NotNil(t, member)
	assert.Equal(t, "user-2", member.ID)
	assert.Equal(t, "New@Example.com", repo.findUserEmail)
	assert.Equal(t, "org-1", repo.addedOrgID)
	assert.Equal(t, "user-2", repo.addedUserID)
	assert.Equal(t, OrgRoleMember, repo.addedRole)
}

func TestServiceAddMember_UnknownEmailIs404(t *testing.T) {
	repo := orgAdminFake()
	svc := testOrgService(repo)

	_, err := svc.AddMember(context.Background(), "org-1", "admin-1", "nobody@example.com", OrgRoleMember)

	expectHTTPStatus(t, err, 404)
	assert.Zero(t, repo.addMemberCalls)
}

func TestServiceAddMember_InvalidRoleIs400(t *testing.T) {
	svc := testOrgService(orgAdminFake())

	_, err := svc.AddMember(context.Background(), "org-1", "admin-1", "a@example.com", "owner")

	expectHTTPStatus(t, err, 400)
}

func TestServiceAddMember_RequiresOrgAdmin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		repo   *fakeOrgRepo
		caller string
		want   int
	}{
		{"unauthenticated", &fakeOrgRepo{}, "", 401},
		{"plain member", &fakeOrgRepo{member: true, role: OrgRoleMember}, "user-1", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := testOrgService(tc.repo)
			_, err := svc.AddMember(context.Background(), "org-1", tc.caller, "a@example.com", OrgRoleMember)
			expectHTTPStatus(t, err, tc.want)
		})
	}
}

func TestServiceUpdateMemberRole_Success(t *testing.T) {
	repo := orgAdminFake()
	repo.updateRoleResult = true
	repo.membershipRoleOverride = OrgRoleMember
	repo.hasMembershipRoleOverride = true
	svc := testOrgService(repo)

	require.NoError(t, svc.UpdateMemberRole(context.Background(), "org-1", "admin-1", "user-2", OrgRoleAdmin))
	assert.Equal(t, 1, repo.updateRoleCalls)
	assert.Equal(t, OrgRoleAdmin, repo.updatedRole)
}

func TestServiceUpdateMemberRole_LastAdminIs403(t *testing.T) {
	repo := orgAdminFake()
	repo.adminCount = 1
	repo.membershipRoleOverride = OrgRoleAdmin
	repo.hasMembershipRoleOverride = true
	svc := testOrgService(repo)

	err := svc.UpdateMemberRole(context.Background(), "org-1", "admin-1", "admin-2", OrgRoleMember)

	expectHTTPStatus(t, err, 403)
	assert.Zero(t, repo.updateRoleCalls)
}

func TestServiceUpdateMemberRole_MissingMemberIs404(t *testing.T) {
	repo := orgAdminFake()
	repo.role = "" // no membership for the caller lookup is separate; overwrite below
	repo.member = true
	svc := testOrgService(repo)
	// IsOrgAdmin uses role, so keep admin for the gate while GetMembershipRole
	// returns empty via a dedicated fake.
	repo.role = OrgRoleAdmin
	repo.membershipRoleOverride = ""
	repo.hasMembershipRoleOverride = true

	err := svc.UpdateMemberRole(context.Background(), "org-1", "admin-1", "user-2", OrgRoleMember)

	expectHTTPStatus(t, err, 404)
	assert.Zero(t, repo.updateRoleCalls)
}

func TestServiceRemoveMember_LastAdminIs403(t *testing.T) {
	repo := orgAdminFake()
	repo.adminCount = 1
	repo.membershipRoleOverride = OrgRoleAdmin
	repo.hasMembershipRoleOverride = true
	svc := testOrgService(repo)

	err := svc.RemoveMember(context.Background(), "org-1", "admin-1", "admin-2")

	expectHTTPStatus(t, err, 403)
	assert.Zero(t, repo.removeCalls)
}

func TestServiceRemoveMember_SoleProjectAdminIs409(t *testing.T) {
	repo := orgAdminFake()
	repo.soleProjects = []string{"Project A"}
	repo.membershipRoleOverride = OrgRoleMember
	repo.hasMembershipRoleOverride = true
	svc := testOrgService(repo)

	err := svc.RemoveMember(context.Background(), "org-1", "admin-1", "user-2")

	expectHTTPStatus(t, err, 409)
	assert.Zero(t, repo.removeCalls)
}

func TestServiceRemoveMember_CascadesAndRevokesTokens(t *testing.T) {
	repo := orgAdminFake()
	repo.removed = true
	repo.removedProjectIDs = []string{"p1", "p2"}
	repo.membershipRoleOverride = OrgRoleMember
	repo.hasMembershipRoleOverride = true
	revoker := &fakeTokenRevoker{}
	svc := testOrgService(repo)
	svc.tokenRevoker = revoker

	require.NoError(t, svc.RemoveMember(context.Background(), "org-1", "admin-1", "user-2"))

	assert.Equal(t, 1, repo.removeCalls)
	assert.Equal(t, []string{"p1", "p2"}, revoker.revoked)
}

func TestServiceRemoveMember_MissingIs404(t *testing.T) {
	repo := orgAdminFake()
	repo.removed = false
	repo.membershipRoleOverride = ""
	repo.hasMembershipRoleOverride = true
	svc := testOrgService(repo)

	err := svc.RemoveMember(context.Background(), "org-1", "admin-1", "user-2")

	expectHTTPStatus(t, err, 404)
}
