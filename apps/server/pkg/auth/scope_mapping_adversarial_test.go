package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
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
		"ZITADEL_USERINFO_GRANT_ALL_SCOPES",
		"ZITADEL_OIDC_DEFAULT_SCOPES",
		"DISABLE_ZITADEL_INTROSPECTION",
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
// must not re-enable the legacy all-grant on the userinfo fallback.
func TestAdversarialIntrospectionOutageDoesNotReenableAllGrant(t *testing.T) {
	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	// Introspection IS configured (this disables the all-grant) ...
	m.cfg.Zitadel.ClientJWT = "fake-client-jwt"
	m.cfg.Zitadel.UserinfoGrantAllScopes = true // explicit operator flag still on
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
	if len(user.Scopes) == len(GetAllScopes()) {
		t.Fatalf("introspection outage re-enabled the all-grant: %v", user.Scopes)
	}
	if len(user.Scopes) != 0 {
		t.Fatalf("scopes = %v, want none (fail closed during outage)", user.Scopes)
	}
}

// Shipped default posture: with no Zitadel credentials, the userinfo path still
// receives the full catalogue (pre-existing pilot behaviour preserved by #730).
// This documents that #667 is only mitigated when the operator configures
// introspection or sets ZITADEL_USERINFO_GRANT_ALL_SCOPES=false.
func TestAdversarialShippedDefaultStillAllGrantsUserinfo(t *testing.T) {
	clearZitadelEnv(t)
	cfg, err := config.NewConfig(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("config.NewConfig: %v", err)
	}
	if !cfg.Zitadel.UserinfoGrantAllScopes {
		t.Fatal("expected the shipped default ZITADEL_USERINFO_GRANT_ALL_SCOPES=true")
	}

	m := &Middleware{
		cfg:     cfg,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		userSvc: newFakeUserProfiles(),
		zitadelSvc: &fakeIntrospector{
			userInfo: &UserInfoResult{Sub: "default-posture", Email: "u@example.com"},
		},
	}

	if !m.oidcAllGrantEnabled() {
		t.Fatal("expected all-grant to be enabled under the shipped default config")
	}
	user, err := m.validateToken(context.Background(), "oidc-token", "")
	if err != nil {
		t.Fatalf("validateToken: %v", err)
	}
	if len(user.Scopes) != len(GetAllScopes()) {
		t.Fatalf("shipped default did not grant all scopes: got %d want %d", len(user.Scopes), len(GetAllScopes()))
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

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", "foreign-project", []string{"openid", "profile"})
	if !scopesEqual(got, []string{"data:write"}) {
		t.Fatalf("scopes = %v, want the configured default for a non-member", got)
	}
}

// Explicit Memory scopes in the token are returned verbatim with no project or
// membership check. This is by design (issuer-granted), but a token carrying
// Memory scopes is honoured for ANY declared project. Requires issuer
// cooperation to be exploitable.
func TestAdversarialExplicitScopesBypassMembership(t *testing.T) {
	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil } // non-member

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", "foreign-project", []string{"data:write", "openid"})
	if !scopesEqual(got, []string{"data:write"}) {
		t.Fatalf("scopes = %v, want explicit data:write verbatim", got)
	}
}

// A cached userinfo entry (auth_source=userinfo) under an introspection-
// configured deployment must not be upgraded to the full catalogue.
func TestAdversarialCachedUserinfoEntryNoAllGrantWhenIntrospectionConfigured(t *testing.T) {
	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	m.cfg.Zitadel.ClientJWT = "fake-client-jwt"
	m.cfg.Zitadel.UserinfoGrantAllScopes = true
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
	if len(user.Scopes) == len(GetAllScopes()) {
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
	if got := m.resolveOIDCScopes(context.Background(), memberID, projectID, []string{"openid", "profile"}); !scopesEqual(got, viewerReadOnlyScopes) {
		t.Fatalf("member scopes = %v, want the viewer read-only set", got)
	}

	// A different user of the same project has no membership: zero scopes,
	// not merely a different set.
	got := m.resolveOIDCScopes(context.Background(), otherID, projectID, []string{"openid", "profile"})
	if len(got) != 0 {
		t.Fatalf("wrong-user scopes = %v, want none (membership is user-id bound)", got)
	}
}

// The vocabulary filter must reject non-Memory scopes, including the
// never-issuable admin:all umbrella, so Zitadel OIDC noise cannot become a grant.
func TestAdversarialVocabularyFilterRejectsForeignScopes(t *testing.T) {
	for _, s := range []string{"openid", "profile", "email", "offline_access", "admin:all", "urn:zitadel:iam", "foo:bar", ""} {
		if got := filterMemoryScopes([]string{s}); len(got) != 0 {
			t.Errorf("filterMemoryScopes(%q) = %v, want none", s, got)
		}
	}
	// Sanity: real Memory scopes survive.
	if got := filterMemoryScopes([]string{"data:read", " search ", "data:read"}); !scopesEqual(got, []string{"data:read", "search"}) {
		t.Errorf("vocabulary filter = %v, want [data:read search]", got)
	}
}
