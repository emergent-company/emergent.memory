package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// --- fakes -----------------------------------------------------------------

type fakeIntrospector struct {
	introResult *IntrospectionResult
	introErr    error

	userInfo    *UserInfoResult
	userInfoErr error
}

func (f *fakeIntrospector) Introspect(ctx context.Context, token string) (*IntrospectionResult, error) {
	return f.introResult, f.introErr
}

func (f *fakeIntrospector) GetUserInfo(ctx context.Context, accessToken string) (*UserInfoResult, error) {
	return f.userInfo, f.userInfoErr
}

type fakeUserProfiles struct {
	profiles map[string]*UserProfile
}

func newFakeUserProfiles() *fakeUserProfiles {
	return &fakeUserProfiles{profiles: map[string]*UserProfile{}}
}

func (f *fakeUserProfiles) GetByID(ctx context.Context, id string) (*UserProfile, error) {
	for _, p := range f.profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeUserProfiles) EnsureProfile(ctx context.Context, subjectID string, info *UserProfileInfo) (*UserProfile, bool, error) {
	if p, ok := f.profiles[subjectID]; ok {
		return p, false, nil
	}
	p := &UserProfile{ID: "uuid-" + subjectID, ZitadelUserID: subjectID}
	f.profiles[subjectID] = p
	return p, true, nil
}

func newTestMiddleware(t *testing.T) *Middleware {
	t.Helper()
	cfg, err := config.NewConfig(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("config.NewConfig: %v", err)
	}
	return &Middleware{
		cfg:     cfg,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		userSvc: newFakeUserProfiles(),
	}
}

func scopesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}

// --- resolveOIDCScopes -----------------------------------------------------

func TestResolveOIDCScopes(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"

	roleOK := func(role string) projectRoleLookup {
		return func(ctx context.Context, p, u string) (string, error) { return role, nil }
	}

	tests := []struct {
		name       string
		rawScopes  []string
		projectID  string
		defaults   []string
		roleLookup projectRoleLookup
		want       []string
	}{
		{
			name:      "introspection with standard OIDC scopes and no grant yields no scopes",
			rawScopes: []string{"openid", "profile", "email", "offline_access"},
			projectID: projectID,
			want:      nil,
		},
		{
			name:       "viewer membership yields the read-only set",
			rawScopes:  []string{"openid", "profile"},
			projectID:  projectID,
			roleLookup: roleOK(RoleProjectViewer),
			want:       []string{"data:read", "schema:read", "agents:read", "projects:read"},
		},
		{
			name:       "unmapped admin role yields no scopes without a default",
			rawScopes:  []string{"openid"},
			projectID:  projectID,
			roleLookup: roleOK(RoleProjectAdmin),
			want:       nil,
		},
		{
			name:       "legacy owner role yields no scopes without a default",
			rawScopes:  []string{"openid"},
			projectID:  projectID,
			roleLookup: roleOK("owner"),
			want:       nil,
		},
		{
			name:       "configured default set is applied when there is no grant",
			rawScopes:  []string{"openid", "profile"},
			projectID:  projectID,
			defaults:   []string{"data:read", "search"},
			roleLookup: roleOK(""),
			want:       []string{"data:read", "search"},
		},
		{
			name:       "configured default set is applied to unmapped roles",
			rawScopes:  []string{"openid"},
			projectID:  projectID,
			defaults:   []string{"data:read"},
			roleLookup: roleOK("owner"),
			want:       []string{"data:read"},
		},
		{
			name:      "configured default set is applied without a project context",
			rawScopes: []string{"openid"},
			defaults:  []string{"projects:read"},
			want:      []string{"projects:read"},
		},
		{
			name:      "explicit memory scopes are used verbatim",
			rawScopes: []string{"openid", "data:write", "search"},
			projectID: projectID,
			defaults:  []string{"data:read"},
			want:      []string{"data:write", "search"},
		},
		{
			name:       "explicit memory scopes win over a mapped viewer role",
			rawScopes:  []string{"data:write"},
			projectID:  projectID,
			roleLookup: roleOK(RoleProjectViewer),
			want:       []string{"data:write"},
		},
		{
			name:       "standard OIDC scopes are not an explicit grant",
			rawScopes:  []string{"openid", "profile", "email", "offline_access"},
			projectID:  projectID,
			defaults:   []string{"data:read"},
			roleLookup: roleOK(""),
			want:       []string{"data:read"},
		},
		{
			name:       "role lookup error fails closed even with a default configured",
			rawScopes:  []string{"openid"},
			projectID:  projectID,
			defaults:   []string{"data:read", "data:write"},
			roleLookup: func(ctx context.Context, p, u string) (string, error) { return "", errors.New("db down") },
			want:       nil,
		},
		{
			name:      "duplicate explicit scopes are de-duplicated",
			rawScopes: []string{"data:read", "data:read", "openid"},
			projectID: projectID,
			want:      []string{"data:read"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestMiddleware(t)
			m.cfg.Zitadel.OIDCDefaultScopes = tt.defaults
			m.roleLookup = tt.roleLookup

			got := m.resolveOIDCScopes(context.Background(), "user-uuid", tt.projectID, tt.rawScopes)
			if !scopesEqual(got, tt.want) {
				t.Fatalf("resolveOIDCScopes() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- roleToScopes ----------------------------------------------------------

func TestRoleToScopes(t *testing.T) {
	scopes, ok := roleToScopes(RoleProjectViewer)
	if !ok {
		t.Fatal("roleToScopes(project_viewer) not mapped")
	}
	if !scopesEqual(scopes, []string{"data:read", "schema:read", "agents:read", "projects:read"}) {
		t.Fatalf("roleToScopes(project_viewer) = %v", scopes)
	}

	for _, role := range []string{RoleProjectAdmin, RoleProjectUser, "owner", "", "unknown"} {
		if _, ok := roleToScopes(role); ok {
			t.Errorf("roleToScopes(%q) should be unmapped (fail closed)", role)
		}
	}
}

// --- all-grant gate --------------------------------------------------------

func TestOIDCAllGrantEnabled(t *testing.T) {
	tests := []struct {
		name                 string
		grantAll             bool
		disableIntrospection bool
		clientJWT            string
		clientJWTPath        string
		want                 bool
	}{
		{name: "flag on, introspection unconfigured", grantAll: true, want: true},
		{name: "flag off, introspection unconfigured", grantAll: false, want: false},
		{name: "flag on, client JWT configured", grantAll: true, clientJWT: "jwt", want: false},
		{name: "flag on, client JWT path configured", grantAll: true, clientJWTPath: "/tmp/key.json", want: false},
		{name: "flag on, introspection disabled", grantAll: true, disableIntrospection: true, clientJWT: "jwt", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestMiddleware(t)
			m.cfg.Zitadel.UserinfoGrantAllScopes = tt.grantAll
			m.cfg.Zitadel.DisableIntrospection = tt.disableIntrospection
			m.cfg.Zitadel.ClientJWT = tt.clientJWT
			m.cfg.Zitadel.ClientJWTPath = tt.clientJWTPath

			if got := m.oidcAllGrantEnabled(); got != tt.want {
				t.Fatalf("oidcAllGrantEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- validateToken pipeline ------------------------------------------------

func TestValidateTokenIntrospectionPath(t *testing.T) {
	const projectID = "22222222-2222-2222-2222-222222222222"

	t.Run("standard OIDC scopes with viewer membership resolve to read-only", func(t *testing.T) {
		m := newTestMiddleware(t)
		m.zitadelSvc = &fakeIntrospector{introResult: &IntrospectionResult{
			Active: true,
			Sub:    "user-1",
			Email:  "user@example.com",
			Scope:  "openid profile email offline_access",
			Exp:    4102444800,
		}}
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) {
			if p != projectID {
				t.Fatalf("role lookup project = %q, want %q", p, projectID)
			}
			return RoleProjectViewer, nil
		}

		user, err := m.validateToken(context.Background(), "oidc-token", projectID)
		if err != nil {
			t.Fatalf("validateToken: %v", err)
		}
		if !scopesEqual(user.Scopes, []string{"data:read", "schema:read", "agents:read", "projects:read"}) {
			t.Fatalf("scopes = %v, want viewer read-only set", user.Scopes)
		}
	})

	t.Run("no membership and no default yields no scopes", func(t *testing.T) {
		m := newTestMiddleware(t)
		m.zitadelSvc = &fakeIntrospector{introResult: &IntrospectionResult{
			Active: true,
			Sub:    "user-2",
			Scope:  "openid profile email offline_access",
			Exp:    4102444800,
		}}
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

		user, err := m.validateToken(context.Background(), "oidc-token", projectID)
		if err != nil {
			t.Fatalf("validateToken: %v", err)
		}
		if len(user.Scopes) != 0 {
			t.Fatalf("scopes = %v, want none (fail closed)", user.Scopes)
		}
	})

	t.Run("configured default set is applied", func(t *testing.T) {
		m := newTestMiddleware(t)
		m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:read", "search"}
		m.zitadelSvc = &fakeIntrospector{introResult: &IntrospectionResult{
			Active: true,
			Sub:    "user-3",
			Scope:  "openid profile",
			Exp:    4102444800,
		}}
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectAdmin, nil }

		user, err := m.validateToken(context.Background(), "oidc-token", projectID)
		if err != nil {
			t.Fatalf("validateToken: %v", err)
		}
		if !scopesEqual(user.Scopes, []string{"data:read", "search"}) {
			t.Fatalf("scopes = %v, want configured default set", user.Scopes)
		}
	})
}

func TestValidateTokenUserinfoPath(t *testing.T) {
	sub := "user-userinfo"

	t.Run("all-grant preserved while introspection is unconfigured", func(t *testing.T) {
		m := newTestMiddleware(t)
		m.cfg.Zitadel.UserinfoGrantAllScopes = true
		m.zitadelSvc = &fakeIntrospector{userInfo: &UserInfoResult{Sub: sub, Email: "u@example.com"}}

		user, err := m.validateToken(context.Background(), "oidc-token", "")
		if err != nil {
			t.Fatalf("validateToken: %v", err)
		}
		if !scopesEqual(user.Scopes, GetAllScopes()) {
			t.Fatalf("scopes = %v, want full catalogue (pilot posture)", user.Scopes)
		}
	})

	t.Run("all-grant suppressed once introspection is configured", func(t *testing.T) {
		m := newTestMiddleware(t)
		m.cfg.Zitadel.UserinfoGrantAllScopes = true
		m.cfg.Zitadel.ClientJWT = "fake-client-jwt"
		// Introspection is configured, so the fake returns a result and the
		// userinfo path is not exercised; assert the resolved scopes instead.
		m.zitadelSvc = &fakeIntrospector{
			userInfo: &UserInfoResult{Sub: sub, Email: "u@example.com"},
			introResult: &IntrospectionResult{
				Active: true,
				Sub:    sub,
				Scope:  "openid profile",
				Exp:    4102444800,
			},
		}
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectAdmin, nil }

		user, err := m.validateToken(context.Background(), "oidc-token", "")
		if err != nil {
			t.Fatalf("validateToken: %v", err)
		}
		if scopesEqual(user.Scopes, GetAllScopes()) {
			t.Fatal("scopes must not be the full catalogue once introspection is configured")
		}
		if len(user.Scopes) != 0 {
			t.Fatalf("scopes = %v, want none", user.Scopes)
		}
	})

	t.Run("all-grant disabled falls back to resolution", func(t *testing.T) {
		m := newTestMiddleware(t)
		m.cfg.Zitadel.UserinfoGrantAllScopes = false
		m.cfg.Zitadel.OIDCDefaultScopes = []string{"projects:read"}
		m.zitadelSvc = &fakeIntrospector{userInfo: &UserInfoResult{Sub: sub, Email: "u@example.com"}}

		user, err := m.validateToken(context.Background(), "oidc-token", "")
		if err != nil {
			t.Fatalf("validateToken: %v", err)
		}
		if !scopesEqual(user.Scopes, []string{"projects:read"}) {
			t.Fatalf("scopes = %v, want configured default set", user.Scopes)
		}
	})
}

func TestValidateTokenUnknownIssuerFailsClosed(t *testing.T) {
	m := newTestMiddleware(t)
	// Both introspection and userinfo fail; JWT verification is not implemented.
	m.zitadelSvc = &fakeIntrospector{
		introErr:    errors.New("introspection unavailable"),
		userInfoErr: errors.New("unauthorized"),
	}
	m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:read"}

	_, err := m.validateToken(context.Background(), "opaque-token", "33333333-3333-3333-3333-333333333333")
	if err == nil {
		t.Fatal("validateToken() = nil error, want failure for unknown issuer")
	}
}

// --- cache hydration -------------------------------------------------------

func TestClaimsCacheRoundTrip(t *testing.T) {
	original := &TokenClaims{
		Sub:        "user-1",
		Email:      "u@example.com",
		Scopes:     []string{"openid", "profile", "data:read"},
		GivenName:  "Given",
		FamilyName: "Family",
		Name:       "Name",
		AuthSource: authSourceIntrospection,
	}

	rehydrated := claimsFromCacheData(claimsToCacheData(original), original.ExpiresAt)
	if rehydrated.Sub != original.Sub || rehydrated.Email != original.Email {
		t.Fatalf("identity round trip = %+v", rehydrated)
	}
	if !scopesEqual(rehydrated.Scopes, original.Scopes) {
		t.Fatalf("scopes round trip = %v, want %v", rehydrated.Scopes, original.Scopes)
	}
	if rehydrated.AuthSource != authSourceIntrospection {
		t.Fatalf("auth source = %q, want introspection", rehydrated.AuthSource)
	}
}

func TestClaimsFromCacheDataLegacyEntryDropsScopes(t *testing.T) {
	// Legacy entry: pre-#667 userinfo cached the full catalogue with no
	// auth_source key. It must not become an explicit Memory grant.
	legacy := map[string]any{
		"sub":   "user-legacy",
		"email": "legacy@example.com",
		"scope": strings.Join(GetAllScopes(), " "),
	}

	claims := claimsFromCacheData(legacy, time.Now().Add(time.Minute))
	if claims.Sub != "user-legacy" {
		t.Fatalf("sub = %q", claims.Sub)
	}
	if len(claims.Scopes) != 0 {
		t.Fatalf("legacy cached scopes = %v, want none", claims.Scopes)
	}
	if claims.AuthSource != authSourceIntrospection {
		t.Fatalf("auth source = %q, want introspection", claims.AuthSource)
	}
}

func TestLegacyCachedAllScopesAreNotAnExplicitGrant(t *testing.T) {
	m := newTestMiddleware(t)
	m.cfg.Zitadel.OIDCDefaultScopes = []string{"projects:read"}
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	legacy := map[string]any{"scope": strings.Join(GetAllScopes(), " ")}
	claims := claimsFromCacheData(legacy, time.Now().Add(time.Minute))

	got := m.resolveOIDCScopes(context.Background(), "user-legacy", "44444444-4444-4444-4444-444444444444", claims.Scopes)
	if scopesEqual(got, GetAllScopes()) {
		t.Fatal("legacy cached full catalogue must not be replayed as an explicit grant")
	}
	if !scopesEqual(got, []string{"projects:read"}) {
		t.Fatalf("scopes = %v, want the configured default set", got)
	}
}

// --- non-OIDC auth paths are untouched -------------------------------------

func TestValidateTokenDevTestTokenBypassesResolution(t *testing.T) {
	m := newTestMiddleware(t)
	// A default set is configured so a resolver bug would change the outcome.
	m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:write"}
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectViewer, nil }

	user, err := m.validateToken(context.Background(), "with-scope", "")
	if err != nil {
		t.Fatalf("validateToken: %v", err)
	}
	want := []string{"documents:read", "documents:write", "project:read"}
	if !scopesEqual(user.Scopes, want) {
		t.Fatalf("dev test token scopes = %v, want %v (must bypass role resolution)", user.Scopes, want)
	}
}
