package workspace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsSHA(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"full SHA", "abc123def456789012345678901234567890abcd", true},
		{"short SHA (7 chars)", "abc123d", true},
		{"short SHA (8 chars)", "abc123de", true},
		{"branch name", "main", false},
		{"feature branch", "feature/auth", false},
		{"too short (6 chars)", "abc123", false},
		{"mixed case SHA", "aBcDeFg", false}, // git SHAs are lowercase hex
		{"not hex", "zzzzzzzz", false},
		{"empty", "", false},
		{"tag name", "v1.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isSHA(tt.input))
		})
	}
}

func TestCloneConstants(t *testing.T) {
	assert.Equal(t, 3, maxCloneRetries)
	assert.Equal(t, 2*time.Second, initialRetryDelay)
	assert.Equal(t, 300000, cloneTimeoutMs)
	assert.Equal(t, 120000, pushPullTimeoutMs)
}

func TestCheckoutService_BuildCloneURL_NoProvider(t *testing.T) {
	cs := &CheckoutService{credProvider: nil}

	url, err := cs.buildCloneURL(nil, "https://github.com/org/repo")
	assert.NoError(t, err)
	assert.Equal(t, "https://github.com/org/repo", url)
}

func TestCheckoutService_CloneRepository_EmptyURL(t *testing.T) {
	cs := &CheckoutService{}
	err := cs.CloneRepository(nil, nil, "", "", "")
	assert.NoError(t, err) // No-op for empty URL
}
