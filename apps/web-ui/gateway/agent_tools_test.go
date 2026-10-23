package main

import (
	"reflect"
	"testing"
)

func TestAgentToolGranted(t *testing.T) {
	cases := []struct {
		name string
		def  *AgentDefinition
		tool string
		want bool
	}{
		{
			name: "exact whitelist entry",
			def:  &AgentDefinition{Tools: []string{"entity-create"}},
			tool: "entity-create",
			want: true,
		},
		{
			name: "glob whitelist entry",
			def:  &AgentDefinition{Tools: []string{"entity-*"}},
			tool: "entity-delete",
			want: true,
		},
		{
			name: "wildcard grants everything",
			def:  &AgentDefinition{Tools: []string{"*"}},
			tool: "relationship-create",
			want: true,
		},
		{
			name: "not whitelisted",
			def:  &AgentDefinition{Tools: []string{"entity-query"}},
			tool: "entity-delete",
			want: false,
		},
		{
			name: "empty whitelist denies all (server semantics)",
			def:  &AgentDefinition{Tools: nil},
			tool: "entity-create",
			want: false,
		},
		{
			name: "banned exact entry",
			def:  &AgentDefinition{Tools: []string{"*"}, BannedTools: []string{"entity-delete"}},
			tool: "entity-delete",
			want: false,
		},
		{
			name: "banned glob entry",
			def:  &AgentDefinition{Tools: []string{"*"}, BannedTools: []string{"relationship-*"}},
			tool: "relationship-create",
			want: false,
		},
		{
			name: "disabled per-tool policy",
			def: &AgentDefinition{
				Tools:        []string{"entity-create"},
				ToolPolicies: map[string]ToolPolicy{"entity-create": {Disabled: true}},
			},
			tool: "entity-create",
			want: false,
		},
		{
			name: "confirm-only policy still grants",
			def: &AgentDefinition{
				Tools:        []string{"entity-create"},
				ToolPolicies: map[string]ToolPolicy{"entity-create": {Confirm: true}},
			},
			tool: "entity-create",
			want: true,
		},
		{
			name: "disabled group policy",
			def: &AgentDefinition{
				Tools:        []string{"entity-create"},
				ToolPolicies: map[string]ToolPolicy{"@group:graph-write": {Disabled: true}},
				ToolGroups: []ToolGroup{
					{ID: "graph-read", Tools: []string{"entity-query"}},
					{ID: "graph-write", Tools: []string{"entity-create"}},
				},
			},
			tool: "entity-create",
			want: false,
		},
		{
			name: "group policy does not leak to other groups",
			def: &AgentDefinition{
				Tools:        []string{"entity-create", "entity-query"},
				ToolPolicies: map[string]ToolPolicy{"@group:graph-read": {Disabled: true}},
				ToolGroups: []ToolGroup{
					{ID: "graph-read", Tools: []string{"entity-query"}},
					{ID: "graph-write", Tools: []string{"entity-create"}},
				},
			},
			tool: "entity-create",
			want: true,
		},
		{
			name: "display-only other group policy is ignored",
			def: &AgentDefinition{
				Tools:        []string{"entity-create"},
				ToolPolicies: map[string]ToolPolicy{"@group:other": {Disabled: true}},
				ToolGroups:   []ToolGroup{{ID: "other", Tools: []string{"entity-create"}}},
			},
			tool: "entity-create",
			want: true,
		},
		{
			name: "default deny blocks unlisted policy",
			def:  &AgentDefinition{Tools: []string{"entity-create"}, DefaultToolPolicy: "deny"},
			tool: "entity-create",
			want: false,
		},
		{
			name: "default deny is overridden by an explicit allow entry",
			def: &AgentDefinition{
				Tools:             []string{"entity-create"},
				DefaultToolPolicy: "deny",
				ToolPolicies:      map[string]ToolPolicy{"entity-create": {}},
			},
			tool: "entity-create",
			want: true,
		},
		{
			name: "nil definition grants nothing",
			def:  nil,
			tool: "entity-create",
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := agentToolGranted(c.def, c.tool); got != c.want {
				t.Errorf("agentToolGranted(%s) = %v, want %v", c.tool, got, c.want)
			}
		})
	}
}

func TestAgentToolEligibility(t *testing.T) {
	full := []string{"entity-create", "entity-delete", "entity-edges-get", "relationship-create", "relationship-delete"}
	cases := []struct {
		name            string
		def             *AgentDefinition
		wantMissing     []string
		wantRecommended []string
	}{
		{
			name: "full tool set is eligible",
			def:  &AgentDefinition{Tools: full},
		},
		{
			name:            "update instead of create is enough",
			def:             &AgentDefinition{Tools: []string{"entity-update", "entity-delete", "entity-edges-get", "relationship-create"}},
			wantRecommended: []string{"relationship-delete"},
		},
		{
			name:            "relationship-delete is recommended, not required",
			def:             &AgentDefinition{Tools: []string{"entity-create", "entity-delete", "entity-edges-get", "relationship-create"}},
			wantRecommended: []string{"relationship-delete"},
		},
		{
			name:            "no write tool",
			def:             &AgentDefinition{Tools: []string{"entity-delete", "entity-edges-get", "relationship-create"}},
			wantMissing:     []string{"entity-create or entity-update"},
			wantRecommended: []string{"relationship-delete"},
		},
		{
			name:            "missing retire",
			def:             &AgentDefinition{Tools: []string{"entity-create", "entity-edges-get", "relationship-create"}},
			wantMissing:     []string{"entity-delete"},
			wantRecommended: []string{"relationship-delete"},
		},
		{
			name:            "missing edge read and rewire",
			def:             &AgentDefinition{Tools: []string{"entity-create"}},
			wantMissing:     []string{"entity-delete", "entity-edges-get", "relationship-create"},
			wantRecommended: []string{"relationship-delete"},
		},
		{
			name:            "empty tool list is ineligible",
			def:             &AgentDefinition{},
			wantMissing:     []string{"entity-create or entity-update", "entity-delete", "entity-edges-get", "relationship-create"},
			wantRecommended: []string{"relationship-delete"},
		},
		{
			name:        "nil definition is ineligible",
			def:         nil,
			wantMissing: []string{"agent definition unavailable"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			missing, recommended := agentToolEligibility(c.def)
			if !reflect.DeepEqual(missing, c.wantMissing) {
				t.Errorf("missing = %v, want %v", missing, c.wantMissing)
			}
			if !reflect.DeepEqual(recommended, c.wantRecommended) {
				t.Errorf("recommended = %v, want %v", recommended, c.wantRecommended)
			}
		})
	}
}
