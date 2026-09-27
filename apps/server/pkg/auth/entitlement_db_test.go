package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// These tests pin the single app-side org-administration decision seam
// (issue #812 §4.5, issue #1162) against a real throwaway database. The
// no-widening property is the point: CanAdministerOrg must NOT admit a platform
// superadmin or a project role, while CanAdministerOrgOrPlatform preserves the
// invitation surfaces' existing superadmin_full override without inventing a
// second membership query.

// mustExecAuth runs a seeding statement, failing the test on error.
func mustExecAuth(t *testing.T, ctx context.Context, db *bun.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.NewRaw(query, args...).Exec(ctx); err != nil {
		t.Fatalf("seed %q: %v", query, err)
	}
}

func TestCanAdministerOrg(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	orgA := uuid.NewString()
	orgB := uuid.NewString()

	orgAdminA := seedAuthTestUser(t, ctx, db)
	plainMemberA := seedAuthTestUser(t, ctx, db)
	orgAdminB := seedAuthTestUser(t, ctx, db)
	superadminOnly := seedAuthTestUser(t, ctx, db)
	projectAdminOnly := seedAuthTestUser(t, ctx, db)

	mustExecAuth(t, ctx, db, `INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`, orgA, orgAdminA)
	mustExecAuth(t, ctx, db, `INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'member')`, orgA, plainMemberA)
	mustExecAuth(t, ctx, db, `INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`, orgB, orgAdminB)
	mustExecAuth(t, ctx, db, `INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, superadminOnly)
	mustExecAuth(t, ctx, db, `INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, 'project_admin')`, uuid.NewString(), projectAdminOnly)

	cases := []struct {
		name   string
		orgID  string
		userID string
		want   bool
	}{
		{"org_admin of the addressed org is authorized", orgA, orgAdminA, true},
		{"plain member is refused", orgA, plainMemberA, false},
		{"org_admin of another org is refused", orgA, orgAdminB, false},
		{"platform superadmin without org membership is refused (no widening)", orgA, superadminOnly, false},
		{"project_admin alone is refused", orgA, projectAdminOnly, false},
		{"empty org is refused", "", orgAdminA, false},
		{"empty user is refused", orgA, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanAdministerOrg(ctx, db, tc.orgID, tc.userID)
			if err != nil {
				t.Fatalf("CanAdministerOrg(%q, %q): %v", tc.orgID, tc.userID, err)
			}
			if got != tc.want {
				t.Fatalf("CanAdministerOrg(%q, %q) = %v, want %v", tc.orgID, tc.userID, got, tc.want)
			}
		})
	}

	t.Run("nil db is refused", func(t *testing.T) {
		got, err := CanAdministerOrg(ctx, nil, orgA, orgAdminA)
		if err != nil {
			t.Fatalf("CanAdministerOrg(nil db): %v", err)
		}
		if got {
			t.Fatalf("CanAdministerOrg with a nil db = true, want false (fail closed)")
		}
	})
}

func TestCanAdministerOrgOrPlatform(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	orgA := uuid.NewString()
	orgB := uuid.NewString()

	orgAdminA := seedAuthTestUser(t, ctx, db)
	plainMemberA := seedAuthTestUser(t, ctx, db)
	orgAdminB := seedAuthTestUser(t, ctx, db)
	fullSuperadmin := seedAuthTestUser(t, ctx, db)
	readonlySuperadmin := seedAuthTestUser(t, ctx, db)

	mustExecAuth(t, ctx, db, `INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`, orgA, orgAdminA)
	mustExecAuth(t, ctx, db, `INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'member')`, orgA, plainMemberA)
	mustExecAuth(t, ctx, db, `INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`, orgB, orgAdminB)
	mustExecAuth(t, ctx, db, `INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, fullSuperadmin)
	mustExecAuth(t, ctx, db, `INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_readonly')`, readonlySuperadmin)

	cases := []struct {
		name   string
		orgID  string
		userID string
		want   bool
	}{
		{"org_admin of the addressed org is authorized", orgA, orgAdminA, true},
		{"full superadmin is authorized (preserved invitation override)", orgA, fullSuperadmin, true},
		{"readonly superadmin is refused", orgA, readonlySuperadmin, false},
		{"plain member is refused", orgA, plainMemberA, false},
		{"foreign org_admin is refused", orgA, orgAdminB, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanAdministerOrgOrPlatform(ctx, db, tc.orgID, tc.userID)
			if err != nil {
				t.Fatalf("CanAdministerOrgOrPlatform(%q, %q): %v", tc.orgID, tc.userID, err)
			}
			if got != tc.want {
				t.Fatalf("CanAdministerOrgOrPlatform(%q, %q) = %v, want %v", tc.orgID, tc.userID, got, tc.want)
			}
		})
	}
}
