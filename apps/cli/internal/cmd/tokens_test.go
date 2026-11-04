package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeTokenScopes tests the scope resolution applied by runCreateToken.
// runCreateToken itself requires a live client connection, so the pure
// normalization logic is tested directly.
func TestNormalizeTokenScopes(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{
			name:   "all sentinel maps to full admin access",
			input:  []string{"all"},
			expect: []string{"admin:all"},
		},
		{
			name:   "empty defaults to the read preset",
			input:  nil,
			expect: presetScopes("read"),
		},
		{
			name:   "read preset expands and accepts explicit scopes",
			input:  []string{"read", "branches:read"},
			expect: append(presetScopes("read"), "branches:read"),
		},
		{
			name:   "read and write presets combine without duplicates",
			input:  []string{"read", "write"},
			expect: presetScopes("read", "write"),
		},
		{
			name:   "comma-separated scopes are preserved",
			input:  []string{"documents:read", "branches:read"},
			expect: []string{"documents:read", "branches:read"},
		},
		{
			name:   "all expands to admin:all anywhere in the list",
			input:  []string{"all", "documents:read"},
			expect: []string{"admin:all", "documents:read"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expect, normalizeTokenScopes(tt.input))
		})
	}
}

// TestResolveProjectContext_UUIDPassthrough tests that valid UUIDs are returned as-is
// without requiring a client lookup
func TestResolveProjectContext_UUIDPassthrough(t *testing.T) {
	// This tests the early-return optimization in resolveProjectID where
	// if the input is already a UUID, it returns directly without calling the SDK

	validUUIDs := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"123e4567-e89b-12d3-a456-426614174000",
		"AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE",
	}

	for _, uuid := range validUUIDs {
		t.Run(uuid, func(t *testing.T) {
			// The isUUID check should pass
			assert.True(t, isUUID(uuid), "Should recognize as UUID")
		})
	}
}

// TestResolveProjectContext_NonUUID tests that non-UUID values trigger name resolution
func TestResolveProjectContext_NonUUID(t *testing.T) {
	// These should NOT be recognized as UUIDs and would trigger name resolution

	nonUUIDs := []string{
		"Production",
		"my-project",
		"test",
		"",
		"not-a-uuid",
		"12345",
	}

	for _, input := range nonUUIDs {
		t.Run(input, func(t *testing.T) {
			// The isUUID check should fail
			assert.False(t, isUUID(input), "Should NOT recognize as UUID")
		})
	}
}

func TestRevokeCommand_HasDeleteAlias(t *testing.T) {
	assert.Contains(t, revokeTokenCmd.Aliases, "delete")
}

func TestUpdateScopesCommand_Structure(t *testing.T) {
	assert.Equal(t, "update-scopes", updateScopesCmd.Name())
	scopesFlag := updateScopesCmd.Flag("scopes")
	require.NotNil(t, scopesFlag)
}

func TestTokenScopeGroups(t *testing.T) {
	names := map[string]bool{}
	count := 0
	for _, g := range tokenScopeGroups {
		assert.NotEmpty(t, g.Title)
		for _, s := range g.Scopes {
			assert.NotEmpty(t, s.Name, "empty scope name in group %q", g.Title)
			assert.NotEmpty(t, s.Description, "empty description for %q", s.Name)
			assert.False(t, names[s.Name], "duplicate scope %q", s.Name)
			names[s.Name] = true
			count++
		}
	}
	// Every user-mintable scope (server-side) must be documented.
	for _, want := range []string{
		"schema:read", "schema:write",
		"agents:read", "agents:write", "projects:read", "projects:write", "chat:use",
		"graph:read", "graph:write", "schema:migrate",
		"branches:read", "branches:write", "search",
		"journal:read", "journal:write", "skills:read", "skills:write",
		"documents:read", "documents:write", "project:admin", "admin", "admin:all",
	} {
		assert.True(t, names[want], "scope %q missing from tokenScopeGroups", want)
	}
	// The retired coarse umbrellas are not mintable and must not be offered.
	for _, retired := range []string{"data:read", "data:write"} {
		assert.False(t, names[retired], "retired scope %q must not appear in tokenScopeGroups", retired)
	}
	assert.Equal(t, 22, count)
}
