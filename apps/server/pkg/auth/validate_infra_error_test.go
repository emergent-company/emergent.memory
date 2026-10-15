package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// These tests pin the issue #1481 error semantics: a validation path that
// fails for infrastructure reasons must surface as a retryable 503
// service_unavailable, while a genuine invalid/inactive token stays 401.

// cacheMiss is the normal "token not in the introspection cache" result.
func cacheMiss(context.Context, string) (*TokenClaims, error) {
	return nil, sql.ErrNoRows
}

func assertAppError(t *testing.T, err error, wantStatus int, wantCode string) {
	t.Helper()
	appErr, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("error = %T (%v), want *apperror.Error", err, err)
	}
	if appErr.HTTPStatus != wantStatus {
		t.Fatalf("HTTP status = %d, want %d (err: %v)", appErr.HTTPStatus, wantStatus, err)
	}
	if appErr.Code != wantCode {
		t.Fatalf("error code = %q, want %q", appErr.Code, wantCode)
	}
}

// transport error -> 503, not 401: the token may be perfectly valid and the
// client must retry rather than discard the credential.
func TestValidateTokenTransportErrorIsServiceUnavailable(t *testing.T) {
	m := newTestMiddleware(t)
	m.introspectCacheLookup = cacheMiss
	m.zitadelSvc = &fakeIntrospector{
		introErr:    fmt.Errorf("%w: dial tcp 10.0.0.1:443: connection refused", ErrIntrospectionUnavailable),
		userInfoErr: fmt.Errorf("%w: userinfo request: connection refused", ErrIntrospectionUnavailable),
	}

	user, err := m.validateToken(context.Background(), "opaque-token", "")
	if user != nil {
		t.Fatalf("validateToken() returned a user %+v; an infra failure must not authenticate", user)
	}
	assertAppError(t, err, http.StatusServiceUnavailable, apperror.ErrServiceUnavailable.Code)
}

// DB cache unavailable + introspection/userinfo down -> 503 as well.
func TestValidateTokenDBCacheUnavailableIsServiceUnavailable(t *testing.T) {
	m := newTestMiddleware(t)
	m.introspectCacheLookup = func(context.Context, string) (*TokenClaims, error) {
		return nil, errors.New("sql: database is closed")
	}
	m.zitadelSvc = &fakeIntrospector{
		introErr:    errors.New("introspection call failed: connection refused"),
		userInfoErr: errors.New("userinfo request: connection refused"),
	}

	_, err := m.validateToken(context.Background(), "opaque-token", "")
	assertAppError(t, err, http.StatusServiceUnavailable, apperror.ErrServiceUnavailable.Code)
}

// A genuinely invalid token (Zitadel reports it inactive / userinfo 401) stays
// 401 invalid_token.
func TestValidateTokenInactiveIsUnauthorized(t *testing.T) {
	m := newTestMiddleware(t)
	m.introspectCacheLookup = cacheMiss
	m.zitadelSvc = &fakeIntrospector{
		introResult: &IntrospectionResult{Active: false},
		userInfoErr: errUserInfoInvalidToken,
	}

	_, err := m.validateToken(context.Background(), "opaque-token", "")
	assertAppError(t, err, http.StatusUnauthorized, apperror.ErrInvalidToken.Code)
}

// An authoritative introspection rejection must stay 401 even when the userinfo
// fallback then fails for infrastructure reasons. The definitive reject must
// take precedence so it can never be laundered into a retryable 503.
func TestValidateTokenInactivePrecedesInfraFallback(t *testing.T) {
	m := newTestMiddleware(t)
	m.introspectCacheLookup = cacheMiss
	m.zitadelSvc = &fakeIntrospector{
		introResult: &IntrospectionResult{Active: false},
		userInfoErr: fmt.Errorf("%w: userinfo request: connection refused", ErrIntrospectionUnavailable),
	}

	user, err := m.validateToken(context.Background(), "opaque-token", "")
	if user != nil {
		t.Fatalf("validateToken() returned a user %+v; an inactive token must not authenticate", user)
	}
	assertAppError(t, err, http.StatusUnauthorized, apperror.ErrInvalidToken.Code)
}

// An authoritative introspection rejection MUST NOT be laundered into a
// successful authentication by a later validation path: a token Zitadel
// declared inactive is rejected (401) even if the userinfo endpoint then
// appears to accept it. Regression for the fail-open branch where an
// inactive introspection verdict fell through to userinfo. (Copilot #1508.)
func TestValidateTokenInactiveNotLaunderedByUserinfoSuccess(t *testing.T) {
	m := newTestMiddleware(t)
	m.introspectCacheLookup = cacheMiss
	m.zitadelSvc = &fakeIntrospector{
		introResult: &IntrospectionResult{Active: false},
		userInfo:    &UserInfoResult{Sub: "ghost-user", Email: "ghost@example.com"},
	}
	m.roleLookup = func(context.Context, string, string) (string, error) { return "", nil }

	user, err := m.validateToken(context.Background(), "revoked-token", "")
	if user != nil {
		t.Fatalf("validateToken() authenticated %+v from userinfo after introspection declared the token inactive", user)
	}
	assertAppError(t, err, http.StatusUnauthorized, apperror.ErrInvalidToken.Code)
}

// A missing owner profile is a genuine invalid token (401); any other profile
// lookup failure is infrastructure (503), matching the token-lookup semantics.
func TestClassifyProfileLookupError(t *testing.T) {
	t.Run("missing user is unauthorized", func(t *testing.T) {
		err := classifyProfileLookupError(fmt.Errorf("user not found: %w", sql.ErrNoRows))
		assertAppError(t, err, http.StatusUnauthorized, apperror.ErrInvalidToken.Code)
	})
	t.Run("db failure is service unavailable", func(t *testing.T) {
		err := classifyProfileLookupError(errors.New("sql: database is closed"))
		assertAppError(t, err, http.StatusServiceUnavailable, apperror.ErrServiceUnavailable.Code)
	})
}

// A valid cached introspection entry keeps working even while introspection
// transport fails — the last-known-good cache is consulted before the breaker.
func TestValidateTokenCacheHitSurvivesTransportBlip(t *testing.T) {
	m := newTestMiddleware(t)
	m.introspectCacheLookup = func(context.Context, string) (*TokenClaims, error) {
		return &TokenClaims{
			Sub:        "cached-user",
			AuthSource: authSourceIntrospection,
			ExpiresAt:  time.Now().Add(time.Hour),
		}, nil
	}
	m.zitadelSvc = &fakeIntrospector{
		introErr:    fmt.Errorf("%w: dial tcp: connection refused", ErrIntrospectionUnavailable),
		userInfoErr: fmt.Errorf("%w: userinfo request: connection refused", ErrIntrospectionUnavailable),
	}
	m.roleLookup = func(context.Context, string, string) (string, error) { return "", nil }

	user, err := m.validateToken(context.Background(), "oidc-token", "")
	if err != nil {
		t.Fatalf("validateToken() with a warm cache = %v, want success during transport blip", err)
	}
	if user == nil || user.Sub != "cached-user" {
		t.Fatalf("validateToken() user = %+v, want Sub=cached-user", user)
	}
}

// Standalone mode has no external OIDC provider. An unrecognised token must be
// a definitive 401, not a retryable 503 produced by an unreachable default
// issuer, and the external validation paths must not be consulted at all.
// Regression for the e2e API contract (TestAuth_InvalidTokenReturns401).
func TestValidateTokenStandaloneUnknownTokenIsUnauthorized(t *testing.T) {
	m := newTestMiddleware(t)
	m.cfg.Standalone.Enabled = true
	m.cfg.Standalone.APIKey = "standalone-key"
	// If the external paths were consulted, this would authenticate the token.
	m.zitadelSvc = &fakeIntrospector{
		userInfo: &UserInfoResult{Sub: "should-not-be-reached", Email: "x@example.com"},
	}
	m.roleLookup = func(context.Context, string, string) (string, error) { return "", nil }

	user, err := m.validateToken(context.Background(), "random-invalid-token", "")
	if user != nil {
		t.Fatalf("validateToken() in standalone authenticated %+v; an unknown token must be rejected", user)
	}
	assertAppError(t, err, http.StatusUnauthorized, apperror.ErrInvalidToken.Code)
}

// Dev test tokens are gated on an environment allowlist: accepted in local/dev
// (including the standalone e2e stack, which runs STANDALONE_MODE=true with
// ENVIRONMENT=local) and rejected in production and preview (issue #1833). This
// test covers the standalone+local case — the e2e API stack's secondary
// identities must authenticate so authorization layers decide 403/200, not auth.
func TestValidateTokenStandaloneLocalAllowsDevTestTokens(t *testing.T) {
	cases := []struct {
		token      string
		wantScopes []string
	}{
		{"e2e-attacker", GetAllScopes()},
		{"graph-read", []string{"graph:read", "graph:search:read"}},
		{"no-scope", []string{}},
		{"all-scopes", GetAllScopes()},
	}
	for _, tc := range cases {
		m := newTestMiddleware(t)
		m.cfg.Standalone.Enabled = true
		m.cfg.Standalone.APIKey = "standalone-key"
		// Environment stays "local" (the config default).

		user, err := m.validateToken(context.Background(), tc.token, "")
		if err != nil {
			t.Fatalf("validateToken(%q) in standalone-local errored: %v; dev test tokens must authenticate", tc.token, err)
		}
		if user == nil {
			t.Fatalf("validateToken(%q) in standalone-local returned nil user; dev test tokens must authenticate", tc.token)
		}
		if !scopesEqual(user.Scopes, tc.wantScopes) {
			t.Fatalf("validateToken(%q) scopes = %v, want %v", tc.token, user.Scopes, tc.wantScopes)
		}
	}
}

// A standalone preview (STANDALONE_MODE=true, ENVIRONMENT=preview) must reject
// the published dev test tokens: the environment allowlist is false there, and
// the standalone path fails closed with 401 rather than reaching the token map
// (issue #1833).
func TestValidateTokenStandalonePreviewRejectsDevTestTokens(t *testing.T) {
	for _, tok := range []string{"e2e-test-user", "e2e-query-token", "e2e-arbitrary", "all-scopes", "with-scope"} {
		m := newTestMiddleware(t)
		m.cfg.Standalone.Enabled = true
		m.cfg.Standalone.APIKey = "standalone-key"
		m.cfg.Environment = "preview"

		user, err := m.validateToken(context.Background(), tok, "")
		if user != nil {
			t.Fatalf("validateToken(%q) in standalone preview authenticated %+v; dev test tokens must be rejected", tok, user)
		}
		assertAppError(t, err, http.StatusUnauthorized, apperror.ErrInvalidToken.Code)
	}
}

// A non-standalone preview using Zitadel auth (PREVIEW_AUTH_MODE=zitadel sets
// STANDALONE_MODE=false with ENVIRONMENT=preview) must also reject the published
// dev test tokens. checkTestToken is gated off, so the introspection path — not
// the token map — decides the credential, and an inactive introspection verdict
// surfaces as 401 (issue #1833).
func TestValidateTokenZitadelPreviewRejectsDevTestTokens(t *testing.T) {
	for _, tok := range []string{"e2e-test-user", "e2e-arbitrary", "all-scopes", "with-scope"} {
		m := newTestMiddleware(t)
		m.cfg.Environment = "preview" // standalone stays disabled (default)
		m.introspectCacheLookup = cacheMiss
		m.zitadelSvc = &fakeIntrospector{
			introResult: &IntrospectionResult{Active: false},
			userInfoErr: errUserInfoInvalidToken,
		}
		m.roleLookup = func(context.Context, string, string) (string, error) { return "", nil }

		user, err := m.validateToken(context.Background(), tok, "")
		if user != nil {
			t.Fatalf("validateToken(%q) in zitadel preview authenticated %+v; dev test tokens must be rejected", tok, user)
		}
		assertAppError(t, err, http.StatusUnauthorized, apperror.ErrInvalidToken.Code)
	}
}

// TestValidateTokenDevTestTokenEnvironmentAllowlist is the full truth table for
// the test-token gate: accepted in local/development regardless of standalone
// mode, rejected in preview/production regardless of standalone mode, and the
// DEBUG escape hatch re-enables acceptance everywhere (issue #1833).
func TestValidateTokenDevTestTokenEnvironmentAllowlist(t *testing.T) {
	cases := []struct {
		name        string
		standalone  bool
		environment string
		debug       bool
		wantAuth    bool
	}{
		{"standalone local", true, "local", false, true},
		{"non-standalone local", false, "local", false, true},
		{"standalone development", true, "development", false, true},
		{"non-standalone development", false, "development", false, true},
		{"standalone preview", true, "preview", false, false},
		{"non-standalone preview", false, "preview", false, false},
		{"standalone production", true, "production", false, false},
		{"non-standalone production", false, "production", false, false},
		{"standalone preview debug", true, "preview", true, true},
		{"non-standalone production debug", false, "production", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestMiddleware(t)
			m.cfg.Standalone.Enabled = tc.standalone
			m.cfg.Standalone.APIKey = "standalone-key"
			m.cfg.Environment = tc.environment
			m.cfg.Debug = tc.debug
			// Rejection cases that are non-standalone fall through to OIDC
			// introspection; give a deterministic inactive verdict so they resolve
			// authoritatively to 401 (standalone rejections fail closed earlier).
			m.introspectCacheLookup = cacheMiss
			m.zitadelSvc = &fakeIntrospector{
				introResult: &IntrospectionResult{Active: false},
				userInfoErr: errUserInfoInvalidToken,
			}
			m.roleLookup = func(context.Context, string, string) (string, error) { return "", nil }

			user, err := m.validateToken(context.Background(), "all-scopes", "")
			if tc.wantAuth {
				if err != nil || user == nil {
					t.Fatalf("validateToken() = (user=%v, err=%v), want authenticated", user, err)
				}
				return
			}
			if user != nil {
				t.Fatalf("validateToken() authenticated %+v, want rejection", user)
			}
			assertAppError(t, err, http.StatusUnauthorized, apperror.ErrInvalidToken.Code)
		})
	}
}
