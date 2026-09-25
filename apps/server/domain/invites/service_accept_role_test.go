package invites

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"

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
			svc, testDB, orgID, userID, inviterID := newAcceptService(t)
			defer testDB.Close()

			invite, err := svc.Create(ctx, &CreateInviteRequest{
				OrgID:     orgID,
				Email:     "invitee@example.com",
				Role:      tc.inviteRole,
				InviterID: inviterID,
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
	svc, testDB, orgID, userID, _ := newAcceptService(t)
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

// TestAcceptInviteRefusesOrgAdminMintedByMember proves the acceptance-side
// defence in depth (issue #967): an org_admin invitation whose recorded inviter
// is a plain member (not org_admin, not superadmin_full) is refused, even though
// the stored role is valid. This closes the pre-existing-invite gap — a member
// who minted an org_admin invite before the create-side gate existed cannot
// self-escalate by accepting it.
func TestAcceptInviteRefusesOrgAdminMintedByMember(t *testing.T) {
	ctx := context.Background()
	svc, testDB, orgID, userID, _ := newAcceptService(t)
	defer testDB.Close()

	// A plain member inviter (not org_admin).
	memberInviterID := uuid.New().String()
	db := testDB.GetDB()
	if _, err := db.NewRaw(
		`INSERT INTO core.user_profiles (id, zitadel_user_id, first_name, last_name, created_at, updated_at)
		 VALUES (?, ?, 'Member', 'Inviter', NOW(), NOW())`,
		memberInviterID, uuid.New().String(),
	).Exec(ctx); err != nil {
		t.Fatalf("insert member inviter profile: %v", err)
	}
	if _, err := db.NewRaw(
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
		 VALUES (?, ?, 'member', NOW())`,
		orgID, memberInviterID,
	).Exec(ctx); err != nil {
		t.Fatalf("insert member inviter membership: %v", err)
	}

	invite, err := svc.Create(ctx, &CreateInviteRequest{
		OrgID:     orgID,
		Email:     "invitee@example.com",
		Role:      "org_admin",
		InviterID: memberInviterID,
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	if err := svc.Accept(ctx, userID, invite.Token); err == nil {
		t.Fatal("Accept must refuse an org_admin invite minted by a non-org_admin inviter")
	}

	repo := orgs.NewRepository(testDB.GetDB(), slog.Default())
	role, err := repo.GetMembershipRole(ctx, orgID, userID)
	if err != nil {
		t.Fatalf("GetMembershipRole: %v", err)
	}
	if role != "" {
		t.Fatalf("no org membership should be granted for an org_admin invite minted by a member, got %q", role)
	}
}

// TestAcceptInviteRefusesOrgAdminWithoutInviter proves the acceptance-side
// defence in depth fails closed when an org_admin invitation has no recorded
// inviter: there is no authority to assert, so the grant is refused (issue #967).
func TestAcceptInviteRefusesOrgAdminWithoutInviter(t *testing.T) {
	ctx := context.Background()
	svc, testDB, orgID, userID, _ := newAcceptService(t)
	defer testDB.Close()

	// An org_admin invite with no inviter recorded (a pre-inviter-tracking
	// invitation, or a row written outside the handler).
	invite, err := svc.Create(ctx, &CreateInviteRequest{
		OrgID: orgID,
		Email: "invitee@example.com",
		Role:  "org_admin",
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	if err := svc.Accept(ctx, userID, invite.Token); err == nil {
		t.Fatal("Accept must refuse an org_admin invite with no recorded inviter")
	}

	repo := orgs.NewRepository(testDB.GetDB(), slog.Default())
	role, err := repo.GetMembershipRole(ctx, orgID, userID)
	if err != nil {
		t.Fatalf("GetMembershipRole: %v", err)
	}
	if role != "" {
		t.Fatalf("no org membership should be granted for an org_admin invite with no inviter, got %q", role)
	}
}
