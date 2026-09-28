package apitoken

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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

func strPtr(s string) *string { return &s }

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
				ProjectID:   strPtr("proj-456"),
				UserID:      strPtr("user-789"),
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
			name: "account token (nil project)",
			token: &ApiToken{
				ID:          "token-acct",
				ProjectID:   nil,
				UserID:      strPtr("user-789"),
				Name:        "Account Token",
				TokenHash:   "hashacct",
				TokenPrefix: "emt_acct1234",
				Scopes:      []string{"projects:read"},
				CreatedAt:   now,
				LastUsedAt:  nil,
				RevokedAt:   nil,
			},
			checkDTO: func(t *testing.T, dto ApiTokenDTO) {
				if dto.ProjectID != nil {
					t.Errorf("ProjectID = %v, want nil for account token", dto.ProjectID)
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
				ProjectID:   strPtr("proj-789"),
				UserID:      strPtr("user-012"),
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
				ProjectID:   strPtr("proj-012"),
				UserID:      strPtr("user-345"),
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
				ProjectID:   strPtr("proj-empty"),
				UserID:      strPtr("user-empty"),
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
				ProjectID:   strPtr("proj-nil"),
				UserID:      strPtr("user-nil"),
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
	expected := []string{
		"schema:read", "schema:write", "data:read", "data:write",
		"agents:read", "agents:write", "projects:read", "projects:write", "chat:use",
		"graph:read", "graph:write", "schema:migrate",
		"branches:read", "branches:write", "search",
		"journal:read", "journal:write",
		"skills:read", "skills:write",
		"documents:read", "documents:write",
		"admin",
		"admin:all",
		"project:admin",
		"mcp:agent-call",
		"share:agent-chat",
		"device:api",
		"webhook:trigger",
	}
	if len(ValidApiTokenScopes) != len(expected) {
		t.Errorf("ValidApiTokenScopes has %d items, want %d", len(ValidApiTokenScopes), len(expected))
	}
	for i, scope := range expected {
		if ValidApiTokenScopes[i] != scope {
			t.Errorf("ValidApiTokenScopes[%d] = %q, want %q", i, ValidApiTokenScopes[i], scope)
		}
	}
}

// The agent-share marker must be reserved to the internal share mint path: all
// user-facing token create/update entry points reject it. Each rejection runs
// before any repository access, so this stays a non-DB test.
func TestUserFacingTokenPathsRejectReservedAgentCallScope(t *testing.T) {
	svc := &Service{}
	scopes := []string{agentCallScope}

	cases := map[string]func() error{
		"Create": func() error {
			_, err := svc.Create(context.Background(), "", "", "test", scopes)
			return err
		},
		"CreateAccountToken": func() error {
			_, err := svc.CreateAccountToken(context.Background(), "", "test", scopes)
			return err
		},
		"UpdateScopes": func() error {
			_, err := svc.UpdateScopes(context.Background(), "tok", "", "", scopes)
			return err
		},
		"UpdateAccountTokenScopes": func() error {
			_, err := svc.UpdateAccountTokenScopes(context.Background(), "tok", "", scopes)
			return err
		},
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected rejection of the reserved agent-call scope")
			}
			if !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("error = %q, want it to mention reserved", err.Error())
			}
		})
	}
}

// The share:agent-chat marker must be reserved to the internal share mint path:
// all user-facing token create/update entry points reject it. Non-DB.
func TestUserFacingTokenPathsRejectReservedShareChatScope(t *testing.T) {
	svc := &Service{}
	scopes := []string{shareAgentChatScope}

	cases := map[string]func() error{
		"Create": func() error {
			_, err := svc.Create(context.Background(), "", "", "test", scopes)
			return err
		},
		"CreateAccountToken": func() error {
			_, err := svc.CreateAccountToken(context.Background(), "", "test", scopes)
			return err
		},
		"UpdateScopes": func() error {
			_, err := svc.UpdateScopes(context.Background(), "tok", "", "", scopes)
			return err
		},
		"UpdateAccountTokenScopes": func() error {
			_, err := svc.UpdateAccountTokenScopes(context.Background(), "tok", "", scopes)
			return err
		},
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected rejection of the reserved share:agent-chat scope")
			}
			if !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("error = %q, want it to mention reserved", err.Error())
			}
		})
	}
}

// The device:api marker must be reserved to the internal device mint path: all
// user-facing token create/update entry points reject it. Non-DB.
func TestUserFacingTokenPathsRejectReservedDeviceAPIScope(t *testing.T) {
	svc := &Service{}
	scopes := []string{deviceAPIScope}

	cases := map[string]func() error{
		"Create": func() error {
			_, err := svc.Create(context.Background(), "", "", "test", scopes)
			return err
		},
		"CreateAccountToken": func() error {
			_, err := svc.CreateAccountToken(context.Background(), "", "test", scopes)
			return err
		},
		"UpdateScopes": func() error {
			_, err := svc.UpdateScopes(context.Background(), "tok", "", "", scopes)
			return err
		},
		"UpdateAccountTokenScopes": func() error {
			_, err := svc.UpdateAccountTokenScopes(context.Background(), "tok", "", scopes)
			return err
		},
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected rejection of the reserved device:api scope")
			}
			if !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("error = %q, want it to mention reserved", err.Error())
			}
		})
	}
}

// The device ceiling is a hardcoded, three-scope set: the reserved marker plus
// the read-only agents/data umbrella scopes. It must never be widened.
func TestDeviceAPIScopesIsTheExactCeiling(t *testing.T) {
	want := []string{deviceAPIScope, "agents:read", "data:read"}
	if len(deviceAPIScopes) != len(want) {
		t.Fatalf("deviceAPIScopes has %d items, want %d", len(deviceAPIScopes), len(want))
	}
	for _, w := range want {
		found := false
		for _, got := range deviceAPIScopes {
			if got == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("deviceAPIScopes is missing %q", w)
		}
	}
}

// The webhook trigger ceiling is a hardcoded four-scope set: the reserved
// marker plus agents:read/agents:write (the trigger route's agents:write gate)
// and the data:read read family. It must never be widened.
func TestWebhookTriggerScopesIsTheExactCeiling(t *testing.T) {
	want := []string{webhookTriggerScope, "agents:read", "agents:write", "data:read"}
	if len(webhookTriggerScopes) != len(want) {
		t.Fatalf("webhookTriggerScopes has %d items, want %d", len(webhookTriggerScopes), len(want))
	}
	for _, w := range want {
		found := false
		for _, got := range webhookTriggerScopes {
			if got == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("webhookTriggerScopes is missing %q", w)
		}
	}
}

// The webhook:trigger marker must be reserved to the internal webhook mint
// path: all user-facing token create/update entry points reject it. Non-DB.
func TestUserFacingTokenPathsRejectReservedWebhookTriggerScope(t *testing.T) {
	svc := &Service{}
	scopes := []string{webhookTriggerScope}

	cases := map[string]func() error{
		"Create": func() error {
			_, err := svc.Create(context.Background(), "", "", "test", scopes)
			return err
		},
		"CreateAccountToken": func() error {
			_, err := svc.CreateAccountToken(context.Background(), "", "test", scopes)
			return err
		},
		"UpdateScopes": func() error {
			_, err := svc.UpdateScopes(context.Background(), "tok", "", "", scopes)
			return err
		},
		"UpdateAccountTokenScopes": func() error {
			_, err := svc.UpdateAccountTokenScopes(context.Background(), "tok", "", scopes)
			return err
		},
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected rejection of the reserved webhook:trigger scope")
			}
			if !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("error = %q, want it to mention reserved", err.Error())
			}
		})
	}
}

// The ephemeral sandbox ceiling must never carry a platform scope: admin and
// admin:all require superadmin_full (see checkPlatformScopeGrant), but the
// ephemeral mint runs on behalf of ordinary project members. Fail-first: if a
// platform scope is (re)added to ephemeralScopes, this test goes red.
func TestEphemeralScopesNeverCarryPlatformScope(t *testing.T) {
	if len(ephemeralScopes) == 0 {
		t.Fatal("ephemeralScopes must be non-empty")
	}
	for _, sc := range ephemeralScopes {
		if platformScopes[sc] {
			t.Fatalf("ephemeralScopes carries platform scope %q; sandbox tokens must never hold admin authority", sc)
		}
	}
}

func TestScopesContainPlatformScope(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		want   bool
	}{
		{"admin all alone", []string{"admin:all"}, true},
		{"admin all in mixed list", []string{"data:read", "admin:all", "graph:write"}, true},
		{"bare admin alone", []string{"admin"}, true},
		{"bare admin in mixed list", []string{"admin", "data:read"}, true},
		{"unrelated scopes", []string{"data:read", "graph:write"}, false},
		{"empty list", []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scopesContainPlatformScope(tt.scopes); got != tt.want {
				t.Errorf("scopesContainPlatformScope(%v) = %v, want %v", tt.scopes, got, tt.want)
			}
		})
	}
}

func TestScopesContainProjectAdmin(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		want   bool
	}{
		{"project admin alone", []string{"project:admin"}, true},
		{"project admin in mixed list", []string{"data:read", "project:admin", "graph:write"}, true},
		{"unrelated scopes", []string{"data:read", "graph:write"}, false},
		{"platform admin not project admin", []string{"admin", "admin:all"}, false},
		{"empty list", []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scopesContainProjectAdmin(tt.scopes); got != tt.want {
				t.Errorf("scopesContainProjectAdmin(%v) = %v, want %v", tt.scopes, got, tt.want)
			}
		})
	}
}

// project:admin is a project-scoped umbrella: a project_admin (or the owning
// org's org_admin) may mint it on a project-bound token, but a non-admin may
// not, and an account-level (unbound) token may never carry it. admin and
// admin:all remain platform scopes gated by superadmin_full — project authority
// must not buy platform authority.
func TestService_ProjectAdminScopeMinting(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())
	svc := NewService(db, repo, nil, slog.Default())
	ctx := context.Background()

	t.Run("project_admin mints project:admin on a project token", func(t *testing.T) {
		userID := uuid.NewString()
		projectID := seedProjectWithMembership(t, db, userID, "project_admin")

		dto, err := svc.Create(ctx, projectID, userID, "pa-token", []string{"project:admin"})
		require.NoError(t, err, "project_admin must mint project:admin")
		require.NotNil(t, dto)
		require.Contains(t, dto.Scopes, "project:admin")
	})

	t.Run("owning org_admin mints project:admin", func(t *testing.T) {
		userID := uuid.NewString()
		projectID := seedOwningOrgAdmin(t, db, userID)

		dto, err := svc.Create(ctx, projectID, userID, "org-token", []string{"project:admin"})
		require.NoError(t, err, "owning org_admin must mint project:admin")
		require.NotNil(t, dto)
	})

	t.Run("non-admin cannot mint project:admin", func(t *testing.T) {
		userID := uuid.NewString()
		projectID := seedProjectWithMembership(t, db, userID, "project_user")

		_, err := svc.Create(ctx, projectID, userID, "na-token", []string{"project:admin"})
		require.Error(t, err, "project_user must not mint project:admin")
	})

	t.Run("account-level token cannot carry project:admin", func(t *testing.T) {
		userID := uuid.NewString()
		seedUser(t, db, userID)

		_, err := svc.CreateAccountToken(ctx, userID, "acct-token", []string{"project:admin"})
		require.Error(t, err, "account-level token must not mint project:admin")
	})

	t.Run("update account token scopes cannot carry project:admin", func(t *testing.T) {
		userID := uuid.NewString()
		seedUser(t, db, userID)

		_, err := svc.UpdateAccountTokenScopes(ctx, uuid.NewString(), userID, []string{"project:admin"})
		require.Error(t, err, "account-level update must not mint project:admin")
	})

	t.Run("project_admin still cannot mint platform admin", func(t *testing.T) {
		userID := uuid.NewString()
		projectID := seedProjectWithMembership(t, db, userID, "project_admin")

		_, err := svc.Create(ctx, projectID, userID, "pa-admin", []string{"admin"})
		require.Error(t, err, "project_admin must not mint bare admin")

		_, err = svc.Create(ctx, projectID, userID, "pa-admin-all", []string{"admin:all"})
		require.Error(t, err, "project_admin must not mint admin:all")
	})
}
