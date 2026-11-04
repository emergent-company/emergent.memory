package mcp

import "testing"

// TestBranchToolsUseGraphScopes pins MCP/REST branch authorization parity:
// /api/graph/branches requires graph:read/graph:write, so the graph-branch-*
// tools must too. Gating them on branches:* made them invisible to a default
// data:write token that could still call the REST endpoints (#2227).
func TestBranchToolsUseGraphScopes(t *testing.T) {
	want := map[string]string{
		"graph-branch-list":            "graph:read",
		"graph-branch-get":             "graph:read",
		"graph-branch-create":          "graph:write",
		"graph-branch-update":          "graph:write",
		"graph-branch-merge":           "graph:write",
		"graph-branch-merge-readiness": "graph:read",
		"graph-branch-fork":            "graph:write",
		"graph-branch-delete":          "graph:write",
	}
	for tool, scope := range want {
		if got := toolRequiredScope[tool]; got != scope {
			t.Errorf("%s requires %q, want %q (REST parity)", tool, got, scope)
		}
	}

	// A token with the default CLI scopes must be able to discover and call
	// every branch tool: data:write expands to graph:write.
	expanded := expandScopesSet([]string{"data:read", "data:write"})
	for tool := range want {
		scope := toolRequiredScope[tool]
		if _, ok := expanded[scope]; !ok {
			t.Errorf("default data:* token cannot see %s: %q missing from expanded scopes", tool, scope)
		}
	}
}
