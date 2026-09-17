package toolgroups

import (
	"testing"
)

// The static scopes that appear as values in mcp.toolRequiredScope. This list
// must stay in sync with that map: adding a scope there without a group here
// must fail the assertion below rather than silently land a tool in "other".
var staticRequiredScopes = []string{
	"graph:read",
	"admin",
	"schema:read",
	"schema:write",
	"schema:migrate",
	"graph:write",
	"branches:read",
	"branches:write",
	"search",
	"journal:read",
	"journal:write",
}

func TestEveryStaticRequiredScopeMapsToKnownGroup(t *testing.T) {
	for _, scope := range staticRequiredScopes {
		g, ok := scopeToGroup[scope]
		if !ok || g == "" || g == GroupOther {
			t.Errorf("scope %q maps to %q, want a known non-other group", scope, g)
		}
	}
}

func TestGroupsOrderMatchesContract(t *testing.T) {
	want := []string{
		"search", "graph-read", "graph-write", "schema-read", "schema-write",
		"schema-migrate", "branches", "journal", "documents", "skills", "agents",
		"projects", "chat", "admin", "workspace-read", "workspace-exec", "web", "other",
	}
	if len(Groups) != len(want) {
		t.Fatalf("len(Groups) = %d, want %d", len(Groups), len(want))
	}
	seen := make(map[string]bool, len(Groups))
	for i, g := range Groups {
		if g.ID != want[i] {
			t.Errorf("Groups[%d].ID = %q, want %q", i, g.ID, want[i])
		}
		if seen[g.ID] {
			t.Errorf("duplicate group id %q", g.ID)
		}
		seen[g.ID] = true
		if g.Label == "" || g.Description == "" {
			t.Errorf("group %q has empty label or description", g.ID)
		}
	}
}

func TestGroupForTool_ScopeDerived(t *testing.T) {
	cases := map[string]string{
		"entity-search":          "graph-read",
		"entity-create":          "graph-write",
		"entity-delete":          "graph-write",
		"schema-get":             "schema-read",
		"schema-create":          "schema-write",
		"schema-migrate-execute": "schema-migrate",
		"graph-branch-create":    "branches",
		"search-hybrid":          "search",
		"journal-list":           "journal",
		"journal-add-note":       "journal",
	}
	for tool, want := range cases {
		if got := GroupForTool(tool); got != want {
			t.Errorf("GroupForTool(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestGroupForTool_StaticMappings(t *testing.T) {
	workspaceRead := map[string]bool{
		"workspace_read": true, "workspace_glob": true,
		"workspace_grep": true, "ast_grep": true,
	}
	workspaceExec := map[string]bool{
		"workspace_write": true, "workspace_edit": true, "workspace_git": true,
		"workspace_bash": true, "run_python": true, "run_go": true,
	}
	web := map[string]bool{
		"google_search": true, "url_context": true, "code_execution": true,
		"webfetch": true, "brave_search": true, "reddit_search": true,
	}

	for tool := range workspaceRead {
		if got := GroupForTool(tool); got != GroupWorkspaceRead {
			t.Errorf("GroupForTool(%q) = %q, want %q", tool, got, GroupWorkspaceRead)
		}
	}
	for tool := range workspaceExec {
		if got := GroupForTool(tool); got != GroupWorkspaceExec {
			t.Errorf("GroupForTool(%q) = %q, want %q", tool, got, GroupWorkspaceExec)
		}
	}
	for tool := range web {
		if got := GroupForTool(tool); got != GroupWeb {
			t.Errorf("GroupForTool(%q) = %q, want %q", tool, got, GroupWeb)
		}
	}
}

// TestWorkspaceSandboxKeysMapToWorkspaceGroups ensures every sandbox config key
// (sandbox.ValidToolNames) resolves through its agent-facing tool name to the
// correct workspace group, never to "other".
func TestWorkspaceSandboxKeysMapToWorkspaceGroups(t *testing.T) {
	cases := map[string]string{
		// sandbox key → agent-facing name
		"bash":       "workspace_bash",
		"read":       "workspace_read",
		"write":      "workspace_write",
		"edit":       "workspace_edit",
		"glob":       "workspace_glob",
		"grep":       "workspace_grep",
		"git":        "workspace_git",
		"run_python": "run_python",
		"run_go":     "run_go",
		"ast_grep":   "ast_grep",
	}
	for sandboxKey, agentName := range cases {
		got := GroupForTool(agentName)
		if got != GroupWorkspaceRead && got != GroupWorkspaceExec {
			t.Errorf("sandbox key %q (agent tool %q) maps to %q, want a workspace group",
				sandboxKey, agentName, got)
		}
	}
}

func TestGroupForTool_UnknownFallsToOther(t *testing.T) {
	for _, tool := range []string{
		"",
		"totally-unknown-tool",
		"some_external_server_tool", // external MCP prefix
		"relay1_entity-search",      // relay node tool
	} {
		if got := GroupForTool(tool); got != GroupOther {
			t.Errorf("GroupForTool(%q) = %q, want %q", tool, got, GroupOther)
		}
	}
}

// TestGroupForScope exercises the catalog-authoritative resolver: a non-empty
// scope maps through the single scope→group table, while an empty scope falls
// back to the static tool-name map.
func TestGroupForScope(t *testing.T) {
	cases := []struct {
		scope    string
		toolName string
		want     string
	}{
		{"documents:write", "document-create", "documents"},
		{"skills:read", "skill-list", "skills"},
		{"agents:write", "agent-create", "agents"},
		{"projects:read", "project-get", "projects"},
		{"chat:use", "chat-list", "chat"},
		{"admin", "mcp-server-list", "admin"},
		{"admin:all", "x", "admin"},
		{"graph:write", "entity-delete", "graph-write"},
		{"", "workspace_bash", "workspace-exec"},
		{"", "webfetch", "web"},
		{"", "unknown-tool", "other"},
		{"data:read", "whatever", "other"}, // umbrella scope, never a tool's RequiredScope
		{"account:read", "account-key-list", "other"},
	}
	for _, tc := range cases {
		if got := GroupForScope(tc.scope, tc.toolName); got != tc.want {
			t.Errorf("GroupForScope(%q, %q) = %q, want %q", tc.scope, tc.toolName, got, tc.want)
		}
	}
}
