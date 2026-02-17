package auth

import (
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/emergent-company/emergent/internal/config"
)

func TestMiddleware_checkStandaloneAPIKey(t *testing.T) {
	tests := []struct {
		name           string
		standaloneMode string
		standaloneKey  string
		requestKey     string
		wantUser       bool // true if AuthUser should be returned
	}{
		{
			name:           "standalone mode disabled",
			standaloneMode: "false",
			standaloneKey:  "test-key-123",
			requestKey:     "test-key-123",
			wantUser:       false, // Should return nil when disabled
		},
		{
			name:           "standalone mode enabled with matching key",
			standaloneMode: "true",
			standaloneKey:  "test-key-123",
			requestKey:     "test-key-123",
			wantUser:       true, // Should return AuthUser
		},
		{
			name:           "standalone mode enabled with wrong key",
			standaloneMode: "true",
			standaloneKey:  "test-key-123",
			requestKey:     "wrong-key",
			wantUser:       false, // Should return nil
		},
		{
			name:           "standalone mode enabled with no key in request",
			standaloneMode: "true",
			standaloneKey:  "test-key-123",
			requestKey:     "",
			wantUser:       false, // Should return nil
		},
		{
			name:           "standalone mode enabled with no configured key",
			standaloneMode: "true",
			standaloneKey:  "",
			requestKey:     "some-key",
			wantUser:       false, // Should return nil (not configured)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			os.Setenv("STANDALONE_MODE", tt.standaloneMode)
			os.Setenv("STANDALONE_API_KEY", tt.standaloneKey)
			defer func() {
				os.Unsetenv("STANDALONE_MODE")
				os.Unsetenv("STANDALONE_API_KEY")
			}()

			// Load config
			log := slog.Default()
			cfg, err := config.NewConfig(log)
			if err != nil {
				t.Fatalf("failed to create config: %v", err)
			}

			// Create middleware with config
			m := &Middleware{
				cfg: cfg,
			}

			// Create request with X-API-Key header
			req, err := http.NewRequest("GET", "http://example.com/test", nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			if tt.requestKey != "" {
				req.Header.Set("X-API-Key", tt.requestKey)
			}

			// Call checkStandaloneAPIKey
			user := m.checkStandaloneAPIKey(req)

			// Verify result
			if tt.wantUser && user == nil {
				t.Errorf("checkStandaloneAPIKey() = nil, want AuthUser")
			}
			if !tt.wantUser && user != nil {
				t.Errorf("checkStandaloneAPIKey() = AuthUser, want nil")
			}

			// If user is returned, verify it has all scopes
			if user != nil {
				if user.Sub != "standalone" {
					t.Errorf("user.Sub = %q, want %q", user.Sub, "standalone")
				}
				if len(user.Scopes) == 0 {
					t.Errorf("user.Scopes is empty, want all scopes")
				}
			}
		})
	}
}

func TestMiddleware_checkStandaloneAPIKey_CaseSensitive(t *testing.T) {
	os.Setenv("STANDALONE_MODE", "true")
	os.Setenv("STANDALONE_API_KEY", "TestKey123")
	defer func() {
		os.Unsetenv("STANDALONE_MODE")
		os.Unsetenv("STANDALONE_API_KEY")
	}()

	log := slog.Default()
	cfg, err := config.NewConfig(log)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}
	m := &Middleware{cfg: cfg}

	tests := []struct {
		name       string
		requestKey string
		wantUser   bool
	}{
		{
			name:       "exact match",
			requestKey: "TestKey123",
			wantUser:   true,
		},
		{
			name:       "lowercase mismatch",
			requestKey: "testkey123",
			wantUser:   false,
		},
		{
			name:       "uppercase mismatch",
			requestKey: "TESTKEY123",
			wantUser:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "http://example.com/test", nil)
			req.Header.Set("X-API-Key", tt.requestKey)

			user := m.checkStandaloneAPIKey(req)

			if tt.wantUser && user == nil {
				t.Errorf("checkStandaloneAPIKey() = nil, want AuthUser")
			}
			if !tt.wantUser && user != nil {
				t.Errorf("checkStandaloneAPIKey() = AuthUser, want nil")
			}
		})
	}
}

func TestMiddleware_checkStandaloneAPIKey_SpecialCharacters(t *testing.T) {
	os.Setenv("STANDALONE_MODE", "true")
	os.Setenv("STANDALONE_API_KEY", "test-key_123.abc")
	defer func() {
		os.Unsetenv("STANDALONE_MODE")
		os.Unsetenv("STANDALONE_API_KEY")
	}()

	log := slog.Default()
	cfg, err := config.NewConfig(log)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}
	m := &Middleware{cfg: cfg}

	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-API-Key", "test-key_123.abc")

	user := m.checkStandaloneAPIKey(req)
	if user == nil {
		t.Errorf("checkStandaloneAPIKey() with special characters = nil, want AuthUser")
	}
}
