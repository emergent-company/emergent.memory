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

// TestHealAgentModelRef guards the runtime self-heal used by
// EnsureGraphQueryAgent. A valid explicit model must survive untouched (or be
// canonicalized), while an unparsable legacy bare name must be cleared so the
// executor falls back to the project default instead of failing every query.
func TestHealAgentModelRef(t *testing.T) {
	temperature := float32(0.2)
	cases := []struct {
		name     string
		in       *ModelConfig
		wantNil  bool
		wantName string
	}{
		{name: "nil is a no-op", in: nil, wantNil: true},
		{name: "canonical prefixed name survives", in: &ModelConfig{Name: "google/gemini-3.1-flash-lite-preview"},
			wantName: "google/gemini-3.1-flash-lite-preview"},
		{name: "structured provider is folded", in: &ModelConfig{Name: "gpt-4o", Provider: "openai"},
			wantName: "openai/gpt-4o"},
		{name: "bare name without provider is cleared", in: &ModelConfig{Name: "gemini-3.1-flash-lite-preview"},
			wantName: ""},
		{name: "malformed empty provider is cleared", in: &ModelConfig{Name: "/gpt-4o"},
			wantName: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			temperatureCopy := temperature
			if tc.in != nil {
				tc.in.Temperature = &temperatureCopy
			}
			healAgentModelRef(tc.in)
			if tc.wantNil {
				require.Nil(t, tc.in)
				return
			}
			require.Equal(t, tc.wantName, tc.in.Name)
			require.Empty(t, tc.in.Provider)
			require.NotNil(t, tc.in.Temperature, "non-reference fields must be preserved")
			require.Equal(t, temperature, *tc.in.Temperature)
		})
	}
}

// TestFoldAgentModelRef guards the read-side fold (#1465) for legacy rows in
// kb.agent_definitions.model that still hold the structured shape
// {"name":"<bare>","provider":"<slug>"} (never backfilled). Each case asserts the
// returned copy is normalized without mutating the input.
func TestFoldAgentModelRef(t *testing.T) {
	cases := []struct {
		name     string
		in       *ModelConfig
		wantName string
		wantProv string
		wantNil  bool
	}{
		{name: "nil returns nil", in: nil, wantNil: true},
		{name: "bare name plus provider folds into prefixed name",
			in:       &ModelConfig{Name: "deepseek-v4-flash", Provider: "openai"},
			wantName: "openai/deepseek-v4-flash"},
		{name: "prefixed name plus redundant provider clears provider",
			in:       &ModelConfig{Name: "openai/deepseek-v4-flash", Provider: "openai"},
			wantName: "openai/deepseek-v4-flash"},
		{name: "unparsable bare name without provider left untouched",
			in: &ModelConfig{Name: "gpt-4o"}, wantName: "gpt-4o"},
		{name: "conflicting providers left untouched",
			in:       &ModelConfig{Name: "openai/gpt-4o", Provider: "google"},
			wantName: "openai/gpt-4o", wantProv: "google"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Snapshot the input before folding to prove it is never mutated.
			var inName, inProv string
			if tc.in != nil {
				inName, inProv = tc.in.Name, tc.in.Provider
			}

			got := FoldAgentModelRef(tc.in)
			if tc.wantNil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.wantName, got.Name)
			require.Equal(t, tc.wantProv, got.Provider, "folded provider must be cleared")
			require.Equal(t, inName, tc.in.Name, "input Name must not be mutated")
			require.Equal(t, inProv, tc.in.Provider, "input Provider must not be mutated")
		})
	}
}

// TestFoldAgentModelRef_PreservesOtherKeys proves the folded copy keeps the
// non-model-reference fields (temperature, native tools) and only rewrites Name
// and Provider.
func TestFoldAgentModelRef_PreservesOtherKeys(t *testing.T) {
	temperature := float32(0.7)
	in := &ModelConfig{
		Name:        "deepseek-v4-flash",
		Provider:    "openai",
		Temperature: &temperature,
		NativeTools: []string{"google_search"},
	}

	got := FoldAgentModelRef(in)
	require.NotNil(t, got)
	require.Equal(t, "openai/deepseek-v4-flash", got.Name)
	require.Empty(t, got.Provider)
	require.NotNil(t, got.Temperature)
	require.Equal(t, temperature, *got.Temperature)
	require.Equal(t, []string{"google_search"}, got.NativeTools)
}
