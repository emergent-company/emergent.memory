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
)

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
// The write scopes are umbrella scopes (see scopeImplies in middleware.go):
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
		"mcp:agent-call", "share:agent-chat",
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

// resolveOIDCScopes derives the effective Memory scopes for an authenticated
// OIDC session. It fails closed and never returns GetAllScopes().
//
// Resolution order (first match wins, no union):
//  1. explicit Memory scopes carried by the token, verbatim;
//  2. the mapped canonical role for projectID;
//  3. the operator-configured default scope set — only when the user has no
//     project membership at all;
//  4. empty.
//
// A project-role lookup error yields an empty set, and so does a membership
// whose role is not one of the canonical roles (#736 decision B, strict):
// the default scope set is a grant for non-members, never a fallback for
// failures or unrecognised roles.
func (m *Middleware) resolveOIDCScopes(ctx context.Context, userID, projectID string, rawScopes []string) []string {
	if explicit := filterMemoryScopes(rawScopes); len(explicit) > 0 {
		return explicit
	}

	if projectID != "" && userID != "" {
		role, err := m.lookupProjectRole(ctx, projectID, userID)
		if err != nil {
			m.log.Warn("failed to resolve project role for scope mapping; failing closed",
				slog.String("project_id", projectID),
				logger.Error(err),
			)
			return nil
		}
		if role != "" {
			scopes, ok := roleToScopes(role)
			if !ok {
				m.log.Warn("unrecognised project role for scope mapping; failing closed",
					slog.String("project_id", projectID),
					slog.String("role", role),
				)
				return nil
			}
			return scopes
		}
	}

	return m.defaultOIDCScopes()
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

// introspectionConfigured reports whether RFC 7662 introspection is enabled and
// has client credentials — the same precondition ZitadelService.Introspect uses.
func (m *Middleware) introspectionConfigured() bool {
	if m.cfg == nil {
		return false
	}
	return m.cfg.Zitadel.IntrospectionConfigured()
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
