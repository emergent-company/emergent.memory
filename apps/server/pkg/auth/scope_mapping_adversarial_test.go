package auth

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// This file holds second-pass adversarial tests for PR #730 (fail-closed OIDC
// scope mapping). They exist to try to FALSIFY the security claims made by that
// PR; each test documents whether the claim holds or a defect is real.

// clearZitadelEnv makes the test independent of ambient ZITADEL_* env vars.
func clearZitadelEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"ZITADEL_CLIENT_JWT",
		"ZITADEL_CLIENT_JWT_PATH",
		"DISABLE_ZITADEL_INTROSPECTION",
		"MEMORY_OIDC_DEFAULT_SCOPES",
	} {
		prev, had := os.LookupEnv(k)
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unset %s: %v", k, err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(k, prev)
			}
		})
	}
}

// Claim 3: an introspection outage (credentials configured, endpoint down)
// must fail closed on the userinfo fallback, not grant a broad set.
func TestAdversarialIntrospectionOutageFailsClosed(t *testing.T) {
	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	// Introspection IS configured ...
	m.cfg.Zitadel.ClientJWT = "fake-client-jwt"
	// ... but it fails at runtime, forcing the userinfo fallback.
	m.zitadelSvc = &fakeIntrospector{
		introErr: errors.New("zitadel introspection unreachable"),
		userInfo: &UserInfoResult{Sub: "outage-user", Email: "u@example.com"},
	}
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	user, err := m.validateToken(context.Background(), "oidc-token", "")
	if err != nil {
		t.Fatalf("validateToken: %v", err)
	}
	if scopesEqual(user.Scopes, GetAllScopes()) {
		t.Fatalf("introspection outage granted the full catalogue: %v", user.Scopes)
	}
	if len(user.Scopes) != 0 {
		t.Fatalf("scopes = %v, want none (fail closed during outage)", user.Scopes)
	}
}

// A token carrying Memory scope names never mints them: the userinfo fallback
// resolves through application entitlements → default → empty, so a non-member
// with an empty default receives nothing regardless of what the token claims.
func TestAdversarialTokenScopesAreNeverAGrant(t *testing.T) {
	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil } // non-member

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", "foreign-project", []string{"data:write", "schema:write", "openid"}, nil)
	if len(got) != 0 {
		t.Fatalf("scopes = %v, want none (token-carried Memory scopes are not a grant)", got)
	}
}

// The configured default set is applied to a caller who is NOT a member of the
// declared project (role lookup returns ""), not only to mapped-role misses.
// With a non-empty default this makes the "account default" indistinguishable
// from a per-project grant for arbitrary declared projects. It is only a
// widening when the operator sets a default; out of the box the default is
// empty and this returns nothing.
func TestAdversarialNonMemberReceivesConfiguredDefault(t *testing.T) {
	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:write"}
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil } // non-member

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", "foreign-project", []string{"openid", "profile"}, nil)
	if !scopesEqual(got, []string{"data:write"}) {
		t.Fatalf("scopes = %v, want the configured default for a non-member", got)
	}
}

// A cached userinfo entry (auth_source=userinfo) must not be upgraded to a
// broad grant: cache round-trips only raw claims, and scopes are re-resolved on
// every request through the standard fail-closed path.
func TestAdversarialCachedUserinfoEntryHasNoGrant(t *testing.T) {
	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	m.cfg.Zitadel.ClientJWT = "fake-client-jwt"
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	claims := claimsFromCacheData(map[string]any{
		"sub":         "cached-user",
		"auth_source": string(authSourceUserinfo),
		"scope":       "",
	}, time.Now().Add(time.Minute))

	user, err := m.finalizeOIDCUser(context.Background(), claims, "")
	if err != nil {
		t.Fatalf("finalizeOIDCUser: %v", err)
	}
	if scopesEqual(user.Scopes, GetAllScopes()) {
		t.Fatalf("cached userinfo entry replayed as all-grant: %v", user.Scopes)
	}
	if len(user.Scopes) != 0 {
		t.Fatalf("scopes = %v, want none", user.Scopes)
	}
}

// User-id binding, layer 1 (resolver wiring). The project-role lookup is keyed
// on BOTH the declared project and the authenticated user's internal id.
// resolveOIDCScopes must pass the authenticated user's id as the lookup's user
// argument and must fail closed for a user whose membership does not exist —
// even when a DIFFERENT user genuinely holds a membership in the same project.
//
// This pins the resolver→lookup call site (and is exercised under -short/CI).
// The SQL column binding (WHERE project_id = ? AND user_id = ?) is pinned
// separately by TestDBProjectRoleBindsProjectAndUser and
// TestResolveOIDCScopesWrongUserHasNoMembership, which run only against a real
// database.
func TestAdversarialResolveScopesBindsAuthenticatedUser(t *testing.T) {
	clearZitadelEnv(t)

	const (
		projectID = "55555555-5555-5555-5555-555555555555"
		memberID  = "user-membership-holder"
		otherID   = "user-without-membership"
	)

	m := newTestMiddleware(t)
	// Only (projectID, memberID) resolves to a role. Any other (project, user)
	// pair returns "" — i.e. "not a member". A resolver that dropped or
	// swapped the user dimension would fail the sanity check below.
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) {
		if p == projectID && u == memberID {
			return RoleProjectViewer, nil
		}
		return "", nil
	}

	// Sanity: the real member resolves to the viewer read-only set, so the
	// wrong-user assertion below is not vacuous.
	if got := m.resolveOIDCScopes(context.Background(), memberID, projectID, []string{"openid", "profile"}, nil); !scopesEqual(got, viewerReadOnlyScopes) {
		t.Fatalf("member scopes = %v, want the viewer read-only set", got)
	}

	// A different user of the same project has no membership: zero scopes,
	// not merely a different set.
	got := m.resolveOIDCScopes(context.Background(), otherID, projectID, []string{"openid", "profile"}, nil)
	if len(got) != 0 {
		t.Fatalf("wrong-user scopes = %v, want none (membership is user-id bound)", got)
	}
}
