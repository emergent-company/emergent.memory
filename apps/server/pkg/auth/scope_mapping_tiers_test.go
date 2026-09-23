package auth

import (
	"context"
	"encoding/json"
	"testing"
)

// §4 — entitlement tiers. Each tier's exact set is asserted with set equality
// (wantScopeSet), never a length check. superadmin_readonly's denial of the full
// catalogue is explicit, and org_admin's non-widening of the project tier is
// proven (§4.4).

const (
	projID = "11111111-1111-1111-1111-111111111111"
	orgA   = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	orgB   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

func tierMiddleware(t *testing.T) *Middleware {
	t.Helper()
	m := newTestMiddleware(t)
	// No token-scope trust, so the token branch is off and app tiers are reached.
	m.cfg.Zitadel.TrustTokenScopes = false
	return m
}

func TestTierSuperadminFullIsTerminalFullCatalogue(t *testing.T) {
	m := tierMiddleware(t)
	m.superadminLookup = func(ctx context.Context, u string) (string, error) { return RoleSuperadminFull, nil }
	// Confounders that must NOT leak in: a mapped role and a configured default.
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectAdmin, nil }
	m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:read"}

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, nil)
	wantScopeSet(t, got, GetAllScopes())
}

func TestTierSuperadminReadonlyDoesNotReceiveFullCatalogue(t *testing.T) {
	m := tierMiddleware(t)
	m.superadminLookup = func(ctx context.Context, u string) (string, error) { return RoleSuperadminReadonly, nil }
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, nil)
	if scopesEqual(got, GetAllScopes()) {
		t.Fatalf("superadmin_readonly received the full catalogue: %v", got)
	}
	// With no other entitlement and no default, a readonly superadmin receives no
	// write or admin scope — nothing at the scope layer (issue #812 Q9).
	if len(got) != 0 {
		t.Fatalf("superadmin_readonly scopes = %v, want none", got)
	}
}

func TestTierOrgAdminExactSet(t *testing.T) {
	m := tierMiddleware(t)
	m.projectOrgLookup = func(ctx context.Context, p string) (string, error) { return orgA, nil }
	m.orgAdminLookup = func(ctx context.Context, o, u string) (bool, error) { return o == orgA, nil }
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, nil)
	wantScopeSet(t, got, []string{"org:read", "org:invite:create", "org:project:create", "org:project:delete"})
}

func TestTierOrgAndProjectCombine(t *testing.T) {
	m := tierMiddleware(t)
	m.projectOrgLookup = func(ctx context.Context, p string) (string, error) { return orgA, nil }
	m.orgAdminLookup = func(ctx context.Context, o, u string) (bool, error) { return o == orgA, nil }
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectViewer, nil }

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, nil)
	wantScopeSet(t, got, []string{
		"org:read", "org:invite:create", "org:project:create", "org:project:delete",
		"data:read", "schema:read", "agents:read", "projects:read",
	})
}

// §4.4 — org_admin must add no project:* / data / schema / agent scope, so the
// org∪project union adds nothing a pure project member would lack.
func TestOrgAdminScopesNeverWidenProjectTier(t *testing.T) {
	for _, s := range orgAdminScopes {
		for _, prefix := range []string{"project:", "data:", "schema:", "agent", "agents:", "documents:", "chunks:", "chat:", "graph:", "search:", "mcp:"} {
			if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
				t.Fatalf("org_admin scope %q collides with project/data/schema/agent family %q", s, prefix)
			}
		}
	}

	// And the union of org_admin + project_viewer carries no project write scope
	// and no data/schema/agent write scope.
	m := tierMiddleware(t)
	m.projectOrgLookup = func(ctx context.Context, p string) (string, error) { return orgA, nil }
	m.orgAdminLookup = func(ctx context.Context, o, u string) (bool, error) { return o == orgA, nil }
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectViewer, nil }

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, nil)
	for _, s := range got {
		for _, banned := range []string{"data:write", "schema:write", "agents:write", "projects:write", "project:write", "documents:write"} {
			if s == banned {
				t.Fatalf("org_admin widened the project tier to %q (full set %v)", banned, got)
			}
		}
	}
}

func TestTierOrgAdminScopedToRequestOrg(t *testing.T) {
	m := tierMiddleware(t)
	// The user is org_admin in orgA only. The declared project is owned by orgB.
	m.projectOrgLookup = func(ctx context.Context, p string) (string, error) { return orgB, nil }
	m.orgAdminLookup = func(ctx context.Context, o, u string) (bool, error) { return o == orgA, nil }
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, nil)
	if len(got) != 0 {
		t.Fatalf("org_admin in A leaked into project owned by B: %v", got)
	}
}

func TestTierOrgAdminNoOrgContextDenied(t *testing.T) {
	m := tierMiddleware(t)
	// No project declared → no trusted org context → tier 2 not granted, even if
	// the user is org_admin somewhere.
	m.orgAdminLookup = func(ctx context.Context, o, u string) (bool, error) { return true, nil }
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", "", []string{"openid"}, nil)
	if len(got) != 0 {
		t.Fatalf("no-org-context scopes = %v, want none (tier 2 not granted)", got)
	}
}

func TestTierNoEntitlementFallsToDefaultThenEmpty(t *testing.T) {
	m := tierMiddleware(t)
	m.superadminLookup = func(ctx context.Context, u string) (string, error) { return "", nil }
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	if got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, nil); len(got) != 0 {
		t.Fatalf("no-entitlement-no-default scopes = %v, want empty", got)
	}

	m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:read"}
	if got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, nil); !scopesEqual(got, []string{"data:read"}) {
		t.Fatalf("no-entitlement scopes = %v, want configured default", got)
	}
}

// --- standing Zitadel project role → superadmin_full (issue #812 Q6) ---------

func TestRoleDerivedSuperadmin(t *testing.T) {
	roles := []ZitadelProjectRole{{Name: "platform-admin", OrgID: orgA}}

	t.Run("flag off denies role-derived superadmin", func(t *testing.T) {
		m := tierMiddleware(t)
		m.cfg.Zitadel.TrustRoleSuperadmin = false
		m.cfg.Zitadel.SuperadminRole = "platform-admin"
		m.cfg.Zitadel.SuperadminOrgID = orgA
		m.superadminLookup = func(ctx context.Context, u string) (string, error) { return "", nil }
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

		got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, roles)
		if len(got) != 0 {
			t.Fatalf("role-derived superadmin with flag off = %v, want none", got)
		}
	})

	t.Run("exact triple match grants superadmin_full", func(t *testing.T) {
		m := tierMiddleware(t)
		m.cfg.Zitadel.TrustRoleSuperadmin = true
		m.cfg.Zitadel.SuperadminRole = "platform-admin"
		m.cfg.Zitadel.SuperadminOrgID = orgA
		m.superadminLookup = func(ctx context.Context, u string) (string, error) { return "", nil }

		got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, roles)
		wantScopeSet(t, got, GetAllScopes())
	})

	t.Run("org mismatch denies", func(t *testing.T) {
		m := tierMiddleware(t)
		m.cfg.Zitadel.TrustRoleSuperadmin = true
		m.cfg.Zitadel.SuperadminRole = "platform-admin"
		m.cfg.Zitadel.SuperadminOrgID = orgB // expected org differs from the token's org
		m.superadminLookup = func(ctx context.Context, u string) (string, error) { return "", nil }
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

		got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, roles)
		if len(got) != 0 {
			t.Fatalf("org-mismatched role = %v, want none (fail closed)", got)
		}
	})

	t.Run("role name mismatch denies", func(t *testing.T) {
		m := tierMiddleware(t)
		m.cfg.Zitadel.TrustRoleSuperadmin = true
		m.cfg.Zitadel.SuperadminRole = "different-role"
		m.cfg.Zitadel.SuperadminOrgID = orgA
		m.superadminLookup = func(ctx context.Context, u string) (string, error) { return "", nil }
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

		got := m.resolveOIDCScopes(context.Background(), "user-uuid", projID, []string{"openid"}, roles)
		if len(got) != 0 {
			t.Fatalf("role-name-mismatched role = %v, want none (fail closed)", got)
		}
	})
}

// The role mapping must resolve to superadmin_full ONLY — never a read-only
// grant or fine-grained scopes — and it is gated on the exact issuer.
func TestTrustedSuperadminRolesIssuerGating(t *testing.T) {
	roles := []ZitadelProjectRole{{Name: "platform-admin", OrgID: orgA}}

	m := tierMiddleware(t)
	m.cfg.Zitadel.TrustRoleSuperadmin = true
	m.cfg.Zitadel.SuperadminRole = "platform-admin"
	m.cfg.Zitadel.SuperadminOrgID = orgA
	m.cfg.Zitadel.Issuer = "https://zitadel.example.com"

	if got := m.trustedSuperadminRoles("https://zitadel.example.com", roles); len(got) != 1 {
		t.Fatalf("matching issuer should trust roles, got %v", got)
	}
	if got := m.trustedSuperadminRoles("https://evil.example.com", roles); got != nil {
		t.Fatalf("foreign issuer must not trust roles, got %v", got)
	}
	if got := m.trustedSuperadminRoles("", roles); got != nil {
		t.Fatalf("empty issuer must not trust roles, got %v", got)
	}

	m.cfg.Zitadel.TrustRoleSuperadmin = false
	if got := m.trustedSuperadminRoles("https://zitadel.example.com", roles); got != nil {
		t.Fatalf("flag off must not trust roles, got %v", got)
	}
}

func TestExtractZitadelProjectRoles(t *testing.T) {
	claims := map[string]any{
		"urn:zitadel:iam:org:project:123:roles": map[string]any{
			"platform-admin": map[string]any{"orgID": orgA, "projectID": "123"},
			"member":         map[string]any{"orgId": orgB, "projectId": "123"},
		},
		"openid": "ok",
	}
	roles := extractZitadelProjectRoles(claims)
	want := map[string]string{
		"platform-admin": orgA,
		"member":         orgB,
	}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	for _, r := range roles {
		if want[r.Name] != r.OrgID {
			t.Fatalf("role %q org = %q, want %q", r.Name, r.OrgID, want[r.Name])
		}
	}

	// Unexpected shapes fail closed.
	if got := extractZitadelProjectRoles(map[string]any{"urn:zitadel:iam:org:project:1:roles": "not-an-object"}); len(got) != 0 {
		t.Fatalf("malformed claim produced roles %v, want none", got)
	}
	if got := extractZitadelProjectRoles(nil); len(got) != 0 {
		t.Fatalf("nil claims produced roles %v, want none", got)
	}
}

// The standing role mapping is DEAD unless the introspection body's
// urn:zitadel:iam:org:project:*:roles claim actually reaches the extractor. This
// test round-trips a REAL introspection JSON body through json.Unmarshal into
// introspectionResponse (the same path rs.Introspect uses) and asserts both the
// extraction and the full resolution path. It is the regression guard for the
// nil-Claims bug: encoding/json does not route unknown keys into a bare
// map field.
func TestRoleDerivedSuperadminFromIntrospectionBody(t *testing.T) {
	const issuer = "https://zitadel.example.com"
	const wantOrg = "org-aaa"
	const body = `{
		"active": true,
		"sub": "user-123",
		"iss": "https://zitadel.example.com",
		"exp": 4102444800,
		"scope": "openid profile",
		"email": "admin@example.com",
		"urn:zitadel:iam:org:project:123:roles": {
			"platform-admin": {"orgID": "org-aaa", "projectID": "123"}
		}
	}`

	var resp introspectionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal introspection body: %v", err)
	}

	// The standard fields must still decode alongside the role claim.
	if resp.Issuer != issuer {
		t.Fatalf("iss = %q, want %q (standard field decoding must be preserved)", resp.Issuer, issuer)
	}
	if resp.Subject != "user-123" {
		t.Fatalf("sub = %q, want %q", resp.Subject, "user-123")
	}

	// The role claim must actually reach the extractor.
	roles := extractZitadelProjectRoles(resp.Claims)
	if len(roles) != 1 || roles[0].Name != "platform-admin" || roles[0].OrgID != wantOrg {
		t.Fatalf("extracted roles = %v, want exactly [platform-admin @ %s]", roles, wantOrg)
	}

	newResolve := func() *Middleware {
		m := tierMiddleware(t)
		m.cfg.Zitadel.SuperadminRole = "platform-admin"
		m.cfg.Zitadel.SuperadminOrgID = wantOrg
		m.cfg.Zitadel.Issuer = issuer
		m.superadminLookup = func(ctx context.Context, u string) (string, error) { return "", nil } // no app-side row
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }
		return m
	}

	t.Run("exact triple grants superadmin_full", func(t *testing.T) {
		m := newResolve()
		m.cfg.Zitadel.TrustRoleSuperadmin = true
		trusted := m.trustedSuperadminRoles(issuer, roles)
		got := m.resolveOIDCScopes(context.Background(), "user-123", projID, []string{"openid"}, trusted)
		wantScopeSet(t, got, GetAllScopes())
	})

	t.Run("flag off denies", func(t *testing.T) {
		m := newResolve()
		m.cfg.Zitadel.TrustRoleSuperadmin = false
		trusted := m.trustedSuperadminRoles(issuer, roles)
		if got := m.resolveOIDCScopes(context.Background(), "user-123", projID, []string{"openid"}, trusted); len(got) != 0 {
			t.Fatalf("flag-off scopes = %v, want none", got)
		}
	})

	t.Run("issuer mismatch denies", func(t *testing.T) {
		m := newResolve()
		m.cfg.Zitadel.TrustRoleSuperadmin = true
		trusted := m.trustedSuperadminRoles("https://evil.example.com", roles)
		if trusted != nil {
			t.Fatalf("foreign issuer must not trust roles, got %v", trusted)
		}
		if got := m.resolveOIDCScopes(context.Background(), "user-123", projID, []string{"openid"}, trusted); len(got) != 0 {
			t.Fatalf("issuer-mismatch scopes = %v, want none", got)
		}
	})

	t.Run("org mismatch denies", func(t *testing.T) {
		m := newResolve()
		m.cfg.Zitadel.TrustRoleSuperadmin = true
		m.cfg.Zitadel.SuperadminOrgID = "org-other" // differs from the token's org-aaa
		trusted := m.trustedSuperadminRoles(issuer, roles)
		if got := m.resolveOIDCScopes(context.Background(), "user-123", projID, []string{"openid"}, trusted); len(got) != 0 {
			t.Fatalf("org-mismatch scopes = %v, want none", got)
		}
	})
}

// A body with no roles claim is nil-safe: the extractor returns nothing and the
// resolution path never grants superadmin.
func TestRoleDerivedSuperadminNoRolesClaim(t *testing.T) {
	const body = `{
		"active": true,
		"sub": "user-123",
		"iss": "https://zitadel.example.com",
		"exp": 4102444800,
		"scope": "openid profile"
	}`
	var resp introspectionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal introspection body: %v", err)
	}
	if got := extractZitadelProjectRoles(resp.Claims); len(got) != 0 {
		t.Fatalf("no-roles body extracted %v, want none", got)
	}

	m := tierMiddleware(t)
	m.cfg.Zitadel.TrustRoleSuperadmin = true
	m.cfg.Zitadel.SuperadminRole = "platform-admin"
	m.cfg.Zitadel.SuperadminOrgID = "org-aaa"
	m.cfg.Zitadel.Issuer = "https://zitadel.example.com"
	m.superadminLookup = func(ctx context.Context, u string) (string, error) { return "", nil }
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	trusted := m.trustedSuperadminRoles("https://zitadel.example.com", nil)
	if got := m.resolveOIDCScopes(context.Background(), "user-123", projID, []string{"openid"}, trusted); len(got) != 0 {
		t.Fatalf("no-roles resolution scopes = %v, want none", got)
	}
}
