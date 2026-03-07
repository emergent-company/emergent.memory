package auth

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/labstack/echo/v4"
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

func TestRequireProjectScope_AccountToken_AllowsAnyProject(t *testing.T) {
	m := &Middleware{}

	// An account token has APITokenProjectID == "" (no project binding)
	user := &AuthUser{
		ID:                "user-1",
		APITokenID:        "token-1",
		APITokenProjectID: "", // account token — no binding
		Scopes:            []string{"projects:read"},
	}

	called := false
	handler := m.RequireProjectScope()(func(c echo.Context) error {
		called = true
		return nil
	})

	// projectId in URL is some arbitrary project — should be allowed through
	c := makeEchoCtx("project-abc", user)
	err := handler(c)

	if err != nil {
		t.Errorf("RequireProjectScope() returned error %v for account token; want nil", err)
	}
	if !called {
		t.Error("RequireProjectScope() did not call next for account token")
	}
}

func TestRequireProjectScope_ProjectToken_BlocksDifferentProject(t *testing.T) {
	m := &Middleware{}

	user := &AuthUser{
		ID:                "user-1",
		APITokenID:        "token-1",
		APITokenProjectID: "project-bound", // token is bound to this project
		Scopes:            []string{"data:read"},
	}

	handler := m.RequireProjectScope()(func(c echo.Context) error {
		return nil
	})

	// Request targets a DIFFERENT project
	c := makeEchoCtx("project-other", user)
	err := handler(c)

	if err == nil {
		t.Error("RequireProjectScope() should have returned an error for mismatched project, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusForbidden {
		t.Errorf("RequireProjectScope() error = %v, want 403 HTTPError", err)
	}
}

func TestRequireProjectScope_ProjectToken_AllowsMatchingProject(t *testing.T) {
	m := &Middleware{}

	user := &AuthUser{
		ID:                "user-1",
		APITokenID:        "token-1",
		APITokenProjectID: "project-bound",
		Scopes:            []string{"data:read"},
	}

	called := false
	handler := m.RequireProjectScope()(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("project-bound", user)
	err := handler(c)

	if err != nil {
		t.Errorf("RequireProjectScope() returned error %v for matching project; want nil", err)
	}
	if !called {
		t.Error("RequireProjectScope() did not call next for matching project")
	}
}

func TestRequireProjectScope_OAuthSession_PassesThrough(t *testing.T) {
	m := &Middleware{}

	// OAuth session: APITokenProjectID is "" AND APITokenID is ""
	user := &AuthUser{
		ID:                "user-1",
		APITokenID:        "",
		APITokenProjectID: "",
		Scopes:            []string{"data:read"},
	}

	called := false
	handler := m.RequireProjectScope()(func(c echo.Context) error {
		called = true
		return nil
	})

	c := makeEchoCtx("any-project", user)
	err := handler(c)

	if err != nil {
		t.Errorf("RequireProjectScope() returned error %v for OAuth session; want nil", err)
	}
	if !called {
		t.Error("RequireProjectScope() did not call next for OAuth session")
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
