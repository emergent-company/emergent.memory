package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
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
		// Default entitlement-tier seams: no superadmin grant, no project org
		// context, no org_admin — so unit tests without an explicit seam exercise
		// the project-role/default path only.
		superadminLookup: func(ctx context.Context, u string) (string, error) { return "", nil },
		projectOrgLookup: func(ctx context.Context, p string) (string, error) { return "", nil },
		orgAdminLookup:   func(ctx context.Context, o, u string) (bool, error) { return false, nil },
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

// wantScopeSet asserts got is EXACTLY want as a set. It compares sorted copies
// (order-insensitive) so a same-cardinality swap of distinct scopes fails, and
// it rejects duplicates in got that a multiset count could hide. Scope
// assertions in this file use this helper, never a bare len() comparison.
func wantScopeSet(t *testing.T, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if len(g) != len(w) {
		t.Fatalf("scope set = %v, want exactly %v", got, want)
	}
	for i := range g {
		if g[i] != w[i] {
			t.Fatalf("scope set = %v, want exactly %v", got, want)
		}
	}
	for i := 1; i < len(g); i++ {
		if g[i] == g[i-1] {
			t.Fatalf("scope set contains duplicate %q: %v", g[i], got)
		}
	}
}

// sortedKeys returns the keys of a scope set as a sorted slice (for exact
// comparison of expandScopes output).
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
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
			name:       "user membership yields the viewer set plus data:write",
			rawScopes:  []string{"openid"},
			projectID:  projectID,
			roleLookup: roleOK(RoleProjectUser),
			want:       []string{"data:read", "schema:read", "agents:read", "projects:read", "data:write"},
		},
		{
			name:       "admin membership yields the user set plus agents:write and schema:write",
			rawScopes:  []string{"openid"},
			projectID:  projectID,
			roleLookup: roleOK(RoleProjectAdmin),
			want:       []string{"data:read", "schema:read", "agents:read", "projects:read", "data:write", "agents:write", "schema:write"},
		},
		{
			name:       "legacy owner role yields no scopes without a default",
			rawScopes:  []string{"openid"},
			projectID:  projectID,
			roleLookup: roleOK("owner"),
			want:       nil,
		},
		{
			// #736 decision B: an unrecognised role fails closed even when an
			// operator default is configured — the default is for non-members.
			name:       "legacy owner role yields no scopes even with a default configured",
			rawScopes:  []string{"openid"},
			projectID:  projectID,
			defaults:   []string{"data:write"},
			roleLookup: roleOK("owner"),
			want:       nil,
		},
		{
			name:       "typo role yields no scopes even with a default configured",
			rawScopes:  []string{"openid"},
			projectID:  projectID,
			defaults:   []string{"data:read"},
			roleLookup: roleOK("project_admn"),
			want:       nil,
		},
		{
			name:       "configured default set is applied when there is no membership",
			rawScopes:  []string{"openid", "profile"},
			projectID:  projectID,
			defaults:   []string{"data:read", "search"},
			roleLookup: roleOK(""),
			want:       []string{"data:read", "search"},
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

			got := m.resolveOIDCScopes(context.Background(), "user-uuid", tt.projectID, tt.rawScopes, nil)
			wantScopeSet(t, got, tt.want)
		})
	}
}

// --- roleToScopes ----------------------------------------------------------

func TestRoleToScopes(t *testing.T) {
	tests := []struct {
		role string
		want []string
	}{
		{
			role: RoleProjectViewer,
			want: []string{"data:read", "schema:read", "agents:read", "projects:read"},
		},
		{
			role: RoleProjectUser,
			want: []string{"data:read", "schema:read", "agents:read", "projects:read", "data:write"},
		},
		{
			role: RoleProjectAdmin,
			want: []string{"data:read", "schema:read", "agents:read", "projects:read", "data:write", "agents:write", "schema:write"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			scopes, ok := roleToScopes(tt.role)
			if !ok {
				t.Fatalf("roleToScopes(%q) not mapped", tt.role)
			}
			wantScopeSet(t, scopes, tt.want)
		})
	}

	// Unrecognised role strings are unmapped and must fail closed — including
	// the legacy project role `owner` that migration 00165 rewrites, and
	// case/typo variants.
	for _, role := range []string{"owner", "admin", "", "unknown", "project_admn", "Project_Admin", "PROJECT_USER"} {
		if _, ok := roleToScopes(role); ok {
			t.Errorf("roleToScopes(%q) should be unmapped (fail closed)", role)
		}
	}
}

// exclusionPrefixes are the scope families a project role must never reach:
// account/org administration and the reserved mcp admin scope (#736 decision A).
var excludedRoleScopes = []string{
	"admin",
	"admin:read",
	"admin:write",
	"admin:all",
	"mcp:admin",
	"org:read",
	"org:invite:create",
	"org:project:create",
	"org:project:delete",
	"project:invite:create",
}

// The three role sets must be nested: admin ⊇ user ⊇ viewer, both before and
// after umbrella expansion, since the expanded set is what authorization
// actually consumes.
func TestRoleScopeInvariantAdminSupersetUserSupersetViewer(t *testing.T) {
	rawViewer, _ := roleToScopes(RoleProjectViewer)
	rawUser, _ := roleToScopes(RoleProjectUser)
	rawAdmin, _ := roleToScopes(RoleProjectAdmin)

	superset := func(t *testing.T, outer, inner []string, label string) {
		t.Helper()
		set := make(map[string]bool, len(outer))
		for _, s := range outer {
			set[s] = true
		}
		for _, s := range inner {
			if !set[s] {
				t.Fatalf("%s: %q missing from outer set %v (inner %v)", label, s, outer, inner)
			}
		}
	}

	superset(t, rawUser, rawViewer, "raw user ⊇ viewer")
	superset(t, rawAdmin, rawUser, "raw admin ⊇ user")

	superset(t, sortedKeys(expandScopes(rawUser)), sortedKeys(expandScopes(rawViewer)), "expanded user ⊇ viewer")
	superset(t, sortedKeys(expandScopes(rawAdmin)), sortedKeys(expandScopes(rawUser)), "expanded admin ⊇ user")
}

// The slices returned by roleToScopes must never share a backing array — with
// package state, or across roles. A shared array would let one caller's
// in-place edit leak privileges into another role's set. This pins the copy
// semantics that `withScopes`/roleToScopes rely on.
func TestRoleScopeSetsDoNotShareBackingArray(t *testing.T) {
	want := map[string][]string{
		RoleProjectViewer: {"data:read", "schema:read", "agents:read", "projects:read"},
		RoleProjectUser:   {"data:read", "schema:read", "agents:read", "projects:read", "data:write"},
		RoleProjectAdmin:  {"data:read", "schema:read", "agents:read", "projects:read", "data:write", "agents:write", "schema:write"},
	}

	// 1. Independently fetched slices must not alias across roles: overwriting
	//    every element of the viewer slice must not change a separately fetched
	//    user slice.
	viewer, ok := roleToScopes(RoleProjectViewer)
	if !ok {
		t.Fatal("roleToScopes(project_viewer) not mapped")
	}
	user, ok := roleToScopes(RoleProjectUser)
	if !ok {
		t.Fatal("roleToScopes(project_user) not mapped")
	}
	for i := range viewer {
		viewer[i] = "admin:all"
	}
	wantScopeSet(t, user, want[RoleProjectUser])

	// 2. An in-place edit (and an append that would spill into any shared spare
	//    capacity) must not corrupt package state: a fresh read of each role
	//    still yields its exact set.
	for role := range want {
		scopes, ok := roleToScopes(role)
		if !ok {
			t.Fatalf("roleToScopes(%q) not mapped", role)
		}
		for i := range scopes {
			scopes[i] = "admin:all"
		}
		_ = append(scopes, "admin:all")
	}
	for role, w := range want {
		got, ok := roleToScopes(role)
		if !ok {
			t.Fatalf("roleToScopes(%q) not mapped", role)
		}
		wantScopeSet(t, got, w)
	}
}

// Role-derived scopes are umbrella scopes, and their expansion is an accepted,
// deliberate widening (#736 decision A). This test PINS the exact expanded set
// per role so any future change to scopeImplies that widens a role's effective
// grant must update this test and be re-reviewed. It also asserts the expansion
// never reaches an excluded admin/org/account scope, which would be a blocker.
func TestRoleScopeUmbrellaExpansionIsPinned(t *testing.T) {
	tests := []struct {
		role string
		want []string
	}{
		{
			role: RoleProjectViewer,
			want: []string{
				"data:read", "documents:read", "chunks:read", "search:read", "graph:read",
				"graph:search:read", "extraction:read", "schema:read", "tasks:read",
				"user-activity:read", "notifications:read", "search", "journal:read",
				"agents:read", "chat:use", "skills:read", "projects:read",
			},
		},
		{
			role: RoleProjectUser,
			want: []string{
				"data:read", "documents:read", "chunks:read", "search:read", "graph:read",
				"graph:search:read", "extraction:read", "schema:read", "tasks:read",
				"user-activity:read", "notifications:read", "search", "journal:read",
				"agents:read", "chat:use", "skills:read", "projects:read",
				"data:write", "documents:write", "documents:delete", "chunks:write",
				"graph:write", "ingest:write", "extraction:write", "tasks:write",
				"user-activity:write", "notifications:write", "schema:write", "journal:write",
			},
		},
		{
			role: RoleProjectAdmin,
			want: []string{
				"data:read", "documents:read", "chunks:read", "search:read", "graph:read",
				"graph:search:read", "extraction:read", "schema:read", "tasks:read",
				"user-activity:read", "notifications:read", "search", "journal:read",
				"agents:read", "chat:use", "skills:read", "projects:read",
				"data:write", "documents:write", "documents:delete", "chunks:write",
				"graph:write", "ingest:write", "extraction:write", "tasks:write",
				"user-activity:write", "notifications:write", "schema:write", "journal:write",
				"agents:write", "chat:admin", "skills:write", "schema:migrate",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			scopes, ok := roleToScopes(tt.role)
			if !ok {
				t.Fatalf("roleToScopes(%q) not mapped", tt.role)
			}
			expanded := sortedKeys(expandScopes(scopes))
			wantScopeSet(t, expanded, tt.want)

			for _, s := range expanded {
				for _, ex := range excludedRoleScopes {
					if s == ex {
						t.Fatalf("role %q expansion reached excluded scope %q", tt.role, s)
					}
				}
				if strings.HasPrefix(s, "admin") || strings.HasPrefix(s, "org:") || strings.HasPrefix(s, "account:") {
					t.Fatalf("role %q expansion reached excluded scope family %q", tt.role, s)
				}
			}
		})
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

	t.Run("configured default set is applied to a user with no membership", func(t *testing.T) {
		m := newTestMiddleware(t)
		m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:read", "search"}
		m.zitadelSvc = &fakeIntrospector{introResult: &IntrospectionResult{
			Active: true,
			Sub:    "user-3",
			Scope:  "openid profile",
			Exp:    4102444800,
		}}
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

		user, err := m.validateToken(context.Background(), "oidc-token", projectID)
		if err != nil {
			t.Fatalf("validateToken: %v", err)
		}
		if !scopesEqual(user.Scopes, []string{"data:read", "search"}) {
			t.Fatalf("scopes = %v, want configured default set", user.Scopes)
		}
	})

	t.Run("admin membership resolves the write-inclusive admin set end to end", func(t *testing.T) {
		m := newTestMiddleware(t)
		// A configured default must not leak into a mapped role's result.
		m.cfg.Zitadel.OIDCDefaultScopes = []string{"admin:write"}
		m.zitadelSvc = &fakeIntrospector{introResult: &IntrospectionResult{
			Active: true,
			Sub:    "user-admin",
			Scope:  "openid profile",
			Exp:    4102444800,
		}}
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectAdmin, nil }

		user, err := m.validateToken(context.Background(), "oidc-token", projectID)
		if err != nil {
			t.Fatalf("validateToken: %v", err)
		}
		wantScopeSet(t, user.Scopes, []string{
			"data:read", "schema:read", "agents:read", "projects:read",
			"data:write", "agents:write", "schema:write",
		})
	})

	t.Run("unrecognised membership role fails closed despite a configured default", func(t *testing.T) {
		m := newTestMiddleware(t)
		m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:write", "search"}
		m.zitadelSvc = &fakeIntrospector{introResult: &IntrospectionResult{
			Active: true,
			Sub:    "user-legacy",
			Scope:  "openid profile",
			Exp:    4102444800,
		}}
		m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "owner", nil }

		user, err := m.validateToken(context.Background(), "oidc-token", projectID)
		if err != nil {
			t.Fatalf("validateToken: %v", err)
		}
		if len(user.Scopes) != 0 {
			t.Fatalf("scopes = %v, want none (unmapped role must fail closed)", user.Scopes)
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

	got := m.resolveOIDCScopes(context.Background(), "user-legacy", "44444444-4444-4444-4444-444444444444", claims.Scopes, nil)
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
