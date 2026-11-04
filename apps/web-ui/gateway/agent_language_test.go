package main

import "testing"

// TestAgentLanguageValue covers the Config["language"] reader used to fill the
// settings form's Language field.
func TestAgentLanguageValue(t *testing.T) {
	cases := []struct {
		name string
		a    *AgentDefinition
		want string
	}{
		{name: "nil agent", a: nil, want: ""},
		{name: "nil config", a: &AgentDefinition{ID: "a1", Name: "diane"}, want: ""},
		{name: "string value", a: &AgentDefinition{Config: map[string]any{"language": "Spanish"}}, want: "Spanish"},
		{name: "whitespace trimmed", a: &AgentDefinition{Config: map[string]any{"language": "  Japanese "}}, want: "Japanese"},
		{name: "non-string value", a: &AgentDefinition{Config: map[string]any{"language": 42}}, want: ""},
		{name: "missing key", a: &AgentDefinition{Config: map[string]any{"spawnPolicy": map[string]any{}}}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentLanguageValue(tc.a); got != tc.want {
				t.Errorf("agentLanguageValue() = %q, want %q", got, tc.want)
			}
		})
	}
}
