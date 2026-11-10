package sources

import (
	"testing"

	"github.com/google/uuid"
)

// sourceWithMcpSlug builds the minimum a name-resolution test needs: a provider
// type and the stable slug that namespaces its tools.
func sourceWithMcpSlug(provider, slug string) *Source {
	s := slug
	return &Source{ID: uuid.New(), Provider: provider, McpSlug: &s}
}

func gmailToolRegistry() *Registry {
	return NewRegistry(newGmailProvider("", ""))
}

// TestMatchSourceToolPrefersLongestSlug covers must-fix 1: one account's slug
// can be a dash-prefix of another's (alice and alice-work from alice@ and
// alice.work@), so a first-prefix match resolves the longer account's tools to
// the shorter account and fails them as unknown actions.
func TestMatchSourceToolPrefersLongestSlug(t *testing.T) {
	alice := sourceWithMcpSlug("gmail", "alice")
	aliceWork := sourceWithMcpSlug("gmail", "alice-work")
	// Shortest first: an implementation that keeps the first match passes the
	// short case and fails the long one, which is the bug being pinned.
	sources := []*Source{alice, aliceWork}

	cases := []struct {
		tool   string
		source *Source
		action string
	}{
		{"gmail-alice-thread-get", alice, "thread-get"},
		{"gmail-alice-work-thread-get", aliceWork, "thread-get"},
		{"gmail-alice-work-message-search", aliceWork, "message-search"},
		{"gmail-alice-message-send", alice, "message-send"},
	}
	for _, tc := range cases {
		ref := matchSourceTool(tc.tool, sources, gmailToolRegistry())
		if ref == nil {
			t.Errorf("%s did not resolve", tc.tool)
			continue
		}
		if ref.source.ID != tc.source.ID {
			t.Errorf("%s resolved to %s, want %s", tc.tool, *ref.source.McpSlug, *tc.source.McpSlug)
		}
		if ref.action != tc.action {
			t.Errorf("%s resolved action %q, want %q", tc.tool, ref.action, tc.action)
		}
	}

	// Order independence: the same set in the other order must agree.
	reversed := []*Source{aliceWork, alice}
	if ref := matchSourceTool("gmail-alice-thread-get", reversed, gmailToolRegistry()); ref == nil || ref.source.ID != alice.ID {
		t.Fatalf("resolution depends on list order: %+v", ref)
	}
}

// TestMatchSourceToolClaimsOnlyRegisteredNames covers must-fix 2: dispatch asks
// this before its relay/registry fallbacks, so a name that merely looks like a
// source tool (foreign gmail-*, unknown action) must not be claimed.
func TestMatchSourceToolClaimsOnlyRegisteredNames(t *testing.T) {
	sources := []*Source{sourceWithMcpSlug("gmail", "alice")}
	registry := gmailToolRegistry()

	for _, tool := range []string{
		// Another account's tool: the prefix matches "alice", the action does not.
		"gmail-alice-work-message-search",
		// No such account.
		"gmail-other-message-search",
		// Registered prefix, unregistered action.
		"gmail-alice-not-an-action",
		// Provider with no source tools / no such provider.
		"github-repo-list",
		"gmail-message-search",
	} {
		if ref := matchSourceTool(tool, sources, registry); ref != nil {
			t.Errorf("%s was claimed by source %s", tool, *ref.source.McpSlug)
		}
	}

	// A source with no slug (provider without MCP tooling) claims nothing.
	noSlug := &Source{ID: uuid.New(), Provider: "gmail"}
	if ref := matchSourceTool("gmail-alice-thread-get", []*Source{noSlug}, registry); ref != nil {
		t.Fatal("a source without an MCP slug claimed a tool")
	}

	// Write tools are claimed by name even when the source has not opted in; the
	// opt-in is enforced at execution, not by hiding the name from dispatch.
	if ref := matchSourceTool("gmail-alice-draft-send", sources, registry); ref == nil || !isGmailWriteAction(ref.action) {
		t.Fatalf("draft-send did not resolve as a write action: %+v", ref)
	}
}

// TestSourceAdminToolNamesAreClosed pins that only the fixed admin tool set is
// claimed under the source- prefix.
func TestSourceAdminToolNamesAreClosed(t *testing.T) {
	for _, name := range []string{
		"source-list", "source-get", "source-scope-preview", "source-scope-update", "source-sync-trigger",
	} {
		if !sourceAdminToolNames[name] {
			t.Errorf("%s is not a registered admin tool", name)
		}
	}
	for _, name := range []string{"source-scope-reset", "source-delete-everything", "source-forget"} {
		if sourceAdminToolNames[name] {
			t.Errorf("%s is claimed as an admin tool", name)
		}
	}
}

// TestSourceScopeUpdateRejectsWriteOptInFromMCP covers must-fix 5: an agent run
// reaches source-scope-update with the token-scope check bypassed, so the
// write-tool switch must not be settable through the MCP surface at all.
func TestSourceScopeUpdateRejectsWriteOptInFromMCP(t *testing.T) {
	for _, tool := range sourceAdminToolDefinitions() {
		if tool.Name != "source-scope-update" && tool.Name != "source-scope-preview" {
			continue
		}
		if _, ok := tool.InputSchema.Properties["mcp_write_tools_enabled"]; ok {
			t.Fatalf("%s still advertises mcp_write_tools_enabled", tool.Name)
		}
	}

	if _, err := scopeRequestFromArgs(map[string]any{
		"source_id":               uuid.New().String(),
		"preview_hash":            "x",
		"base_scope_hash":         "y",
		"mcp_write_tools_enabled": true,
	}); err == nil {
		t.Fatal("scopeRequestFromArgs accepted mcp_write_tools_enabled from MCP")
	}

	// The filter/object-type half of the switch stays agent-callable.
	if _, err := scopeRequestFromArgs(map[string]any{
		"source_id": uuid.New().String(),
		"filter":    map[string]any{"labels": []any{"Label_1"}},
	}); err != nil {
		t.Fatalf("scopeRequestFromArgs rejected a structured filter: %v", err)
	}
}
