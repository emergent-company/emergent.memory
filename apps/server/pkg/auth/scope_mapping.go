package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Canonical kb.project_memberships role values. These mirror
// domain/projects (entity.go); pkg/auth cannot import that package because the
// domain packages depend on pkg/auth.
const (
	RoleProjectAdmin  = "project_admin"
	RoleProjectUser   = "project_user"
	RoleProjectViewer = "project_viewer"

	// core.superadmins role values (migration 00070). Only superadmin_full is a
	// scope tier; superadmin_readonly receives nothing at the scope layer (its
	// read surfaces are role-gated) — issue #812 Q9.
	RoleSuperadminFull     = "superadmin_full"
	RoleSuperadminReadonly = "superadmin_readonly"
)

// orgAdminScopes is the organization-administration scope set granted to an
// org_admin membership (tier 2). It SHALL contain no project:* scope and no
// data/schema/agent scope, so it can never widen the project tier (§4.4).
var orgAdminScopes = []string{"org:read", "org:invite:create", "org:project:create", "org:project:delete"}

// viewerReadOnlyScopes mirrors projects.ViewerReadOnlyScopes — the read-only
// baseline shared by every canonical project role.
var viewerReadOnlyScopes = []string{"data:read", "schema:read", "agents:read", "projects:read"}

// Canonical role → scope sets (#736, operator decision A: conservative
// write-inclusive).
//
// The sets are nested by construction: project_admin ⊇ project_user ⊇
// project_viewer, so an admin can always do everything a user can, and a user
// everything a viewer can. They deliberately exclude admin*/mcp:admin/org:*/
// project:invite:create/account:* — a project role is not an account or
// organisation administrator.
//
// The write scopes are umbrella scopes (see ScopeImplies in middleware.go):
// schema:write also expands to schema:migrate, agents:write to chat:admin, and
// data:write to the related writes plus journal:write. That expansion is an
// accepted, deliberate property (#736 decision A) and is pinned by
// TestRoleScopeUmbrellaExpansionIsPinned.
var (
	// roleUserScopes = viewer + write to knowledge data.
	roleUserScopes = withScopes(viewerReadOnlyScopes, "data:write")
	// roleAdminScopes = user + write to agents and schemas.
	roleAdminScopes = withScopes(roleUserScopes, "agents:write", "schema:write")
)

// withScopes returns a fresh slice of base followed by extra, so the derived
// role sets never share a backing array.
func withScopes(base []string, extra ...string) []string {
	out := make([]string, 0, len(base)+len(extra))
	out = append(out, base...)
	return append(out, extra...)
}

// memoryScopeVocabulary is the set of scopes the Memory platform understands.
// Raw OIDC scopes (openid, profile, email, offline_access, ...) are not members
// of this set and are therefore never treated as an explicit Memory grant.
//
// It is the union of GetAllScopes() and the fine-grained/coarse scopes accepted
// by API token creation (domain/apitoken.ValidApiTokenScopes) that GetAllScopes
// does not list. domain/apitoken cannot be imported here (domain packages depend
// on pkg/auth), so the difference is mirrored explicitly — keep it in sync.
var memoryScopeVocabulary = func() map[string]bool {
	m := map[string]bool{}
	for _, s := range GetAllScopes() {
		m[s] = true
	}
	for _, s := range []string{
		// Fine-grained scopes satisfying MCP tool RequiredScope.
		"search", "journal:read", "journal:write",
		"branches:read", "branches:write",
		"skills:read", "skills:write",
		"schema:migrate", "chat:use", "chat:admin", "ingest:write",
		// Coarse scopes accepted on API tokens.
		"schema:write", "projects:read", "projects:write",
		// Reserved internal marker scopes (never present on OIDC tokens).
		"mcp:agent-call", "share:agent-chat", "device:api",
	} {
		m[s] = true
	}
	return m
}()

// projectRoleLookup resolves a user's role in a project. It returns "" when the
// user is not a member. Overridable in tests.
type projectRoleLookup func(ctx context.Context, projectID, userID string) (string, error)

// roleToScopes maps a canonical project membership role to the Memory scopes it
// grants for the project a request is scoped to.
//
// All three canonical roles are mapped explicitly (#736 decision A): viewer is
// read-only, user adds data:write, admin adds agents:write + schema:write. Any
// other role string — a legacy `owner` row, a typo — is unmapped and callers
// must fail closed (see resolveOIDCScopes).
func roleToScopes(role string) ([]string, bool) {
	switch role {
	case RoleProjectAdmin:
		return append([]string(nil), roleAdminScopes...), true
	case RoleProjectUser:
		return append([]string(nil), roleUserScopes...), true
	case RoleProjectViewer:
		return append([]string(nil), viewerReadOnlyScopes...), true
	default:
		return nil, false
	}
}

// filterMemoryScopes returns the members of scopes that are part of the Memory
// scope vocabulary, preserving order and de-duplicating. A non-empty result
// means the token carries an explicit Memory grant.
func filterMemoryScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] || !memoryScopeVocabulary[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ZitadelProjectRole is a project role carried on an introspected token. Only
// its Name and owning OrgID are relevant to the standing superadmin role
// mapping (issue #812 Q6): the grant requires an exact (issuer, org_id, role)
// triple, so any partial match is a mismatch.
//
// Roles are delivered only by RFC 7662 introspection; the userinfo fallback and
// the local JWT path have no role claim and therefore never populate this.
//
// Zitadel wire shape (authoritative): the claim value is
//
//	{role: {orgID: orgDomain}}
//
// — the role name is the outer key, and the ORGANIZATION IDs are the KEYS of
// the inner map (org domains are the values). One role may span multiple orgs.
type ZitadelProjectRole struct {
	Name  string
	OrgID string
}

// zitadelRoleClaimPrefix / zitadelRoleClaimSuffix delimit the Zitadel project
// role claim: urn:zitadel:iam:org:project:{projectID}:roles, whose value is an
// object mapping role name → {orgID: orgDomain} (org IDs are the inner keys).
const (
	zitadelRoleClaimPrefix = "urn:zitadel:iam:org:project:"
	zitadelRoleClaimSuffix = ":roles"
)

// extractZitadelProjectRoles extracts the project roles from the raw
// introspection claims. The claim value is {role: {orgID: orgDomain}}: it
// iterates the inner map's KEYS (the org IDs) and emits one role per
// (roleName, orgID) pair, so a role spanning multiple orgs yields multiple
// entries. An unexpected shape (a non-map inner value, a non-map claim value, or
// an absent claim) yields nothing, so a claim it does not understand can never
// produce a superadmin grant (fail closed).
func extractZitadelProjectRoles(claims map[string]any) []ZitadelProjectRole {
	if len(claims) == 0 {
		return nil
	}
	var roles []ZitadelProjectRole
	for key, val := range claims {
		if !strings.HasPrefix(key, zitadelRoleClaimPrefix) || !strings.HasSuffix(key, zitadelRoleClaimSuffix) {
			continue
		}
		roleMap, ok := val.(map[string]any)
		if !ok {
			continue
		}
		for name, roleVal := range roleMap {
			meta, ok := roleVal.(map[string]any)
			if !ok {
				continue
			}
			for orgID := range meta {
				roles = append(roles, ZitadelProjectRole{Name: name, OrgID: orgID})
			}
		}
	}
	return roles
}

// resolveOIDCScopes derives the effective Memory scopes for an authenticated
// OIDC session. It fails closed and never returns GetAllScopes() except for a
// terminal superadmin_full grant.
//
// Resolution order (app-owned, issue #812 D4):
//  0. explicit Memory scopes carried by the token, verbatim — only while
//     MEMORY_OIDC_TRUST_TOKEN_SCOPES is enabled (terminal when it applies);
//  1. an active superadmin_full grant (app-side core.superadmins row, or a
//     standing Zitadel project role) — terminal: the full catalogue;
//  2. an org_admin membership for the request organization — the
//     organization-administration set;
//  3. the mapped canonical project membership role for projectID;
//  4. the operator-configured default scope set — only when the user has no
//     project membership and no entitlement;
//  5. empty (fail closed).
//
// Tiers 2 and 3 govern disjoint resource families and are combined. A project
// role lookup error or an unrecognised role yields an empty set (never the
// default), and so does a superadmin lookup error (#736 decision B, strict).
// `roles` are the token's Zitadel project roles, already issuer-filtered by the
// caller (see trustedSuperadminRoles).
func (m *Middleware) resolveOIDCScopes(ctx context.Context, userID, projectID string, rawScopes []string, roles []ZitadelProjectRole) []string {
	// Tier 0 — token-carried Memory scopes (terminal while trusted).
	if m.trustTokenScopes() {
		if explicit := filterMemoryScopes(rawScopes); len(explicit) > 0 {
			return explicit
		}
	}

	// Tier 1 — superadmin (terminal). superadmin_readonly is NOT a scope tier.
	if userID != "" {
		role, err := m.resolveSuperadminRole(ctx, userID, roles)
		if err != nil {
			m.log.Warn("failed to resolve superadmin grant; failing closed", logger.Error(err))
			return nil
		}
		if role == RoleSuperadminFull {
			return GetAllScopes()
		}
	}

	// Tiers 2 + 3 — combined (disjoint resource families).
	if projectID != "" && userID != "" {
		var combined []string

		// Tier 3 — project membership role (fail-closed on error/unrecognised).
		projectRole, err := m.lookupProjectRole(ctx, projectID, userID)
		if err != nil {
			m.log.Warn("failed to resolve project role for scope mapping; failing closed",
				slog.String("project_id", projectID),
				logger.Error(err),
			)
			return nil
		}
		if projectRole != "" {
			scopes, ok := roleToScopes(projectRole)
			if !ok {
				m.log.Warn("unrecognised project role for scope mapping; failing closed",
					slog.String("project_id", projectID),
					slog.String("role", projectRole),
				)
				return nil
			}
			combined = append(combined, scopes...)
		}

		// Tier 2 — org_admin for the request org (owning org of the declared
		// project). No trusted org context → tier 2 not granted.
		if orgScopes := m.resolveOrgAdminScopes(ctx, projectID, userID); len(orgScopes) > 0 {
			combined = append(combined, orgScopes...)
		}

		if len(combined) > 0 {
			return dedupScopes(combined)
		}
	}

	// Tier 4 — app-owned default; tier 5 — empty.
	return m.defaultOIDCScopes()
}

// dedupScopes returns a de-duplicated copy of scopes, preserving order.
func dedupScopes(scopes []string) []string {
	seen := make(map[string]bool, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// resolveSuperadminRole returns the effective superadmin role for the user:
// superadmin_full when an active app-side superadmin_full row exists OR when the
// standing-role flag is on and the token carries the exact (org, role) match;
// otherwise the app-side role (superadmin_readonly, or ""). Only
// superadmin_full is a scope tier.
func (m *Middleware) resolveSuperadminRole(ctx context.Context, userID string, roles []ZitadelProjectRole) (string, error) {
	role, err := m.lookupSuperadminRole(ctx, userID)
	if err != nil {
		return "", err
	}
	if role == RoleSuperadminFull {
		return role, nil
	}
	if m.roleMatchesSuperadmin(roles) {
		return RoleSuperadminFull, nil
	}
	return role, nil
}

// roleMatchesSuperadmin reports whether the token carries the exact
// (SuperadminOrgID, SuperadminRole) pair while the standing-role flag is on.
// It maps to superadmin_full only, never superadmin_readonly or fine-grained
// scopes. Fail-closed: any missing config or mismatch yields false.
func (m *Middleware) roleMatchesSuperadmin(roles []ZitadelProjectRole) bool {
	if m.cfg == nil || !m.cfg.Zitadel.TrustRoleSuperadmin {
		return false
	}
	wantRole, wantOrg := m.cfg.Zitadel.SuperadminRole, m.cfg.Zitadel.SuperadminOrgID
	if wantRole == "" || wantOrg == "" {
		return false
	}
	for _, r := range roles {
		if r.Name == wantRole && r.OrgID == wantOrg {
			return true
		}
	}
	return false
}

// trustedSuperadminRoles returns the token roles eligible for a role-derived
// superadmin grant: only when the standing-role flag is enabled AND the token
// issuer matches the configured issuer exactly. Anything else yields nil so a
// mismatched or foreign issuer can never mint superadmin (issue #812 Q6).
//
// A role-derived superadmin is INTROSPECTION-ONLY by construction: roles are
// extracted from the RFC 7662 introspection response; the userinfo fallback
// (authSourceUserinfo) and the local JWT path carry no role claims, so their
// roles slice is empty and this returns nil. Never cache the derived grant — the
// roles are raw identity claims; the superadmin grant is re-resolved per request.
func (m *Middleware) trustedSuperadminRoles(issuer string, roles []ZitadelProjectRole) []ZitadelProjectRole {
	if m.cfg == nil || !m.cfg.Zitadel.TrustRoleSuperadmin {
		return nil
	}
	if issuer == "" || issuer != m.cfg.Zitadel.GetIssuer() {
		return nil
	}
	return roles
}

// resolveOrgAdminScopes returns the org-administration set when the user is an
// org_admin of the request organization (the owning org of the declared
// project), or nil otherwise. A missing org context or a lookup error yields nil
// (tier 2 not granted — fail closed for the org tier only).
func (m *Middleware) resolveOrgAdminScopes(ctx context.Context, projectID, userID string) []string {
	orgID, err := m.lookupProjectOrg(ctx, projectID)
	if err != nil {
		m.log.Warn("failed to resolve project organization for org-admin tier; tier 2 not granted",
			slog.String("project_id", projectID), logger.Error(err))
		return nil
	}
	if orgID == "" {
		return nil
	}
	isOrgAdmin, err := m.lookupOrgAdmin(ctx, orgID, userID)
	if err != nil {
		m.log.Warn("failed to resolve org_admin membership; tier 2 not granted",
			slog.String("org_id", orgID), logger.Error(err))
		return nil
	}
	if !isOrgAdmin {
		return nil
	}
	return append([]string(nil), orgAdminScopes...)
}

// trustTokenScopes reports whether Memory scope names carried on a validated
// OIDC token are honoured as a grant. Introduced enabled (default) so Release N
// changes no behaviour; flips to disabled in the following release.
func (m *Middleware) trustTokenScopes() bool {
	if m.cfg == nil {
		return false
	}
	return m.cfg.Zitadel.TrustTokenScopes
}

// defaultOIDCScopes returns a copy of the configured default scope set, or nil
// when unset.
func (m *Middleware) defaultOIDCScopes() []string {
	if m.cfg == nil || len(m.cfg.Zitadel.OIDCDefaultScopes) == 0 {
		return nil
	}
	return append([]string(nil), m.cfg.Zitadel.OIDCDefaultScopes...)
}

// lookupProjectRole returns the user's role in a project ("" when not a member),
// using the test seam when one is installed.
func (m *Middleware) lookupProjectRole(ctx context.Context, projectID, userID string) (string, error) {
	if m.roleLookup != nil {
		return m.roleLookup(ctx, projectID, userID)
	}
	return m.dbProjectRole(ctx, projectID, userID)
}

// dbProjectRole reads the role from kb.project_memberships. A missing row
// returns ("", nil) — "no membership". A row whose stored role is empty is
// dirty data, not an absence, so it returns an error; resolveOIDCScopes then
// fails closed instead of granting the configured default to an unrecognised
// membership (#736 decision B).
func (m *Middleware) dbProjectRole(ctx context.Context, projectID, userID string) (string, error) {
	if m.db == nil {
		return "", errors.New("auth: no database available for project role lookup")
	}

	var role string
	err := m.db.NewSelect().
		TableExpr("kb.project_memberships").
		Column("role").
		Where("project_id = ?", projectID).
		Where("user_id = ?", userID).
		Limit(1).
		Scan(ctx, &role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if strings.TrimSpace(role) == "" {
		return "", fmt.Errorf("auth: project membership for project %s has an empty role", projectID)
	}
	return role, nil
}

// superadminRoleLookup resolves the app-side superadmin role for a user ("" when
// none). Overridable in tests.
type superadminRoleLookup func(ctx context.Context, userID string) (string, error)

// projectOrgLookup resolves the owning organization of a project ("" when the
// project has none or does not exist). Overridable in tests.
type projectOrgLookup func(ctx context.Context, projectID string) (string, error)

// orgAdminLookup reports whether the user is an org_admin in the given
// organization. Overridable in tests.
type orgAdminLookup func(ctx context.Context, orgID, userID string) (bool, error)

// orgMemberLookup reports whether the user holds any membership role in the
// given organization. Overridable in tests.
type orgMemberLookup func(ctx context.Context, orgID, userID string) (bool, error)

// lookupSuperadminRole resolves the app-side superadmin role using the seam.
func (m *Middleware) lookupSuperadminRole(ctx context.Context, userID string) (string, error) {
	if m.superadminLookup != nil {
		return m.superadminLookup(ctx, userID)
	}
	return m.dbSuperadminRole(ctx, userID)
}

// lookupProjectOrg resolves the owning org of a project using the seam.
func (m *Middleware) lookupProjectOrg(ctx context.Context, projectID string) (string, error) {
	if m.projectOrgLookup != nil {
		return m.projectOrgLookup(ctx, projectID)
	}
	return m.dbProjectOrg(ctx, projectID)
}

// lookupOrgAdmin resolves org_admin membership using the seam.
func (m *Middleware) lookupOrgAdmin(ctx context.Context, orgID, userID string) (bool, error) {
	if m.orgAdminLookup != nil {
		return m.orgAdminLookup(ctx, orgID, userID)
	}
	return m.dbOrgAdmin(ctx, orgID, userID)
}

// lookupOrgMember resolves org membership (any role) using the seam.
func (m *Middleware) lookupOrgMember(ctx context.Context, orgID, userID string) (bool, error) {
	if m.orgMemberLookup != nil {
		return m.orgMemberLookup(ctx, orgID, userID)
	}
	return m.dbOrgMember(ctx, orgID, userID)
}

// dbSuperadminRole reads the active superadmin role from core.superadmins. A
// missing or revoked row returns ("", nil) — "no superadmin grant".
func (m *Middleware) dbSuperadminRole(ctx context.Context, userID string) (string, error) {
	if m.db == nil {
		return "", errors.New("auth: no database available for superadmin role lookup")
	}
	var role string
	err := m.db.NewSelect().
		TableExpr("core.superadmins").
		Column("role").
		Where("user_id = ?", userID).
		Where("revoked_at IS NULL").
		Limit(1).
		Scan(ctx, &role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return role, nil
}

// dbProjectOrg reads the owning organization of a project. A missing project
// returns ("", nil) — no trusted org context, so tier 2 is not granted.
func (m *Middleware) dbProjectOrg(ctx context.Context, projectID string) (string, error) {
	if m.db == nil {
		return "", errors.New("auth: no database available for project org lookup")
	}
	var orgID string
	err := m.db.NewSelect().
		TableExpr("kb.projects").
		Column("organization_id").
		Where("id = ?", projectID).
		Limit(1).
		Scan(ctx, &orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return orgID, nil
}

// dbOrgAdmin reports whether the user holds an org_admin membership in the given
// organization.
func (m *Middleware) dbOrgAdmin(ctx context.Context, orgID, userID string) (bool, error) {
	if m.db == nil {
		return false, errors.New("auth: no database available for org_admin lookup")
	}
	var ok bool
	err := m.db.NewRaw(`
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

// dbOrgMember reports whether the user holds any membership role in the given
// organization. It is the middleware-level counterpart to
// orgs.Repository.IsUserMember (pkg/auth cannot import domain/orgs without an
// import cycle), and the source org is always resolved server-side via
// lookupProjectOrg — never from a request-controlled value.
func (m *Middleware) dbOrgMember(ctx context.Context, orgID, userID string) (bool, error) {
	if m.db == nil {
		return false, errors.New("auth: no database available for org membership lookup")
	}
	var ok bool
	err := m.db.NewRaw(`
		SELECT EXISTS(
			SELECT 1 FROM kb.organization_memberships
			WHERE organization_id = ? AND user_id = ?
		)
	`, orgID, userID).Scan(ctx, &ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// oidcAllGrantEnabled reports whether the legacy all-or-nothing grant may be
// applied. It requires the explicit flag AND introspection to be unconfigured,
// so enabling introspection disables the all-grant and an introspection outage
// cannot re-enable it.
func (m *Middleware) oidcAllGrantEnabled() bool {
	if m.cfg == nil {
		return false
	}
	return m.cfg.Zitadel.UserinfoAllGrantActive()
}
