package apitoken

import (
	"strings"
	"testing"
	"time"
)

func TestHashToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "standard token",
			token: "emt_abc123def456",
		},
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "long token",
			token: "emt_" + strings.Repeat("a", 64),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := hashToken(tt.token)
			// SHA-256 produces 64 hex characters
			if len(hash) != 64 {
				t.Errorf("hashToken() length = %d, want 64", len(hash))
			}
			// Should be deterministic
			hash2 := hashToken(tt.token)
			if hash != hash2 {
				t.Errorf("hashToken() not deterministic")
			}
		})
	}
}

func TestHashTokenDifferentInputs(t *testing.T) {
	hash1 := hashToken("token1")
	hash2 := hashToken("token2")
	if hash1 == hash2 {
		t.Error("different tokens should produce different hashes")
	}
}

func TestGetTokenPrefix(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected string
	}{
		{
			name:     "standard token",
			token:    "emt_abc123def456xyz",
			expected: "emt_abc123de",
		},
		{
			name:     "exactly 12 chars",
			token:    "emt_abc12345",
			expected: "emt_abc12345",
		},
		{
			name:     "less than 12 chars",
			token:    "emt_abc",
			expected: "emt_abc",
		},
		{
			name:     "empty token",
			token:    "",
			expected: "",
		},
		{
			name:     "11 chars",
			token:    "emt_abc1234",
			expected: "emt_abc1234",
		},
		{
			name:     "13 chars",
			token:    "emt_abc123456",
			expected: "emt_abc12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTokenPrefix(tt.token)
			if result != tt.expected {
				t.Errorf("getTokenPrefix(%q) = %q, want %q", tt.token, result, tt.expected)
			}
		})
	}
}

func TestGenerateToken(t *testing.T) {
	token, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken() error = %v", err)
	}

	// Should start with prefix
	if !strings.HasPrefix(token, TokenPrefix) {
		t.Errorf("generateToken() = %q, should start with %q", token, TokenPrefix)
	}

	// Should be 68 chars (4 prefix + 64 hex)
	expectedLen := len(TokenPrefix) + TokenRandomBytes*2
	if len(token) != expectedLen {
		t.Errorf("generateToken() length = %d, want %d", len(token), expectedLen)
	}

	// Should be unique
	token2, _ := generateToken()
	if token == token2 {
		t.Error("generateToken() should produce unique tokens")
	}
}

func TestApiToken_ToDTO(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-time.Hour)
	revoked := now.Add(-time.Minute)

	tests := []struct {
		name     string
		token    *ApiToken
		checkDTO func(t *testing.T, dto ApiTokenDTO)
	}{
		{
			name: "basic token (not revoked)",
			token: &ApiToken{
				ID:          "token-123",
				ProjectID:   "proj-456",
				UserID:      "user-789",
				Name:        "My API Token",
				TokenHash:   "hash123",
				TokenPrefix: "emt_abc12345",
				Scopes:      []string{"data:read", "data:write"},
				CreatedAt:   now,
				LastUsedAt:  nil,
				RevokedAt:   nil,
			},
			checkDTO: func(t *testing.T, dto ApiTokenDTO) {
				if dto.ID != "token-123" {
					t.Errorf("ID = %q, want %q", dto.ID, "token-123")
				}
				if dto.Name != "My API Token" {
					t.Errorf("Name = %q, want %q", dto.Name, "My API Token")
				}
				if dto.TokenPrefix != "emt_abc12345" {
					t.Errorf("TokenPrefix = %q, want %q", dto.TokenPrefix, "emt_abc12345")
				}
				if len(dto.Scopes) != 2 || dto.Scopes[0] != "data:read" {
					t.Errorf("Scopes = %v, want [data:read, data:write]", dto.Scopes)
				}
				if !dto.CreatedAt.Equal(now) {
					t.Errorf("CreatedAt = %v, want %v", dto.CreatedAt, now)
				}
				if dto.LastUsedAt != nil {
					t.Errorf("LastUsedAt = %v, want nil", dto.LastUsedAt)
				}
				if dto.IsRevoked {
					t.Errorf("IsRevoked = true, want false")
				}
			},
		},
		{
			name: "token with last used time",
			token: &ApiToken{
				ID:          "token-456",
				ProjectID:   "proj-789",
				UserID:      "user-012",
				Name:        "Used Token",
				TokenHash:   "hash456",
				TokenPrefix: "emt_def67890",
				Scopes:      []string{"schema:read"},
				CreatedAt:   now,
				LastUsedAt:  &lastUsed,
				RevokedAt:   nil,
			},
			checkDTO: func(t *testing.T, dto ApiTokenDTO) {
				if dto.LastUsedAt == nil {
					t.Error("LastUsedAt = nil, want non-nil")
				} else if !dto.LastUsedAt.Equal(lastUsed) {
					t.Errorf("LastUsedAt = %v, want %v", *dto.LastUsedAt, lastUsed)
				}
				if dto.IsRevoked {
					t.Errorf("IsRevoked = true, want false")
				}
			},
		},
		{
			name: "revoked token",
			token: &ApiToken{
				ID:          "token-789",
				ProjectID:   "proj-012",
				UserID:      "user-345",
				Name:        "Revoked Token",
				TokenHash:   "hash789",
				TokenPrefix: "emt_ghi01234",
				Scopes:      []string{"data:read"},
				CreatedAt:   now,
				LastUsedAt:  &lastUsed,
				RevokedAt:   &revoked,
			},
			checkDTO: func(t *testing.T, dto ApiTokenDTO) {
				if !dto.IsRevoked {
					t.Error("IsRevoked = false, want true")
				}
			},
		},
		{
			name: "token with empty scopes",
			token: &ApiToken{
				ID:          "token-empty",
				ProjectID:   "proj-empty",
				UserID:      "user-empty",
				Name:        "Empty Scopes",
				TokenHash:   "hashempty",
				TokenPrefix: "emt_jkl56789",
				Scopes:      []string{},
				CreatedAt:   now,
				LastUsedAt:  nil,
				RevokedAt:   nil,
			},
			checkDTO: func(t *testing.T, dto ApiTokenDTO) {
				if dto.Scopes == nil {
					t.Error("Scopes = nil, want empty slice")
				}
				if len(dto.Scopes) != 0 {
					t.Errorf("len(Scopes) = %d, want 0", len(dto.Scopes))
				}
			},
		},
		{
			name: "token with nil scopes",
			token: &ApiToken{
				ID:          "token-nil",
				ProjectID:   "proj-nil",
				UserID:      "user-nil",
				Name:        "Nil Scopes",
				TokenHash:   "hashnil",
				TokenPrefix: "emt_mno01234",
				Scopes:      nil,
				CreatedAt:   now,
				LastUsedAt:  nil,
				RevokedAt:   nil,
			},
			checkDTO: func(t *testing.T, dto ApiTokenDTO) {
				// nil scopes should stay nil (not converted to empty)
				if dto.Scopes != nil {
					t.Errorf("Scopes = %v, want nil", dto.Scopes)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dto := tt.token.ToDTO()
			tt.checkDTO(t, dto)
		})
	}
}

func TestValidApiTokenScopes(t *testing.T) {
	// Verify the expected scopes are defined
	expected := []string{"schema:read", "data:read", "data:write", "agents:read", "agents:write"}
	if len(ValidApiTokenScopes) != len(expected) {
		t.Errorf("ValidApiTokenScopes has %d items, want %d", len(ValidApiTokenScopes), len(expected))
	}
	for i, scope := range expected {
		if ValidApiTokenScopes[i] != scope {
			t.Errorf("ValidApiTokenScopes[%d] = %q, want %q", i, ValidApiTokenScopes[i], scope)
		}
	}
}
