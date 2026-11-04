package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// TestApplyACPRestrictions_DeviceToolsStripped proves the device MCP
// configuration tools (device-targeted create/update and the device server list)
// are opt-in ACP tools: stripped from every agent unless explicitly whitelisted.
func TestApplyACPRestrictions_DeviceToolsStripped(t *testing.T) {
	tp := buildTestPool(t, []string{
		ToolNameACPMCPServerCreate,
		ToolNameACPUpdateMCPServer,
		ToolNameACPDeviceServerList,
		"graph-query",
	})
	cache := tp.cache["test-project"]

	var defs []mcp.ToolDefinition
	for _, name := range cache.toolNames {
		defs = append(defs, cache.toolDefs[name])
	}

	result := tp.applyACPRestrictions(defs, nil)
	names := toolNames(result)

	assert.NotContains(t, names, ToolNameACPMCPServerCreate, "device-capable create must be stripped unless opted in")
	assert.NotContains(t, names, ToolNameACPUpdateMCPServer, "device-capable update must be stripped unless opted in")
	assert.NotContains(t, names, ToolNameACPDeviceServerList, "device server list must be stripped unless opted in")
	assert.Contains(t, names, "graph-query", "non-ACP tools must be retained")
}

func TestApplyACPRestrictions_DeviceToolsKeptWhenOptedIn(t *testing.T) {
	tp := buildTestPool(t, []string{
		ToolNameACPMCPServerCreate,
		ToolNameACPUpdateMCPServer,
		ToolNameACPDeviceServerList,
		"graph-query",
	})
	cache := tp.cache["test-project"]

	var defs []mcp.ToolDefinition
	for _, name := range cache.toolNames {
		defs = append(defs, cache.toolDefs[name])
	}

	result := tp.applyACPRestrictions(defs, &AgentDefinition{Tools: []string{"*"}})
	names := toolNames(result)

	assert.Contains(t, names, ToolNameACPMCPServerCreate)
	assert.Contains(t, names, ToolNameACPUpdateMCPServer)
	assert.Contains(t, names, ToolNameACPDeviceServerList)
}
