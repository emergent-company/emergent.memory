package auth

import (
	"context"

	"github.com/uptrace/bun"
)

// RoleOrgAdmin is the canonical kb.organization_memberships org-administration
// role. It is the single org-scoped authority the org-administration decision
// recognises (issue #812 §4.5, issue #1162). A legacy `owner` row is not a
// grant (migration normalises it to org_admin).
const RoleOrgAdmin = "org_admin"

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

// CanAdministerOrg is the single app-side decision check for org-scoped
// administration (issue #812 §4.5, issue #1162): does userID hold org-admin
// authority over orgID? The only source is an org_admin membership in THAT
// organization (kb.organization_memberships). Fail closed: no membership, a
// plain member, an empty user/org, or a lookup error yields false.
//
// It deliberately consults no project tier and does not admit a platform
// superadmin — org-scoped decision points that previously refused a platform
// superadmin who was not an org_admin keep that refusal, so routing them through
// this seam can only preserve or narrow authority, never widen it. Callers that
// need the platform override (only the org-invitation surfaces, which already
// admitted an active superadmin_full) use CanAdministerOrgOrPlatform.
//
// Every org-scoped decision point MUST consume this check rather than
// re-deriving the role locally (org settings mutations, project create/delete
// within an org).
func CanAdministerOrg(ctx context.Context, db bun.IDB, orgID, userID string) (bool, error) {
	if db == nil || orgID == "" || userID == "" {
		return false, nil
	}
	var ok bool
	err := db.NewRaw(`
		SELECT EXISTS(
			SELECT 1 FROM kb.organization_memberships
			WHERE organization_id = ? AND user_id = ? AND role = 'org_admin'
		)
	`, orgID, userID).Scan(ctx, &ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// CanAdministerOrgOrPlatform is CanAdministerOrg composed with the shared
// platform superadmin seam: org-admin authority over orgID, or an active
// superadmin_full grant. It exists for the organization-invitation decision
// points, which already admitted an active superadmin_full before this seam
// (issue #967); it adds no membership query of its own and is expressed in terms
// of CanAdministerOrg, so there is still exactly one org-role read.
//
// A superadmin_readonly grant does NOT qualify, and neither does a project role:
// the platform override is the same superadmin_full boundary used everywhere
// else (issue #812 Q9/#949).
func CanAdministerOrgOrPlatform(ctx context.Context, db bun.IDB, orgID, userID string) (bool, error) {
	ok, err := CanAdministerOrg(ctx, db, orgID, userID)
	if err != nil || ok {
		return ok, err
	}
	if db == nil || userID == "" {
		return false, nil
	}
	role, err := superadminRole(ctx, db, userID)
	if err != nil {
		return false, err
	}
	return role == RoleSuperadminFull, nil
}
