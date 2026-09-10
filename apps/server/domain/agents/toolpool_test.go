package agents

import (
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
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
	ToolNameACPMCPServerList,
	ToolNameACPMCPServerGet,
	ToolNameACPSearchMCPRegistry,
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
	assert.NotContains(t, names, ToolNameACPMCPServerList)
	assert.NotContains(t, names, ToolNameACPMCPServerGet)
	assert.NotContains(t, names, ToolNameACPSearchMCPRegistry)
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
	assert.NotContains(t, names, ToolNameACPMCPServerList)
	assert.NotContains(t, names, ToolNameACPMCPServerGet)
	assert.NotContains(t, names, ToolNameACPSearchMCPRegistry)
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
	assert.Contains(t, names, ToolNameACPMCPServerList)
	assert.Contains(t, names, ToolNameACPMCPServerGet)
	assert.Contains(t, names, ToolNameACPSearchMCPRegistry)
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
	assert.NotContains(t, names, ToolNameACPMCPServerList)
	assert.Contains(t, names, "graph-query")
}

func TestApplyACPRestrictions_GlobPattern_KeepsMatchingACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	var defs []mcp.ToolDefinition
	for _, name := range cache.toolNames {
		defs = append(defs, cache.toolDefs[name])
	}

	// "acp-*" matches the old names, but new names are "agent-list", "trigger_agent", etc.
	// We should test with "agent-*" and "mcp-*" and "search_*" or just "*-agent" etc.
	// Actually, the constants are what matters.
	agentDef := &AgentDefinition{Tools: []string{"graph-*", "agent-*", "mcp-*", "search_*", "trigger_*"}}
	result := tp.applyACPRestrictions(defs, agentDef)
	names := toolNames(result)

	assert.Contains(t, names, ToolNameACPListAgents)    // "agent-list"
	assert.Contains(t, names, ToolNameACPTriggerRun)    // "trigger_agent"
	assert.Contains(t, names, ToolNameACPGetRunStatus)  // "agent-run-get"
	assert.Contains(t, names, ToolNameACPMCPServerList) // "mcp-server-list"
	assert.Contains(t, names, ToolNameACPMCPServerGet)  // "mcp-server-get"
	assert.Contains(t, names, ToolNameACPSearchMCPRegistry)
	assert.Contains(t, names, "graph-query")
	assert.Contains(t, names, "graph-create")
}

// --- filterToolDefs integration tests ---

func TestFilterToolDefs_EmptyWhitelist_DeniesAllTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	// Empty whitelist → agent definition exists but no tools listed → deny everything.
	// This prevents agents with incomplete configs from silently inheriting all tools.
	agentDef := &AgentDefinition{Tools: []string{}}
	result := tp.filterToolDefs(cache, agentDef, 0, DefaultMaxDepth)

	assert.Empty(t, result, "empty Tools whitelist must grant zero tools")
}

func TestFilterToolDefs_NilAgentDef_StripsACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	// nil agentDef (legacy) → gets all tools, but ACP should be stripped
	result := tp.filterToolDefs(cache, nil, 0, DefaultMaxDepth)
	names := toolNames(result)

	assert.NotContains(t, names, ToolNameACPListAgents)
	assert.NotContains(t, names, ToolNameACPGetRunStatus)
	assert.NotContains(t, names, ToolNameACPSearchMCPRegistry)
}

func TestFilterToolDefs_ExplicitACPOptIn_KeepsACPTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	agentDef := &AgentDefinition{Tools: []string{"graph-query", "agent-*", "mcp-*", "search_*", "trigger_*"}}
	result := tp.filterToolDefs(cache, agentDef, 0, DefaultMaxDepth)
	names := toolNames(result)

	assert.Contains(t, names, ToolNameACPListAgents)
	assert.Contains(t, names, ToolNameACPTriggerRun)
	assert.Contains(t, names, ToolNameACPGetRunStatus)
	assert.Contains(t, names, ToolNameACPMCPServerList)
	assert.Contains(t, names, ToolNameACPMCPServerGet)
	assert.Contains(t, names, ToolNameACPSearchMCPRegistry)
	assert.Contains(t, names, "graph-query")
}

func TestFilterToolDefs_ACPOptIn_SubAgent_StillStripsCoordinationTools(t *testing.T) {
	tp := buildTestPool(t, allTestTools)
	cache := tp.cache["test-project"]

	// Sub-agent (depth=1) with ACP opt-in but no coordination tool opt-in
	agentDef := &AgentDefinition{Tools: []string{"graph-query", "agent-*", "mcp-*", "search_*", "trigger_*"}}
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

// --- matchToolsByWhitelist bare-name resolution ---
//
// The admin API persists agent tool whitelists with BARE tool names
// (web_fetch_exa), but the tool pool keys external MCP tools as
// <slugified server>_ToolName (e2e_mcp_123_web_fetch_exa). These tests pin the
// fallback that resolves a bare whitelist entry to its prefixed pool key(s).

// externalToolFixture describes one external MCP tool by its raw server name
// and bare tool name — the same inputs buildCache receives from the registry.
type externalToolFixture struct {
	serverName string
	toolName   string
}

// externalPoolCache builds a projectToolCache that mirrors a real pool layout:
// builtin tools keyed by bare name; external MCP tools keyed by the same
// externalToolKey(serverName, toolName) formula buildCache uses, with
// bareNameToKeys aliasing the raw bare tool name to the pooled key.
func externalPoolCache(builtinNames []string, extTools []externalToolFixture) *projectToolCache {
	cache := &projectToolCache{
		toolDefs:          make(map[string]mcp.ToolDefinition),
		builtinTools:      make(map[string]bool),
		relayToolInstance: make(map[string]string),
		bareNameToKeys:    make(map[string][]string),
	}
	for _, name := range builtinNames {
		cache.toolDefs[name] = mcp.ToolDefinition{Name: name, InputSchema: mcp.InputSchema{Type: "object"}}
		cache.toolNames = append(cache.toolNames, name)
		cache.builtinTools[name] = true
	}
	for _, ext := range extTools {
		key := externalToolKey(ext.serverName, ext.toolName)
		cache.toolDefs[key] = mcp.ToolDefinition{Name: key, InputSchema: mcp.InputSchema{Type: "object"}}
		cache.toolNames = append(cache.toolNames, key)
		cache.bareNameToKeys[ext.toolName] = append(cache.bareNameToKeys[ext.toolName], key)
	}
	return cache
}

func TestMatchToolsByWhitelist_BareExternalName_ResolvesPrefixedKey(t *testing.T) {
	cache := externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{
			{serverName: "ts", toolName: "web_fetch_exa"},
			{serverName: "exa", toolName: "graph_query"},
		},
	)
	tp := &ToolPool{log: slog.Default()}

	// Bare external tool name (as the admin API persists it) — no such pool key
	// exists, so it must resolve via bareNameToKeys to the prefixed key.
	defs := tp.matchToolsByWhitelist(cache, []string{"web_fetch_exa"})

	require.Len(t, defs, 1, "bare external name must resolve to exactly one prefixed key")
	assert.Equal(t, "ts_web_fetch_exa", defs[0].Name)
	assert.Equal(t, "ts_web_fetch_exa", cache.toolDefs[defs[0].Name].Name)
}

func TestMatchToolsByWhitelist_ExactPrefixedExternalName_StillMatches(t *testing.T) {
	cache := externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{{serverName: "ts", toolName: "web_fetch_exa"}},
	)
	tp := &ToolPool{log: slog.Default()}

	// A whitelist entry that already carries the prefixed pool key must keep
	// matching exactly (no regression of the primary path).
	defs := tp.matchToolsByWhitelist(cache, []string{"ts_web_fetch_exa"})

	require.Len(t, defs, 1)
	assert.Equal(t, "ts_web_fetch_exa", defs[0].Name)
}

func TestMatchToolsByWhitelist_AmbiguousBareName_ResolvesBothServers(t *testing.T) {
	cache := externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{
			{serverName: "alpha", toolName: "web_fetch_exa"},
			{serverName: "beta", toolName: "web_fetch_exa"},
		},
	)
	tp := &ToolPool{log: slog.Default()}

	// Same bare tool name exposed by two external servers — both prefixed keys
	// must be resolved so the agent can reach either server's implementation.
	defs := tp.matchToolsByWhitelist(cache, []string{"web_fetch_exa"})

	names := toolNames(defs)
	assert.ElementsMatch(t, []string{"alpha_web_fetch_exa", "beta_web_fetch_exa"}, names)
}

func TestMatchToolsByWhitelist_BareAndPrefixedEntry_Dedupes(t *testing.T) {
	cache := externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{{serverName: "ts", toolName: "web_fetch_exa"}},
	)
	tp := &ToolPool{log: slog.Default()}

	// Whitelist containing both the bare and the prefixed form of the same tool
	// must yield a single resolved def.
	defs := tp.matchToolsByWhitelist(cache, []string{"web_fetch_exa", "ts_web_fetch_exa"})

	require.Len(t, defs, 1)
	assert.Equal(t, "ts_web_fetch_exa", defs[0].Name)
}

func TestMatchToolsByWhitelist_UnknownBareName_ResolvesNothing(t *testing.T) {
	cache := externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{{serverName: "ts", toolName: "web_fetch_exa"}},
	)
	tp := &ToolPool{log: slog.Default()}

	// An invalid/unknown tool name (neither a pool key nor an indexed bare name)
	// must keep the silent-skip behaviour — it resolves to nothing, no error.
	defs := tp.matchToolsByWhitelist(cache, []string{"memory_lookup"})

	assert.Empty(t, defs)
}

func TestMatchToolsByWhitelist_RealBareBuiltin_StillMatchesExactly(t *testing.T) {
	cache := externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{{serverName: "ts", toolName: "web_fetch_exa"}},
	)
	tp := &ToolPool{log: slog.Default()}

	// A genuine bare builtin pool key (search-knowledge) must keep resolving
	// through the exact-match path — the bare-name fallback is only a rescue
	// when the exact lookup misses.
	defs := tp.matchToolsByWhitelist(cache, []string{"search-knowledge"})

	require.Len(t, defs, 1)
	assert.Equal(t, "search-knowledge", defs[0].Name)
}

func TestResolveTools_BareExternalName_YieldsPrefixedADKTool(t *testing.T) {
	tp := &ToolPool{
		log:        slog.Default(),
		mcpService: &mcp.Service{},
		cache:      make(map[string]*projectToolCache),
	}
	// Leave registryService nil on purpose — resolution and wrapping must not
	// depend on it; the prefixed def routes through the builtin wrapper here.
	tp.cache["test-project"] = externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{{serverName: "ts", toolName: "web_fetch_exa"}},
	)

	agentDef := &AgentDefinition{Tools: []string{"web_fetch_exa"}, Name: "research"}
	tools, err := tp.ResolveTools("test-project", agentDef, 0, DefaultMaxDepth)
	require.NoError(t, err)

	var names []string
	for _, t := range tools {
		if t == nil {
			continue
		}
		names = append(names, t.Name())
	}
	assert.Contains(t, names, "ts_web_fetch_exa",
		"bare whitelist entry must resolve to the prefixed ADK tool in the pipeline")
	assert.Contains(t, names, "set_session_title",
		"hidden builtin set_session_title injection must still apply")
	assert.NotContains(t, names, "web_fetch_exa",
		"the bare name must not leak into the resolved pipeline — only prefixed keys are valid pool members")
}

func TestResolveTools_BareExternalName_WithRegistryService_StillWraps(t *testing.T) {
	tp := &ToolPool{
		log:             slog.Default(),
		mcpService:      &mcp.Service{},
		registryService: &mcpregistry.Service{},
		cache:           make(map[string]*projectToolCache),
	}
	tp.cache["test-project"] = externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{{serverName: "ts", toolName: "web_fetch_exa"}},
	)

	agentDef := &AgentDefinition{Tools: []string{"web_fetch_exa"}, Name: "research"}
	tools, err := tp.ResolveTools("test-project", agentDef, 0, DefaultMaxDepth)
	require.NoError(t, err)

	var names []string
	for _, t := range tools {
		if t == nil {
			continue
		}
		names = append(names, t.Name())
	}
	assert.Contains(t, names, "ts_web_fetch_exa")
}

// --- externalToolKey ---
//
// LLM function names must match ^[A-Za-z0-9_-]{1,64}$; server names such as
// "E2E MCP 123" would otherwise produce pool keys providers silently drop.
// (SlugifyServerName itself is unit-tested in domain/mcpregistry/names_test.go.)

// functionNameRE mirrors the LLM function-name contract enforced by
// tool-calling providers.
var functionNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func requireValidFunctionName(t *testing.T, name string) {
	t.Helper()
	require.Regexp(t, functionNameRE, name, "name %q violates the LLM function-name contract", name)
}

func TestExternalToolKey_AlwaysValidFunctionName(t *testing.T) {
	servers := []string{
		"E2E MCP 123",
		"MyCool-Server!!v2",
		"server/with/slashes & dots.v1",
		"!!!",
		"",
		"normal",
		strings.Repeat("s", 120), // absurdly long server name
	}
	tools := []string{"web_fetch_exa", "graph_query", "a"}
	for _, srv := range servers {
		for _, tool := range tools {
			key := externalToolKey(srv, tool)
			requireValidFunctionName(t, key)
			// The bare tool-name suffix is always preserved whole.
			assert.True(t, strings.HasSuffix(key, tool),
				"externalToolKey(%q, %q) = %q must keep the tool-name suffix", srv, tool, key)
		}
	}
	// Deterministic: same input always yields the same key.
	assert.Equal(t, externalToolKey("E2E MCP 123", "web_fetch_exa"),
		externalToolKey("E2E MCP 123", "web_fetch_exa"))
}

func TestExternalToolKey_TruncatesTo64Chars(t *testing.T) {
	// 120 's' chars slug to a 120-char server prefix — must be cut to budget.
	tool := "web_fetch_exa" // 14 chars
	key := externalToolKey(strings.Repeat("s", 120), tool)
	requireValidFunctionName(t, key)
	assert.LessOrEqual(t, len(key), maxFunctionNameLen, "key must never exceed the 64-char cap")
	assert.True(t, strings.HasSuffix(key, "_"+tool), "key %q must keep the joined tool suffix", key)

	// Truncation landing exactly on an underscore boundary must strip the
	// trailing '_' before the joining '_' (no empty segment, no "___").
	// Single-letter words give a perfectly regular slug: "a_b_c_..._z_a_...".
	// 52 letters → slug length 103; budget = 64-15-1 = 48 is even, so slug[:48]
	// ends on an underscore (letter index 47) and TrimRight drops it, leaving
	// letters a..x (indices 0..46, 24 letters) before the joining "_".
	const abc = "abcdefghijklmnopqrstuvwxyz"
	serverWords := make([]string, 0, 52)
	for pass := 0; pass < 2; pass++ {
		for i := 0; i < 26; i++ {
			serverWords = append(serverWords, string(abc[i]))
		}
	}
	serverName := strings.Join(serverWords, " ")
	toolLong := "abcdefghijklmno" // 15 chars → budget = 64-15-1 = 48
	key = externalToolKey(serverName, toolLong)
	requireValidFunctionName(t, key)
	trimmedLetters := make([]string, 24)
	for i := 0; i < 24; i++ {
		trimmedLetters[i] = string(abc[i])
	}
	expected := strings.Join(trimmedLetters, "_") + "_" + toolLong
	assert.Equal(t, expected, key)
	assert.NotContains(t, key, "___")
}

// --- spacey/non-alnum server names in the pool ---

func TestMatchToolsByWhitelist_SpaceyServerName_ResolvesToSluggedKey(t *testing.T) {
	cache := externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{{serverName: "E2E MCP 123", toolName: "web_fetch_exa"}},
	)
	tp := &ToolPool{log: slog.Default()}

	// buildCache keys this tool as e2e_mcp_123_web_fetch_exa and aliases the raw
	// bare name into bareNameToKeys, so a bare whitelist entry still resolves.
	keys := cache.bareNameToKeys["web_fetch_exa"]
	require.Len(t, keys, 1)
	require.Equal(t, "e2e_mcp_123_web_fetch_exa", keys[0])

	defs := tp.matchToolsByWhitelist(cache, []string{"web_fetch_exa"})
	require.Len(t, defs, 1)
	assert.Equal(t, "e2e_mcp_123_web_fetch_exa", defs[0].Name)
	requireValidFunctionName(t, defs[0].Name)
}

func TestResolveTools_SpaceyServerName_ProducesValidADKToolName(t *testing.T) {
	tp := &ToolPool{
		log:        slog.Default(),
		mcpService: &mcp.Service{},
		cache:      make(map[string]*projectToolCache),
	}
	tp.cache["test-project"] = externalPoolCache(
		[]string{"search-knowledge"},
		[]externalToolFixture{{serverName: "E2E MCP 123", toolName: "web_fetch_exa"}},
	)

	agentDef := &AgentDefinition{Tools: []string{"web_fetch_exa"}, Name: "research"}
	tools, err := tp.ResolveTools("test-project", agentDef, 0, DefaultMaxDepth)
	require.NoError(t, err)

	var names []string
	for _, t := range tools {
		if t == nil {
			continue
		}
		names = append(names, t.Name())
	}
	for _, name := range names {
		// Every tool that reaches the pipeline must be a legal LLM function name
		// — including the hidden set_session_title injection.
		requireValidFunctionName(t, name)
	}
	assert.Contains(t, names, "e2e_mcp_123_web_fetch_exa",
		"spacey server name must resolve to the slugged prefixed ADK tool")
	assert.Contains(t, names, "set_session_title")
	assert.NotContains(t, names, "web_fetch_exa")
}

// --- convertToolResult ---

func TestConvertToolResult(t *testing.T) {
	t.Run("object result stays flattened", func(t *testing.T) {
		out, err := convertToolResult(&mcp.ToolResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: `{"name":"diane","count":3}`}},
		})
		require.NoError(t, err)
		assert.Equal(t, true, out["ok"])
		assert.Equal(t, "diane", out["name"])
		assert.Equal(t, float64(3), out["count"])
	})

	t.Run("object result preserves existing ok", func(t *testing.T) {
		out, err := convertToolResult(&mcp.ToolResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: `{"ok":false,"failed":1}`}},
		})
		require.NoError(t, err)
		assert.Equal(t, false, out["ok"])
		assert.Equal(t, float64(1), out["failed"])
	})

	t.Run("array result is decoded not re-encoded", func(t *testing.T) {
		out, err := convertToolResult(&mcp.ToolResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: `[{"id":"a1"},{"id":"a2"}]`}},
		})
		require.NoError(t, err)
		assert.Equal(t, true, out["ok"])
		results, ok := out["result"].([]any)
		require.True(t, ok, "result should be a decoded array, got %T", out["result"])
		assert.Len(t, results, 2)
	})

	t.Run("numeric scalar result is decoded not re-encoded", func(t *testing.T) {
		out, err := convertToolResult(&mcp.ToolResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: `42`}},
		})
		require.NoError(t, err)
		assert.Equal(t, true, out["ok"])
		assert.Equal(t, float64(42), out["result"])
	})

	t.Run("boolean scalar result is decoded not re-encoded", func(t *testing.T) {
		out, err := convertToolResult(&mcp.ToolResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: `true`}},
		})
		require.NoError(t, err)
		assert.Equal(t, true, out["ok"])
		assert.Equal(t, true, out["result"])
	})

	t.Run("non-json text falls back to string result", func(t *testing.T) {
		out, err := convertToolResult(&mcp.ToolResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: "hello world"}},
		})
		require.NoError(t, err)
		assert.Equal(t, true, out["ok"])
		assert.Equal(t, "hello world", out["result"])
	})

	t.Run("error result surfaces ok=false", func(t *testing.T) {
		out, err := convertToolResult(&mcp.ToolResult{
			IsError: true,
			Content: []mcp.ContentBlock{{Type: "text", Text: "boom"}},
		})
		require.NoError(t, err)
		assert.Equal(t, false, out["ok"])
		assert.Equal(t, "boom", out["error"])
	})

	t.Run("nested envelope round-trips ok/data/meta intact", func(t *testing.T) {
		out, err := convertToolResult(&mcp.ToolResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: `{"ok":true,"data":{"x":1},"meta":{"n":2}}`}},
		})
		require.NoError(t, err)
		// ok must survive untouched (no re-injection of a uniform ok=true).
		assert.Equal(t, true, out["ok"])
		data, ok := out["data"].(map[string]any)
		require.True(t, ok, "data should remain a nested map, got %T", out["data"])
		assert.Equal(t, float64(1), data["x"])
		meta, ok := out["meta"].(map[string]any)
		require.True(t, ok, "meta should remain a nested map, got %T", out["meta"])
		assert.Equal(t, float64(2), meta["n"])
		assert.NotContains(t, out, "result", "enveloped result must not be re-wrapped under result")
	})

	t.Run("failed envelope preserves ok=false and error", func(t *testing.T) {
		out, err := convertToolResult(&mcp.ToolResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: `{"ok":false,"error":"boom","data":{}}`}},
		})
		require.NoError(t, err)
		// ok=false must survive — injecting a uniform ok=true would flip status.
		assert.Equal(t, false, out["ok"])
		assert.Equal(t, "boom", out["error"])
		data, ok := out["data"].(map[string]any)
		require.True(t, ok, "data should remain a nested map, got %T", out["data"])
		assert.Empty(t, data)
	})
}
