package config

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// scopePolicyEnvVars is every environment variable the scope-policy aliasing
// reads, so tests can run independent of the ambient process environment.
var scopePolicyEnvVars = []string{
	"MEMORY_OIDC_DEFAULT_SCOPES",
	"ZITADEL_OIDC_DEFAULT_SCOPES",
	"MEMORY_USERINFO_GRANT_ALL_SCOPES",
	"ZITADEL_USERINFO_GRANT_ALL_SCOPES",
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

// The canonical name must configure the default set when set alone.
func TestScopePolicyAliasCanonicalDefaultScopesOnly(t *testing.T) {
	clearScopePolicyEnv(t)
	t.Setenv("MEMORY_OIDC_DEFAULT_SCOPES", "data:read,search")
	cfg, buf := newScopePolicyConfig(t)

	want := []string{"data:read", "search"}
	if len(cfg.Zitadel.OIDCDefaultScopes) != len(want) || cfg.Zitadel.OIDCDefaultScopes[0] != want[0] || cfg.Zitadel.OIDCDefaultScopes[1] != want[1] {
		t.Fatalf("OIDCDefaultScopes = %v, want %v", cfg.Zitadel.OIDCDefaultScopes, want)
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Errorf("canonical-only config must not emit a deprecation warning; got: %s", buf.String())
	}
}

// The deprecated alias configures the default set and emits a warning naming both.
func TestScopePolicyAliasDeprecatedDefaultScopesOnly(t *testing.T) {
	clearScopePolicyEnv(t)
	t.Setenv("ZITADEL_OIDC_DEFAULT_SCOPES", "data:read")
	cfg, buf := newScopePolicyConfig(t)

	want := []string{"data:read"}
	if len(cfg.Zitadel.OIDCDefaultScopes) != 1 || cfg.Zitadel.OIDCDefaultScopes[0] != want[0] {
		t.Fatalf("OIDCDefaultScopes = %v, want %v", cfg.Zitadel.OIDCDefaultScopes, want)
	}
	out := buf.String()
	for _, want := range []string{"ZITADEL_OIDC_DEFAULT_SCOPES", "MEMORY_OIDC_DEFAULT_SCOPES"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning missing %q; got: %s", want, out)
		}
	}
}

// When both names are set, the canonical name wins and the alias is ignored
// with a warning.
func TestScopePolicyAliasCanonicalWinsWhenBothSet(t *testing.T) {
	clearScopePolicyEnv(t)
	t.Setenv("MEMORY_OIDC_DEFAULT_SCOPES", "data:read")
	t.Setenv("ZITADEL_OIDC_DEFAULT_SCOPES", "data:write")
	cfg, buf := newScopePolicyConfig(t)

	want := []string{"data:read"}
	if len(cfg.Zitadel.OIDCDefaultScopes) != 1 || cfg.Zitadel.OIDCDefaultScopes[0] != want[0] {
		t.Fatalf("OIDCDefaultScopes = %v, want %v (canonical must win)", cfg.Zitadel.OIDCDefaultScopes, want)
	}
	if !strings.Contains(buf.String(), "deprecated") {
		t.Errorf("both-set config must warn about the ignored alias; got: %s", buf.String())
	}
}

// The grant-all alias carries the same precedence matrix, including the true
// default when neither name is set.
func TestScopePolicyAliasGrantAllPrecedence(t *testing.T) {
	t.Run("canonical set", func(t *testing.T) {
		clearScopePolicyEnv(t)
		t.Setenv("MEMORY_USERINFO_GRANT_ALL_SCOPES", "false")
		cfg, buf := newScopePolicyConfig(t)
		if cfg.Zitadel.UserinfoGrantAllScopes {
			t.Fatal("canonical false must win")
		}
		if strings.Contains(buf.String(), "deprecated") {
			t.Errorf("canonical-only grant-all must not warn; got: %s", buf.String())
		}
	})
	t.Run("alias only", func(t *testing.T) {
		clearScopePolicyEnv(t)
		t.Setenv("ZITADEL_USERINFO_GRANT_ALL_SCOPES", "false")
		cfg, buf := newScopePolicyConfig(t)
		if cfg.Zitadel.UserinfoGrantAllScopes {
			t.Fatal("alias false must be honoured")
		}
		out := buf.String()
		for _, want := range []string{"ZITADEL_USERINFO_GRANT_ALL_SCOPES", "MEMORY_USERINFO_GRANT_ALL_SCOPES"} {
			if !strings.Contains(out, want) {
				t.Errorf("warning missing %q; got: %s", want, out)
			}
		}
	})
	t.Run("both set, canonical wins", func(t *testing.T) {
		clearScopePolicyEnv(t)
		t.Setenv("MEMORY_USERINFO_GRANT_ALL_SCOPES", "false")
		t.Setenv("ZITADEL_USERINFO_GRANT_ALL_SCOPES", "true")
		cfg, buf := newScopePolicyConfig(t)
		if cfg.Zitadel.UserinfoGrantAllScopes {
			t.Fatal("canonical false must win over alias true")
		}
		if !strings.Contains(buf.String(), "deprecated") {
			t.Errorf("both-set grant-all must warn; got: %s", buf.String())
		}
	})
	t.Run("neither set defaults true", func(t *testing.T) {
		clearScopePolicyEnv(t)
		cfg, _ := newScopePolicyConfig(t)
		if !cfg.Zitadel.UserinfoGrantAllScopes {
			t.Fatal("UserinfoGrantAllScopes default should be true")
		}
	})
}
