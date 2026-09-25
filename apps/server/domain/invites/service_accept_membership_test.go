package invites

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// newAcceptService builds a Service wired to an isolated test database and seeds
// an org, a user profile, the invitee's email address, and an org_admin inviter.
// The invite itself is created through Service.Create so the acceptance path is
// exercised end-to-end. The inviter is seeded because the acceptance path
// re-verifies that an org_admin grant was minted by an org_admin (issue #967).
func newAcceptService(t *testing.T) (*Service, *testdb.TestDB, string, string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testdb.SetupTestDBOrFail(t, ctx, "invites_accept")
	db := testDB.GetDB()
	svc := NewService(db, nil, &config.Config{}, slog.Default())

	orgID := uuid.New().String()
	userID := uuid.New().String()
	zitadelID := uuid.New().String()

	if _, err := db.NewRaw(
		`INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'Invite Org', NOW(), NOW())`,
		orgID,
	).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.NewRaw(
		`INSERT INTO core.user_profiles (id, zitadel_user_id, first_name, last_name, created_at, updated_at)
		 VALUES (?, ?, 'Invitee', 'User', NOW(), NOW())`,
		userID, zitadelID,
	).Exec(ctx); err != nil {
		t.Fatalf("insert user profile: %v", err)
	}
	if _, err := db.NewRaw(
		`INSERT INTO core.user_emails (user_id, email, verified, created_at) VALUES (?, ?, true, NOW())`,
		userID, "invitee@example.com",
	).Exec(ctx); err != nil {
		t.Fatalf("insert user email: %v", err)
	}

	// Seed an org_admin inviter: the acceptance path re-verifies that an
	// org_admin grant was minted by an org_admin (issue #967), so the fixture
	// must provide one.
	inviterID := uuid.New().String()
	if _, err := db.NewRaw(
		`INSERT INTO core.user_profiles (id, zitadel_user_id, first_name, last_name, created_at, updated_at)
		 VALUES (?, ?, 'Inviter', 'Admin', NOW(), NOW())`,
		inviterID, uuid.New().String(),
	).Exec(ctx); err != nil {
		t.Fatalf("insert inviter profile: %v", err)
	}
	if _, err := db.NewRaw(
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
		 VALUES (?, ?, 'org_admin', NOW())`,
		orgID, inviterID,
	).Exec(ctx); err != nil {
		t.Fatalf("insert inviter org membership: %v", err)
	}

	return svc, testDB, orgID, userID, inviterID
}

// TestAcceptInviteGrantsOrgMembership proves the invite-acceptance path actually
// grants the invitee organization membership, using the same org-membership
// check the rest of the codebase relies on (orgs.Repository.IsUserMember reads
// kb.organization_memberships, as do pkg/auth's dbOrgMember/dbOrgAdmin).
//
// This is a behaviour assertion, not a row-count assertion: before the fix the
// accept flow wrote to the non-existent kb.org_memberships table, so Accept
// returned a database error and the invitee never passed a real membership
// check. After the fix the invitee must be a member.
func TestAcceptInviteGrantsOrgMembership(t *testing.T) {
	ctx := context.Background()
	svc, testDB, orgID, userID, inviterID := newAcceptService(t)
	defer testDB.Close()

	invite, err := svc.Create(ctx, &CreateInviteRequest{
		OrgID:     orgID,
		Email:     "invitee@example.com",
		Role:      "project_user",
		InviterID: inviterID,
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	if err := svc.Accept(ctx, userID, invite.Token); err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	repo := orgs.NewRepository(testDB.GetDB(), slog.Default())
	member, err := repo.IsUserMember(ctx, orgID, userID)
	if err != nil {
		t.Fatalf("IsUserMember: %v", err)
	}
	if !member {
		t.Fatal("invitee must be an org member after accepting the invite")
	}
}
