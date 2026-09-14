package main

import (
	"testing"
	"time"
)

// TestLoadConfigAuthDefaults asserts the auth-related env vars parse from the
// environment with the documented defaults when unset.
func TestLoadConfigAuthDefaults(t *testing.T) {
	// Isolate from any env a developer may have set: empty the auth vars so
	// the "defaults" assertions are deterministic.
	for _, k := range []string{
		"ZITADEL_ISSUER", "ZITADEL_CLIENT_ID",
		"ZITADEL_REDIRECT_URI", "AUTH_MODE", "SESSION_SECRET", "SESSION_MAX_AGE",
	} {
		t.Setenv(k, "")
	}

	cfg := LoadConfig()
	if cfg.ZitadelIssuer != "" {
		t.Errorf("ZitadelIssuer = %q, want empty default", cfg.ZitadelIssuer)
	}
	if cfg.ZitadelClientID != "" {
		t.Errorf("ZitadelClientID = %q, want empty default", cfg.ZitadelClientID)
	}
	if cfg.ZitadelRedirectURI != "" {
		t.Errorf("ZitadelRedirectURI = %q, want empty default", cfg.ZitadelRedirectURI)
	}
	if cfg.AuthMode != "session" {
		t.Errorf("AuthMode = %q, want default %q", cfg.AuthMode, "session")
	}
	if cfg.SessionSecret != "" {
		t.Errorf("SessionSecret = %q, want empty default", cfg.SessionSecret)
	}
	if cfg.SessionMaxAge != 30*24*time.Hour {
		t.Errorf("SessionMaxAge = %v, want default 30 days", cfg.SessionMaxAge)
	}
}

// TestLoadConfigAuthEnv asserts the auth vars are read from the environment
// when set.
func TestLoadConfigAuthEnv(t *testing.T) {
	t.Setenv("ZITADEL_ISSUER", "https://zitadel.example.com")
	t.Setenv("ZITADEL_CLIENT_ID", "client-123")
	t.Setenv("ZITADEL_REDIRECT_URI", "https://gw.example.com/auth/callback")
	t.Setenv("AUTH_MODE", "session")
	t.Setenv("SESSION_SECRET", "cookie-signing-key")
	t.Setenv("SESSION_MAX_AGE", "24h")

	cfg := LoadConfig()
	if cfg.ZitadelIssuer != "https://zitadel.example.com" {
		t.Errorf("ZitadelIssuer = %q", cfg.ZitadelIssuer)
	}
	if cfg.ZitadelClientID != "client-123" {
		t.Errorf("ZitadelClientID = %q", cfg.ZitadelClientID)
	}
	if cfg.ZitadelRedirectURI != "https://gw.example.com/auth/callback" {
		t.Errorf("ZitadelRedirectURI = %q", cfg.ZitadelRedirectURI)
	}
	if cfg.AuthMode != "session" {
		t.Errorf("AuthMode = %q", cfg.AuthMode)
	}
	if cfg.SessionSecret != "cookie-signing-key" {
		t.Errorf("SessionSecret = %q", cfg.SessionSecret)
	}
	if cfg.SessionMaxAge != 24*time.Hour {
		t.Errorf("SessionMaxAge = %v, want 24h", cfg.SessionMaxAge)
	}
}

// TestConfigValidate covers the fail-closed auth posture check: session mode
// requires a full Zitadel + cookie config, while dev is an explicit opt-in.
func TestConfigValidate(t *testing.T) {
	full := Config{
		AuthMode:           "session",
		SessionSecret:      "0123456789abcdef0123456789abcdef",
		ZitadelIssuer:      "https://zitadel.example.com",
		ZitadelClientID:    "client-123",
		ZitadelRedirectURI: "https://gw.example.com/auth/callback",
		PublicBaseURL:      "https://gw.example.com",
		MemoryURL:          "https://memory.example.com",
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "session fully configured",
			cfg:     full,
			wantErr: false,
		},
		{
			name:    "session missing everything",
			cfg:     Config{AuthMode: "session"},
			wantErr: true,
		},
		{
			name: "session http base url in development",
			cfg: Config{
				AuthMode:           "session",
				SessionSecret:      "0123456789abcdef0123456789abcdef",
				ZitadelIssuer:      "https://zitadel.example.com",
				ZitadelClientID:    "client-123",
				ZitadelRedirectURI: "http://gw.example.com/auth/callback",
				PublicBaseURL:      "http://gw.example.com",
				MemoryURL:          "https://memory.example.com",
				SentryEnvironment:  "development",
			},
			wantErr: false,
		},
		{
			name: "session http base url in production",
			cfg: Config{
				AuthMode:           "session",
				SessionSecret:      "0123456789abcdef0123456789abcdef",
				ZitadelIssuer:      "https://zitadel.example.com",
				ZitadelClientID:    "client-123",
				ZitadelRedirectURI: "http://gw.example.com/auth/callback",
				PublicBaseURL:      "http://gw.example.com",
				MemoryURL:          "https://memory.example.com",
				SentryEnvironment:  "production",
			},
			wantErr: true,
		},
		{
			name:    "dev opt-in",
			cfg:     Config{AuthMode: "dev"},
			wantErr: false,
		},
		{
			name:    "unknown mode",
			cfg:     Config{AuthMode: "bogus"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}
