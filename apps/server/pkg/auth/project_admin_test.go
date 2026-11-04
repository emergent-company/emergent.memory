package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestCanAdministerProject pins the single app-side project-administration
// decision seam against a real throwaway database. The no-widening property is
// the point: CanAdministerProject must NOT admit an org_admin or a platform
// superadmin — a project-scoped decision point must not be widened by org or
// platform authority.
func TestCanAdministerProject(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	projectA := uuid.NewString()
	projectB := uuid.NewString()

	projectAdminA := seedAuthTestUser(t, ctx, db)
	plainMemberA := seedAuthTestUser(t, ctx, db)
	projectAdminB := seedAuthTestUser(t, ctx, db)
	orgAdminOnly := seedAuthTestUser(t, ctx, db)
	superadminOnly := seedAuthTestUser(t, ctx, db)

	mustExecAuth(t, ctx, db, `INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, 'project_admin')`, projectA, projectAdminA)
	mustExecAuth(t, ctx, db, `INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, 'member')`, projectA, plainMemberA)
	mustExecAuth(t, ctx, db, `INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, 'project_admin')`, projectB, projectAdminB)
	mustExecAuth(t, ctx, db, `INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`, uuid.NewString(), orgAdminOnly)
	mustExecAuth(t, ctx, db, `INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, superadminOnly)

	cases := []struct {
		name      string
		projectID string
		userID    string
		want      bool
	}{
		{"project_admin of the addressed project is authorized", projectA, projectAdminA, true},
		{"plain member is refused", projectA, plainMemberA, false},
		{"project_admin of another project is refused", projectA, projectAdminB, false},
		{"org_admin alone is refused (no widening)", projectA, orgAdminOnly, false},
		{"platform superadmin is refused (no widening)", projectA, superadminOnly, false},
		{"empty project is refused", "", projectAdminA, false},
		{"empty user is refused", projectA, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanAdministerProject(ctx, db, tc.projectID, tc.userID)
			if err != nil {
				t.Fatalf("CanAdministerProject(%q, %q): %v", tc.projectID, tc.userID, err)
			}
			if got != tc.want {
				t.Fatalf("CanAdministerProject(%q, %q) = %v, want %v", tc.projectID, tc.userID, got, tc.want)
			}
		})
	}

	t.Run("nil db is refused", func(t *testing.T) {
		got, err := CanAdministerProject(ctx, nil, projectA, projectAdminA)
		if err != nil {
			t.Fatalf("CanAdministerProject(nil db): %v", err)
		}
		if got {
			t.Fatalf("CanAdministerProject with a nil db = true, want false (fail closed)")
		}
	})
}
