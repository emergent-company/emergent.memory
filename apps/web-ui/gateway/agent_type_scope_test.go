package main

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// TestAgentTypeScopeReaders pins the nil-safe readers for both graph type
// allowlists: nil agent, nil config, an absent key, and a non-list value all
// read nil; a stored []string is returned; a JSON-decoded []any is flattened.
func TestAgentTypeScopeReaders(t *testing.T) {
	tests := []struct {
		name string
		key  string
		data any
		want []string
	}{
		{"nil config", "objectTypes", nil, nil},
		{"absent key", "objectTypes", nil, nil},
		{"[]string", "objectTypes", []string{"person", "org"}, []string{"person", "org"}},
		{"[]any", "objectTypes", []any{"person", "org"}, []string{"person", "org"}},
		{"[]any drops non-strings", "objectTypes", []any{"person", 7, "org"}, []string{"person", "org"}},
		{"non-list", "objectTypes", "person", nil},
		{"relationship []string", "relationshipTypes", []string{"works_for"}, []string{"works_for"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var a *AgentDefinition
			if tc.name != "nil config" {
				cfg := map[string]any{}
				if tc.name != "absent key" {
					cfg[tc.key] = tc.data
				}
				a = &AgentDefinition{ID: "a1", Config: cfg}
			}
			got := agentObjectTypesValue(a)
			if tc.key == "relationshipTypes" {
				got = agentRelationshipTypesValue(a)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s reader = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
	if got := agentObjectTypesValue(nil); got != nil {
		t.Errorf("nil agent = %v, want nil", got)
	}
	if got := agentRelationshipTypesValue(nil); got != nil {
		t.Errorf("nil agent = %v, want nil", got)
	}
}

// TestApplyAgentGraphSectionTypeScope pins the multi-select mapping for the
// graph type scope (the Graph subpage applier): every selected value is stored
// in submission order after trimming, blanks are dropped, an empty selection
// (present but blank — the hidden companion body) removes the key, and a request
// that omits the field leaves the stored value (and every unrelated config key)
// untouched.
func TestApplyAgentGraphSectionTypeScope(t *testing.T) {
	tests := []struct {
		name       string
		config     map[string]any
		form       url.Values
		wantObj    []string
		wantObjSet bool
		wantRel    []string
		wantRelSet bool
	}{
		{
			name:       "selected values persist in order",
			config:     map[string]any{"keep": "me"},
			form:       url.Values{"name": {"diane"}, "objectType": {"person", "org"}, "relationshipType": {"works_for"}},
			wantObj:    []string{"person", "org"},
			wantObjSet: true,
			wantRel:    []string{"works_for"},
			wantRelSet: true,
		},
		{
			name:       "hidden companion blank is dropped, selection kept",
			config:     map[string]any{"keep": "me"},
			form:       url.Values{"name": {"diane"}, "objectType": {"", "  person  "}, "relationshipType": {""}},
			wantObj:    []string{"person"},
			wantObjSet: true,
			wantRel:    nil,
			wantRelSet: false,
		},
		{
			name:       "present-but-empty removes key",
			config:     map[string]any{"objectTypes": []string{"person"}, "relationshipTypes": []string{"works_for"}, "keep": "me"},
			form:       url.Values{"name": {"diane"}, "objectType": {""}, "relationshipType": {""}},
			wantObj:    nil,
			wantObjSet: false,
			wantRel:    nil,
			wantRelSet: false,
		},
		{
			name:       "field absent preserves stored value",
			config:     map[string]any{"objectTypes": []string{"person"}, "relationshipTypes": []string{"works_for"}, "keep": "me"},
			form:       url.Values{"name": {"diane"}},
			wantObj:    []string{"person"},
			wantObjSet: true,
			wantRel:    []string{"works_for"},
			wantRelSet: true,
		},
		{
			name:       "absent preserves absent",
			config:     map[string]any{"keep": "me"},
			form:       url.Values{"name": {"diane"}},
			wantObj:    nil,
			wantObjSet: false,
			wantRel:    nil,
			wantRelSet: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def := &AgentDefinition{ID: "a1", Name: "diane", Config: tc.config}
			if err := applyAgentGraphSection(def, generalSectionContext(t, tc.form)); err != nil {
				t.Fatalf("applyAgentGraphSection: %v", err)
			}
			if got := agentObjectTypesValue(def); !reflect.DeepEqual(got, tc.wantObj) {
				t.Errorf("objectTypes = %v, want %v", got, tc.wantObj)
			}
			if _, ok := def.Config["objectTypes"]; ok != tc.wantObjSet {
				t.Errorf("objectTypes stored = %v, want %v", ok, tc.wantObjSet)
			}
			if got := agentRelationshipTypesValue(def); !reflect.DeepEqual(got, tc.wantRel) {
				t.Errorf("relationshipTypes = %v, want %v", got, tc.wantRel)
			}
			if _, ok := def.Config["relationshipTypes"]; ok != tc.wantRelSet {
				t.Errorf("relationshipTypes stored = %v, want %v", ok, tc.wantRelSet)
			}
			if got := def.Config["keep"]; got != "me" {
				t.Errorf("unrelated config key = %v, want untouched %q", got, "me")
			}
		})
	}
}

// TestAgentGraphSettingsFormTypeScope pins the rendered Graph form contract:
// both scope pickers expose their multi-value field names, offer the compiled
// types, pre-check the stored allowlist, and fall back to an empty note (no
// dangling picker) when the project has no compiled types.
func TestAgentGraphSettingsFormTypeScope(t *testing.T) {
	agent := &AgentDefinition{
		ID:   "a1",
		Name: "diane",
		Config: map[string]any{
			"objectTypes":       []string{"person"},
			"relationshipTypes": []any{"works_for"},
		},
	}
	html := renderHTML(t, agentGraphSettingsForm(agentSettingsData{
		Agent:             agent,
		ObjectTypes:       []CompiledType{{Name: "person", Label: "Person"}, {Name: "org", Label: "Organisation"}},
		RelationshipTypes: []CompiledType{{Name: "works_for", Label: "Works for"}},
	}))

	if !strings.Contains(html, `name="objectType"`) || !strings.Contains(html, `name="relationshipType"`) {
		t.Error("Graph form must expose the objectType / relationshipType fields")
	}
	if !strings.Contains(html, `name="objectType" type="checkbox" value="person" checked`) {
		t.Error("object type present in the allowlist must render pre-checked")
	}
	if strings.Contains(html, `name="objectType" type="checkbox" value="org" checked`) {
		t.Error("object type absent from the allowlist must not render checked")
	}
	if !strings.Contains(html, `name="relationshipType" type="checkbox" value="works_for" checked`) {
		t.Error("relationship type present in the allowlist must render pre-checked")
	}

	empty := renderHTML(t, agentGraphSettingsForm(agentSettingsData{Agent: &AgentDefinition{ID: "a1", Name: "diane"}}))
	if !strings.Contains(empty, "No object types in this project yet.") || !strings.Contains(empty, "No relationship types in this project yet.") {
		t.Error("empty project types must render the pickers' empty note")
	}
}
