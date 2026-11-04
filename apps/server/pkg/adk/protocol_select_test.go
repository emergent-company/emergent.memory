package adk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSelectOpenAIProtocol is the #1455 selection contract: only the
// first-party api.openai.com host selects Responses; a relay/proxy keeps Chat
// Completions.
func TestSelectOpenAIProtocol(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"first-party https", "https://api.openai.com/v1", "openai-responses"},
		{"first-party bare v1", "https://api.openai.com/v1/", "openai-responses"},
		{"first-party scheme-less", "api.openai.com/v1", "openai-responses"},
		{"first-party upper-case host", "https://API.OPENAI.COM/v1", "openai-responses"},
		{"relay host", "https://my-relay.example.com/v1", "openai-chat"},
		{"openrouter relay", "https://openrouter.ai/api/v1", "openai-chat"},
		{"localhost", "http://localhost:11434/v1", "openai-chat"},
		{"subdomain not first-party", "https://api.openai.com.evil.example/v1", "openai-chat"},
		{"empty", "", "openai-chat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SelectOpenAIProtocol(tc.baseURL))
		})
	}
}

// TestHostMatchesAny covers the host-matching primitive, including the
// lookalike-domain case that a naive strings.Contains check would get wrong.
func TestHostMatchesAny(t *testing.T) {
	hosts := []string{"api.openai.com"}
	assert.True(t, HostMatchesAny("https://api.openai.com/v1", hosts))
	assert.True(t, HostMatchesAny("api.openai.com", hosts))
	assert.True(t, HostMatchesAny("https://API.OPENAI.COM/v1", hosts))
	assert.False(t, HostMatchesAny("https://api.openai.com.evil.example/v1", hosts))
	assert.False(t, HostMatchesAny("https://relay.example.com/v1", hosts))
	assert.False(t, HostMatchesAny("", hosts))
	assert.False(t, HostMatchesAny("https://api.openai.com/v1", nil))
}
