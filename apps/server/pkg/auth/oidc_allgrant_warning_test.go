package auth

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
)

func TestWarnIfOIDCAllGrantActive(t *testing.T) {
	tests := []struct {
		name       string
		flag       bool
		introspect bool
		wantLogged bool
	}{
		{name: "flag on, introspection unconfigured", flag: true, wantLogged: true},
		{name: "flag on, introspection configured", flag: true, introspect: true, wantLogged: false},
		{name: "flag off, introspection unconfigured", flag: false, wantLogged: false},
		{name: "flag off, introspection configured", flag: false, introspect: true, wantLogged: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			z := config.ZitadelConfig{UserinfoGrantAllScopes: tt.flag}
			if tt.introspect {
				z.ClientJWT = "jwt"
			}
			m := &Middleware{cfg: &config.Config{Zitadel: z}, log: slog.New(slog.NewTextHandler(&buf, nil))}
			m.warnIfOIDCAllGrantActive()
			out := buf.String()
			if tt.wantLogged {
				if out == "" {
					t.Fatal("expected the all-grant warning to be logged, got nothing")
				}
				for _, want := range []string{"ZITADEL_CLIENT_JWT", "MEMORY_USERINFO_GRANT_ALL_SCOPES=false", "GetAllScopes"} {
					if !strings.Contains(out, want) {
						t.Errorf("warning missing %q; got: %s", want, out)
					}
				}
			} else if out != "" {
				t.Errorf("expected no warning, got: %s", out)
			}
		})
	}
}

// TestNewMiddlewareWarnsWhenAllGrantActive exercises the constructor, not just
// the helper: the contract is a startup warning, so constructing the middleware
// must be what emits it for the active configuration.
func TestNewMiddlewareWarnsWhenAllGrantActive(t *testing.T) {
	tests := []struct {
		name       string
		flag       bool
		introspect bool
		wantWarn   bool
	}{
		{name: "flag on, introspection unconfigured", flag: true, wantWarn: true},
		{name: "flag on, introspection configured", flag: true, introspect: true},
		{name: "flag off, introspection unconfigured", flag: false},
		{name: "flag off, introspection configured", flag: false, introspect: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			z := config.ZitadelConfig{UserinfoGrantAllScopes: tt.flag}
			if tt.introspect {
				z.ClientJWT = "jwt"
			}
			NewMiddleware(MiddlewareParams{
				Cfg: &config.Config{Zitadel: z},
				Log: slog.New(slog.NewTextHandler(&buf, nil)),
			})
			out := buf.String()
			if tt.wantWarn && !strings.Contains(out, "OIDC all-scope grant is ACTIVE") {
				t.Errorf("NewMiddleware did not emit the all-grant warning; log: %s", out)
			}
			if !tt.wantWarn && strings.Contains(out, "OIDC all-scope grant is ACTIVE") {
				t.Errorf("NewMiddleware emitted the all-grant warning for an inactive posture; log: %s", out)
			}
		})
	}
}
