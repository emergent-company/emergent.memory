package config

import (
	"bytes"
	"log/slog"
	"os"
	"testing"
)

// scopePolicyEnvVars is every environment variable the scope-policy knobs read,
// so tests can run independent of the ambient process environment. The
// ZITADEL_* aliases were removed with the app-owned vocabulary migration
// (unify-scope-authority §7.2), so they are no longer read or cleared here.
var scopePolicyEnvVars = []string{
	"MEMORY_OIDC_DEFAULT_SCOPES",
	"MEMORY_USERINFO_GRANT_ALL_SCOPES",
	"MEMORY_OIDC_TRUST_TOKEN_SCOPES",
}

// clearScopePolicyEnv unsets every scope-policy env var for the duration of the
// test, restoring each on cleanup.
func clearScopePolicyEnv(t *testing.T) {
	t.Helper()
	for _, k := range scopePolicyEnvVars {
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

// newScopePolicyConfig loads config with a log sink that captures warnings.
func newScopePolicyConfig(t *testing.T) (*Config, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	cfg, err := NewConfig(slog.New(slog.NewTextHandler(&buf, nil)))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg, &buf
}

// The app-owned name configures the default scope set.
func TestScopePolicyDefaultScopesConfigured(t *testing.T) {
	clearScopePolicyEnv(t)
	t.Setenv("MEMORY_OIDC_DEFAULT_SCOPES", "data:read,search")
	cfg, _ := newScopePolicyConfig(t)

	want := []string{"data:read", "search"}
	if len(cfg.Zitadel.OIDCDefaultScopes) != len(want) ||
		cfg.Zitadel.OIDCDefaultScopes[0] != want[0] ||
		cfg.Zitadel.OIDCDefaultScopes[1] != want[1] {
		t.Fatalf("OIDCDefaultScopes = %v, want %v", cfg.Zitadel.OIDCDefaultScopes, want)
	}
}

// The app-owned name configures the grant-all knob, which still defaults true.
func TestScopePolicyGrantAllConfigured(t *testing.T) {
	t.Run("explicit false", func(t *testing.T) {
		clearScopePolicyEnv(t)
		t.Setenv("MEMORY_USERINFO_GRANT_ALL_SCOPES", "false")
		cfg, _ := newScopePolicyConfig(t)
		if cfg.Zitadel.UserinfoGrantAllScopes {
			t.Fatal("explicit false must be honoured")
		}
	})
	t.Run("unset defaults true", func(t *testing.T) {
		clearScopePolicyEnv(t)
		cfg, _ := newScopePolicyConfig(t)
		if !cfg.Zitadel.UserinfoGrantAllScopes {
			t.Fatal("UserinfoGrantAllScopes default should be true")
		}
	})
}
