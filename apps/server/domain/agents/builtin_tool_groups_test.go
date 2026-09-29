package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents/toolgroups"
	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// TestToolGroupsFromCatalog_OrderPreservedEmptyOmitted pins the builtin
// tool-groups contract: groups follow toolgroups.Groups' frozen order, empty
// groups are omitted, every group is enabled=true with policy="".
func TestToolGroupsFromCatalog_OrderPreservedEmptyOmitted(t *testing.T) {
	// Catalog order is deliberately non-group-order to prove the frozen order wins.
	catalog := []mcp.ToolDefinition{
		{Name: "schema-list", RequiredScope: "schema:read"},
		{Name: "entity-create", RequiredScope: "graph:write"},
		{Name: "entity-search", RequiredScope: "graph:read"},
		{Name: "search-hybrid", RequiredScope: "search"},
		{Name: "document-create", RequiredScope: "documents:write"},
	}

	groups := ToolGroupsFromCatalog(catalog)

	wantIDs := []string{
		toolgroups.GroupSearch,
		toolgroups.GroupGraphRead,
		toolgroups.GroupGraphWrite,
		toolgroups.GroupSchemaRead,
		toolgroups.GroupDocuments,
	}
	require.Len(t, groups, len(wantIDs), "only non-empty groups should be present")

	gotIDs := make([]string, len(groups))
	for i, g := range groups {
		gotIDs[i] = g.ID
	}
	assert.Equal(t, wantIDs, gotIDs, "groups must follow the frozen toolgroups.Groups order")

	for _, g := range groups {
		assert.True(t, g.Enabled, "group %q must be enabled", g.ID)
		assert.Equal(t, "", g.Policy, "group %q must have empty policy", g.ID)
	}

	byID := map[string][]string{}
	for _, g := range groups {
		byID[g.ID] = g.Tools
	}
	assert.Equal(t, []string{"search-hybrid"}, byID[toolgroups.GroupSearch])
	assert.Equal(t, []string{"entity-search"}, byID[toolgroups.GroupGraphRead])
	assert.Equal(t, []string{"entity-create"}, byID[toolgroups.GroupGraphWrite])
	assert.Equal(t, []string{"schema-list"}, byID[toolgroups.GroupSchemaRead])
	assert.Equal(t, []string{"document-create"}, byID[toolgroups.GroupDocuments])
}

// TestToolGroupsFromCatalog_Empty returns an empty (non-nil) slice when the
// catalog is empty.
func TestToolGroupsFromCatalog_Empty(t *testing.T) {
	assert.Empty(t, ToolGroupsFromCatalog(nil))
	assert.Empty(t, ToolGroupsFromCatalog([]mcp.ToolDefinition{}))
}
