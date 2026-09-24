package invites

import (
	"context"
	"log/slog"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/orgs"
)

// TestAcceptInviteGrantsInvitedOrgRole proves the invite-acceptance path grants
// the role the invitation was created with, not a blanket 'member'. It asserts
// the concrete kb.organization_memberships.role via
// orgs.Repository.GetMembershipRole (the same canonical role reader the rest of
// the codebase relies on), so an org_admin invitation must yield "org_admin" and
// a project-scoped invitation must yield "member" — not merely that a row
// exists.
//
// This is a behaviour assertion, not a row-count assertion: with the previous
// hardcoded `role = 'member'` insert, the org_admin case fails because the
// invitee is granted plain member membership.
func TestAcceptInviteGrantsInvitedOrgRole(t *testing.T) {
	cases := []struct {
		name       string
		inviteRole string
		wantRole   string
	}{
		{"org_admin invite grants org_admin membership", "org_admin", "org_admin"},
		{"project_user invite grants member membership", "project_user", "member"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, testDB, orgID, userID := newAcceptService(t)
			defer testDB.Close()

			invite, err := svc.Create(ctx, &CreateInviteRequest{
				OrgID: orgID,
				Email: "invitee@example.com",
				Role:  tc.inviteRole,
			})
			if err != nil {
				t.Fatalf("create invite: %v", err)
			}

			if err := svc.Accept(ctx, userID, invite.Token); err != nil {
				t.Fatalf("accept invite: %v", err)
			}

			repo := orgs.NewRepository(testDB.GetDB(), slog.Default())
			role, err := repo.GetMembershipRole(ctx, orgID, userID)
			if err != nil {
				t.Fatalf("GetMembershipRole: %v", err)
			}
			if role != tc.wantRole {
				t.Fatalf("granted org role = %q, want %q", role, tc.wantRole)
			}
		})
	}
}

// TestAcceptInviteRejectsUnexpectedRole proves the acceptance path fails closed
// on a stored role that Create's validation would never allow, instead of
// inserting it blindly. Such a role must produce an error and grant no
// membership.
func TestAcceptInviteRejectsUnexpectedRole(t *testing.T) {
	ctx := context.Background()
	svc, testDB, orgID, userID := newAcceptService(t)
	defer testDB.Close()

	const token = "unexpected-role-token"
	if _, err := testDB.GetDB().NewRaw(`
		INSERT INTO kb.invites (id, organization_id, email, role, token, status, expires_at, created_at)
		VALUES (uuid_generate_v4(), ?, 'invitee@example.com', 'superadmin_full', ?, 'pending', NOW() + interval '1 day', NOW())
	`, orgID, token).Exec(ctx); err != nil {
		t.Fatalf("insert invite: %v", err)
	}

	if err := svc.Accept(ctx, userID, token); err == nil {
		t.Fatal("Accept must fail closed on an unexpected stored role")
	}

	repo := orgs.NewRepository(testDB.GetDB(), slog.Default())
	role, err := repo.GetMembershipRole(ctx, orgID, userID)
	if err != nil {
		t.Fatalf("GetMembershipRole: %v", err)
	}
	if role != "" {
		t.Fatalf("no membership should be granted for an unexpected role, got %q", role)
	}
}
