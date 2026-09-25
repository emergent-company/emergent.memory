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

// TestAcceptProjectScopedInviteWritesInvitedProjectRole proves the acceptance
// path writes kb.project_memberships.role exactly equal to the invited
// project-scoped role, never an out-of-vocabulary value (issue #979). The bug
// class is that a project-scoped invite carries its stored role straight into
// kb.project_memberships.role, so a project-scoped org_admin invite would write
// "org_admin" into a project-scoped role column. Pinning the mapping for every
// valid project-scoped role stops that value from silently drifting.
func TestAcceptProjectScopedInviteWritesInvitedProjectRole(t *testing.T) {
	cases := []string{"project_admin", "project_user", "project_viewer"}

	for _, invitedRole := range cases {
		t.Run(invitedRole, func(t *testing.T) {
			ctx := context.Background()
			svc, testDB, orgID, userID, inviterID := newAcceptService(t)
			defer testDB.Close()
			db := testDB.GetDB()

			projectID := uuid.New().String()
			if _, err := db.NewRaw(
				`INSERT INTO kb.projects (id, organization_id, name, created_at, updated_at) VALUES (?, ?, 'Test Project', NOW(), NOW())`,
				projectID, orgID,
			).Exec(ctx); err != nil {
				t.Fatalf("insert project: %v", err)
			}

			invite, err := svc.Create(ctx, &CreateInviteRequest{
				OrgID:     orgID,
				ProjectID: projectID,
				Email:     "invitee@example.com",
				Role:      invitedRole,
				InviterID: inviterID,
			})
			if err != nil {
				t.Fatalf("create invite: %v", err)
			}

			if err := svc.Accept(ctx, userID, invite.Token); err != nil {
				t.Fatalf("accept invite: %v", err)
			}

			var got string
			if err := db.NewRaw(
				`SELECT role FROM kb.project_memberships WHERE project_id = ? AND user_id = ?`,
				projectID, userID,
			).Scan(ctx, &got); err != nil {
				t.Fatalf("read project_memberships.role: %v", err)
			}
			if got != invitedRole {
				t.Fatalf("project_memberships.role = %q, want %q (must equal the invited project-scoped role)", got, invitedRole)
			}
		})
	}
}

// TestAcceptInviteRefusesOrgAdminAfterInviterDemoted proves the acceptance-side
// inviter re-verification (issue #967) against a live demotion: an org_admin
// invitation minted by a then-org_admin inviter is refused once that inviter has
// been demoted to member, and no membership row is written. This is the
// committed regression test for the check #973 added — without it the
// re-verification could be removed with no CI signal (issue #979).
func TestAcceptInviteRefusesOrgAdminAfterInviterDemoted(t *testing.T) {
	ctx := context.Background()
	svc, testDB, orgID, userID, inviterID := newAcceptService(t)
	defer testDB.Close()
	db := testDB.GetDB()

	invite, err := svc.Create(ctx, &CreateInviteRequest{
		OrgID:     orgID,
		Email:     "invitee@example.com",
		Role:      "org_admin",
		InviterID: inviterID,
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	// Demote the inviter from org_admin to a plain member.
	if _, err := db.NewRaw(
		`UPDATE kb.organization_memberships SET role = 'member' WHERE organization_id = ? AND user_id = ?`,
		orgID, inviterID,
	).Exec(ctx); err != nil {
		t.Fatalf("demote inviter: %v", err)
	}

	if err := svc.Accept(ctx, userID, invite.Token); err == nil {
		t.Fatal("Accept must refuse an org_admin invite after the inviter is demoted")
	}

	// Assert no membership row was written (row count, not just the refusal).
	var count int
	if err := db.NewRaw(
		`SELECT COUNT(*) FROM kb.organization_memberships WHERE organization_id = ? AND user_id = ?`,
		orgID, userID,
	).Scan(ctx, &count); err != nil {
		t.Fatalf("count membership rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("no org membership row may be written after refusal, got %d", count)
	}
}

// TestAcceptRefusesLegacyProjectScopedOrgAdmin proves the accept path itself
// fails closed on a PRE-EXISTING (legacy) invitation row that has both a
// project_id and role 'org_admin' (issue #979). Such a row predates the
// create-side rejection and can still exist in kb.invites; accepting it would
// write the out-of-vocabulary value "org_admin" into kb.project_memberships.role
// AND grant org-level admin in kb.organization_memberships. The accept-side
// inviter re-verification (#967) does not catch this — a legacy inviter was a
// genuine org_admin — so the accept path must refuse it directly, fail closed,
// writing no membership rows.
func TestAcceptRefusesLegacyProjectScopedOrgAdmin(t *testing.T) {
	ctx := context.Background()
	svc, testDB, orgID, userID, inviterID := newAcceptService(t)
	defer testDB.Close()
	db := testDB.GetDB()

	projectID := uuid.New().String()
	if _, err := db.NewRaw(
		`INSERT INTO kb.projects (id, organization_id, name, created_at, updated_at) VALUES (?, ?, 'Legacy Project', NOW(), NOW())`,
		projectID, orgID,
	).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	// A legacy-shaped invite row: project_id set AND role = org_admin, minted by
	// a then-org_admin inviter — written directly, bypassing the create gate.
	const token = "legacy-project-org-admin-token"
	if _, err := db.NewRaw(`
		INSERT INTO kb.invites (id, organization_id, project_id, email, role, token, status, expires_at, created_at, invited_by_user_id)
		VALUES (uuid_generate_v4(), ?, ?, 'invitee@example.com', 'org_admin', ?, 'pending', NOW() + interval '1 day', NOW(), ?)
	`, orgID, projectID, token, inviterID).Exec(ctx); err != nil {
		t.Fatalf("insert legacy invite: %v", err)
	}

	if err := svc.Accept(ctx, userID, token); err == nil {
		// Pre-guard this path succeeds and writes junk; surface the values so a
		// regression failure is self-diagnosing.
		var projRole, orgRole string
		if e := db.NewRaw(
			`SELECT role FROM kb.project_memberships WHERE project_id = ? AND user_id = ?`,
			projectID, userID,
		).Scan(ctx, &projRole); e != nil {
			projRole = "<read error: " + e.Error() + ">"
		}
		if e := db.NewRaw(
			`SELECT role FROM kb.organization_memberships WHERE organization_id = ? AND user_id = ?`,
			orgID, userID,
		).Scan(ctx, &orgRole); e != nil {
			orgRole = "<read error: " + e.Error() + ">"
		}
		t.Fatalf("Accept must refuse a legacy project-scoped org_admin invite; it wrote project_memberships.role=%q and organization_memberships.role=%q", projRole, orgRole)
	}

	// No project membership row (and no junk role) may be written.
	var projCount int
	if err := db.NewRaw(
		`SELECT COUNT(*) FROM kb.project_memberships WHERE project_id = ? AND user_id = ?`,
		projectID, userID,
	).Scan(ctx, &projCount); err != nil {
		t.Fatalf("count project membership rows: %v", err)
	}
	if projCount != 0 {
		t.Fatalf("no project_memberships row may be written after refusal, got %d", projCount)
	}

	// No org membership row (and in particular no elevated org_admin grant).
	var orgCount int
	if err := db.NewRaw(
		`SELECT COUNT(*) FROM kb.organization_memberships WHERE organization_id = ? AND user_id = ?`,
		orgID, userID,
	).Scan(ctx, &orgCount); err != nil {
		t.Fatalf("count org membership rows: %v", err)
	}
	if orgCount != 0 {
		t.Fatalf("no organization_memberships row may be written after refusal, got %d", orgCount)
	}
}
