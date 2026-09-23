package auth

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// §2 — MEMORY_OIDC_TRUST_TOKEN_SCOPES gates the token-scope grant branch. When
// enabled (the Release N default) a vocabulary-matching token scope is honoured
// verbatim and terminally; when disabled the token branch is skipped entirely and
// the resolver proceeds to app-derived entitlements → default → empty, never
// unioning token scopes with app-derived scopes.

func TestTrustTokenScopesOnHonoursTokenScopesVerbatim(t *testing.T) {
	m := newTestMiddleware(t)
	m.cfg.Zitadel.TrustTokenScopes = true
	m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:read"}
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectViewer, nil }

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", "proj", []string{"schema:write", "openid"}, nil)
	wantScopeSet(t, got, []string{"schema:write"})
}

func TestTrustTokenScopesOffIgnoresTokenScopes(t *testing.T) {
	m := newTestMiddleware(t)
	m.cfg.Zitadel.TrustTokenScopes = false
	m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:read"}
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return RoleProjectViewer, nil }

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", "proj", []string{"schema:write", "openid"}, nil)
	// Exact set equality: the viewer read-only set, never a union with the token
	// scope, never the configured default.
	wantScopeSet(t, got, []string{"data:read", "schema:read", "agents:read", "projects:read"})
}

func TestTrustTokenScopesOffNoEntitlementEmpty(t *testing.T) {
	m := newTestMiddleware(t)
	m.cfg.Zitadel.TrustTokenScopes = false
	m.roleLookup = func(ctx context.Context, p, u string) (string, error) { return "", nil }

	got := m.resolveOIDCScopes(context.Background(), "user-uuid", "proj", []string{"schema:write"}, nil)
	if len(got) != 0 {
		t.Fatalf("scopes = %v, want none (token scope ignored, no entitlement, no default)", got)
	}
}

func TestWarnIfTokenScopesTrusted(t *testing.T) {
	tests := []struct {
		name     string
		trust    bool
		wantWarn bool
	}{
		{name: "trust on warns", trust: true, wantWarn: true},
		{name: "trust off silent", trust: false, wantWarn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			m := &Middleware{
				cfg: &config.Config{Zitadel: config.ZitadelConfig{TrustTokenScopes: tt.trust}},
				log: slog.New(slog.NewTextHandler(&buf, nil)),
			}
			m.warnIfTokenScopesTrusted()
			out := buf.String()
			if tt.wantWarn {
				if !strings.Contains(out, "MEMORY_OIDC_TRUST_TOKEN_SCOPES") {
					t.Fatalf("expected warning naming the flag; got: %s", out)
				}
			} else if out != "" {
				t.Fatalf("expected no warning; got: %s", out)
			}
		})
	}
}
