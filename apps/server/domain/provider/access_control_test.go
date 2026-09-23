package provider

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// newAccessControlRepo builds a Repository wired to an isolated test database.
func newAccessControlRepo(t *testing.T) (*Repository, *testdb.TestDB) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testdb.SetupTestDBOrFail(t, ctx, "accesscontrol")
	return NewRepository(testDB.GetDB(), slog.Default()), testDB
}

// TestAssertCallerOwnsOrg_EmptyContextUnauthorized covers the empty-context case
// that used to self-satisfy (issue #841): with no user and no org in the context,
// the check must fail closed rather than consult a request-controlled org.
func TestAssertCallerOwnsOrg_EmptyContextUnauthorized(t *testing.T) {
	repo, testDB := newAccessControlRepo(t)
	defer testDB.Close()

	err := assertCallerOwnsOrg(context.Background(), repo, uuid.New().String())
	appErr, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", appErr.HTTPStatus)
	}
}

// TestAssertCallerOwnsOrg_NonMemberForbidden verifies a caller whose user is not
// a member of the target org is rejected with 403.
func TestAssertCallerOwnsOrg_NonMemberForbidden(t *testing.T) {
	repo, testDB := newAccessControlRepo(t)
	defer testDB.Close()

	ctx := context.Background()
	db := testDB.GetDB()

	orgID := uuid.New().String()
	userID := uuid.New().String()
	insertOrgAndUser(t, ctx, db, orgID, userID)
	// No membership row → the caller is not a member of orgID.

	err := assertCallerOwnsOrg(auth.ContextWithUser(ctx, &auth.AuthUser{ID: userID}), repo, orgID)
	appErr, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.HTTPStatus != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", appErr.HTTPStatus)
	}
}

// TestAssertCallerOwnsOrg_MemberAllowed verifies a caller whose user is a member
// of the target org is admitted.
func TestAssertCallerOwnsOrg_MemberAllowed(t *testing.T) {
	repo, testDB := newAccessControlRepo(t)
	defer testDB.Close()

	ctx := context.Background()
	db := testDB.GetDB()

	orgID := uuid.New().String()
	userID := uuid.New().String()
	insertOrgAndUser(t, ctx, db, orgID, userID)
	if _, err := db.NewRaw(
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`,
		orgID, userID,
	).Exec(ctx); err != nil {
		t.Fatalf("insert membership: %v", err)
	}

	if err := assertCallerOwnsOrg(auth.ContextWithUser(ctx, &auth.AuthUser{ID: userID}), repo, orgID); err != nil {
		t.Fatalf("expected member to pass, got %v", err)
	}
}

// insertOrgAndUser seeds a minimal org and user profile, mirroring the fixtures
// used by internal/testutil, so the membership FK targets are present.
func insertOrgAndUser(t *testing.T, ctx context.Context, db bun.IDB, orgID, userID string) {
	t.Helper()
	if _, err := db.NewRaw(
		`INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'Org', NOW(), NOW())`,
		orgID,
	).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.NewRaw(
		`INSERT INTO core.user_profiles (id, zitadel_user_id, first_name, last_name, created_at, updated_at) VALUES (?, ?, 'T', 'U', NOW(), NOW())`,
		userID, "sub-"+userID,
	).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}
