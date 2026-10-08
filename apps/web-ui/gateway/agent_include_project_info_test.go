package main

import (
	"net/url"
	"testing"
)

// TestAgentIncludeProjectInfoValue pins the nil-safe reader: nil agent, nil
// config, and a non-bool value all read false; a stored bool is returned.
func TestAgentIncludeProjectInfoValue(t *testing.T) {
	tests := []struct {
		name  string
		agent *AgentDefinition
		want  bool
	}{
		{"nil agent", nil, false},
		{"nil config", &AgentDefinition{ID: "a1"}, false},
		{"explicit true", &AgentDefinition{ID: "a1", Config: map[string]any{"includeProjectInfo": true}}, true},
		{"explicit false", &AgentDefinition{ID: "a1", Config: map[string]any{"includeProjectInfo": false}}, false},
		{"non-bool", &AgentDefinition{ID: "a1", Config: map[string]any{"includeProjectInfo": "true"}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentIncludeProjectInfoValue(tc.agent); got != tc.want {
				t.Errorf("agentIncludeProjectInfoValue() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestApplyAgentGeneralSectionIncludeProjectInfo pins the lossless mapping and
// the boolean-control serialization. A browser serializes the hidden "false"
// then, when checked, the toggle's "true", so the checked body is
// ["false","true"] and the unchecked body is ["false"]. The last value wins
// (repo convention, objects.go), so a checked box must store true — a
// first-wins reader would fail the "checked" case. Omitting the field entirely
// leaves the stored value (and every other config key) untouched.
func TestApplyAgentGeneralSectionIncludeProjectInfo(t *testing.T) {
	tests := []struct {
		name       string
		config     map[string]any
		form       url.Values
		wantValue  bool
		wantStored bool // whether the key is present after apply
	}{
		{
			name:       "checked browser body (false,true) stores flag",
			config:     map[string]any{"keep": "me"},
			form:       url.Values{"name": {"diane"}, "includeProjectInfo": {"false", "true"}},
			wantValue:  true,
			wantStored: true,
		},
		{
			name:       "unchecked browser body (false) removes key",
			config:     map[string]any{"includeProjectInfo": true, "keep": "me"},
			form:       url.Values{"name": {"diane"}, "includeProjectInfo": {"false"}},
			wantValue:  false,
			wantStored: false,
		},
		{
			name:       "single true value stores flag",
			config:     map[string]any{"keep": "me"},
			form:       url.Values{"name": {"diane"}, "includeProjectInfo": {"true"}},
			wantValue:  true,
			wantStored: true,
		},
		{
			name:       "absent preserves existing true",
			config:     map[string]any{"includeProjectInfo": true, "keep": "me"},
			form:       url.Values{"name": {"diane"}},
			wantValue:  true,
			wantStored: true,
		},
		{
			name:       "absent preserves absent",
			config:     map[string]any{"keep": "me"},
			form:       url.Values{"name": {"diane"}},
			wantValue:  false,
			wantStored: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def := &AgentDefinition{ID: "a1", Name: "diane", Config: tc.config}
			if err := applyAgentGeneralSection(def, generalSectionContext(t, tc.form)); err != nil {
				t.Fatalf("applyAgentGeneralSection: %v", err)
			}
			if got := agentIncludeProjectInfoValue(def); got != tc.wantValue {
				t.Errorf("includeProjectInfo = %v, want %v", got, tc.wantValue)
			}
			if _, ok := def.Config["includeProjectInfo"]; ok != tc.wantStored {
				t.Errorf("includeProjectInfo stored = %v, want %v", ok, tc.wantStored)
			}
			if got := def.Config["keep"]; got != "me" {
				t.Errorf("unrelated config key = %v, want untouched %q", got, "me")
			}
		})
	}
}
