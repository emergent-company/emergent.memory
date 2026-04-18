package agents

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// buildTestPool creates a ToolPool with a pre-populated cache for testing.
func buildTestPool(t *testing.T, toolNames []string) *ToolPool {
	t.Helper()
	tp := &ToolPool{
		log:   slog.Default(),
		cache: make(map[string]*projectToolCache),
	}
	cache := &projectToolCache{
		toolDefs:     make(map[string]mcp.ToolDefinition),
		builtinTools: make(map[string]bool),
	}
	for _, name := range toolNames {
		cache.toolDefs[name] = mcp.ToolDefinition{Name: name}
		cache.toolNames = append(cache.toolNames, name)
		cache.builtinTools[name] = true
	}
	tp.cache["test-project"] = cache
	return tp
}

// toolNames extracts names from a slice of ToolDefinition.
func toolNames(defs []mcp.ToolDefinition) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

// allTestTools is a representative pool including ACP tools and regular tools.
var allTestTools = []string{
	"graph-query",
	"graph-create",
	"spawn_agents",
	"list_available_agents",
	ToolNameACPListAgents,
	ToolNameACPTriggerRun,
	ToolNameACPGetRunStatus,
}

// --- applyACPRestrictions tests ---

func TestApplyACPRestrictions_NilAgentDef_StripsACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	var defs []mcp.ToolDefinition
	for _, name := range cache.toolNames {
		defs = append(defs, cache.toolDefs[name])
	}

	result := tp.applyACPRestrictions(defs, nil)
	names := toolNames(result)

	assert.NotContains(t, names, ToolNameACPListAgents)
	assert.NotContains(t, names, ToolNameACPTriggerRun)
	assert.NotContains(t, names, ToolNameACPGetRunStatus)
	assert.Contains(t, names, "graph-query")
	assert.Contains(t, names, "graph-create")
}

func TestApplyACPRestrictions_EmptyWhitelist_StripsACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	var defs []mcp.ToolDefinition
	for _, name := range cache.toolNames {
		defs = append(defs, cache.toolDefs[name])
	}

	agentDef := &AgentDefinition{Tools: []string{}}
	result := tp.applyACPRestrictions(defs, agentDef)
	names := toolNames(result)

	assert.NotContains(t, names, ToolNameACPListAgents)
	assert.NotContains(t, names, ToolNameACPTriggerRun)
	assert.NotContains(t, names, ToolNameACPGetRunStatus)
}

func TestApplyACPRestrictions_WildcardWhitelist_KeepsACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	var defs []mcp.ToolDefinition
	for _, name := range cache.toolNames {
		defs = append(defs, cache.toolDefs[name])
	}

	agentDef := &AgentDefinition{Tools: []string{"*"}}
	result := tp.applyACPRestrictions(defs, agentDef)
	names := toolNames(result)

	assert.Contains(t, names, ToolNameACPListAgents)
	assert.Contains(t, names, ToolNameACPTriggerRun)
	assert.Contains(t, names, ToolNameACPGetRunStatus)
}

func TestApplyACPRestrictions_ExplicitACPTool_KeepsOnlyThatTool(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	var defs []mcp.ToolDefinition
	for _, name := range cache.toolNames {
		defs = append(defs, cache.toolDefs[name])
	}

	agentDef := &AgentDefinition{Tools: []string{"graph-query", ToolNameACPTriggerRun}}
	result := tp.applyACPRestrictions(defs, agentDef)
	names := toolNames(result)

	assert.NotContains(t, names, ToolNameACPListAgents)
	assert.Contains(t, names, ToolNameACPTriggerRun)
	assert.NotContains(t, names, ToolNameACPGetRunStatus)
	assert.Contains(t, names, "graph-query")
}

func TestApplyACPRestrictions_GlobPattern_KeepsMatchingACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	var defs []mcp.ToolDefinition
	for _, name := range cache.toolNames {
		defs = append(defs, cache.toolDefs[name])
	}

	agentDef := &AgentDefinition{Tools: []string{"graph-*", "acp-*"}}
	result := tp.applyACPRestrictions(defs, agentDef)
	names := toolNames(result)

	assert.Contains(t, names, ToolNameACPListAgents)
	assert.Contains(t, names, ToolNameACPTriggerRun)
	assert.Contains(t, names, ToolNameACPGetRunStatus)
	assert.Contains(t, names, "graph-query")
	assert.Contains(t, names, "graph-create")
}

// --- filterToolDefs integration tests ---

func TestFilterToolDefs_EmptyWhitelist_StripsACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	// Empty whitelist → gets all tools, but ACP should be stripped
	agentDef := &AgentDefinition{Tools: []string{}}
	result := tp.filterToolDefs(cache, agentDef, 0, DefaultMaxDepth)
	names := toolNames(result)

	assert.NotContains(t, names, ToolNameACPListAgents)
	assert.NotContains(t, names, ToolNameACPTriggerRun)
	assert.NotContains(t, names, ToolNameACPGetRunStatus)
}

func TestFilterToolDefs_NilAgentDef_StripsACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	// nil agentDef (legacy) → gets all tools, but ACP should be stripped
	result := tp.filterToolDefs(cache, nil, 0, DefaultMaxDepth)
	names := toolNames(result)

	assert.NotContains(t, names, ToolNameACPListAgents)
	assert.NotContains(t, names, ToolNameACPGetRunStatus)
}

func TestFilterToolDefs_ExplicitACPOptIn_KeepsACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	agentDef := &AgentDefinition{Tools: []string{"graph-query", "acp-*"}}
	result := tp.filterToolDefs(cache, agentDef, 0, DefaultMaxDepth)
	names := toolNames(result)

	assert.Contains(t, names, ToolNameACPListAgents)
	assert.Contains(t, names, ToolNameACPTriggerRun)
	assert.Contains(t, names, ToolNameACPGetRunStatus)
	assert.Contains(t, names, "graph-query")
}

func TestFilterToolDefs_ACPOptIn_SubAgent_StillStripsCoordinationTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	// Sub-agent (depth=1) with ACP opt-in but no coordination tool opt-in
	agentDef := &AgentDefinition{Tools: []string{"graph-query", "acp-*"}}
	result := tp.filterToolDefs(cache, agentDef, 1, DefaultMaxDepth)
	names := toolNames(result)

	// ACP tools kept (explicit opt-in)
	assert.Contains(t, names, ToolNameACPListAgents)
	// Coordination tools stripped (no opt-in, depth > 0)
	assert.NotContains(t, names, ToolNameSpawnAgents)
	assert.NotContains(t, names, ToolNameListAvailableAgents)
}

// --- ToolPool.ToolNames sanity check ---

func TestToolPool_ToolNames_ReturnsAllCachedNames(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	names := tp.ToolNames("test-project")
	require.Equal(t, len(allTestTools), len(names))
	for _, n := range allTestTools {
		assert.Contains(t, names, n)
	}
}
