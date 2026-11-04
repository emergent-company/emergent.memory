package agents

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// TestAgentToolScopesAreInMCPVocabulary guards the MCP umbrella-scope projection
// against drift from handler-provided tools: every RequiredScope an agent tool
// declares must be a scope the MCP tool-scope vocabulary knows about, otherwise
// an umbrella scope could imply it while the MCP surface silently drops it. A
// handler-provided tool must reuse an existing vocabulary scope, or that scope
// must be added to mcp's derivation deliberately.
func TestAgentToolScopesAreInMCPVocabulary(t *testing.T) {
	for _, tool := range (&MCPToolHandler{}).GetAgentToolDefinitions() {
		if tool.RequiredScope == "" {
			continue
		}
		if !mcp.IsToolScope(tool.RequiredScope) {
			t.Errorf("agent tool %q requires scope %q not present in the MCP tool-scope vocabulary", tool.Name, tool.RequiredScope)
		}
	}
}
