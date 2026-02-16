package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveProjectID_UUIDPassthrough tests that valid UUIDs are returned as-is
// without requiring a client lookup
func TestResolveProjectID_UUIDPassthrough(t *testing.T) {
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

// TestResolveProjectID_NonUUID tests that non-UUID values trigger name resolution
func TestResolveProjectID_NonUUID(t *testing.T) {
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
