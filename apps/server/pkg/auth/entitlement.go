package auth

import (
	"context"

	"github.com/uptrace/bun"
)

// CanGrantAdminAll is the single app-side decision check for platform-scoped
// authorization (issue #812 §4.3, superseded by issue #949): only an active
// superadmin_full (core.superadmins, revoked_at IS NULL AND
// role = 'superadmin_full') may mint a platform-tier token scope (admin or
// admin:all). Bare admin is folded into admin:all + platform per #1041 E1, so
// the same check gates both. An org_admin membership no longer qualifies:
// org-scoped authority must not buy platform-scoped power (an org_admin could
// otherwise mint an account-level admin:all token and reach cross-tenant
// platform surfaces). This reverses the earlier #812 §4.3 decision that
// admitted any-org org_admin. It is consumed by domain/apitoken.CanGrantAdminAll
// instead of a bespoke EXISTS query.
//
// The check deliberately consults no project tier: a bare project_admin never
// qualifies. A superadmin_readonly grant does not qualify (issue #810/#812 Q9).
func CanGrantAdminAll(ctx context.Context, db bun.IDB, userID string) (bool, error) {
	var allowed bool
	err := db.NewRaw(`
		SELECT EXISTS(
			SELECT 1 FROM core.superadmins WHERE user_id = ? AND revoked_at IS NULL AND role = 'superadmin_full'
		)
	`, userID).Scan(ctx, &allowed)
	if err != nil {
		return false, err
	}
	return allowed, nil
}
