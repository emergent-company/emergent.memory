package auth

import (
	"context"
	"database/sql"
	"errors"
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

// viewerReadOnlyScopes mirrors projects.ViewerReadOnlyScopes — the only
// authoritative role → scope mapping that exists in the codebase.
var viewerReadOnlyScopes = []string{"data:read", "schema:read", "agents:read", "projects:read"}

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
// Only project_viewer is mapped: its scope set is defined authoritatively in
// code (projects.ViewerReadOnlyScopes). project_admin and project_user have no
// in-code scope set, and inventing one is a security policy decision — see
// openspec/changes/oidc-scope-mapping/design.md "Decisions Needed". Those roles
// fall through to the operator-configured default scope set.
func roleToScopes(role string) ([]string, bool) {
	switch role {
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
//  2. the mapped role for projectID (project_viewer);
//  3. the operator-configured default scope set;
//  4. empty.
//
// A project-role lookup error yields an empty set — the default scope set is a
// grant, not a fallback for failures.
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
		if scopes, ok := roleToScopes(role); ok {
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

// dbProjectRole reads the role from kb.project_memberships.
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
	return role, nil
}

// introspectionConfigured reports whether RFC 7662 introspection is enabled and
// has client credentials — the same precondition ZitadelService.Introspect uses.
func (m *Middleware) introspectionConfigured() bool {
	if m.cfg == nil || m.cfg.Zitadel.DisableIntrospection {
		return false
	}
	return m.cfg.Zitadel.ClientJWT != "" || m.cfg.Zitadel.ClientJWTPath != ""
}

// oidcAllGrantEnabled reports whether the legacy all-or-nothing grant may be
// applied. It requires the explicit flag AND introspection to be unconfigured,
// so enabling introspection disables the all-grant and an introspection outage
// cannot re-enable it.
func (m *Middleware) oidcAllGrantEnabled() bool {
	if m.cfg == nil {
		return false
	}
	return m.cfg.Zitadel.UserinfoGrantAllScopes && !m.introspectionConfigured()
}
