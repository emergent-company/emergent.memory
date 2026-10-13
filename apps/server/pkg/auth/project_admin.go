package auth

import (
	"context"

	"github.com/uptrace/bun"
)

// CanAdministerProject is the single app-side decision check for project-scoped
// administration: does userID hold project-admin authority over projectID? The
// only source is a project_admin membership in THAT project
// (kb.project_memberships). Fail closed: no membership, a plain member, an empty
// user/project, or a lookup error yields false.
//
// It deliberately consults no org tier and does not admit an org_admin or a
// platform superadmin — a project-scoped decision point must not be widened by
// org or platform authority. This mirrors CanAdministerOrg, which is the
// equivalent org-scoped seam.
func CanAdministerProject(ctx context.Context, db bun.IDB, projectID, userID string) (bool, error) {
	if db == nil || projectID == "" || userID == "" {
		return false, nil
	}
	var ok bool
	err := db.NewRaw(`
		SELECT EXISTS(
			SELECT 1 FROM kb.project_memberships
			WHERE project_id = ? AND user_id = ? AND role = ?
		)
	`, projectID, userID, RoleProjectAdmin).Scan(ctx, &ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}
