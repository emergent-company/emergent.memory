package auth

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func TestMiddleware_extractToken(t *testing.T) {
	// extractToken is a method that only uses the http.Request
	// It doesn't use any Middleware fields, so we can test with a minimal Middleware
	m := &Middleware{}

	tests := []struct {
		name       string
		authHeader string
		queryToken string
		want       string
	}{
		{
			name:       "bearer token in header",
			authHeader: "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9",
			want:       "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9",
		},
		{
			name:       "bearer token with lowercase",
			authHeader: "Bearer token123",
			want:       "token123",
		},
		{
			name:       "no token",
			authHeader: "",
			want:       "",
		},
		{
			name:       "non-bearer auth header",
			authHeader: "Basic dXNlcjpwYXNz",
			want:       "",
		},
		{
			name:       "token in query parameter",
			queryToken: "query-token-123",
			want:       "query-token-123",
		},
		{
			name:       "header takes precedence over query",
			authHeader: "Bearer header-token",
			queryToken: "query-token",
			want:       "header-token",
		},
		{
			name:       "empty bearer prefix",
			authHeader: "Bearer ",
			want:       "",
		},
		{
			name:       "bearer without space",
			authHeader: "Bearertoken",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a request with the appropriate header/query
			reqURL := "http://example.com/test"
			if tt.queryToken != "" {
				reqURL += "?token=" + url.QueryEscape(tt.queryToken)
			}

			req, err := http.NewRequest("GET", reqURL, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			got := m.extractToken(req)
			if got != tt.want {
				t.Errorf("extractToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMiddleware_extractToken_MultipleQueryParams(t *testing.T) {
	m := &Middleware{}

	// Test that other query params don't interfere
	req, _ := http.NewRequest("GET", "http://example.com/test?foo=bar&token=mytoken&baz=qux", nil)
	got := m.extractToken(req)
	if got != "mytoken" {
		t.Errorf("extractToken() with multiple params = %q, want %q", got, "mytoken")
	}
}

func TestMiddleware_extractToken_SpecialCharactersInToken(t *testing.T) {
	m := &Middleware{}

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "token with dots",
			token: "part1.part2.part3",
		},
		{
			name:  "token with dashes",
			token: "abc-def-ghi",
		},
		{
			name:  "token with underscores",
			token: "emt_abc_def_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "http://example.com/test", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)

			got := m.extractToken(req)
			if got != tt.token {
				t.Errorf("extractToken() = %q, want %q", got, tt.token)
			}
		})
	}
}

// helpers for middleware unit tests

func makeEchoCtx(projectIDParam string, user *AuthUser) echo.Context {
	e := echo.New()
	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	rec := &fakeResponseWriter{}
	c := e.NewContext(req, rec)
	if projectIDParam != "" {
		c.SetParamNames("projectId")
		c.SetParamValues(projectIDParam)
	}
	if user != nil {
		c.Set(string(UserContextKey), user)
	}
	return c
}

// fakeResponseWriter satisfies http.ResponseWriter for echo context creation.
type fakeResponseWriter struct{ header http.Header }

func (f *fakeResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}
func (f *fakeResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (f *fakeResponseWriter) WriteHeader(int)             {}

func TestRequireProjectTokenScope_AccountToken_AllowsAnyProject(t *testing.T) {
	m := &Middleware{}

	// An account token has APITokenProjectID == "" (no project binding)
	user := &AuthUser{
		ID:                "user-1",
		APITokenID:        "token-1",
		APITokenProjectID: "", // account token — no binding
		Scopes:            []string{"projects:read"},
	}

	called := false
	handler := m.RequireProjectTokenScope()(func(c echo.Context) error {
		called = true
		return nil
	})

	// projectId in URL is some arbitrary project — should be allowed through
	c := makeEchoCtx("project-abc", user)
	err := handler(c)

	if err != nil {
		t.Errorf("RequireProjectTokenScope() returned error %v for account token; want nil", err)
	}
	if !called {
		t.Error("RequireProjectTokenScope() did not call next for account token")
	}
}

func TestRequireProjectTokenScope_ProjectToken_BlocksDifferentProject(t *testing.T) {
	m := &Middleware{}

	user := &AuthUser{
		ID:                "user-1",
		APITokenID:        "token-1",
		APITokenProjectID: "project-bound", // token is bound to this project
		Scopes:            []string{"data:read"},
	}

	handler := m.RequireProjectTokenScope()(func(c echo.Context) error {
		return nil
	})

	// Request targets a DIFFERENT project
	c := makeEchoCtx("project-other", user)
	err := handler(c)

	if err == nil {
		t.Error("RequireProjectTokenScope() should have returned an error for mismatched project, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusForbidden {
		t.Errorf("RequireProjectTokenScope() error = %v, want 403 HTTPError", err)
	}
}

func TestRequireProjectTokenScope_ProjectToken_AllowsMatchingProject(t *testing.T) {
	m := &Middleware{}

	user := &AuthUser{
		ID:                "user-1",
		APITokenID:        "token-1",
		APITokenProjectID: "project-bound",
		Scopes:            []string{"data:read"},
	}

	called := false
	handler := m.RequireProjectTokenScope()(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("project-bound", user)
	err := handler(c)

	if err != nil {
		t.Errorf("RequireProjectTokenScope() returned error %v for matching project; want nil", err)
	}
	if !called {
		t.Error("RequireProjectTokenScope() did not call next for matching project")
	}
}

func TestRequireProjectTokenScope_OAuthSession_PassesThrough(t *testing.T) {
	m := &Middleware{}

	// OAuth session: APITokenProjectID is "" AND APITokenID is ""
	user := &AuthUser{
		ID:                "user-1",
		APITokenID:        "",
		APITokenProjectID: "",
		Scopes:            []string{"data:read"},
	}

	called := false
	handler := m.RequireProjectTokenScope()(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("any-project", user)
	err := handler(c)

	if err != nil {
		t.Errorf("RequireProjectTokenScope() returned error %v for OAuth session; want nil", err)
	}
	if !called {
		t.Error("RequireProjectTokenScope() did not call next for OAuth session")
	}
}

func TestRequireProjectMember_NoUser_Unauthorized(t *testing.T) {
	m := &Middleware{}
	called := false
	handler := m.RequireProjectMember()(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("project-1", nil)
	err := handler(c)
	if err == nil {
		t.Fatal("RequireProjectMember() should have returned an error for a missing user")
	}
	if status, _ := apperror.ToHTTPError(err); status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if called {
		t.Error("RequireProjectMember() called next for a missing user")
	}
}

func TestRequireProjectMember_APIToken_PassesThrough(t *testing.T) {
	m := &Middleware{}
	user := &AuthUser{ID: "user-1", APITokenID: "token-1", APITokenProjectID: "project-1"}
	called := false
	handler := m.RequireProjectMember()(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("project-1", user)
	if err := handler(c); err != nil {
		t.Fatalf("RequireProjectMember() returned error %v for API token; want nil", err)
	}
	if !called {
		t.Error("RequireProjectMember() did not call next for API token")
	}
}

func TestRequireProjectMember_SessionMember_Allows(t *testing.T) {
	m := &Middleware{
		projectOrgLookup: func(_ context.Context, _ string) (string, error) { return "org-1", nil },
		orgMemberLookup:  func(_ context.Context, _, _ string) (bool, error) { return true, nil },
	}
	user := &AuthUser{ID: "user-1"}
	called := false
	handler := m.RequireProjectMember()(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("project-1", user)
	if err := handler(c); err != nil {
		t.Fatalf("RequireProjectMember() returned error %v for a member; want nil", err)
	}
	if !called {
		t.Error("RequireProjectMember() did not call next for a member")
	}
}

func TestRequireProjectMember_SessionNonMember_Forbidden(t *testing.T) {
	m := &Middleware{
		projectOrgLookup: func(_ context.Context, _ string) (string, error) { return "org-1", nil },
		orgMemberLookup:  func(_ context.Context, _, _ string) (bool, error) { return false, nil },
	}
	user := &AuthUser{ID: "user-1"}
	called := false
	handler := m.RequireProjectMember()(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("project-1", user)
	err := handler(c)
	if err == nil {
		t.Fatal("RequireProjectMember() should have returned an error for a non-member")
	}
	if status, _ := apperror.ToHTTPError(err); status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", status)
	}
	if called {
		t.Error("RequireProjectMember() called next for a non-member")
	}
}

func TestRequireProjectMember_UnknownProject_NotFound(t *testing.T) {
	m := &Middleware{
		projectOrgLookup: func(_ context.Context, _ string) (string, error) { return "", nil },
		orgMemberLookup:  func(_ context.Context, _, _ string) (bool, error) { return false, nil },
	}
	user := &AuthUser{ID: "user-1"}
	called := false
	handler := m.RequireProjectMember()(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("project-unknown", user)
	err := handler(c)
	if err == nil {
		t.Fatal("RequireProjectMember() should have returned an error for an unknown project")
	}
	if status, _ := apperror.ToHTTPError(err); status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
	if called {
		t.Error("RequireProjectMember() called next for an unknown project")
	}
}

func TestRequireAPITokenScopes_AccountToken_BlocksMissingScope(t *testing.T) {
	m := &Middleware{}

	user := &AuthUser{
		ID:         "user-1",
		APITokenID: "token-1", // marks this as an emt_* token call
		Scopes:     []string{"data:read"},
	}

	handler := m.RequireAPITokenScopes("projects:read")(func(c echo.Context) error {
		return nil
	})

	c := makeEchoCtx("", user)
	err := handler(c)

	if err == nil {
		t.Error("RequireAPITokenScopes() should block token missing projects:read, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusForbidden {
		t.Errorf("RequireAPITokenScopes() error = %v, want 403 HTTPError", err)
	}
}

func TestRequireAPITokenScopes_AccountToken_AllowsWithScope(t *testing.T) {
	m := &Middleware{}

	user := &AuthUser{
		ID:         "user-1",
		APITokenID: "token-1",
		Scopes:     []string{"projects:read"},
	}

	called := false
	handler := m.RequireAPITokenScopes("projects:read")(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("", user)
	err := handler(c)

	if err != nil {
		t.Errorf("RequireAPITokenScopes() returned error %v; want nil", err)
	}
	if !called {
		t.Error("RequireAPITokenScopes() did not call next")
	}
}

func TestRequireAPITokenScopes_ProjectsWrite_ImpliesRead(t *testing.T) {
	m := &Middleware{}

	// projects:write should imply projects:read via scopeImplies
	user := &AuthUser{
		ID:         "user-1",
		APITokenID: "token-1",
		Scopes:     []string{"projects:write"},
	}

	called := false
	handler := m.RequireAPITokenScopes("projects:read")(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("", user)
	err := handler(c)

	if err != nil {
		t.Errorf("RequireAPITokenScopes() returned error %v for projects:write token requiring projects:read; want nil", err)
	}
	if !called {
		t.Error("RequireAPITokenScopes() did not call next")
	}
}

func TestExpandScopes_AdminAll(t *testing.T) {
	got := expandScopes([]string{"admin:all"})

	want := []string{
		"admin:read", "admin:write", "chat:admin", "mcp:admin",
		"data:read", "data:write", "projects:write", "documents:delete",
	}
	for _, scope := range want {
		if !got[scope] {
			t.Errorf("expandScopes([\"admin:all\"]) missing %q", scope)
		}
	}
}

func TestExpandScopes_ProjectsWrite_NoAdminImplication(t *testing.T) {
	got := expandScopes([]string{"projects:write"})

	if got["admin"] {
		t.Error(`expandScopes(["projects:write"]) must not contain "admin"`)
	}
	if !got["projects:read"] {
		t.Error(`expandScopes(["projects:write"]) missing "projects:read"`)
	}
}

func TestRequireAPITokenScopes_OAuthSession_BypassesCheck(t *testing.T) {
	m := &Middleware{}

	// OAuth session (no APITokenID) — scope check must be skipped entirely
	user := &AuthUser{
		ID:         "user-1",
		APITokenID: "", // not an emt_* token
		Scopes:     []string{},
	}

	called := false
	handler := m.RequireAPITokenScopes("projects:read")(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("", user)
	err := handler(c)

	if err != nil {
		t.Errorf("RequireAPITokenScopes() returned error %v for OAuth session; want nil", err)
	}
	if !called {
		t.Error("RequireAPITokenScopes() did not call next for OAuth session")
	}
}
