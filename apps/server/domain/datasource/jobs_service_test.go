package datasource

import (
	"strings"
	"testing"
)

func TestTruncateError(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedLen int
	}{
		{
			name:        "empty string",
			input:       "",
			expectedLen: 0,
		},
		{
			name:        "short message",
			input:       "short error",
			expectedLen: 11,
		},
		{
			name:        "exactly 1000 chars",
			input:       strings.Repeat("a", 1000),
			expectedLen: 1000,
		},
		{
			name:        "1001 chars truncated to 1000",
			input:       strings.Repeat("a", 1001),
			expectedLen: 1000,
		},
		{
			name:        "long message truncated",
			input:       strings.Repeat("b", 2000),
			expectedLen: 1000,
		},
		{
			name:        "999 chars unchanged",
			input:       strings.Repeat("c", 999),
			expectedLen: 999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateError(tt.input)
			if len(result) != tt.expectedLen {
				t.Errorf("truncateError() len = %d, want %d", len(result), tt.expectedLen)
			}
			// Verify content is preserved when input <= 1000 chars (no truncation)
			if len(tt.input) <= 1000 && result != tt.input {
				t.Errorf("truncateError() = %q, want %q", result, tt.input)
			}
			// Verify truncation takes first 1000 chars when input > 1000
			if len(tt.input) > 1000 && result != tt.input[:1000] {
				t.Errorf("truncateError() should truncate to first 1000 chars")
			}
		})
	}
}
