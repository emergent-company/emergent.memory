package agentcompat

import "strings"

// InternalToolPrefix is prepended to every built-in Memory tool when exposing
// them through the OpenAI-compatible endpoint.  This creates a reserved
// namespace so client-supplied tools can never shadow internal ones.
const InternalToolPrefix = "memory_"

// internalToolNames is the canonical set of built-in Memory MCP tool names.
// Keep in sync with the case statements in domain/mcp/service.go ExecuteTool.
// When a request contains tools[], any name that collides with the
// "memory_<name>" form is rejected as a conflict.
var internalToolNames = map[string]struct{}{
	// ── Graph / entity ─────────────────────────────────────────────────────
	"project-get":            {},
	"project-create":         {},
	"entity-type-list":       {},
	"entity-query":           {},
	"entity-history":         {},
	"entity-search":          {},
	"entity-edges-get":       {},
	"entity-create":          {},
	"entity-update":          {},
	"entity-delete":          {},
	"entity-restore":         {},
	"session-get-messages":   {},
	"relationship-create":    {},
	"relationship-list":      {},
	"relationship-update":    {},
	"relationship-delete":    {},
	"tag-list":               {},

	// ── Search ──────────────────────────────────────────────────────────────
	"search-hybrid":    {},
	"search-semantic":  {},
	"search-similar":   {},
	"search-knowledge": {},

	// ── Graph traversal ─────────────────────────────────────────────────────
	"graph-traverse": {},

	// ── Branches ────────────────────────────────────────────────────────────
	"graph-branch-list":   {},
	"graph-branch-create": {},
	"graph-branch-merge":  {},
	"graph-branch-delete": {},

	// ── Schema ──────────────────────────────────────────────────────────────
	"schema-version":             {},
	"schema-list":                {},
	"schema-get":                 {},
	"schema-list-available":      {},
	"schema-list-installed":      {},
	"schema-assign":              {},
	"schema-assignment-update":   {},
	"schema-uninstall":           {},
	"schema-create":              {},
	"schema-delete":              {},
	"schema-history":             {},
	"schema-compiled-types":      {},
	"schema-migration-preview":   {},
	"schema-migrate-preview":     {},
	"schema-migrate-execute":     {},
	"schema-migrate-rollback":    {},
	"schema-migrate-commit":      {},
	"schema-migration-job-status": {},
	"migration-archive-list":     {},
	"migration-archive-get":      {},

	// ── Journal ─────────────────────────────────────────────────────────────
	"journal-list":     {},
	"journal-add-note": {},

	// ── Documents ───────────────────────────────────────────────────────────
	"document-list":   {},
	"document-get":    {},
	"document-upload": {},
	"document-delete": {},

	// ── Skills ──────────────────────────────────────────────────────────────
	"skill-list":   {},
	"skill-get":    {},
	"skill-create": {},
	"skill-update": {},
	"skill-delete": {},

	// ── Agents ──────────────────────────────────────────────────────────────
	"agent-def-list":          {},
	"agent-def-get":           {},
	"agent-def-create":        {},
	"update_agent_definition": {},
	"agent-def-delete":        {},
	"agent-list":              {},
	"agent-get":               {},
	"agent-create":            {},
	"update_agent":            {},
	"agent-delete":            {},
	"trigger_agent":           {},
	"agent-run-list":          {},
	"agent-run-get":           {},
	"agent-run-messages":      {},
	"agent-run-tool-calls":    {},
	"agent-run-status":        {},
	"agent-list-available":    {},
	"agent-question-list":         {},
	"agent-question-list-project": {},
	"agent-question-respond":      {},
	"adk-session-list": {},
	"adk-session-get":  {},
	"acp-list-agents":     {},
	"acp-trigger-run":     {},
	"acp-get-run-status":  {},
	"acp-get-run-events":  {},
	"set_session_title":   {},

	// ── MCP registry ────────────────────────────────────────────────────────
	"mcp-server-list":    {},
	"mcp-server-get":     {},
	"mcp-server-create":  {},
	"update_mcp_server":  {},
	"mcp-server-delete":  {},
	"toggle_mcp_server_tool": {},
	"sync_mcp_server_tools":  {},
	"search_mcp_registry":    {},
	"mcp-registry-get":       {},
	"mcp-registry-install":   {},
	"mcp-server-inspect":     {},

	// ── Provider ────────────────────────────────────────────────────────────
	"provider-list-org":          {},
	"provider-configure-org":     {},
	"provider-configure-project": {},
	"provider-models-list":       {},
	"provider-usage-get":         {},
	"provider-test":              {},

	// ── Embeddings ──────────────────────────────────────────────────────────
	"embedding-status":        {},
	"embedding-pause":         {},
	"embedding-resume":        {},
	"embedding-config-update": {},

	// ── Tokens ──────────────────────────────────────────────────────────────
	"token-list":   {},
	"token-create": {},
	"token-get":    {},
	"token-revoke": {},

	// ── Tracing ─────────────────────────────────────────────────────────────
	"trace-list": {},
	"trace-get":  {},

	// ── Domain / discovery ──────────────────────────────────────────────────
	"classify-document":    {},
	"list-installed-schemas": {},
	"finalize-discovery":   {},
	"queue-reextraction":   {},

	// ── Web ─────────────────────────────────────────────────────────────────
	"web-search-brave":  {},
	"web-fetch":         {},
	"web-search-reddit": {},

	// ── Session todos ───────────────────────────────────────────────────────
	"session-todo-list":   {},
	"session-todo-update": {},
}

// ExposedName returns the external name used in the OpenAI API for an internal
// tool.  "search-knowledge" → "memory_search-knowledge".
func ExposedName(internalName string) string {
	return InternalToolPrefix + internalName
}

// InternalName strips the prefix from an exposed tool name, returning the
// bare internal name.  Returns ("", false) if the name is not prefixed.
func InternalName(exposedName string) (string, bool) {
	after, ok := strings.CutPrefix(exposedName, InternalToolPrefix)
	return after, ok
}

// IsInternalTool reports whether name (after stripping the prefix) is a known
// built-in tool.  Accepts both the bare and prefixed form.
func IsInternalTool(name string) bool {
	bare, stripped := strings.CutPrefix(name, InternalToolPrefix)
	if stripped {
		_, ok := internalToolNames[bare]
		return ok
	}
	_, ok := internalToolNames[name]
	return ok
}

// IsClientTool reports whether the tool name refers to a client-supplied tool
// (i.e. it is NOT a known internal tool in either bare or prefixed form).
func IsClientTool(name string, clientTools []ClientToolDef) bool {
	for _, t := range clientTools {
		if t.Function.Name == name {
			return true
		}
	}
	return false
}

// ValidateClientTools returns an error string if any client tool name conflicts
// with the reserved "memory_" namespace (exact matches to exposed internal names).
// Returns "" when all names are clean.
func ValidateClientTools(tools []ClientToolDef) string {
	for _, t := range tools {
		if strings.HasPrefix(t.Function.Name, InternalToolPrefix) {
			return "tool name '" + t.Function.Name + "' uses the reserved prefix 'memory_'; choose a different name"
		}
	}
	return ""
}

// SystemPromptAppendix returns a short instruction block appended to the agent's
// system prompt that describes the memory_ prefix convention when client tools
// are present.
func SystemPromptAppendix(hasClientTools bool) string {
	if !hasClientTools {
		return ""
	}
	return `
## Tool naming convention
Built-in Memory tools are available under the "memory_" prefix (e.g. memory_search-knowledge, memory_entity-create).
Additional tools provided by the caller have plain names without any prefix.
Use memory_* tools to read/write the knowledge graph.
Use caller-provided tools when you need information or actions outside the graph.`
}
