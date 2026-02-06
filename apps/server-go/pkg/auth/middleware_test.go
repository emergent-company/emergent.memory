package auth

import (
	"net/http"
	"net/url"
	"testing"
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
