package agents

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalizeAgentModelConfig is the focused guard for the single model-ref
// funnel used by every agent write path. Each accepted/rejected shape is a
// mutation check: dropping the structured-provider handling, the double-slash
// rejection, or the conflict check flips one of these cases.
func TestNormalizeAgentModelConfig(t *testing.T) {
	cases := []struct {
		name        string
		in          *ModelConfig
		wantName    string
		wantProv    string
		wantErrPart string
	}{
		{name: "nil config", in: nil, wantName: ""},
		{name: "no override", in: &ModelConfig{}, wantName: ""},
		{name: "canonical prefix", in: &ModelConfig{Name: "openai/gpt-4o"}, wantName: "openai/gpt-4o"},
		{name: "trims and folds whitespace", in: &ModelConfig{Name: " openai / gpt-4o "}, wantName: "openai/gpt-4o"},
		{name: "structured provider", in: &ModelConfig{Name: "gpt-4o", Provider: "openai"}, wantName: "openai/gpt-4o"},
		{name: "structured redundant provider", in: &ModelConfig{Name: "openai/gpt-4o", Provider: "openai"}, wantName: "openai/gpt-4o"},
		{name: "bare name no provider rejected", in: &ModelConfig{Name: "gpt-4o"}, wantErrPart: "provider/model"},
		{name: "provider without model rejected", in: &ModelConfig{Provider: "openai"}, wantErrPart: "provider"},
		{name: "conflicting providers rejected", in: &ModelConfig{Name: "openai/gpt-4o", Provider: "google"}, wantErrPart: "conflicts"},
		{name: "empty provider segment rejected", in: &ModelConfig{Name: "/gpt-4o"}, wantErrPart: "provider/model"},
		// #1466: double separator must be rejected, not treated as a bare name
		// even when a structured provider is present.
		{name: "double separator rejected", in: &ModelConfig{Name: "openai//gpt-4o"}, wantErrPart: "provider/model"},
		{name: "double separator with provider rejected", in: &ModelConfig{Name: "openai//gpt-4o", Provider: "openai"}, wantErrPart: "provider/model"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NormalizeAgentModelConfig(tc.in)
			if tc.wantErrPart != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErrPart)
				return
			}
			require.NoError(t, err)
			if tc.in != nil {
				require.Equal(t, tc.wantName, tc.in.Name)
				require.Equal(t, tc.wantProv, tc.in.Provider, "structured provider must be folded into name and cleared")
			}
		})
	}
}

// TestNormalizeAgentModelConfig_ErrorMessageMentionsProviderModel pins the
// message contract the REST/MCP handlers surface to clients.
func TestNormalizeAgentModelConfig_ErrorMessageMentionsProviderModel(t *testing.T) {
	err := NormalizeAgentModelConfig(&ModelConfig{Name: "gpt-4o"})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "provider/model"), "got %q", err.Error())
}
