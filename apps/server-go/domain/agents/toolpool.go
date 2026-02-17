package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"sync"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/emergent-company/emergent/domain/mcp"
	"github.com/emergent-company/emergent/domain/mcpregistry"
)

// Coordination tool names that are restricted for sub-agents by default.
const (
	ToolNameSpawnAgents         = "spawn_agents"
	ToolNameListAvailableAgents = "list_available_agents"
)

// coordinationTools is the set of tools denied to sub-agents by default.
var coordinationTools = map[string]bool{
	ToolNameSpawnAgents:         true,
	ToolNameListAvailableAgents: true,
}

// DefaultMaxDepth is the default maximum agent spawning depth.
const DefaultMaxDepth = 2

// ToolPoolConfig holds configuration for creating a ToolPool.
type ToolPoolConfig struct {
	MCPService      *mcp.Service
	RegistryService *mcpregistry.Service
	Logger          *slog.Logger
}

// ToolPool maintains a per-project cache of available tools, combining
// built-in MCP tools with external MCP server tools into a unified set.
// Tool resolution filters this pool per agent definition at pipeline build time.
type ToolPool struct {
	mcpService      *mcp.Service
	registryService *mcpregistry.Service
	log             *slog.Logger

	// Per-project cache of tool definitions
	mu    sync.RWMutex
	cache map[string]*projectToolCache
}

// projectToolCache holds the cached tool definitions for a single project.
type projectToolCache struct {
	// toolDefs maps tool name → ToolDefinition for fast lookup
	toolDefs map[string]mcp.ToolDefinition
	// toolNames is the ordered list of tool names (for deterministic iteration)
	toolNames []string
}

// NewToolPool creates a new ToolPool.
func NewToolPool(cfg ToolPoolConfig) *ToolPool {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	return &ToolPool{
		mcpService:      cfg.MCPService,
		registryService: cfg.RegistryService,
		log:             log,
		cache:           make(map[string]*projectToolCache),
	}
}

// getOrBuildCache returns the cached tool definitions for a project,
// building the cache on first access.
func (tp *ToolPool) getOrBuildCache(projectID string) *projectToolCache {
	// Fast path: read lock
	tp.mu.RLock()
	if c, ok := tp.cache[projectID]; ok {
		tp.mu.RUnlock()
		return c
	}
	tp.mu.RUnlock()

	// Slow path: write lock and build
	tp.mu.Lock()
	defer tp.mu.Unlock()

	// Double-check after acquiring write lock
	if c, ok := tp.cache[projectID]; ok {
		return c
	}

	c := tp.buildCache(projectID)
	tp.cache[projectID] = c
	return c
}

// buildCache creates the tool cache for a project by combining all tool sources.
func (tp *ToolPool) buildCache(projectID string) *projectToolCache {
	cache := &projectToolCache{
		toolDefs: make(map[string]mcp.ToolDefinition),
	}

	// 1. Built-in MCP tools from domain/mcp/service.go
	if tp.mcpService != nil {
		builtinDefs := tp.mcpService.GetToolDefinitions()
		for _, td := range builtinDefs {
			cache.toolDefs[td.Name] = td
			cache.toolNames = append(cache.toolNames, td.Name)
		}
		tp.log.Debug("loaded built-in MCP tools into pool",
			slog.String("project_id", projectID),
			slog.Int("count", len(builtinDefs)),
		)
	}

	// 2. External MCP server tools from mcpregistry
	if tp.registryService != nil {
		extTools, err := tp.registryService.GetEnabledToolsForProject(context.Background(), projectID)
		if err != nil {
			tp.log.Warn("failed to load external MCP tools into pool",
				slog.String("project_id", projectID),
				slog.String("error", err.Error()),
			)
		} else {
			for _, et := range extTools {
				// Prefix external tool names: servername_toolname (Diane convention)
				prefixedName := et.ServerName + "_" + et.ToolName
				desc := ""
				if et.Description != nil {
					desc = *et.Description
				}
				td := mcp.ToolDefinition{
					Name:        prefixedName,
					Description: desc,
					InputSchema: mapToInputSchema(et.InputSchema),
				}
				cache.toolDefs[prefixedName] = td
				cache.toolNames = append(cache.toolNames, prefixedName)
			}
			if len(extTools) > 0 {
				tp.log.Debug("loaded external MCP tools into pool",
					slog.String("project_id", projectID),
					slog.Int("count", len(extTools)),
				)
			}
		}
	}

	return cache
}

// InvalidateCache removes the cached tool pool for a project,
// forcing a rebuild on the next access.
// Call this when a project's MCP server configuration changes.
func (tp *ToolPool) InvalidateCache(projectID string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	delete(tp.cache, projectID)
	tp.log.Info("invalidated tool pool cache",
		slog.String("project_id", projectID),
	)
}

// InvalidateAll removes all cached tool pools.
func (tp *ToolPool) InvalidateAll() {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.cache = make(map[string]*projectToolCache)
	tp.log.Info("invalidated all tool pool caches")
}

// ResolveTools filters the project's ToolPool to only the tools allowed by the
// agent definition's tools whitelist, then wraps them as ADK tool.Tool instances.
//
// Parameters:
//   - projectID: the project context
//   - agentDef: the agent definition with tools whitelist (may be nil for legacy agents)
//   - depth: the current agent spawn depth (0 = top-level)
//   - maxDepth: the maximum allowed spawn depth (use DefaultMaxDepth if 0)
//
// Tool filtering is enforced at the Go level — the ADK pipeline only receives
// resolved tools. The LLM cannot call tools not in this set.
func (tp *ToolPool) ResolveTools(projectID string, agentDef *AgentDefinition, depth int, maxDepth int) ([]tool.Tool, error) {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}

	cache := tp.getOrBuildCache(projectID)

	// Determine which tool definitions to include
	resolvedDefs := tp.filterToolDefs(cache, agentDef, depth, maxDepth)

	// Wrap resolved definitions as ADK tools
	return tp.wrapTools(projectID, resolvedDefs)
}

// filterToolDefs selects tool definitions from the cache based on the agent
// definition's tools whitelist and depth restrictions.
func (tp *ToolPool) filterToolDefs(cache *projectToolCache, agentDef *AgentDefinition, depth int, maxDepth int) []mcp.ToolDefinition {
	var defs []mcp.ToolDefinition

	if agentDef == nil || len(agentDef.Tools) == 0 {
		// No tools whitelist or nil definition — return all tools
		// (legacy agents without a definition get everything)
		for _, name := range cache.toolNames {
			defs = append(defs, cache.toolDefs[name])
		}
	} else {
		defs = tp.matchToolsByWhitelist(cache, agentDef.Tools)
	}

	// Apply sub-agent tool restrictions
	defs = tp.applyDepthRestrictions(defs, agentDef, depth, maxDepth)

	return defs
}

// matchToolsByWhitelist filters tool definitions using the agent's tools whitelist.
// Supports exact name matching, glob patterns (via path.Match), and wildcard "*".
func (tp *ToolPool) matchToolsByWhitelist(cache *projectToolCache, whitelist []string) []mcp.ToolDefinition {
	var result []mcp.ToolDefinition
	matched := make(map[string]bool) // track which tools have been matched

	for _, pattern := range whitelist {
		// Wildcard: return all tools
		if pattern == "*" {
			result = result[:0] // clear any partial results
			matched = make(map[string]bool)
			for _, name := range cache.toolNames {
				result = append(result, cache.toolDefs[name])
				matched[name] = true
			}
			// Wildcard overrides everything — no point checking more patterns
			return result
		}

		// Check if this is a glob pattern (contains glob metacharacters)
		isGlob := isGlobPattern(pattern)

		if isGlob {
			// Glob pattern: match against all tool names
			matchCount := 0
			for _, name := range cache.toolNames {
				if matched[name] {
					continue
				}
				ok, err := path.Match(pattern, name)
				if err != nil {
					tp.log.Warn("invalid glob pattern in tools whitelist",
						slog.String("pattern", pattern),
						slog.String("error", err.Error()),
					)
					break
				}
				if ok {
					result = append(result, cache.toolDefs[name])
					matched[name] = true
					matchCount++
				}
			}
			if matchCount == 0 {
				tp.log.Warn("glob pattern matched no tools",
					slog.String("pattern", pattern),
				)
			}
		} else {
			// Exact name match
			if matched[pattern] {
				continue
			}
			if td, ok := cache.toolDefs[pattern]; ok {
				result = append(result, td)
				matched[pattern] = true
			} else {
				// Tool not found — log warning, skip (do not fail)
				tp.log.Warn("tool not found in pool, skipping",
					slog.String("tool", pattern),
				)
			}
		}
	}

	return result
}

// applyDepthRestrictions removes coordination tools from sub-agents unless
// explicitly opted in and within the max depth limit.
func (tp *ToolPool) applyDepthRestrictions(defs []mcp.ToolDefinition, agentDef *AgentDefinition, depth int, maxDepth int) []mcp.ToolDefinition {
	if depth == 0 {
		// Top-level agents have no restrictions
		return defs
	}

	// Build set of explicitly requested coordination tools
	explicitlyRequested := make(map[string]bool)
	if agentDef != nil {
		for _, t := range agentDef.Tools {
			if coordinationTools[t] {
				explicitlyRequested[t] = true
			}
		}
	}

	var filtered []mcp.ToolDefinition
	for _, td := range defs {
		if !coordinationTools[td.Name] {
			// Not a coordination tool — always include
			filtered = append(filtered, td)
			continue
		}

		// This is a coordination tool at depth > 0
		if depth >= maxDepth {
			// At or beyond max depth — always remove, regardless of opt-in
			tp.log.Warn("removing coordination tool at max depth",
				slog.String("tool", td.Name),
				slog.Int("depth", depth),
				slog.Int("max_depth", maxDepth),
			)
			continue
		}

		if explicitlyRequested[td.Name] {
			// Explicitly opted in and within depth limit — keep
			tp.log.Debug("sub-agent retains coordination tool via opt-in",
				slog.String("tool", td.Name),
				slog.Int("depth", depth),
			)
			filtered = append(filtered, td)
		} else {
			// Not explicitly requested — remove by default at depth > 0
			tp.log.Debug("removing coordination tool from sub-agent (no opt-in)",
				slog.String("tool", td.Name),
				slog.Int("depth", depth),
			)
		}
	}

	return filtered
}

// wrapTools wraps MCP tool definitions as ADK tool.Tool instances that
// delegate execution to the MCP service.
func (tp *ToolPool) wrapTools(projectID string, defs []mcp.ToolDefinition) ([]tool.Tool, error) {
	if tp.mcpService == nil {
		return nil, nil
	}

	tools := make([]tool.Tool, 0, len(defs))

	for _, td := range defs {
		// Skip coordination tool stubs — they'll be registered separately
		// by the executor when coordination tools are implemented (Task Group 6)
		if coordinationTools[td.Name] {
			continue
		}

		t, err := tp.wrapSingleTool(projectID, td)
		if err != nil {
			tp.log.Warn("failed to wrap tool, skipping",
				slog.String("tool", td.Name),
				slog.String("error", err.Error()),
			)
			continue
		}
		tools = append(tools, t)
	}

	return tools, nil
}

// wrapSingleTool wraps a single MCP tool definition as an ADK tool.
// For builtin tools, it delegates to mcp.Service.ExecuteTool().
// For external tools (prefixed with server name), it delegates to
// mcpregistry.Service.CallExternalTool() which proxies through the
// external MCP server connection.
func (tp *ToolPool) wrapSingleTool(projectID string, td mcp.ToolDefinition) (tool.Tool, error) {
	// Capture for closure
	toolName := td.Name

	// Check if this is an external tool (has server name prefix)
	isExternal := mcpregistry.IsExternalTool(toolName)

	if isExternal && tp.registryService != nil {
		// External tool: route through proxy
		regSvc := tp.registryService
		pid := projectID
		return functiontool.New(
			functiontool.Config{
				Name:        toolName,
				Description: td.Description,
			},
			func(ctx tool.Context, args map[string]any) (map[string]any, error) {
				result, err := regSvc.CallExternalTool(ctx, pid, toolName, args)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return convertToolResult(result)
			},
		)
	}

	// Builtin tool: route through mcp.Service
	svc := tp.mcpService
	pid := projectID
	return functiontool.New(
		functiontool.Config{
			Name:        toolName,
			Description: td.Description,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			result, err := svc.ExecuteTool(ctx, pid, toolName, args)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			return convertToolResult(result)
		},
	)
}

// convertToolResult converts an MCP ToolResult to a map for the ADK function tool response.
func convertToolResult(result *mcp.ToolResult) (map[string]any, error) {
	if result != nil && len(result.Content) > 0 {
		// If the external tool reported an error, include that flag
		if result.IsError {
			var textParts []string
			for _, block := range result.Content {
				if block.Text != "" {
					textParts = append(textParts, block.Text)
				}
			}
			errMsg := "tool execution failed"
			if len(textParts) > 0 {
				errMsg = textParts[0]
			}
			return map[string]any{"error": errMsg}, nil
		}

		var textParts []string
		for _, block := range result.Content {
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		}
		if len(textParts) == 1 {
			// Try to parse as JSON first
			var parsed map[string]any
			if err := json.Unmarshal([]byte(textParts[0]), &parsed); err == nil {
				return parsed, nil
			}
			return map[string]any{"result": textParts[0]}, nil
		}
		return map[string]any{"results": textParts}, nil
	}

	return map[string]any{"result": "ok"}, nil
}

// isGlobPattern returns true if the string contains glob metacharacters.
func isGlobPattern(s string) bool {
	for _, c := range s {
		switch c {
		case '*', '?', '[':
			return true
		}
	}
	return false
}

// ToolNames returns the names of all tools in the pool for a project.
// Useful for debugging and admin endpoints.
func (tp *ToolPool) ToolNames(projectID string) []string {
	cache := tp.getOrBuildCache(projectID)
	names := make([]string, len(cache.toolNames))
	copy(names, cache.toolNames)
	return names
}

// ToolCount returns the number of tools in the pool for a project.
func (tp *ToolPool) ToolCount(projectID string) int {
	cache := tp.getOrBuildCache(projectID)
	return len(cache.toolNames)
}

// String returns a human-readable description of the tool pool for a project.
func (tp *ToolPool) String(projectID string) string {
	cache := tp.getOrBuildCache(projectID)
	return fmt.Sprintf("ToolPool[project=%s, tools=%d]", projectID, len(cache.toolNames))
}

// mapToInputSchema converts a generic map[string]any (from JSONB) back to mcp.InputSchema.
// This is the inverse of mcpregistry.schemaToMap().
func mapToInputSchema(m map[string]any) mcp.InputSchema {
	if m == nil {
		return mcp.InputSchema{Type: "object"}
	}

	// Marshal map to JSON, then unmarshal into InputSchema
	data, err := json.Marshal(m)
	if err != nil {
		return mcp.InputSchema{Type: "object"}
	}
	var schema mcp.InputSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return mcp.InputSchema{Type: "object"}
	}
	if schema.Type == "" {
		schema.Type = "object"
	}
	return schema
}
