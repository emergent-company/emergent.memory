package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsUUID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid UUID v4",
			input:    "550e8400-e29b-41d4-a716-446655440000",
			expected: true,
		},
		{
			name:     "valid UUID lowercase",
			input:    "123e4567-e89b-12d3-a456-426614174000",
			expected: true,
		},
		{
			name:     "valid UUID uppercase",
			input:    "123E4567-E89B-12D3-A456-426614174000",
			expected: true,
		},
		{
			name:     "valid UUID mixed case",
			input:    "123e4567-E89B-12d3-A456-426614174000",
			expected: true,
		},
		{
			name:     "invalid - too short",
			input:    "550e8400-e29b-41d4-a716",
			expected: false,
		},
		{
			name:     "invalid - too long",
			input:    "550e8400-e29b-41d4-a716-446655440000-extra",
			expected: false,
		},
		{
			name:     "invalid - missing hyphens",
			input:    "550e8400e29b41d4a716446655440000",
			expected: false,
		},
		{
			name:     "invalid - wrong hyphen positions",
			input:    "550e8400-e29b41d4-a716-446655440000",
			expected: false,
		},
		{
			name:     "invalid - hyphen at position 0",
			input:    "-50e8400-e29b-41d4-a716-446655440000",
			expected: false,
		},
		{
			name:     "invalid - contains non-hex chars (g)",
			input:    "550e8400-e29b-41d4-a716-44665544000g",
			expected: false,
		},
		{
			name:     "invalid - contains non-hex chars (z)",
			input:    "550e8400-e29b-41d4-a716-4466554400z0",
			expected: false,
		},
		{
			name:     "project name",
			input:    "My Project",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "single word",
			input:    "Production",
			expected: false,
		},
		{
			name:     "number string",
			input:    "12345",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isUUID(tt.input)
			assert.Equal(t, tt.expected, result, "isUUID(%q) should be %v", tt.input, tt.expected)
		})
	}
}
