package auth

import (
	"context"

	"github.com/uptrace/bun"
)

// CanGrantAdminAll is the single app-side decision check for organization-scoped
// authorization (issue #812 §4.3): an active superadmin_full (core.superadmins,
// revoked_at IS NULL AND role = 'superadmin_full') OR an org_admin in at least
// one organization (kb.organization_memberships). It is consumed by
// domain/apitoken.CanGrantAdminAll instead of a bespoke EXISTS query.
//
// The check deliberately consults no project tier: a bare project_admin never
// qualifies. A superadmin_readonly grant does not qualify (issue #810/#812 Q9),
// and the existing any-organization semantics of org_admin eligibility are
// preserved.
func CanGrantAdminAll(ctx context.Context, db bun.IDB, userID string) (bool, error) {
	var allowed bool
	err := db.NewRaw(`
		SELECT
			EXISTS(SELECT 1 FROM core.superadmins WHERE user_id = ? AND revoked_at IS NULL AND role = 'superadmin_full')
			OR
			EXISTS(SELECT 1 FROM kb.organization_memberships WHERE user_id = ? AND role = 'org_admin')
	`, userID, userID).Scan(ctx, &allowed)
	if err != nil {
		return false, err
	}
	return allowed, nil
}
