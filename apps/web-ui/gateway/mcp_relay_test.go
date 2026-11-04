package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// This file unit-tests the relay normalization helpers directly. The
// MemoryClient HTTP paths are covered in mcp_relay_client_test.go; here we pin
// the shape-tolerant decoding and the small picker helpers edge cases.

// TestRelayExtractToolsShapes locks extractRelayTools' tolerance of the
// connector payload variants: wrapped vs bare arrays, nested "tool" objects,
// top-level precedence, whitespace handling, and skipped nameless entries.
func TestRelayExtractToolsShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []RelayTool
	}{
		{
			"wrapped array",
			`{"tools":[{"name":"a","description":"A"},{"name":"b"}]}`,
			[]RelayTool{{Name: "a", Description: "A"}, {Name: "b"}},
		},
		{
			"bare array",
			`[{"name":"c","description":"C"}]`,
			[]RelayTool{{Name: "c", Description: "C"}},
		},
		{
			"nested tool object fallback",
			`{"tools":[{"tool":{"name":"d","description":"D"}}]}`,
			[]RelayTool{{Name: "d", Description: "D"}},
		},
		{
			"top-level wins over nested",
			`{"tools":[{"name":"top","description":"TopDesc","tool":{"name":"nested","description":"NestedDesc"}}]}`,
			[]RelayTool{{Name: "top", Description: "TopDesc"}},
		},
		{
			"blank top-level name falls back to nested",
			`{"tools":[{"name":"   ","tool":{"name":"fallback","description":"F"}}]}`,
			[]RelayTool{{Name: "fallback", Description: "F"}},
		},
		{
			"blank top-level description falls back to nested",
			`{"tools":[{"name":"e","description":"  ","tool":{"description":"nested desc"}}]}`,
			[]RelayTool{{Name: "e", Description: "nested desc"}},
		},
		{
			"name and description trimmed",
			`{"tools":[{"name":"  pad  ","description":"  pad desc  "}]}`,
			[]RelayTool{{Name: "pad", Description: "pad desc"}},
		},
		{
			"non-object and nameless entries skipped",
			`{"tools":["str",42,{"name":"ok"},null,[],{"description":"x"}]}`,
			[]RelayTool{{Name: "ok"}},
		},
		{
			"blank name with no nested name skipped",
			`{"tools":[{"name":"   "}]}`,
			nil,
		},
		{"invalid JSON", `not json`, nil},
		{"null body", `null`, nil},
		{"empty object", `{}`, nil},
		{"tools not an array", `{"tools":"nope"}`, nil},
		{"empty array", `[]`, nil},
		{"empty tools array", `{"tools":[]}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractRelayTools(json.RawMessage(tc.raw))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extractRelayTools(%s) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestRelayNodeHelperEdges covers the empty/no-match boundaries of the picker
// helpers: a node with no tools, a nil agent, and an all-empty group list.
func TestRelayNodeHelperEdges(t *testing.T) {
	empty := relayNode{Session: RelaySession{InstanceID: "idle"}}
	if names := empty.agentToolNames(); len(names) != 0 {
		t.Errorf("agentToolNames on a toolless node = %v, want empty", names)
	}
	if relayNodeInUse(empty, &AgentDefinition{Tools: []string{"idle_x"}}) {
		t.Error("relayNodeInUse must be false when the node serves no tools")
	}
	if relayNodeInUse(empty, nil) {
		t.Error("relayNodeInUse must be false for a nil agent")
	}

	if groups := relayPickerGroups(nil); len(groups) != 0 {
		t.Errorf("relayPickerGroups(nil) = %v, want empty", groups)
	}
	if groups := relayPickerGroups([]relayNode{empty, {Session: RelaySession{InstanceID: "also-idle"}}}); len(groups) != 0 {
		t.Errorf("relayPickerGroups must drop toolless nodes, got %v", groups)
	}

	node := relayNode{
		Session: RelaySession{InstanceID: "mac-ada"},
		Tools:   []RelayTool{{Name: "notes_search"}},
	}
	if !relayNodeInUse(node, &AgentDefinition{Tools: []string{"mac-ada_notes_search"}}) {
		t.Error("relayNodeInUse must match the derived <instance>_<tool> name")
	}
	if groups := relayPickerGroups([]relayNode{empty, node}); len(groups) != 1 || groups[0].Session.InstanceID != "mac-ada" {
		t.Errorf("relayPickerGroups = %+v, want only the node with tools", groups)
	}
}
