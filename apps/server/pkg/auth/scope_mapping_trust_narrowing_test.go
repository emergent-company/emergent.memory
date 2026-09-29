package auth

import (
	"context"
	"testing"
)

// §7.4 — the trust-default flip only NARROWS. For every authority tier, with the
// flag off the tier receives exactly its application-derived set and never a
// token-carried Memory scope. The trust-OFF enabled set is a subset of the
// trust-ON enabled set, and no tier gains a scope from the flip.
//
// The comparison uses two token shapes:
//   - a "Memory" token carrying the vocabulary scope schema:write, and
//   - an app-only token carrying no Memory scope, which recovers the tier's
//     application-derived set under trust-ON (tier 0 never fires).
//
// enabled(trust off) ⊆ enabled(trust on) is then checked against the union of
// trust-ON's app-tier and token-tier outcomes, which is the full set of scopes
// trust-ON can enable for that tier.
func TestTrustFlipOnlyNarrowsPerTier(t *testing.T) {
	memToken := []string{"schema:write", "openid"}
	appToken := []string{"openid", "profile"}

	noRole := func(ctx context.Context, p, u string) (string, error) { return "", nil }

	tiers := []struct {
		name  string
		setup func(t *testing.T, m *Middleware)
		want  []string
	}{
		{
			name: "superadmin_full",
			setup: func(t *testing.T, m *Middleware) {
				m.superadminLookup = func(ctx context.Context, u string) (string, error) { return RoleSuperadminFull, nil }
				m.roleLookup = noRole
			},
			want: GetAllScopes(),
		},
		{
			name: "superadmin_readonly",
			setup: func(t *testing.T, m *Middleware) {
				m.superadminLookup = func(ctx context.Context, u string) (string, error) { return RoleSuperadminReadonly, nil }
				m.roleLookup = noRole
			},
			want: []string{},
		},
		{
			name: "org_admin",
			setup: func(t *testing.T, m *Middleware) {
				m.projectOrgLookup = func(ctx context.Context, p string) (string, error) { return orgA, nil }
				m.orgAdminLookup = func(ctx context.Context, o, u string) (bool, error) { return o == orgA, nil }
				m.roleLookup = noRole
			},
			want: []string{"org:read", "org:invite:create", "org:project:create", "org:project:delete"},
		},
		{
			name: "project_viewer",
			setup: func(t *testing.T, m *Middleware) {
				m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectViewer, nil }
			},
			want: viewerReadOnlyScopes,
		},
		{
			name: "app_default_only",
			setup: func(t *testing.T, m *Middleware) {
				m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:read"}
				m.roleLookup = noRole
			},
			want: []string{"data:read"},
		},
		{
			name:  "no_entitlement_empty",
			setup: func(t *testing.T, m *Middleware) { m.roleLookup = noRole },
			want:  []string{},
		},
	}

	for _, tt := range tiers {
		t.Run(tt.name, func(t *testing.T) {
			newM := func(trust bool) *Middleware {
				m := tierMiddleware(t)
				m.cfg.Zitadel.TrustTokenScopes = trust
				tt.setup(t, m)
				return m
			}
			ctx := context.Background()

			off := newM(false).resolveOIDCScopes(ctx, "user-uuid", projID, memToken, nil)
			onApp := newM(true).resolveOIDCScopes(ctx, "user-uuid", projID, appToken, nil)
			onMem := newM(true).resolveOIDCScopes(ctx, "user-uuid", projID, memToken, nil)

			// Exact set with trust off: the app-derived set for this tier, with
			// the token's Memory scope absent.
			wantScopeSet(t, off, tt.want)
			// The flip leaves the application-derived tiers untouched: with a
			// token that carries no Memory scope, both flag states agree exactly.
			wantScopeSet(t, onApp, tt.want)

			// enabled(trust off) ⊆ enabled(trust on).
			onEnabled := append(append([]string{}, onApp...), onMem...)
			if !scopeSubset(off, onEnabled) {
				t.Fatalf("trust-off set %v is not a subset of trust-on set %v (flip must only narrow)", off, onEnabled)
			}
			// No tier gains a scope from the flip.
			for _, s := range off {
				if s == "schema:write" {
					t.Fatalf("tier %s received token-derived scope %q with trust off", tt.name, s)
				}
			}
		})
	}
}

// scopeSubset reports whether every member of sub is present in super.
func scopeSubset(sub, super []string) bool {
	set := make(map[string]bool, len(super))
	for _, s := range super {
		set[s] = true
	}
	for _, s := range sub {
		if !set[s] {
			return false
		}
	}
	return true
}
